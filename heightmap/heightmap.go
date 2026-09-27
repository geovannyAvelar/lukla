package heightmap

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mazznoer/colorgrad"

	"github.com/apeyroux/gosm"
	"github.com/geovannyAvelar/lukla/srtm"
	"github.com/nfnt/resize"
	"github.com/petoc/hgt"
	"github.com/tidwall/geodesic"

	log "github.com/sirupsen/logrus"
)

// Digital Elevation Model (DEM) resolution in meters
const heightDataResolution = 30.0

// Azimuth angle pointing to the south
const southAzimuth = 180

// Azimuth angle pointing to the east
const eastAzimuth = 90

// NoElevationData marks a Point.Elevation whose value could not be read (missing
// DEM file, coordinate outside coverage, decode error, etc). It reuses the void
// value SRTM HGT files already use for "no data" so it can never be confused
// with a valid elevation, including sea level (0).
const NoElevationData int16 = -32768

// Maximum zoom level accepted by GenerateAllTilesInZoomLevel. At zoom z the
// number of tiles grows as 4^z; z=12 already means over 16 million tiles, which
// is treated as the practical ceiling for a single batch request.
const maxBatchZoomLevel = 12

// Number of tiles generated concurrently by GenerateAllTilesInZoomLevel.
const maxConcurrentTileGeneration = 100

// Path separator
var filePathSep = strings.ReplaceAll(strconv.QuoteRune(os.PathSeparator), "'", "")

type heightProfileProcessFunc func(*Point, interface{}, int) error

// Point Elevation of a specific geographic point represented by latitude and longitude (WGS84)
// X and Y represents a point in the heightmap image
type Point struct {
	X, Y      int
	Lat, Lon  float64
	Elevation int16
}

// Generator HeightmapGenerator Generate heightmaps based on a digital elevation model (DEM) dataset
type Generator struct {
	ElevationDataset *hgt.DataDir
	SrtmDownloader   *srtm.Downloader
	Dir              string
}

type ResolutionConfig struct {
	Width                            int
	Height                           int
	ForceInterpolation               bool
	IgnoreWhenOriginalImageIsSmaller bool
}

// GetTileHeightmap Generate a heightmap with the same size of an OpenStreetMap (OSM) tile
func (t Generator) GetTileHeightmap(z, x, y, resolution int) ([]byte, error) {
	byteArray, err := t.getTileFromDisk(x, y, z, resolution)

	if err == nil {
		return byteArray, nil
	}

	osmTile := gosm.NewTileWithXY(x, y, z)
	lat, lon := osmTile.Num2deg()

	tileSide := calculateTileSizeKm(z) * 1000

	byteArray, err = t.CreateHeightMapImage(lat, lon, tileSide,
		ResolutionConfig{Width: resolution, Height: resolution, ForceInterpolation: true,
			IgnoreWhenOriginalImageIsSmaller: false})

	if err != nil {
		return []byte{}, err
	}

	go func() {
		_, err := t.saveTile(x, y, z, resolution, byteArray)
		if err != nil {
			log.Errorf("cannot save tile (%d, %d, %d) to disk. Cause: %s", x, y, z, err)
		}
	}()

	return byteArray, nil
}

func (t Generator) CreateHeightMapImage(lat, lon float64, side float64,
	conf ResolutionConfig) ([]byte, error) {
	step := int(side) / heightDataResolution

	if step >= 100 {
		step -= 100
	}

	upLeft := image.Point{}
	lowRight := image.Point{X: step, Y: step}

	imgRgba := image.NewRGBA(image.Rectangle{Min: upLeft, Max: lowRight})
	gradient, _ := colorgrad.NewGradient().Domain(0, 8865).Build()

	err := t.createHeightProfile(lat, lon, side, imgRgba, func(point *Point, i interface{}, index int) error {
		imgRgba.Set(point.Y, point.X, gradient.At(float64(point.Elevation)))
		return nil
	})

	log.Infof("Height profile created for coordinates (%f, %f)", lat, lon)

	if err != nil {
		return []byte{}, err
	}

	var outputImg image.Image = imgRgba
	validResolution := conf.Width > 0 && conf.Height > 0

	needsResize := validResolution && (conf.ForceInterpolation ||
		(!conf.IgnoreWhenOriginalImageIsSmaller && (conf.Width < step || conf.Height < step)))

	if needsResize {
		log.Infof("Resizing heightmap image for coordinates (%f, %f) to %dx%d",
			lat, lon, conf.Width, conf.Height)

		outputImg = resize.Resize(uint(conf.Width), uint(conf.Height), imgRgba, resize.Lanczos3)
	}

	var b bytes.Buffer

	if err := png.Encode(&b, outputImg); err != nil {
		return []byte{}, errors.New("cannot encode PNG image")
	}

	return b.Bytes(), nil
}

func (t Generator) GetPointsElevations(points []Point) []Point {
	for i, p := range points {
		if t.SrtmDownloader != nil {
			_, err := t.SrtmDownloader.DownloadDemFile(p.Lat, p.Lon)

			if err != nil {
				msg := "cannot download digital elevation model file for coordinate %f, %f. Cause: %s"
				log.Warnf(msg, p.Lat, p.Lon, err)
			}
		}

		e, _, err := t.ElevationDataset.ElevationAt(p.Lat, p.Lon)

		if err != nil {
			log.Warnf("cannot read elevation for coordinate %f, %f. Cause: %s", p.Lat, p.Lon, err)
			points[i].Elevation = NoElevationData
			continue
		}

		points[i].Elevation = e
	}

	return points
}

