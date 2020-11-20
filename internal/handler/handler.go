package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/reconquest/pkg/log"
	"gitlab.com/reconquest/bitbucket-pool-manager/internal/config"
	"gitlab.com/reconquest/bitbucket-pool-manager/internal/operator"
)

type Handler struct {
	config   *config.Config
	operator *operator.Operator
}

func NewHandler(
	config *config.Config,
	operator *operator.Operator,
) *Handler {
	return &Handler{
		config:   config,
		operator: operator,
	}
}

func (handler *Handler) GetAllContainers(
	writer http.ResponseWriter, request *http.Request,
) {
	containers, err := handler.operator.GetAllContaniersFromDocker()
	if err != nil {
		log.Errorf(
			err,
			"unable to get data from the docker",
		)
		fmt.Fprintln(writer, err)
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	err = json.NewEncoder(writer).Encode(containers)
	if err != nil {
		log.Errorf(
			err,
			"unable to encode containers data to json",
		)

		fmt.Fprintln(writer, err)
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func (handler *Handler) GetFreeContainer(
	writer http.ResponseWriter, request *http.Request,
) {
	container, err := handler.operator.GetFreeContanier()
	if err != nil && err != operator.ErrContainersAllocated {
		fmt.Fprintln(writer, err)
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	if err == operator.ErrContainersAllocated {
		newContainer, err := handler.operator.CreateFreeContainer()
		if err != nil {
			log.Errorf(
				err,
				"unable to create free container",
			)

			fmt.Fprintln(writer, err)
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}

		err = json.NewEncoder(writer).Encode(newContainer)
		if err != nil {
			log.Errorf(
				err,
				"unable to encode container data to json",
			)

			fmt.Fprintln(writer, err)
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}

		return
	}

	err = json.NewEncoder(writer).Encode(container)
	if err != nil {
		log.Errorf(
			err,
			"unable to encode container data to json",
		)

		fmt.Fprintln(writer, err)
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func (handler *Handler) GetContainerByID(
	writer http.ResponseWriter, request *http.Request,
) {
	vars := mux.Vars(request)
	containerID := vars["id"]
	container, err := handler.operator.GetContainerByID(containerID)
	if err != nil {
		log.Errorf(
			err,
			"unable to get container by id",
		)
		fmt.Fprintln(writer, err)
		writer.WriteHeader(http.StatusInternalServerError)

		return
	}

	err = json.NewEncoder(writer).Encode(container)
	if err != nil {
		log.Errorf(
			err,
			"unable to encode container data to json",
		)

		fmt.Fprintln(writer, err)
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func (handler *Handler) CreateContainer(
	writer http.ResponseWriter, request *http.Request,
) {
	container, err := handler.operator.HandleNewContainer()
	if err != nil {
		log.Errorf(
			err,
			"unable to create and start container",
		)
		fmt.Fprintln(writer, err)
		writer.WriteHeader(http.StatusInternalServerError)

		return
	}

	err = json.NewEncoder(writer).Encode(container)
	if err != nil {
		log.Errorf(
			err,
			"unable to encode containers data to json",
		)

		fmt.Fprintln(writer, err)
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func (handler *Handler) RemoveContainer(
	writer http.ResponseWriter, request *http.Request,
) {
	vars := mux.Vars(request)
	containerID := vars["id"]
	err := handler.operator.RemoveContainerByID(containerID)
	if err != nil {
		log.Errorf(
			err,
			"unable to remove container",
		)
		fmt.Fprintln(writer, err)
		writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(writer, "container successfully removed: %s", containerID)
}
