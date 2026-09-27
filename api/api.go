package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/geovannyAvelar/lukla/heightmap"
	"github.com/go-chi/chi"
	"github.com/gorilla/handlers"

	log "github.com/sirupsen/logrus"
)

const (
	minLatitude  = -90.0
	maxLatitude  = 90.0
	minLongitude = -180.0
	maxLongitude = 180.0

	defaultSide = 10000.0
	maxSide     = 50000.0

	defaultResolution = 256
	maxResolution     = 2048

	// maxTileZoomLevel bounds a single tile request. Web map tile schemes
	// rarely go past zoom 20 in practice.
	maxTileZoomLevel = 20

	// maxBatchZoomLevel bounds the /processTiles/{z} endpoint. At zoom z the
	// number of tiles grows as 4^z, so this is kept well below
	// maxTileZoomLevel to avoid an accidental huge batch job.
	maxBatchZoomLevel = 12

	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	// writeTimeout is generous because heightmap/tile generation can be slow.
	writeTimeout    = 5 * time.Minute
	idleTimeout     = 120 * time.Second
	shutdownTimeout = 30 * time.Second
)

type HttpApi struct {
	Router         *chi.Mux
	HeightmapGen   HeightMapGenerator
	BasePath       string
	AllowedOrigins []string
}

type HeightMapGenerator interface {
	GetTileHeightmap(ctx context.Context, z, x, y, resolution int) ([]byte, error)
	CreateHeightMapImage(ctx context.Context, lat, lon float64, side float64, conf heightmap.ResolutionConfig) ([]byte, error)
	GetPointsElevations(points []heightmap.Point) []heightmap.Point
	GenerateAllTilesInZoomLevel(ctx context.Context, zoomLevel int) (heightmap.TileGenerationResult, error)
}

type coordinate struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
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
		r.Get("/health", a.handleHealth)
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

	srv := &http.Server{
		Addr:              host,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	shutdownErr := make(chan error, 1)

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh

		log.Info("Shutting down HTTP server...")

		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		shutdownErr <- srv.Shutdown(ctx)
	}()

	log.Info("Listening at " + host + a.BasePath)

	err := srv.ListenAndServe()

	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return <-shutdownErr
}

func (a HttpApi) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "application/json")
	writeResponse(w, []byte(`{"status":"ok"}`))
}

func (a HttpApi) handleTile(w http.ResponseWriter, r *http.Request) {
	tileCoords, err := a.parseTileCoordinates(r)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resolution, err := a.parseTileResolution(r)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	bytes, err := a.HeightmapGen.GetTileHeightmap(r.Context(), tileCoords["z"], tileCoords["x"], tileCoords["y"],
		resolution)

	if err != nil {
		http.Error(w, "cannot generate heightmap. "+err.Error(), http.StatusBadRequest)
		return
	}

	contentDisposition := fmt.Sprintf("inline; filename=\"%d.png\"", tileCoords["y"])

	w.Header().Add("Content-Type", "image/png")
	w.Header().Add("Content-Disposition", contentDisposition)
	writeResponse(w, bytes)
}

