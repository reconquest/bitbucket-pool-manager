package operator

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/kovetskiy/stash"
	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
	"gitlab.com/reconquest/bitbucket-pool-manager/internal/config"
	"gitlab.com/reconquest/bitbucket-pool-manager/internal/constants"
	"gitlab.com/reconquest/bitbucket-pool-manager/internal/docker"
	"gitlab.com/reconquest/bitbucket-pool-manager/internal/options"
)

type Operator struct {
	config *config.Config
	docker docker.DockerService
	opts   options.DocoptOptions
}

type StartupStatus struct {
	State    string
	Progress struct {
		Message    string
		Percentage int
	}
}

var ErrContainersAllocated = errors.New("all free containers allocated")

func NewOperator(
	config *config.Config,
	docker docker.DockerService,
	opts options.DocoptOptions,
) *Operator {
	return &Operator{
		config: config,
		docker: docker,
		opts:   opts,
	}
}

func (operator *Operator) CreateNetwork() error {
	err := operator.docker.CreateNetwork()
	if err != nil {
		return karma.Format(
			err,
			"unable to create network",
		)
	}

	return nil
}

func (operator *Operator) RunInitialContainers() error {
	log.Info("validating number of existing containers")
	result, total, err := operator.isExceedsNumberOfCreatedContainers(
		constants.INITIAL_NUMBER_OF_CONTAINERS,
	)
	if err != nil {
		return karma.Format(
			err,
			"unable to validate number of existing containers",
		)
	}

	if result {
		log.Infof(
			nil,
			"initial containers have already created, number of initial containers: %d",
			constants.INITIAL_NUMBER_OF_CONTAINERS,
		)
		return nil
	}

	log.Info("creating initial containers")
	for i := 0; i < constants.INITIAL_NUMBER_OF_CONTAINERS-total; i++ {
		_, err := operator.HandleNewContainer()
		if err != nil {
			return karma.Format(
				err,
				"unable to handle new container",
			)
		}
	}

	log.Info("initial containers successfully created")

	return nil
}

func (operator *Operator) GetContainerByID(id string) (*types.Container, error) {
	container, err := operator.docker.GetContainerByID(id)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to get container by id from the docker, container_id: %s",
			id,
		)
	}

	return container, nil
}

func (operator *Operator) HandleNewContainer() (*types.Container, error) {
	container, err := operator.CreateAndStartContainer()
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to create and start container",
		)
	}

	bitbucketURL := operator.GetURI("", container.PortHTTP)
	err = operator.ValidateStartupStatus(bitbucketURL, container)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to validate startup status of container, container_id: %s",
			container.ID,
		)
	}

	err = operator.InstallAddonAndSetLicense(bitbucketURL)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to set license, container_id: %s",
			container.ID,
		)
	}

	createdContainer, err := operator.docker.GetContainerByID(container.ID)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to get container from docker, container_id: %s",
			container.ID,
		)
	}

	return createdContainer, nil
}

func getDateOfAllocatedContainer(name string) (time.Time, error) {
	splittedName := strings.Split(name, "---")
	if len(splittedName) < 1 {
		return time.Time{}, karma.Describe("container_name", name).
			Reason(errors.New("unable to get date of allocated container"))
	}

	date := strings.TrimSpace(
		strings.Replace(
			strings.Replace(
				strings.Replace(
					splittedName[1], "--", " ", 1), ".", ":", -1,
			), constants.ALLOCATED_CONTAINER_STATUS, "", -1),
	)

	allocatedTime, err := time.Parse(constants.TIME_FORMAT, date)
	if err != nil {
		return time.Time{}, karma.Describe(
			"container_name", name,
		).Reason(err)
	}

	return allocatedTime, nil
}

func getOverdueContainers(
	containers []types.Container,
) ([]types.Container, error) {
	var result []types.Container
	for _, container := range containers {
		expirationDate, err := getDateOfAllocatedContainer(container.Names[0])
		if err != nil {
			return nil, err
		}

		nowDate, err := time.Parse(
			constants.TIME_FORMAT,
			time.Now().Format(constants.TIME_FORMAT),
		)
		if err != nil {
			return nil, err
		}

		if nowDate.After(expirationDate) {
			result = append(result, container)
		}
	}

	return result, nil
}

