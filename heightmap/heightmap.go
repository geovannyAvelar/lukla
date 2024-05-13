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
const heightDataResolution = 30

// Azimuth angle pointing to the south
const southAzimuth = 180

// Azimuth angle pointing to the east
const eastAzimuth = 90

// Path separator
var filePathSep = strings.ReplaceAll(strconv.QuoteRune(os.PathSeparator), "'", "")

// Tile side in meters for each zoom level in OpenStreetMap (OSM)
var zoomLevelSide = map[int]int{
	10: 35817,
	11: 18023,
	12: 4635,
	13: 4503,
	14: 2251,
	15: 292,
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

	tileSide := 0
	if val, ok := zoomLevelSide[z]; ok {
		tileSide = val
	} else {
		return []byte{}, errors.New("invalid zoom level. Minimum zoom level is 10, maximum is 15")
	}

	bytes, err = t.CreateHeightMapImage(lat, lon, tileSide,
		ResolutionConfig{resolution, resolution, false})

	if err != nil {
		return []byte{}, err
	}

	go t.saveTile(x, y, z, resolution, bytes)

	return bytes, nil
}

func (t Generator) CreateHeightMapImage(lat, lon float64, side int,
	conf ResolutionConfig) ([]byte, error) {
	elevation, err := t.createHeightProfile(lat, lon, side)

	if err != nil {
		return []byte{}, err
	}

	gradient, _ := colorgrad.NewGradient().Domain(float64(elevation.MinHeight),
		float64(elevation.MaxHeight)).Build()

	upLeft := image.Point{0, 0}
	lowRight := image.Point{elevation.Width, elevation.Height}

	imgRgba := image.NewRGBA(image.Rectangle{upLeft, lowRight})

	for _, p := range elevation.Points {
		imgRgba.Set(p.X, p.Y, gradient.At(float64(p.Elevation)))
	}

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	if !conf.IgnoreWhenOriginalImageIsSmaller {
		if conf.Height > elevation.Height && conf.Width > elevation.Width {
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

func (t Generator) createHeightProfile(lat, lon float64, side int) (Elevation, error) {
	step := int(math.Ceil(float64(side)/float64(heightDataResolution))) + 1

	points := make([]Point, step*step)

	var minHeight int16 = 0
	var maxHeight int16 = 0

	i := 0
	for x := 0; x < side; x = x + heightDataResolution {
		var newLat float64
		var newLon float64
		geodesic.WGS84.Direct(lat, lon, southAzimuth, float64(x), &newLat, &newLon, nil)

		for y := 0; y < side; y = y + heightDataResolution {
			var pLat, pLon float64
			geodesic.WGS84.Direct(newLat, newLon, eastAzimuth, float64(y), &pLat, &pLon, nil)

			if t.SrtmDownloader != nil {
				_, err := t.SrtmDownloader.DownloadDemFile(pLat, pLon)

				if err != nil {
					msg := "cannot download digital elevation model file for coordinate %f, %f. Cause: %s"
					log.Warnf(msg, pLat, pLon, err)
				}
			}

			e, _, _ := t.ElevationDataset.ElevationAt(pLat, pLon)

			if x == 0 && y == 0 {
				minHeight = e
			}

			if e < minHeight {
				minHeight = e
			}

			if e > maxHeight {
				maxHeight = e
			}

			points[i] = Point{x / heightDataResolution, y / heightDataResolution, pLat, pLon, e}

			i++

			if y == side-(side%heightDataResolution) {
				var pLat, pLon float64
				geodesic.WGS84.Direct(newLat, newLon, southAzimuth, float64(side), &pLat, &pLon, nil)

				e, _, _ := t.ElevationDataset.ElevationAt(pLat, pLon)

				points[i] = Point{x / heightDataResolution, (y / heightDataResolution) + 1, pLat, pLon, e}

				geodesic.WGS84.Direct(newLat, newLon, eastAzimuth, float64(side), &pLat, &pLon, nil)

				e1, _, _ := t.ElevationDataset.ElevationAt(pLat, pLon)

				points[i+1] = Point{x/heightDataResolution + 1, y / heightDataResolution, pLat, pLon, e1}
			}
		}
	}

	return Elevation{
		Width:     step,
		Height:    step,
		MinHeight: minHeight,
		MaxHeight: maxHeight,
		Points:    points,
	}, nil
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
