package internal

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
)

func GetAllowedOrigins() []string {
	envVar := os.Getenv("LUKLA_ALLOWED_ORIGINS")

	if envVar != "" {
		return strings.Split(envVar, ",")
	}

	log.Warn("LUKLA_ALLOWED_ORIGINS enviroment variable is not defined. Accepting only local connections")

	return []string{GetLocalHost()}
}

func GetApiPort() int {
	envVar := os.Getenv("LUKLA_PORT")

	if envVar != "" {
		port, err := strconv.Atoi(envVar)

		if err == nil {
			return port
		}

		log.Warn("Cannot parse LUKLA_PORT enviroment variable. Port must be an integer.")
	}

	log.Warn("LUKLA_PORT is not defined.")
	log.Warn("Using default port 9000.")

	return 9000
}

func GetLocalHost() string {
	logLevel := log.GetLevel()

	log.SetLevel(0)
	port := GetApiPort()
	log.SetLevel(logLevel)

	return fmt.Sprintf("http://localhost:%d", port)
}

func GetRootPath() string {
	root := os.Getenv("LUKLA_BASE_PATH")

	if root != "" && len(root) > 0 {
		if root[0] == '/' {
			return root
		}
	}

	log.Warn("LUKLA_BASE_PATH enviroment variable is not defined. Default is /")

	return "/"
}

func GetTilesPath() string {
	path := os.Getenv("LUKLA_TILES_PATH")

	if path != "" {
		return path
	}

	log.Warn("LUKLA_TILES_PATH enviroment variable is not defined." +
		"Tiles will be stored in ./data/tiles folder")

	return "data/tiles"
}

func GetDigitalElevationModelPath() string {
	path := os.Getenv("DEM_FILES_PATH")

	if path != "" {
		return path
	}

	log.Warn("DEM_FILES_PATH enviroment variable is not defined." +
		"Tiles will be stored in ./data/dem folder")

	return "data/dem"
}