func (operator *Operator) CleanAllocatedContainers() error {
	containers, err := operator.docker.GetAllocatedContainers()
	if err != nil {
		return karma.Format(
			err,
			"unable to get allocated containers from docker",
		)
	}

	if len(containers) == 0 {
		return nil
	}

	overdueContainers, err := getOverdueContainers(containers)
	if err != nil {
		return karma.Format(
			err,
			"unable to get allocated overdue containers from docker",
		)
	}

	if len(overdueContainers) == 0 {
		return nil
	}

	log.Info("removing allocated containers")
	err = operator.RemoveContainers(overdueContainers)
	if err != nil {
		return karma.Format(
			err,
			"unable to remove containers",
		)
	}

	log.Info("outdated allocated containers successfully removed")
	return nil
}

func (operator *Operator) RemoveContainerByID(id string) error {
	log.Infof(nil, "removing container by id: %s", id)
	container, err := operator.docker.GetContainerByID(id)
	if err != nil {
		return karma.Format(
			err,
			"unable to get container by id from the docker, container_id: %s",
			id,
		)
	}

	var containers []types.Container
	containers = append(containers, *container)
	err = operator.RemoveContainers(containers)
	if err != nil {
		return karma.Format(
			err,
			"unable to remove container, container_id: %s",
			container.ID,
		)
	}

	log.Infof(nil, "container successfully removed, container_id: %s", id)

	return nil
}

func (operator *Operator) RemoveContainers(containers []types.Container) error {
	if len(containers) == 0 {
		return nil
	}

	for _, container := range containers {
		status, err := operator.GetStatusOfContainerByID(container.ID)
		if err != nil {
			return karma.Format(
				err,
				"unable to get status of container, container_id: %s",
				container.ID,
			)
		}

		switch status {
		case constants.CONTAINER_STATUS_UP:
			err = operator.docker.StopContainer(container.ID)
			if err != nil {
				return karma.Format(
					err,
					"unable to stop container, container_id: %s",
					container.ID,
				)
			}

			log.Infof(
				nil,
				"docker container successfully stopped, container_id: %s",
				container.ID,
			)
		case constants.CONTAINER_STATUS_EXITED:
			break
		default:
			return karma.Format(
				err,
				"unable to get status of container, container_id: %s",
				container.ID,
			)
		}

		err = operator.docker.RemoveContainer(container.ID)
		if err != nil {
			return karma.Format(
				err,
				"unable to remove container, container_id: %s",
				container.ID,
			)
		}

		log.Infof(
			nil,
			"docker container successfully removed, container_id: %s",
			container.ID,
		)
	}

	return nil
}

func (operator *Operator) InstallAddonAndSetLicense(bitbucketURL string) error {
	parsedURL, err := url.Parse(bitbucketURL)
	if err != nil {
		return karma.Format(
			err,
			"unable to parse url: %s",
			bitbucketURL,
		)
	}

	stash := stash.NewClient(
		operator.config.Bitbucket.Username,
		operator.config.Bitbucket.Password,
		parsedURL,
	)

	log.Info("receiving upm token")
	token, err := stash.GetUPMToken()
	if err != nil {
		return karma.Format(
			err,
			"unable to get upm token by url: %s",
			parsedURL,
		)
	}

	log.Info("installing addon")
	result, err := stash.InstallAddon(token, operator.opts.AddonPath)
	if err != nil {
		return karma.Format(
			err,
			"unable to install addon on bitbucket, addon_path: %s",
			operator.opts.AddonPath,
		)
	}

	log.Infof(nil, "addon successfully installed, result: %s", result)

	log.Info("setting license for addon")
	license, err := readFile(operator.opts.LicensePath)
	if err != nil {
		return karma.Format(
			err,
			"unable to read file with license",
		)
	}

	err = stash.SetAddonLicense(constants.ADDON_KEY, license)
	if err != nil {
		return karma.Format(
			err,
			"unable to set license for addon, license_path: %s",
			operator.opts.LicensePath,
		)
	}

	log.Infof(nil, "license successfully set")

	return nil
}

func (operator *Operator) SetAllocatedStatusForContainer(
	container types.Container,
) error {
	err := operator.docker.SetAllocatedStatusForContainer(container)
	if err != nil {
		return karma.Format(
			err,
			"unable to set allocated status for container, container_id: %s",
			container.ID,
		)
	}
	log.Infof(
		nil,
		"status of container set on 'allocated', container_id: %s",
		container.ID,
	)

	return nil
}

