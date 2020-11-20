package constants

import "time"

const (
	BITBUCKET_IMAGE              = "atlassian/bitbucket-server"
	ADDON_KEY                    = "io.reconquest.snake"
	MAX_NUMBER_OF_CONTAINERS     = 6
	INITIAL_NUMBER_OF_CONTAINERS = 2

	CLEANING_INTERVAL          = 1 * time.Hour
	IS_ALLOCATED_TRUE          = true
	ALLOCATED_CONTAINER_STATUS = "allocated"
	NEW_CONTAINER_STATUS       = "new"

	CONTAINER_STATUS_STARTED = "STARTED"
	CONTAINER_STATUS_EXITED  = "Exited"
	CONTAINER_STATUS_UP      = "Up"
	CONTAINER_STATUS_UNKNOWN = "Unknown"

	DOCKER_NETWORK_NAME = ""
	TIME_FORMAT         = "2006-Jan-2-15:04:07"
)
