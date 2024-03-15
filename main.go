package main

import (
	"github.com/geovannyAvelar/lukla/api"
	"github.com/geovannyAvelar/lukla/heightmap"
	"github.com/geovannyAvelar/lukla/internal"
	"github.com/geovannyAvelar/lukla/srtm"
	"github.com/go-chi/chi"
	"github.com/joho/godotenv"
	"github.com/petoc/hgt"
	log "github.com/sirupsen/logrus"
)

func main() {
	if err := godotenv.Load(".env"); err != nil {
		log.Warn("Error loading env file")
	}

	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

	h, err := hgt.OpenDataDir(internal.GetDigitalElevationModelPath(), nil)

	if err != nil {
		panic(err)
	}

	defer h.Close()

	srtmDownloader := &srtm.Downloader{
		Dir: internal.GetDigitalElevationModelPath(),
		Api: srtm.EarthdataApi{Username: internal.GetEarthDataApiUsername(),
			Password: internal.GetEarthDataApiPassword()},
	}

	heightmapGen := heightmap.HeightmapGenerator{
		ElevationDataset: h,
		SrtmDownloader:   srtmDownloader,
		Dir:              internal.GetTilesPath(),
	}

	api := api.HttpApi{
		Router:         chi.NewRouter(),
		HeightmapGen:   heightmapGen,
		BasePath:       internal.GetRootPath(),
		AllowedOrigins: internal.GetAllowedOrigins(),
	}

	api.Run(internal.GetApiPort())
}
