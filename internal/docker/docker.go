package docker

import (
	"context"
	"io"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
	"gitlab.com/reconquest/bitbucket-pool-manager/internal/config"
	"gitlab.com/reconquest/bitbucket-pool-manager/internal/constants"
)

type DockerService interface {
	CreateContainer(name, image, portHTTP, portSSH string) (string, error)
	StartContainer(string) error
	RemoveContainer(string) error
	StopContainer(string) error
	GetContainersListByPrefix(string) ([]types.Container, error)
	GetContainersByIDs([]string) ([]types.Container, error)
	GetContainerByID(string) (*types.Container, error)
	GetFreeContainers() ([]types.Container, error)
	GetAllocatedContainers() ([]types.Container, error)
	SetAllocatedStatusForContainer(container types.Container) error
	CreateNetwork() error
}

type Docker struct {
	cli    *client.Client
	config *config.Config
}

type ContainerData struct {
	Name          string    `json:"name" bson:"name"`
	Image         string    `json:"image" bson:"image"`
	ID            string    `json:"containerID" bson:"container_id"`
	Username      string    `json:"username" bson:"username"`
	Password      string    `json:"password" bson:"password"`
	PortHTTP      string    `json:"httpPort" bson:"http_port"`
	PortSSH       string    `json:"sshPort" bson:"ssh_port"`
	Date          time.Time `json:"date" bson:"date"`
	IsAllocated   bool      `json:"isAllocated" bson:"is_allocated"`
	AllocatedTime time.Time `json:"allocatedTime" bson:"allocated_time"`
}

func NewDocker(cli *client.Client, config *config.Config) *Docker {
	return &Docker{
		cli:    cli,
		config: config,
	}
}

func addIDToVolume(name string) string {
	max := 2000000
	min := 1000000
	rand.Seed(time.Now().UnixNano())
	return name + "-volume-" + strconv.Itoa(rand.Intn(max-min)+min)
}

func (docker *Docker) createHostConfig(
	portHTTP, portSSH string,
) *container.HostConfig {
	volumeName := addIDToVolume(docker.config.Prefix)
	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: volumeName,
				Target: "/var/atlassian/application-data/bitbucket",
			},
		},
		PortBindings: nat.PortMap{
			"7990/tcp": []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: portHTTP,
				},
			},
			"7999/tcp": []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: portSSH,
				},
			},
		},
	}

	return hostConfig
}

func (docker *Docker) createNetworkConfig() *network.NetworkingConfig {
	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{},
	}

	gatewayConfig := &network.EndpointSettings{
		Gateway: constants.DOCKER_NETWORK_NAME,
	}

	networkConfig.EndpointsConfig[constants.DOCKER_NETWORK_NAME] = gatewayConfig
	return networkConfig
}

func (docker *Docker) CreateContainer(
	name, image, portHTTP, portSSH string,
) (string, error) {
	log.Infof(nil, "pulling image: %s", image)
	reader, err := docker.cli.ImagePull(
		context.Background(), image, types.ImagePullOptions{},
	)
	if err != nil {
		return "", karma.Format(
			err,
			"unable to create image",
		)
	}

	_, err = io.Copy(os.Stdout, reader)
	if err != nil {
		return "", karma.Format(
			err,
			"unable to copy stdout to reader",
		)
	}

	hostConfig := docker.createHostConfig(portHTTP, portSSH)
	networkConfig := docker.createNetworkConfig()
	resp, err := docker.cli.ContainerCreate(
		context.Background(), &container.Config{
			Image: image,
			Env: []string{
				"ELASTICSEARCH_ENABLED=" +
					docker.config.Bitbucket.ElasticSearchEnabled,
				// "SERVER_PROXY_NAME=" +
				// 	docker.config.Bitbucket.ServerProxyName,
				"JVM_SUPPORT_RECOMMENDED_ARGS=" +
					docker.config.Bitbucket.JvmSupportRecommendedArgs,
			},
		}, hostConfig, networkConfig, name,
	)
	if err != nil {
		return "", karma.Format(
			err,
			"unable to create container",
		)
	}

	log.Infof(
		karma.Describe("container id", resp.ID).
			Describe("container name", name).
			Describe("container image", image).
			Describe("ssh port", portSSH).
			Describe("http port", portHTTP),
		"container created successfully",
	)
	return resp.ID, nil
}

func (docker *Docker) StartContainer(id string) error {
	err := docker.cli.ContainerStart(
		context.Background(), id, types.ContainerStartOptions{},
	)
	if err != nil {
		return karma.Format(
			err,
			"unable to start container",
		)
	}

	return nil
}

func (docker *Docker) RemoveContainers(ids []string) error {
	for _, id := range ids {
		err := docker.RemoveContainer(id)
		if err != nil {
			return karma.Format(
				err,
				"unable to remove container",
			)
		}
	}

	return nil
}

