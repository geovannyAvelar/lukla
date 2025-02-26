package heightmap

import (
	"bytes"
	"math"
	"os"
	"testing"

	"github.com/petoc/hgt"
)

const demDatasetDir = "testdata/dem"
const tilesDir = "./testdata/tiles"

func TestCreateHeightProfile(t *testing.T) {
	t.Parallel()

	h, err := hgt.OpenDataDir(demDatasetDir, nil)

	if err != nil {
		panic(err)
	}

	defer h.Close()

	heightmapGen := Generator{
		ElevationDataset: h,
		Dir:              tilesDir,
	}

	side := 2251
	step := int(math.Ceil(float64(side)/float64(heightDataResolution))) + 1

	elevation := &Elevation{
		Width:     step,
		Height:    step,
		MinHeight: 0,
		MaxHeight: 0,
		Points:    make([]Point, step*step),
	}

	err = heightmapGen.createHeightProfile(27.687397, 86.731814, side, elevation, createInMemoryHeightProfile)

	if err != nil {
		t.Errorf("cannot create height profile. cause: %s", err)
	}
}

func TestSaveTile(t *testing.T) {
	t.Parallel()

	heightmapGen := Generator{
		Dir: tilesDir,
	}

	path, err := heightmapGen.saveTile(1, 1, 1, 256, []byte{0})

	if err != nil {
		t.Errorf("cannot save tile. cause: %s", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("file %s does not exists", path)
	}

	os.Remove(path)
}

func TestGetTileFromDisk(t *testing.T) {
	t.Parallel()

	tilePath := tilesDir + "/256/0/0/0.png"
	err := os.WriteFile(tilePath, []byte{0}, 0700)

	if err != nil {
		t.Errorf("cannot create file %s. cause: %s", tilePath, err)
		return
	}

	heightmapGen := Generator{
		Dir: tilesDir,
	}

	b, err := heightmapGen.getTileFromDisk(0, 0, 0, 256)

	if err != nil {
		t.Errorf("cannot get tile. cause: %s", err)
	}

	if !bytes.Equal(b, []byte{0}) {
		t.Errorf("tile payloads doesn't match")
	}

	os.Remove(tilePath)
}

func TestFormatTilePath(t *testing.T) {
	t.Parallel()

	expected := tilesDir + "/256/0/0/0.png"
	path := formatTilePath(tilesDir, 0, 0, 0, 256)

	if path != expected {
		t.Errorf("expected %s but received %s", expected, path)
	}
}

func TestGetPointsElevations(t *testing.T) {
	t.Parallel()

	h, err := hgt.OpenDataDir(demDatasetDir, nil)

	if err != nil {
		panic(err)
	}

	defer h.Close()

	heightmapGen := Generator{
		ElevationDataset: h,
	}

	var points = make([]Point, 2)
	points[0] = Point{
		Lat: 0.0,
		Lon: 0.0,
	}
	points[1] = Point{
		Lat: 27.687397,
		Lon: 86.731814,
	}

	altitudes := heightmapGen.GetPointsElevations(points)

	if len(altitudes) != len(points) {
		t.Error("cannot get altitudes all points. Altitudes and points slices with different length")
	}
}
