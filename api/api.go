package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/geovannyAvelar/lukla/heightmap"
	"github.com/go-chi/chi"
	"github.com/gorilla/handlers"

	log "github.com/sirupsen/logrus"
)

type HttpApi struct {
	Router         *chi.Mux
	HeightmapGen   HeightMapGenerator
	BasePath       string
	AllowedOrigins []string
}

type HeightMapGenerator interface {
	GetTileHeightmap(z, x, y, resolution int) ([]byte, error)
	CreateHeightMapImage(lat, lon float64, side float64, conf heightmap.ResolutionConfig) ([]byte, error)
	GetPointsElevations(points []heightmap.Point) []heightmap.Point
	GenerateAllTilesInZoomLevel(zoomLevel int)
}

type coordinate struct {
	Latitude  float64 `json:"longitude"`
	Longitude float64 `json:"latitude"`
	Elevation int16   `json:"elevation"`
}

func (c coordinate) toPoint() heightmap.Point {
	return heightmap.Point{
		Lat: c.Latitude,
		Lon: c.Longitude,
	}
}

func (a HttpApi) Run(port int) error {
	if port < 0 || port > 65535 {
		return errors.New("invalid HTTP port")
	}

	a.Router.Route(a.BasePath, func(r chi.Router) {
		r.Get("/heightmap", a.handleSquare)
		r.Post("/heightmap/points", a.handleHeightmapProfile)
		r.Get("/{z}/{x}/{y}.png", a.handleTile)
		r.Get("/{resolution}/{z}/{x}/{y}.png", a.handleTile)
		r.Post("/processTiles/{z}", a.processAllTiles)
	})

	headersOk := handlers.AllowedHeaders([]string{"X-Requested-With"})
	originsOk := handlers.AllowedOrigins(a.AllowedOrigins)
	methodsOk := handlers.AllowedMethods([]string{"GET", "HEAD", "POST", "PUT", "OPTIONS"})

	handler := handlers.CORS(originsOk, headersOk, methodsOk)(a.Router)

	host := fmt.Sprintf(":%d", port)
	log.Info("Listening at " + host + a.BasePath)

	return http.ListenAndServe(host, handler)
}

func (a HttpApi) handleTile(w http.ResponseWriter, r *http.Request) {
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

func (a HttpApi) handleSquare(w http.ResponseWriter, r *http.Request) {
	lat, lon, err := a.parseSquareCoordinates(r)
	side := a.parseSquareSide(r)
	res := a.parseSquareResolution(r)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	b, err := a.HeightmapGen.CreateHeightMapImage(lat, lon, side,
		heightmap.ResolutionConfig{Width: res, Height: res})

	if err != nil {
		http.Error(w, "cannot generate heightmap. "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Add("Content-Type", "image/png")
	w.Header().Add("Content-Disposition", "inline; filename=\"heightmap.png\"")
	w.Write(b)
}

func (a HttpApi) handleHeightmapProfile(w http.ResponseWriter, r *http.Request) {
	bytes, err := io.ReadAll(r.Body)

	if err != nil {
		http.Error(w, "cannot generate heightmap profile. Cause: "+err.Error(), http.StatusBadRequest)
		return
	}

	var coordinates []coordinate
	err = json.Unmarshal(bytes, &coordinates)

	if err != nil {
		http.Error(w, "cannot generate heightmap profile. Cause: "+err.Error(), http.StatusBadRequest)
		return
	}

	points := a.getElevations(coordinates)
	bytes, err = json.Marshal(points)

	if err != nil {
		http.Error(w, "cannot generate heightmap profile. Cause: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(bytes)
}

func (a HttpApi) processAllTiles(w http.ResponseWriter, r *http.Request) {
	zParam := chi.URLParam(r, "z")

	z, zParseErr := strconv.Atoi(zParam)

	if zParseErr != nil || z < 0 {
		http.Error(w, "invalid zoom level", http.StatusBadRequest)
		return
	}

	a.HeightmapGen.GenerateAllTilesInZoomLevel(z)
}

func (a HttpApi) getElevations(coordinates []coordinate) []coordinate {
	points := make([]heightmap.Point, len(coordinates))

	for i, c := range coordinates {
		points[i] = c.toPoint()
	}

	points = a.HeightmapGen.GetPointsElevations(points)

	for i, p := range points {
		coordinates[i].Elevation = p.Elevation
	}

	return coordinates
}

func (a HttpApi) parseTileCoordinates(r *http.Request) (map[string]int, error) {
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

func (a HttpApi) parseTileResolution(r *http.Request) int {
	resParam := chi.URLParam(r, "resolution")
	resolution, errResParse := strconv.Atoi(resParam)

	if errResParse != nil || resolution <= 0 || resolution > 2048 {
		resolution = 256
	}

	return resolution
}

func (a HttpApi) parseSquareCoordinates(r *http.Request) (float64, float64, error) {
	latParam := r.URL.Query().Get("lat")
	lonParam := r.URL.Query().Get("lon")

	lat, errLat := strconv.ParseFloat(latParam, 64)
	lon, errLon := strconv.ParseFloat(lonParam, 64)

	if errLat != nil || errLon != nil {
		return 0.0, 0.0, errors.New("invalid coordinates")
	}

	return lat, lon, nil
}

func (a HttpApi) parseSquareSide(r *http.Request) float64 {
	sideParam := r.URL.Query().Get("side")
	side, err := strconv.ParseFloat(sideParam, 64)

	if err != nil {
		return 10000
	}

	if side > 50000 {
		return 10000
	}

	return side
}

func (a HttpApi) parseSquareResolution(r *http.Request) int {
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
