package main

import (
	"github.com/geovannyAvelar/lukla/api"
	"github.com/geovannyAvelar/lukla/heightmap"
	"github.com/geovannyAvelar/lukla/internal"
	"github.com/go-chi/chi"
	"github.com/petoc/hgt"
	log "github.com/sirupsen/logrus"
)

func main() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: true,
	})

	h, err := hgt.OpenDataDir(internal.GetDigitalElevationModelPath(), nil)

	if err != nil {
		panic(err)
	}

	defer h.Close()

	heightmapGen := heightmap.HeightmapGenerator{
		ElevationDataset: h,
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