func (operator *Operator) GetFreeContanier() (*types.Container, error) {
	containers, err := operator.docker.GetFreeContainers()
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to get free container",
		)
	}

	if len(containers) == 0 {
		return nil, ErrContainersAllocated
	}

	// err = operator.docker.SetAllocatedStatusForContainer(containers[0])
	// if err != nil {
	// 	return nil, karma.Format(
	// 		err,
	// 		"unable to set allocated status for container, container_id: %s",
	// 		containers[0].ID,
	// 	)
	// }

	return &containers[0], nil
}

func (operator *Operator) CreateFreeContainer() (*types.Container, error) {
	container, err := operator.HandleNewContainer()
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to handle new container",
		)
	}

	err = operator.docker.SetAllocatedStatusForContainer(*container)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to set allocated status for container, container_id: %s",
			container.ID,
		)
	}

	return container, nil
}

func (operator *Operator) getBitbucketImageWithVersion() (string, error) {
	if operator.config.Bitbucket.Version == "latest" {
		return constants.BITBUCKET_IMAGE + ":latest", nil
	}

	expression := regexp.MustCompile(`^[0-9]([.][0-9])([.][0-9])?$`)
	if expression.MatchString(operator.config.Bitbucket.Version) {
		return constants.BITBUCKET_IMAGE +
			":" +
			operator.config.Bitbucket.Version, nil
	}

	return "", errors.New("wrong bitbucket version")
}

func (operator *Operator) CreateAndStartContainer() (*docker.ContainerData, error) {
	result, _, err := operator.isExceedsNumberOfCreatedContainers(
		constants.MAX_NUMBER_OF_CONTAINERS,
	)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to validate max number of created containers",
		)
	}

	if result {
		return nil, karma.Describe(
			"limit", constants.MAX_NUMBER_OF_CONTAINERS,
		).Reason(
			errors.New("limit of created containers exceeded"),
		)
	}

	log.Info("creating container")
	image, err := operator.getBitbucketImageWithVersion()
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to get bitbucket image and version",
		)
	}

	containerName := AddIDToContainerName(operator.config.Prefix)
	log.Info("receiving free http and ssh ports for new container")
	portHTTP, portSSH, err := getPorts()
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to get free http and ssh ports for new container",
		)
	}

	containerID, err := operator.docker.CreateContainer(
		containerName, image, portHTTP, portSSH,
	)
	if err != nil {
		return nil, karma.Describe(
			"container_name", containerName,
		).Format(
			err,
			"unable to create container",
		)
	}

	container := docker.ContainerData{
		Name:     containerName,
		Image:    image,
		ID:       containerID,
		Username: operator.config.Bitbucket.Username,
		Password: operator.config.Bitbucket.Password,
		PortHTTP: portHTTP,
		PortSSH:  portSSH,
	}

	log.Info("starting container")
	err = operator.docker.StartContainer(container.ID)
	if err != nil {
		return nil, karma.Describe(
			"container_id", container.ID,
		).Format(
			err,
			"unable to start container",
		)
	}

	return &container, nil
}

func (operator *Operator) ValidateStartupStatus(
	bitbucketURL string,
	container *docker.ContainerData,
) error {
	log.Info("validating startup status of a container")
	var message string
	for {
		time.Sleep(time.Second)
		status, err := operator.GetStartupStatus(bitbucketURL)
		if err != nil {
			return karma.Format(
				err,
				"unable to get container startup status",
			)
		}

		if status == nil {
			time.Sleep(time.Second)
			continue
		}

		if message != status.Progress.Message {
			log.Infof(
				nil,
				"bb: %s setup: %3d%% %s | %s",
				container.Image,
				status.Progress.Percentage,
				strings.ToLower(status.State),
				status.Progress.Message,
			)

			message = status.Progress.Message
		}

		if status.State == constants.CONTAINER_STATUS_STARTED {
			break
		}

		time.Sleep(time.Second)
	}

	return nil
}

func (operator *Operator) GetStartupStatus(baseURL string) (*StartupStatus, error) {
	bitbucketURL := baseURL + "/system/startup"
	request, err := http.NewRequest(
		http.MethodGet,
		bitbucketURL,
		nil,
	)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to create http request",
		)
	}

	request.Header.Set("Accept", "application/json")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		if err, ok := err.(*url.Error); ok {
			if neterr, ok := err.Err.(*net.OpError); ok {
				// skip network error while bitbucket is starting
				log.Tracef(nil, "%s: %v", bitbucketURL, neterr)
				return nil, nil
			}

			if err.Err == io.EOF {
				// skip incomplete reads
				return nil, nil
			}

			if err.Err.Error() == "http: server closed idle connection" {
				return nil, nil
			}
		}

		return nil, karma.Format(
			err,
			"unable to request startup status",
		)
	}

	var status StartupStatus

	defer response.Body.Close()
	err = json.NewDecoder(response.Body).Decode(&status)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to decode startup status",
		)
	}

	return &status, nil
}

