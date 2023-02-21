package heightmap

import (
	"bytes"
	"os"
	"testing"

	"github.com/petoc/hgt"
)

const DEM_DATASET_DIR = "testdata/dem"
const TILES_DIR = "./testdata/tiles"

func TestCreateHeightProfile(t *testing.T) {
	t.Parallel()

	h, err := hgt.OpenDataDir(DEM_DATASET_DIR, nil)

	if err != nil {
		panic(err)
	}

	defer h.Close()

	heightmapGen := HeightmapGenerator{
		ElevationDataset: h,
		Dir:              TILES_DIR,
	}

	_, err = heightmapGen.createHeightProfile(27.687397, 86.731814, 2251)

	if err != nil {
		t.Errorf("cannot create height profile. cause: %s", err)
	}
}

func TestSaveTile(t *testing.T) {
	t.Parallel()

	heightmapGen := HeightmapGenerator{
		Dir: TILES_DIR,
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

	tilePath := TILES_DIR + "/256/0/0/0.png"
	err := os.WriteFile(tilePath, []byte{0}, 0700)

	if err != nil {
		t.Errorf("cannot create file %s. cause: %s", tilePath, err)
		return
	}

	heightmapGen := HeightmapGenerator{
		Dir: TILES_DIR,
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

	expected := TILES_DIR + "/256/0/0/0.png"
	path := formatTilePath(TILES_DIR, 0, 0, 0, 256)

	if path != expected {
		t.Errorf("expected %s but received %s", expected, path)
	}
}
