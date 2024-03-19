package cmd

import (
	"github.com/geovannyAvelar/lukla/api"
	"github.com/geovannyAvelar/lukla/heightmap"
	"github.com/geovannyAvelar/lukla/internal"
	"github.com/geovannyAvelar/lukla/srtm"
	"github.com/go-chi/chi"
	"github.com/joho/godotenv"
	"github.com/petoc/hgt"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"net/http"
	"time"
)

var dotenvPath string
var allowedOrigins string
var port int
var basePath string
var tilesPath string
var demPath string
var httpClientTimeout int
var earthdataUser string
var earthdataPassword string

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

func loadDotEnv(path string) {
	if err := godotenv.Load(path); err != nil {
		log.Warnf("Error loading dot env file %s", path)
	}
}

func createHgtDataDir() *hgt.DataDir {
	if demPath == "" {
		demPath = internal.GetDigitalElevationModelPath()
	}

	h, err := hgt.OpenDataDir(demPath, nil)

	if err != nil {
		handleErr(err)
	}

	return h
}

func createHttpClient() *http.Client {
	var timeout time.Duration

	if httpClientTimeout <= 0 {
		timeout = internal.GetHttpClientTimeout()
	}

	return &http.Client{
		Timeout: timeout,
	}
}

func createEarthdataApiClient(client *http.Client) *srtm.EarthdataApi {
	if earthdataUser == "" {
		earthdataUser = internal.GetEarthDataApiUsername()
	}

	if earthdataPassword == "" {
		earthdataPassword = internal.GetEarthDataApiPassword()
	}

	return &srtm.EarthdataApi{
		HttpClient: client,
		Username:   earthdataUser,
		Password:   earthdataPassword,
	}
}

func createSrtmDownloader(client *http.Client, earthdataApi *srtm.EarthdataApi) *srtm.Downloader {
	if demPath == "" {
		demPath = internal.GetDigitalElevationModelPath()
	}

	return &srtm.Downloader{
		HttpClient: client,
		Dir:        demPath,
		Api:        earthdataApi,
	}
}

func createHeightmapGenerator(h *hgt.DataDir, downloader *srtm.Downloader) *heightmap.Generator {
	if tilesPath == "" {
		tilesPath = internal.GetTilesPath()
	}

	return &heightmap.Generator{
		ElevationDataset: h,
		SrtmDownloader:   downloader,
		Dir:              tilesPath,
	}
}

func createHttpApi(heightmapGen *heightmap.Generator) *api.HttpApi {
	return &api.HttpApi{
		Router:         chi.NewRouter(),
		HeightmapGen:   heightmapGen,
		BasePath:       internal.GetRootPath(),
		AllowedOrigins: internal.GetAllowedOrigins(),
	}
}
