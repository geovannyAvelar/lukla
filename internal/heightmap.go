package internal

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
	"github.com/mazznoer/colorgrad"
	"github.com/nfnt/resize"
	"github.com/petoc/hgt"
	"github.com/tidwall/geodesic"
)

const HEIGHT_DATA_RESOLUTION = 30

var FILE_PATH_SEP = strings.ReplaceAll(strconv.QuoteRune(os.PathSeparator), "'", "")

var ZOOM_LEVEL_SIDE = map[int]int{
	10: 35817,
	11: 18023,
	12: 4635,
	13: 4503,
	14: 2251,
	15: 292,
}

type Elevation struct {
	Width     int
	Height    int
	MinHeight int16
	MaxHeight int16
	Points    []Point
}

type Point struct {
	X, Y      int
	Lat, Lon  float64
	Elevation int16
}

type Heightmap struct {
	ElevationDataset *hgt.DataDir
	Dir              string
}

type HeightmapResolutionConfig struct {
	Width  int
	Heigth int
}

func (t *Heightmap) GetTileHeightmap(z, x, y, resolution int) ([]byte, error) {
	bytes, err := t.getTileFromDisk(x, y, z, resolution)

	if err == nil {
		return bytes, nil
	}

	osmTile := gosm.NewTileWithXY(x, y, z)
	lat, lon := osmTile.Num2deg()

	tileSide := 0
	if val, ok := ZOOM_LEVEL_SIDE[z]; ok {
		tileSide = val
	} else {
		return []byte{}, errors.New("invalid zoom level. Minimum zoom level is 10, maximum is 15")
	}

	bytes, err = t.createHeightMapImage(lat, lon, tileSide,
		&HeightmapResolutionConfig{resolution, resolution})

	if err != nil {
		return []byte{}, err
	}

	go t.saveTile(x, y, z, resolution, bytes)

	return bytes, nil
}

func (t *Heightmap) createHeightMapImage(lat, lon float64, side int,
	conf *HeightmapResolutionConfig) ([]byte, error) {
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

	if conf != nil && conf.Heigth > 0 && conf.Width > 0 {
		resizedImg := resize.Resize(uint(conf.Width), uint(conf.Heigth), imgRgba, resize.Lanczos3)
		err := png.Encode(writer, resizedImg)

		if err != nil {
			return []byte{}, errors.New("cannot resize heightmap image")
		}
	}

	err = png.Encode(writer, imgRgba)

	if err != nil {
		return []byte{}, errors.New("cannot encode PNG image")
	}

	return b.Bytes(), nil
}

func (t *Heightmap) createHeightProfile(lat, lon float64, side int) (Elevation, error) {
	step := int(math.Ceil(float64(side) / float64(HEIGHT_DATA_RESOLUTION)))

	points := make([]Point, step*step)

	var minHeight int16 = 0
	var maxHeight int16 = 0

	i := 0
	for x := 0; x < side; x = x + HEIGHT_DATA_RESOLUTION {
		var new_lat float64
		var new_lon float64
		geodesic.WGS84.Direct(lat, lon, 180, float64(x), &new_lat, &new_lon, nil)

		for y := 0; y < side; y = y + HEIGHT_DATA_RESOLUTION {
			var pLat, pLon float64
			geodesic.WGS84.Direct(new_lat, new_lon, 90, float64(y), &pLat, &pLon, nil)

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

			points[i] = Point{x / HEIGHT_DATA_RESOLUTION, y / HEIGHT_DATA_RESOLUTION, pLat, pLon, e}

			i++
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

func (t *Heightmap) saveTile(x int, y int, z, resolution int, bytes []byte) (string, error) {
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

func (t *Heightmap) getTileFromDisk(x, y, z, resolution int) ([]byte, error) {
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

	return dir + FILE_PATH_SEP + yStr + ".png"
}

func formatTileDirPath(dir string, x, z, resolution int) string {
	resStr := fmt.Sprintf("%d", resolution)
	xStr := fmt.Sprintf("%d", x)
	zStr := fmt.Sprintf("%d", z)

	return dir + FILE_PATH_SEP + resStr + FILE_PATH_SEP + zStr + FILE_PATH_SEP + xStr
}