func (a HttpApi) handleSquare(w http.ResponseWriter, r *http.Request) {
	lat, lon, err := a.parseSquareCoordinates(r)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	side, err := a.parseSquareSide(r)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	res, err := a.parseSquareResolution(r)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	b, err := a.HeightmapGen.CreateHeightMapImage(r.Context(), lat, lon, side,
		heightmap.ResolutionConfig{Width: res, Height: res})

	if err != nil {
		http.Error(w, "cannot generate heightmap. "+err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Add("Content-Type", "image/png")
	w.Header().Add("Content-Disposition", "inline; filename=\"heightmap.png\"")
	writeResponse(w, b)
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

	for _, c := range coordinates {
		if err := validateCoordinates(c.Latitude, c.Longitude); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	points := a.getElevations(coordinates)
	bytes, err = json.Marshal(points)

	if err != nil {
		http.Error(w, "cannot generate heightmap profile. Cause: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Add("Content-Type", "application/json")
	writeResponse(w, bytes)
}

func (a HttpApi) processAllTiles(w http.ResponseWriter, r *http.Request) {
	zParam := chi.URLParam(r, "z")

	z, zParseErr := strconv.Atoi(zParam)

	if zParseErr != nil || z < 0 || z > maxBatchZoomLevel {
		msg := fmt.Sprintf("invalid zoom level: must be an integer between 0 and %d", maxBatchZoomLevel)
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	result, err := a.HeightmapGen.GenerateAllTilesInZoomLevel(r.Context(), z)

	if err != nil {
		http.Error(w, "cannot generate tiles. "+err.Error(), http.StatusBadRequest)
		return
	}

	if r.Context().Err() != nil {
		// The client is already gone (disconnected, or the request's own
		// deadline passed) - nothing to usefully send back.
		log.Warnf("tile batch for zoom %d was canceled after %d/%d tiles (%d succeeded, %d failed)",
			z, result.Succeeded+result.Failed, result.Total, result.Succeeded, result.Failed)
		return
	}

	body, err := json.Marshal(result)

	if err != nil {
		http.Error(w, "cannot encode tile generation result. Cause: "+err.Error(),
			http.StatusInternalServerError)
		return
	}

	w.Header().Add("Content-Type", "application/json")

	switch {
	case result.Failed == 0:
		w.WriteHeader(http.StatusOK)
	case result.Failed == result.Total:
		w.WriteHeader(http.StatusInternalServerError)
	default:
		w.WriteHeader(http.StatusMultiStatus)
	}

	writeResponse(w, body)
}

// writeResponse writes the response body and logs a failure instead of
// silently discarding it, since http.ResponseWriter.Write errors (e.g. the
// client disconnecting mid-response) are not actionable by the caller.
func writeResponse(w http.ResponseWriter, body []byte) {
	if _, err := w.Write(body); err != nil {
		log.Warnf("cannot write HTTP response. Cause: %s", err)
	}
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

// parseTileCoordinates parses and validates x, y and z from the request path.
// z must be within [0, maxTileZoomLevel], and x, y must fall inside the
// [0, 2^z) range defined by the OSM/Web Mercator tile scheme for that zoom.
func (a HttpApi) parseTileCoordinates(r *http.Request) (map[string]int, error) {
	xParam := chi.URLParam(r, "x")
	yParam := chi.URLParam(r, "y")
	zParam := chi.URLParam(r, "z")

	x, xParseErr := strconv.Atoi(xParam)
	y, yParseErr := strconv.Atoi(yParam)
	z, zParseErr := strconv.Atoi(zParam)

	if xParseErr != nil || yParseErr != nil || zParseErr != nil {
		return map[string]int{}, errors.New("invalid tile coordinates: x, y and z must be integers")
	}

	if z < 0 || z > maxTileZoomLevel {
		msg := fmt.Sprintf("invalid tile coordinates: z must be between 0 and %d", maxTileZoomLevel)
		return map[string]int{}, errors.New(msg)
	}

	numTiles := 1 << uint(z)

	if x < 0 || x >= numTiles || y < 0 || y >= numTiles {
		msg := fmt.Sprintf("invalid tile coordinates: x and y must be between 0 and %d at zoom %d",
			numTiles-1, z)
		return map[string]int{}, errors.New(msg)
	}

	return map[string]int{
		"x": x, "y": y, "z": z,
	}, nil
}

// parseTileResolution parses the optional resolution path segment. A missing
// value keeps the default resolution; an explicit but invalid value is
// rejected rather than silently replaced.
func (a HttpApi) parseTileResolution(r *http.Request) (int, error) {
	resParam := chi.URLParam(r, "resolution")

	if resParam == "" {
		return defaultResolution, nil
	}

	resolution, err := strconv.Atoi(resParam)

	if err != nil || resolution <= 0 || resolution > maxResolution {
		msg := fmt.Sprintf("invalid resolution: must be an integer between 1 and %d", maxResolution)
		return 0, errors.New(msg)
	}

	return resolution, nil
}

// validateCoordinates rejects non-finite values explicitly before the range
// checks below: strconv.ParseFloat happily parses "NaN"/"Inf"/"+Inf"/"-Inf"
// with no error, and NaN's IEEE-754 property that every comparison (<, >) is
// false means a bare range check silently accepts it.
func validateCoordinates(lat, lon float64) error {
	if math.IsNaN(lat) || math.IsInf(lat, 0) {
		return errors.New("invalid latitude: must be a finite number")
	}

	if lat < minLatitude || lat > maxLatitude {
		msg := fmt.Sprintf("invalid latitude: must be between %.0f and %.0f", minLatitude, maxLatitude)
		return errors.New(msg)
	}

	if math.IsNaN(lon) || math.IsInf(lon, 0) {
		return errors.New("invalid longitude: must be a finite number")
	}

	if lon < minLongitude || lon > maxLongitude {
		msg := fmt.Sprintf("invalid longitude: must be between %.0f and %.0f", minLongitude, maxLongitude)
		return errors.New(msg)
	}

	return nil
}

func (a HttpApi) parseSquareCoordinates(r *http.Request) (float64, float64, error) {
	latParam := r.URL.Query().Get("lat")
	lonParam := r.URL.Query().Get("lon")

	lat, errLat := strconv.ParseFloat(latParam, 64)
	lon, errLon := strconv.ParseFloat(lonParam, 64)

	if errLat != nil || errLon != nil {
		return 0.0, 0.0, errors.New("invalid coordinates: lat and lon must be numbers")
	}

	if err := validateCoordinates(lat, lon); err != nil {
		return 0.0, 0.0, err
	}

	return lat, lon, nil
}

// parseSquareSide parses the optional "side" query parameter. A missing value
// keeps the default side; an explicit but invalid value is rejected rather
// than silently replaced.
func (a HttpApi) parseSquareSide(r *http.Request) (float64, error) {
	sideParam := r.URL.Query().Get("side")

	if sideParam == "" {
		return defaultSide, nil
	}

	side, err := strconv.ParseFloat(sideParam, 64)

	if err != nil || side <= 0 || side > maxSide {
		msg := fmt.Sprintf("invalid side: must be a number greater than 0 and up to %.0f", maxSide)
		return 0, errors.New(msg)
	}

	return side, nil
}

// parseSquareResolution parses the optional "resolution" query parameter. A
// missing value keeps the default resolution; an explicit but invalid value
// is rejected rather than silently replaced.
func (a HttpApi) parseSquareResolution(r *http.Request) (int, error) {
	resParam := r.URL.Query().Get("resolution")

	if resParam == "" {
		return defaultResolution, nil
	}

	res, err := strconv.Atoi(resParam)

	if err != nil || res <= 0 || res > maxResolution {
		msg := fmt.Sprintf("invalid resolution: must be an integer between 1 and %d", maxResolution)
		return 0, errors.New(msg)
	}

	return res, nil
}