// GenerateAllTilesInZoomLevel generates every tile of an OSM zoom level. Tiles
// are enumerated and dispatched incrementally, bounded by
// maxConcurrentTileGeneration in-flight goroutines at a time, instead of
// materializing all 4^zoomLevel tiles in memory up front. A single tile's
// failure is logged and does not abort the rest of the batch.
func (t Generator) GenerateAllTilesInZoomLevel(zoomLevel int) error {
	if zoomLevel < 0 || zoomLevel > maxBatchZoomLevel {
		return fmt.Errorf("zoom level %d is out of the allowed range [0, %d] for batch tile generation",
			zoomLevel, maxBatchZoomLevel)
	}

	numTiles := int(math.Exp2(float64(zoomLevel)))

	sem := make(chan struct{}, maxConcurrentTileGeneration)
	var wg sync.WaitGroup

	for x := 0; x < numTiles; x++ {
		for y := 0; y < numTiles; y++ {
			x, y := x, y

			wg.Add(1)
			sem <- struct{}{}

			go func() {
				defer wg.Done()
				defer func() { <-sem }()

				start := time.Now()

				_, err := t.GetTileHeightmap(zoomLevel, x, y, 256)

				if err != nil {
					log.Warnf("cannot generate heightmap for tile (%d, %d, %d). Cause: %s",
						x, y, zoomLevel, err)
					return
				}

				log.Infof("Heightmap for tile (%d, %d, %d) generated. Took %s",
					x, y, zoomLevel, time.Since(start))
			}()
		}
	}

	wg.Wait()

	return nil
}

func (t Generator) createHeightProfile(lat, lon float64, side float64, processFuncParam interface{},
	processFunc heightProfileProcessFunc) error {
	i := 0

	side = math.Ceil(side)

	for x := 0; x < int(side); x = x + heightDataResolution {
		var newLat, newLon float64
		geodesic.WGS84.Direct(lat, lon, southAzimuth, float64(x), &newLat, &newLon, nil)

		for y := 0; y < int(side); y = y + heightDataResolution {
			var pLat, pLon float64
			geodesic.WGS84.Direct(newLat, newLon, eastAzimuth, float64(y), &pLat, &pLon, nil)

			if t.SrtmDownloader != nil {
				_, err := t.SrtmDownloader.DownloadDemFile(pLat, pLon)

				if err != nil {
					if errors.Is(err, srtm.ErrTileNotInsideSrtmCoverage) {
						continue
					}

					msg := "cannot download digital elevation model file for coordinate %f, %f. Cause: %s"
					log.Debugf(msg, pLat, pLon, err)
					return err
				}
			}

			e, _, elevationErr := t.ElevationDataset.ElevationAt(pLat, pLon)

			if elevationErr != nil {
				log.Debugf("cannot read elevation for coordinate %f, %f. Cause: %s", pLat, pLon, elevationErr)
				e = NoElevationData
			}

			point := &Point{x / heightDataResolution, y / heightDataResolution, pLat, pLon, e}
			err := processFunc(point, processFuncParam, i)

			if err != nil {
				log.Errorf("cannot process point (%d, %d). Cause: %s", point.X, point.Y, err)
			}

			i++
		}
	}

	return nil
}

// ErrTileNotCached is returned by getTileFromDisk when the requested tile has
// not been generated yet, as opposed to some other read failure.
var ErrTileNotCached = errors.New("tile is not cached")

// saveTile writes a tile to a temporary file in the destination directory and
// renames it into place. The rename is atomic, so a concurrent read of the
// same tile (getTileFromDisk) never observes a partially written file, and two
// concurrent writers of the same tile never corrupt each other's output.
func (t Generator) saveTile(x int, y int, z, resolution int, bytes []byte) (string, error) {
	dir := formatTileDirPath(t.Dir, x, z, resolution)
	err := os.MkdirAll(dir, os.ModePerm)

	if err != nil {
		return "", fmt.Errorf("cannot create directories to store tiles. Cause: %w", err)
	}

	path := fmt.Sprintf("%s/%d.png", dir, y)

	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	log.Infof("Saving tile (%d, %d, %d) to disk", x, y, z)

	tmpFile, err := os.CreateTemp(dir, fmt.Sprintf(".%d.png.tmp-*", y))

	if err != nil {
		return "", fmt.Errorf("cannot create temporary tile file. Cause: %w", err)
	}

	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(bytes); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("cannot write tile file. Cause: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("cannot close tile file. Cause: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("cannot rename tile file. Cause: %w", err)
	}

	return path, nil
}

func (t Generator) getTileFromDisk(x, y, z, resolution int) ([]byte, error) {
	path := formatTilePath(t.Dir, x, y, z, resolution)

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrTileNotCached
		}

		return nil, fmt.Errorf("cannot stat tile file %s. Cause: %w", path, err)
	}

	bytes, err := os.ReadFile(path)

	if err != nil {
		return nil, fmt.Errorf("cannot read tile from disk. Cause: %w", err)
	}

	return bytes, nil
}

func formatTilePath(dir string, x, y, z, resolution int) string {
	dir = formatTileDirPath(dir, x, z, resolution)
	yStr := fmt.Sprintf("%d", y)

	return dir + filePathSep + yStr + ".png"
}

func formatTileDirPath(dir string, x, z, resolution int) string {
	resStr := fmt.Sprintf("%d", resolution)
	xStr := fmt.Sprintf("%d", x)
	zStr := fmt.Sprintf("%d", z)

	return dir + filePathSep + resStr + filePathSep + zStr + filePathSep + xStr
}

func calculateTileSizeKm(zoomLevel int) float64 {
	const earthCircumferenceKm = 40075.0
	return earthCircumferenceKm / math.Exp2(float64(zoomLevel))
}