func (docker *Docker) RemoveContainer(id string) error {
	err := docker.cli.ContainerRemove(
		context.Background(),
		id,
		types.ContainerRemoveOptions{
			RemoveVolumes: true,
			RemoveLinks:   false,
			Force:         true,
		},
	)
	if err != nil {
		return karma.Format(
			err,
			"unable to remove container, container_id: %s",
			id,
		)
	}

	return nil
}

func (docker *Docker) StopContainer(id string) error {
	err := docker.cli.ContainerStop(context.Background(), id, nil)
	if err != nil {
		return karma.Format(
			err,
			"unable to stop container, container_id: %s",
			id,
		)
	}

	return nil
}

func (docker *Docker) GetContainersListByPrefix(
	prefix string,
) ([]types.Container, error) {
	containers, err := docker.cli.ContainerList(
		context.Background(), types.ContainerListOptions{All: true},
	)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to get container list by prefix: %s",
			prefix,
		)
	}
	var result []types.Container
	for _, container := range containers {
		for _, name := range container.Names {
			if strings.Contains(name, prefix) {
				result = append(result, container)
			}
		}
	}

	return result, nil
}

func (docker *Docker) GetContainersByIDs(ids []string) ([]types.Container, error) {
	containers, err := docker.cli.ContainerList(
		context.Background(), types.ContainerListOptions{All: true},
	)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to get container list",
		)
	}

	var result []types.Container
	for _, container := range containers {
		for _, id := range ids {
			if container.ID == id {
				result = append(result, container)
			}
		}
	}

	return result, nil
}

func (docker *Docker) GetContainerByID(id string) (*types.Container, error) {
	var result types.Container
	containers, err := docker.cli.ContainerList(
		context.Background(), types.ContainerListOptions{All: true},
	)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to get container list",
		)
	}

	for _, container := range containers {
		if container.ID == id {
			result = container
		}
	}

	return &result, nil
}

func setAllocatedStatus(name string) string {
	return strings.Replace(
		name, constants.NEW_CONTAINER_STATUS,
		constants.ALLOCATED_CONTAINER_STATUS, 1,
	)
}

func (docker *Docker) SetAllocatedStatusForContainer(
	container types.Container,
) error {
	date := strings.Replace(
		strings.Replace(
			time.Now().Add(constants.CLEANING_INTERVAL).
				Format(constants.TIME_FORMAT), " ", "--", -1,
		), ":", ".", -1,
	)
	newName := setAllocatedStatus(container.Names[0]) + "--" + date
	err := docker.cli.ContainerRename(context.Background(), container.ID, newName)
	if err != nil {
		return karma.Format(
			err,
			"unable to rename container, container_id: %s",
			container.ID,
		)
	}

	return nil
}

func (docker *Docker) GetFreeContainers() ([]types.Container, error) {
	containers, err := docker.GetContainersListByPrefix(docker.config.Prefix)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to get container list",
		)
	}

	if len(containers) == 0 {
		return nil, nil
	}

	var result []types.Container
	for _, container := range containers {
		if strings.Contains(container.Names[0], constants.NEW_CONTAINER_STATUS) {
			result = append(result, container)
		}
	}

	return result, nil
}

func (docker *Docker) GetAllocatedContainers() ([]types.Container, error) {
	containers, err := docker.GetContainersListByPrefix(docker.config.Prefix)
	if err != nil {
		return nil, karma.Format(
			err,
			"unable to get container list",
		)
	}

	if len(containers) == 0 {
		return nil, nil
	}

	var result []types.Container
	for _, container := range containers {
		if strings.Contains(
			container.Names[0],
			constants.ALLOCATED_CONTAINER_STATUS,
		) {
			result = append(result, container)
		}
	}

	return result, nil
}

func (docker *Docker) CreateNetwork() error {
	log.Info("creating network")
	result, err := docker.isDupNetwork(constants.DOCKER_NETWORK_NAME)
	if err != nil {
		return karma.Format(
			err,
			"unable to validate network by name, network_name: %s",
			constants.DOCKER_NETWORK_NAME,
		)
	}

	if result {
		log.Infof(
			karma.Describe("network_name", constants.DOCKER_NETWORK_NAME),
			"network with this name already exists",
		)
		return nil
	}

	response, err := docker.cli.NetworkCreate(
		context.Background(),
		constants.DOCKER_NETWORK_NAME,
		types.NetworkCreate{Driver: "bridge"},
	)
	if err != nil {
		return karma.Format(
			err,
			"unable to create network",
		)
	}

	log.Infof(
		karma.Describe("network_id", response.ID).
			Describe("network_name", constants.DOCKER_NETWORK_NAME),
		"network succesfully created",
	)
	return nil
}

func (docker *Docker) isDupNetwork(name string) (bool, error) {
	networkList, err := docker.cli.NetworkList(context.Background(), types.NetworkListOptions{})
	if err != nil {
		return false, karma.Format(
			err,
			"unable to get network list",
		)
	}

	for _, network := range networkList {
		if network.Name == name {
			return true, nil
		}
	}

	return false, nil
}
