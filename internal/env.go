package internal

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
)

// Get the allowed API origins from LUKLA_ALLOWED_ORIGINS enviroment variable
// If the variable is not present, returns local origin
func GetAllowedOrigins() []string {
	envVar := os.Getenv("LUKLA_ALLOWED_ORIGINS")

	if envVar != "" {
		return strings.Split(envVar, ",")
	}

	log.Warn("LUKLA_ALLOWED_ORIGINS enviroment variable is not defined. Accepting only local connections")

	return []string{GetLocalHost()}
}

// Get the API port from LUKLA_PORT enviroment variable
// If LUKLA_PORT is not filled, uses 9000 port
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

// Returns API hostname (with port)
func GetLocalHost() string {
	logLevel := log.GetLevel()

	log.SetLevel(0)
	port := GetApiPort()
	log.SetLevel(logLevel)

	return fmt.Sprintf("http://localhost:%d", port)
}

// Returns API root path. Default is /
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

// Returns tiles cache directory. Default is 'data/tiles'
func GetTilesPath() string {
	path := os.Getenv("LUKLA_TILES_PATH")

	if path != "" {
		return path
	}

	log.Warn("LUKLA_TILES_PATH enviroment variable is not defined." +
		"Tiles will be stored in ./data/tiles folder")

	return "data/tiles"
}

// Returns digital elevation model (DEM) dataset directory. Fallback is 'data/dem'
func GetDigitalElevationModelPath() string {
	path := os.Getenv("LUKLA_DEM_FILES_PATH")

	if path != "" {
		return path
	}

	log.Warn("LUKLA_DEM_FILES_PATH enviroment variable is not defined." +
		"Tiles will be stored in ./data/dem folder")

	return "data/dem"
}

// Returns the username to authenticate on EarthData API
func GetEarthDataApiUsername() string {
	username := os.Getenv("LUKLA_EARTHDATA_USERNAME")

	if username != "" {
		return username
	}

	log.Warn("LUKLA_EARTHDATA_USERNAME environment variable is not defined." +
		" Lukla cannot download elevation dataset data.")

	return ""
}

// Returns the password to authenticate on EarthData API
func GetEarthDataApiPassword() string {
	password := os.Getenv("LUKLA_EARTHDATA_PASSWORD")

	if password != "" {
		return password
	}

	log.Warn("LUKLA_EARTHDATA_PASSWORD environment variable is not defined." +
		" Lukla cannot download elevation dataset data.")

	return ""
}
