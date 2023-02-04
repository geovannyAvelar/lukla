package internal

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi"
	"github.com/gorilla/handlers"

	log "github.com/sirupsen/logrus"
)

type HttpApi struct {
	Router         *chi.Mux
	Heightmap      Heightmap
	BasePath       string
	AllowedOrigins []string
}

func (a *HttpApi) Run(port int) error {
	if port < 0 || port > 65535 {
		return errors.New("invalid HTTP port")
	}

	a.Router.Route(a.BasePath, func(r chi.Router) {
		r.Get("/{z}/{x}/{y}.png", a.handleTile)
		r.Get("/{resolution}/{z}/{x}/{y}.png", a.handleTile)
	})

	headersOk := handlers.AllowedHeaders([]string{"X-Requested-With"})
	originsOk := handlers.AllowedOrigins(a.AllowedOrigins)
	methodsOk := handlers.AllowedMethods([]string{"GET", "HEAD", "POST", "PUT", "OPTIONS"})

	handler := handlers.CORS(originsOk, headersOk, methodsOk)(a.Router)

	host := fmt.Sprintf(":%d", port)
	log.Info("Listening at " + host + a.BasePath)

	return http.ListenAndServe(host, handler)
}

func (a *HttpApi) handleTile(w http.ResponseWriter, r *http.Request) {
	tileCoords, err := a.parseTileCoordinates(r)
	resolution := a.parseTileResolution(r)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	bytes, err := a.Heightmap.GetTileHeightmap(tileCoords["z"], tileCoords["x"], tileCoords["y"],
		resolution)

	if err != nil {
		http.Error(w, "cannot generate heightmap. "+err.Error(), http.StatusBadRequest)
		return
	}

	contentDisposition := fmt.Sprintf("inline; filename=\"%d.png\"", tileCoords["y"])

	w.Header().Add("Content-Type", "image/png")
	w.Header().Add("Content-Disposition", contentDisposition)
	w.Write(bytes)
}

func (a *HttpApi) parseTileCoordinates(r *http.Request) (map[string]int, error) {
	xParam := chi.URLParam(r, "x")
	yParam := chi.URLParam(r, "y")
	zParam := chi.URLParam(r, "z")

	x, xParseErr := strconv.Atoi(xParam)
	y, yParseErr := strconv.Atoi(yParam)
	z, zParseErr := strconv.Atoi(zParam)

	if xParseErr != nil || yParseErr != nil || zParseErr != nil || x < 0 || y < 0 || z < 0 {
		return map[string]int{}, errors.New("invalid tile coordinates")
	}

	return map[string]int{
		"x": x, "y": y, "z": z,
	}, nil
}

func (a *HttpApi) parseTileResolution(r *http.Request) int {
	resParam := chi.URLParam(r, "resolution")
	resolution, errResParse := strconv.Atoi(resParam)

	if errResParse != nil || resolution <= 0 || resolution > 1024 {
		resolution = 256
	}

	return resolution
}
