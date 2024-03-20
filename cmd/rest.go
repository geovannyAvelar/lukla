package cmd

import (
	"github.com/geovannyAvelar/lukla/api"
	"github.com/geovannyAvelar/lukla/env"
	"github.com/geovannyAvelar/lukla/heightmap"
	"github.com/go-chi/chi"
	"github.com/spf13/cobra"
	"strings"
)

var allowedOrigins string
var port int
var basePath string

func CreateRestCommand() *cobra.Command {
	rest := &cobra.Command{
		Use:   "rest",
		Short: "Lukla REST Application Programming Interface (API) commands",
		Long:  "Lukla REST Application Programming Interface (API) commands",
		Run:   startRestApi,
	}

	rest.Flags().StringVar(&dotenvPath, "env", "", "Dot env file path")
	rest.Flags().StringVar(&allowedOrigins, "allowed-origins", "",
		"API allowed origins, separated by commas (,)")
	rest.Flags().IntVar(&port, "port", 0, "API port")
	rest.Flags().StringVar(&basePath, "base-path", "", "API base path")
	rest.Flags().StringVar(&tilesPath, "tile-path", "", "Tiles path")
	rest.Flags().StringVar(&demPath, "dem-path", "", "Digital Elevation Model (DEM) files path")
	rest.Flags().IntVar(&httpClientTimeout, "http-client-timeout", 0, "HTTP client request timeout")
	rest.Flags().StringVar(&earthdataUser, "earthdata-user", "", "Earthdata API username")
	rest.Flags().StringVar(&earthdataPassword, "earthdata-password", "", "Earthdata API password")

	return rest
}

func startRestApi(cmd *cobra.Command, args []string) {
	if dotenvPath != "" {
		loadDotEnv(dotenvPath)
	}

	h := createHgtDataDir()
	defer h.Close()

	httpClient := createHttpClient()
	earthdataApi := createEarthdataApiClient(httpClient)
	srtmDownloader := createSrtmDownloader(httpClient, earthdataApi)
	heightmapGen := createHeightmapGenerator(h, srtmDownloader)
	rest := createHttpApi(heightmapGen)

	if port == 0 {
		port = internal.GetApiPort()
	}

	rest.Run(port)
}

func createHttpApi(heightmapGen *heightmap.Generator) *api.HttpApi {
	if basePath == "" {
		basePath = internal.GetRootPath()
	}

	var allowedOriginsSlice []string
	if allowedOrigins == "" {
		allowedOriginsSlice = internal.GetAllowedOrigins()
	} else {
		allowedOriginsSlice = strings.Split(allowedOrigins, ",")
	}

	return &api.HttpApi{
		Router:         chi.NewRouter(),
		HeightmapGen:   heightmapGen,
		BasePath:       basePath,
		AllowedOrigins: allowedOriginsSlice,
	}
}
