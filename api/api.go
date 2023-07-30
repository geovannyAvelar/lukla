package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/geovannyAvelar/lukla/heightmap"
	"github.com/go-chi/chi"
	"github.com/gorilla/handlers"

	log "github.com/sirupsen/logrus"
)

type HttpApi struct {
	Router         *chi.Mux
	HeightmapGen   heightmap.HeightmapGenerator
	BasePath       string
	AllowedOrigins []string
}

func (a *HttpApi) Run(port int) error {
	if port < 0 || port > 65535 {
		return errors.New("invalid HTTP port")
	}

	a.Router.Route(a.BasePath, func(r chi.Router) {
		r.Get("/heightmap", a.handleSquare)
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

	bytes, err := a.HeightmapGen.GetTileHeightmap(tileCoords["z"], tileCoords["x"], tileCoords["y"],
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

func (a *HttpApi) handleSquare(w http.ResponseWriter, r *http.Request) {
	lat, lon, err := a.parseSquareCoordinates(r)
	side := a.parseSquareSide(r)
	res := a.parseSquareResolution(r)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	b, err := a.HeightmapGen.CreateHeightMapImage(lat, lon, side,
		heightmap.HeightmapResolutionConfig{Width: res, Heigth: res})

	if err != nil {
		http.Error(w, "cannot generate heightmap. "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Add("Content-Type", "image/png")
	w.Header().Add("Content-Disposition", "inline; filename=\"heightmap.png\"")
	w.Write(b)
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

	if errResParse != nil || resolution <= 0 || resolution > 2048 {
		resolution = 256
	}

	return resolution
}

func (a *HttpApi) parseSquareCoordinates(r *http.Request) (float64, float64, error) {
	latParam := r.URL.Query().Get("lat")
	lonParam := r.URL.Query().Get("lon")

	lat, errLat := strconv.ParseFloat(latParam, 64)
	lon, errLon := strconv.ParseFloat(lonParam, 64)

	if errLat != nil || errLon != nil {
		return 0.0, 0.0, errors.New("invalid coordinates")
	}

	return lat, lon, nil
}

func (a *HttpApi) parseSquareSide(r *http.Request) int {
	sideParam := r.URL.Query().Get("side")
	side, err := strconv.Atoi(sideParam)

	if err != nil {
		return 10000
	}

	if side > 50000 {
		return 10000
	}

	return side
}

func (a *HttpApi) parseSquareResolution(r *http.Request) int {
	resParam := r.URL.Query().Get("resolution")
	res, err := strconv.Atoi(resParam)

	if err != nil {
		return 256
	}

	if res > 2048 {
		return 256
	}

	return res
}
