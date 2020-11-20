package main

import (
	"net/http"
	"time"

	"github.com/docker/docker/client"
	"github.com/docopt/docopt-go"
	"github.com/gorilla/mux"
	"github.com/reconquest/karma-go"
	"github.com/reconquest/pkg/log"
	"gitlab.com/reconquest/bitbucket-pool-manager/internal/config"
	"gitlab.com/reconquest/bitbucket-pool-manager/internal/docker"
	"gitlab.com/reconquest/bitbucket-pool-manager/internal/handler"
	"gitlab.com/reconquest/bitbucket-pool-manager/internal/operator"
	"gitlab.com/reconquest/bitbucket-pool-manager/internal/options"
)

var version = "[manual build]"

var usage = `bitbucket-pool-manager

Creates several bitbuckets for running snake-ci tests.

Usage:
  bitbucket-pool-manager [options] -a <addon-path> -l <license-path>

Options:
  -a --addonpath <addon-path>       Path to addon .jar file.
  -l --licensepath <license-path>   Path to .txt file with license.
  -c --config <path>                Read specified config file. [default: config.yaml]
  --debug                           Enable debug messages.
  -v --version                      Print version.
  -h --help                         Show this help.
`

func main() {
	var err error
	args, err := docopt.ParseArgs(
		usage,
		nil,
		"bitbucket-pool-manager "+version,
	)
	if err != nil {
		log.Fatal(err)
	}

	log.Infof(
		karma.Describe("version", version),
		"bitbucket-pool-manager started",
	)

	if args["--debug"].(bool) {
		log.SetLevel(log.LevelDebug)
	}

	var opts options.DocoptOptions
	err = args.Bind(&opts)
	if err != nil {
		log.Fatal(err)
	}

	log.Infof(nil, "loading configuration file: %q", opts.Config)

	config, err := config.Load(opts.Config)
	if err != nil {
		log.Fatal(err)
	}

	cli, err := client.NewClientWithOpts(
		client.FromEnv, client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		log.Fatal(err)
	}

	docker := docker.NewDocker(cli, config)

	operator := operator.NewOperator(config, docker, opts)
	err = operator.CreateNetwork()
	if err != nil {
		log.Fatal(err)
	}

	err = operator.CleanAllocatedContainers()
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		for {
			time.Sleep(20 * time.Second)
			err = operator.CleanAllocatedContainers()
			if err != nil {
				log.Fatal(err)
			}
		}
	}()

	go func() {
		err = operator.RunInitialContainers()
		if err != nil {
			log.Fatal(err)
		}
	}()

	handler := handler.NewHandler(config, operator)

	router := mux.NewRouter().StrictSlash(true)
	router.HandleFunc(config.BaseURL+"/container/all", handler.GetAllContainers)
	router.HandleFunc(
		config.BaseURL+"/container/", handler.CreateContainer,
	).Methods("POST")
	router.HandleFunc(
		config.BaseURL+"/container/{id}", handler.GetContainerByID,
	).Methods("GET")
	router.HandleFunc(
		config.BaseURL+"/freecontainer", handler.GetFreeContainer,
	).Methods("GET")
	router.HandleFunc(
		config.BaseURL+"/container/{id}", handler.RemoveContainer,
	).Methods("DELETE")

	log.Infof(nil, "listening on %s", config.ListeningPort)
	err = http.ListenAndServe(config.ListeningPort, router)
	if err != nil {
		log.Fatalf(err, "unable to listen and serve")
	}
}
