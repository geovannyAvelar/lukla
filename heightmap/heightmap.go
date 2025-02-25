package heightmap

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/apeyroux/gosm"
	"github.com/geovannyAvelar/lukla/srtm"
	"github.com/mazznoer/colorgrad"
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

// Path separator
var filePathSep = strings.ReplaceAll(strconv.QuoteRune(os.PathSeparator), "'", "")

type heightProfileProcessFunc func(*Point, interface{}, int) error

func createInMemoryHeightProfile(point *Point, i interface{}, index int) error {
	e := i.(*Elevation)
	e.Points[index] = *point

	if point.Elevation < e.MinHeight {
		e.MinHeight = point.Elevation
	}

	if point.Elevation > e.MaxHeight {
		e.MaxHeight = point.Elevation
	}

	return nil
}

// Elevation Represents a heightmap in memory
type Elevation struct {
	Width     int
	Height    int
	MinHeight int16
	MaxHeight int16
	Points    []Point
}

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
	IgnoreWhenOriginalImageIsSmaller bool
}

// GetTileHeightmap Generate a heightmap with the same size of an OpenStreetMap (OSM) tile
func (t Generator) GetTileHeightmap(z, x, y, resolution int) ([]byte, error) {
	bytes, err := t.getTileFromDisk(x, y, z, resolution)

	if err == nil {
		return bytes, nil
	}

	osmTile := gosm.NewTileWithXY(x, y, z)
	lat, lon := osmTile.Num2deg()

	tileSide := calculateTileSizeKm(z) * 1000

	bytes, err = t.CreateHeightMapImage(lat, lon, tileSide,
		ResolutionConfig{resolution, resolution, false})

	if err != nil {
		return []byte{}, err
	}

	go func() {
		_, err := t.saveTile(x, y, z, resolution, bytes)
		if err != nil {
			log.Errorf("cannot save tile (%d, %d, %d) to disk. Cause: %s", x, y, z, err)
		}
	}()

	return bytes, nil
}

func (t Generator) CreateHeightMapImage(lat, lon float64, side float64,
	conf ResolutionConfig) ([]byte, error) {
	step := int(math.Ceil(side/heightDataResolution)) - 1

	upLeft := image.Point{}
	lowRight := image.Point{X: step, Y: step}

	imgRgba := image.NewRGBA(image.Rectangle{Min: upLeft, Max: lowRight})
	gradient, _ := colorgrad.NewGradient().Domain(0, 8865).Build()

	err := t.createHeightProfile(lat, lon, side, imgRgba, func(point *Point, i interface{}, index int) error {
		imgRgba.Set(point.X, point.Y, gradient.At(float64(point.Elevation)))
		return nil
	})

	if err != nil {
		return []byte{}, err
	}

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	if !conf.IgnoreWhenOriginalImageIsSmaller {
		if conf.Height < step && conf.Width < step {
			resizedImg := resize.Resize(uint(conf.Width), uint(conf.Height), imgRgba, resize.Lanczos3)
			err := png.Encode(writer, resizedImg)

			if err != nil {
				return []byte{}, errors.New("cannot resize heightmap image")
			}
		}
	}

	err = png.Encode(writer, imgRgba)

	if err != nil {
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

		points[i].Elevation, _, _ = t.ElevationDataset.ElevationAt(p.Lat, p.Lon)
	}

	return points
}

func (t Generator) createHeightProfile(lat, lon float64, side float64, processFuncParam interface{},
	processFunc heightProfileProcessFunc) error {
	i := 0

	for x := 0; x < int(side); x = x + heightDataResolution {
		var newLat float64
		var newLon float64
		geodesic.WGS84.Direct(lat, lon, southAzimuth, float64(x), &newLat, &newLon, nil)

		for y := 0; y < int(side); y = y + heightDataResolution {
			var pLat, pLon float64
			geodesic.WGS84.Direct(newLat, newLon, eastAzimuth, float64(y), &pLat, &pLon, nil)

			if t.SrtmDownloader != nil {
				_, err := t.SrtmDownloader.DownloadDemFile(pLat, pLon)

				if err != nil {
					msg := "cannot download digital elevation model file for coordinate %f, %f. Cause: %s"
					log.Debugf(msg, pLat, pLon, err)
				}
			}

			e, _, _ := t.ElevationDataset.ElevationAt(pLat, pLon)

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

func (t Generator) saveTile(x int, y int, z, resolution int, bytes []byte) (string, error) {
	dir := formatTileDirPath(t.Dir, x, z, resolution)
	err := os.MkdirAll(dir, os.ModePerm)

	if err != nil {
		return "", fmt.Errorf("cannot create directories to store tiles. Cause: %w", err)
	}

	filepath := fmt.Sprintf("%s/%d.png", dir, y)

	if _, err := os.Stat(filepath); err == nil {
		return filepath, nil
	}

	err = os.WriteFile(filepath, bytes, 0644)

	if err != nil {
		return "", fmt.Errorf("cannot create tile file. Cause: %w", err)
	}

	return filepath, nil
}

func (t Generator) getTileFromDisk(x, y, z, resolution int) ([]byte, error) {
	path := formatTilePath(t.Dir, x, y, z, resolution)

	if _, err := os.Stat(path); err != nil {
		return nil, errors.New("tile is not cached")
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
