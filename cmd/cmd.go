package cmd

import (
	"fmt"
	env "github.com/geovannyAvelar/lukla/env"
	"github.com/geovannyAvelar/lukla/heightmap"
	"github.com/geovannyAvelar/lukla/srtm"
	"github.com/joho/godotenv"
	"github.com/petoc/hgt"
	log "github.com/sirupsen/logrus"
	"net/http"
	"os"
	"time"
)

var dotenvPath string
var tilesPath string
var demPath string
var httpClientTimeout int
var earthdataUser string
var earthdataPassword string

func createHgtDataDir() *hgt.DataDir {
	if demPath == "" {
		demPath = env.GetDigitalElevationModelPath()
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
		timeout = env.GetHttpClientTimeout()
	}

	return &http.Client{
		Timeout: timeout,
	}
}

func createEarthdataApiClient(client *http.Client) *srtm.EarthdataApi {
	if earthdataUser == "" {
		earthdataUser = env.GetEarthDataApiUsername()
	}

	if earthdataPassword == "" {
		earthdataPassword = env.GetEarthDataApiPassword()
	}

	return &srtm.EarthdataApi{
		HttpClient: client,
		Username:   earthdataUser,
		Password:   earthdataPassword,
	}
}

func createSrtmDownloader(client *http.Client, earthdataApi *srtm.EarthdataApi) *srtm.Downloader {
	if demPath == "" {
		demPath = env.GetDigitalElevationModelPath()
	}

	return &srtm.Downloader{
		HttpClient: client,
		Dir:        demPath,
		Api:        earthdataApi,
	}
}

func createHeightmapGenerator(h *hgt.DataDir, downloader *srtm.Downloader) *heightmap.Generator {
	if tilesPath == "" {
		tilesPath = env.GetTilesPath()
	}

	return &heightmap.Generator{
		ElevationDataset: h,
		SrtmDownloader:   downloader,
		Dir:              tilesPath,
	}
}

func loadDotEnv(path string) {
	if err := godotenv.Load(path); err != nil {
		log.Warnf("Error loading dot env file %s", path)
	}
}

func handleErr(msg interface{}) {
	fmt.Println("Error:", msg)
	os.Exit(1)
}