func (operator *Operator) GetStatusOfContainerByID(id string) (string, error) {
	container, err := operator.docker.GetContainerByID(id)
	if err != nil {
		return "", karma.Format(
			err,
			"unable to get containers by id: %s",
			id,
		)
	}

	if container == nil {
		return "", karma.Format(
			errors.New("container doesn't found in docker"),
			"unable to get status container by id: %s",
			id,
		)
	}

	return handleStatusOfContainer(container.Status), nil
}

func (operator *Operator) GetStoppedContainersIDs() ([]string, error) {
	containers, err := operator.docker.GetContainersListByPrefix(
		operator.config.Prefix,
	)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to get containers by prefix: %s",
			operator.config.Prefix,
		)
	}

	var result []string
	for _, container := range containers {
		if container.Status == constants.CONTAINER_STATUS_EXITED {
			result = append(result, container.ID)
		}
	}

	return result, nil
}

func (operator *Operator) GetRunningContainersIDs() ([]string, error) {
	containers, err := operator.docker.GetContainersListByPrefix(
		operator.config.Prefix,
	)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to get containers by prefix: %s",
			operator.config.Prefix,
		)
	}

	var result []string
	for _, container := range containers {
		if container.Status == constants.CONTAINER_STATUS_UP {
			result = append(result, container.ID)
		}
	}

	return result, nil
}

func (operator *Operator) GetAllContaniersFromDocker() ([]types.Container, error) {
	containers, err := operator.docker.GetContainersListByPrefix(
		operator.config.Prefix,
	)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to get containers by prefix: %s",
			operator.config.Prefix,
		)
	}

	return containers, nil
}

func (operator *Operator) GetNumberOfContainersByPrefixFromDocker() (int, error) {
	containers, err := operator.docker.GetContainersListByPrefix(
		operator.config.Prefix,
	)
	if err != nil {
		return 0, karma.Format(
			err,
			"unable to get containers by prefix: %s",
			operator.config.Prefix,
		)
	}

	return len(containers), nil
}

func getPorts() (string, string, error) {
	portHTTP, err := getFreePort()
	if err != nil {
		return "", "", karma.Format(
			err,
			"unable to get free http_port",
		)
	}

	portSSH, err := getFreePort()
	if err != nil {
		return "", "", karma.Format(
			err,
			"unable to get free ssh_port",
		)
	}

	return portHTTP, portSSH, err
}

func (operator *Operator) GetURI(path, portHTTP string) string {
	url := url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%s", operator.config.Bitbucket.URL, portHTTP),
		Path:   path,
	}

	return url.String()
}

func AddIDToContainerName(name string) string {
	max := 2000000
	min := 1000000
	rand.Seed(time.Now().UnixNano())
	return name + "-" + strconv.Itoa(rand.Intn(max-min)+min) +
		"---" + constants.NEW_CONTAINER_STATUS
}

func getFreePort() (string, error) {
	address, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return "", err
	}

	listen, err := net.ListenTCP("tcp", address)
	if err != nil {
		return "", err
	}

	defer listen.Close()
	return strconv.Itoa(listen.Addr().(*net.TCPAddr).Port), nil
}

func readFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", karma.Format(
			err,
			"unable to open file by path: %s",
			path,
		)
	}

	defer file.Close()

	license, err := ioutil.ReadAll(file)
	if err != nil {
		return "", karma.Format(
			err,
			"unable to read file by path: %s",
			path,
		)
	}

	return string(license), nil
}

func (operator *Operator) isExceedsNumberOfCreatedContainers(
	max int,
) (bool, int, error) {
	containers, err := operator.docker.GetContainersListByPrefix(
		operator.config.Prefix,
	)
	if err != nil {
		return false, 0, karma.Format(
			err,
			"unable to get container list",
		)
	}

	if len(containers) >= max {
		return true, 0, nil
	}

	return false, len(containers), nil
}

func handleStatusOfContainer(status string) string {
	if strings.Contains(status, constants.CONTAINER_STATUS_UP) {
		return constants.CONTAINER_STATUS_UP
	}

	if strings.Contains(status, constants.CONTAINER_STATUS_EXITED) {
		return constants.CONTAINER_STATUS_EXITED
	}

	return constants.CONTAINER_STATUS_UNKNOWN
}
