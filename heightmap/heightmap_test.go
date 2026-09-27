package heightmap

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

	side := 2251.0

	err = heightmapGen.createHeightProfile(27.687397, 86.731814, side, nil,
		func(p *Point, i1 interface{}, i2 int) error {
			return nil
		})

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

// writeFlatHgtFile writes a synthetic SRTM3 (3 arc-second, 1201x1201 samples)
// .hgt file where every sample equals elevation, so tests can exercise a
// known, valid elevation reading (including zero) without a real DEM dataset.
func writeFlatHgtFile(t *testing.T, dir, name string, elevation int16) {
	t.Helper()

	const samples = 1201
	data := make([]byte, samples*samples*2)

	for i := 0; i < samples*samples; i++ {
		binary.BigEndian.PutUint16(data[i*2:], uint16(elevation))
	}

	if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
		t.Fatalf("cannot write fixture hgt file %s. Cause: %s", name, err)
	}
}

func TestGetPointsElevationsValidZero(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFlatHgtFile(t, dir, "N10E010.hgt", 0)

	h, err := hgt.OpenDataDir(dir, nil)
	if err != nil {
		t.Fatalf("cannot open DEM dir. Cause: %s", err)
	}
	defer h.Close()

	gen := Generator{ElevationDataset: h}

	points := gen.GetPointsElevations([]Point{{Lat: 10.5, Lon: 10.5}})

	if points[0].Elevation != 0 {
		t.Errorf("expected a valid elevation of 0, got %d", points[0].Elevation)
	}
}

func TestGetPointsElevationsMissingFile(t *testing.T) {
	t.Parallel()

	h, err := hgt.OpenDataDir(demDatasetDir, nil)
	if err != nil {
		t.Fatalf("cannot open DEM dir. Cause: %s", err)
	}
	defer h.Close()

	gen := Generator{ElevationDataset: h}

	// No N27E086.hgt file exists in testdata/dem.
	points := gen.GetPointsElevations([]Point{{Lat: 27.687397, Lon: 86.731814}})

	if points[0].Elevation != NoElevationData {
		t.Errorf("expected NoElevationData (%d) for a missing DEM file, got %d",
			NoElevationData, points[0].Elevation)
	}
}

func TestGetPointsElevationsOutOfCoverage(t *testing.T) {
	t.Parallel()

	h, err := hgt.OpenDataDir(demDatasetDir, nil)
	if err != nil {
		t.Fatalf("cannot open DEM dir. Cause: %s", err)
	}
	defer h.Close()

	gen := Generator{ElevationDataset: h}

	// SRTM only covers latitudes in [-56, 60).
	points := gen.GetPointsElevations([]Point{{Lat: 75.0, Lon: 10.0}})

	if points[0].Elevation != NoElevationData {
		t.Errorf("expected NoElevationData (%d) for a coordinate outside SRTM coverage, got %d",
			NoElevationData, points[0].Elevation)
	}
}

func TestCreateHeightMapImagePNG(t *testing.T) {
	t.Parallel()

	h, err := hgt.OpenDataDir(demDatasetDir, nil)
	if err != nil {
		t.Fatalf("cannot open DEM dir. Cause: %s", err)
	}
	defer h.Close()

	gen := Generator{ElevationDataset: h}

	// side=2100 meters at 30m/px gives a native step of 2100/30 = 70 pixels.
	const side = 2100.0
	const step = 70

	tests := []struct {
		name                  string
		conf                  ResolutionConfig
		wantWidth, wantHeight int
	}{
		{"no resolution requested keeps native size", ResolutionConfig{}, step, step},
		{
			"negative resolution is ignored safely", ResolutionConfig{Width: -5, Height: -5},
			step, step,
		},
		{"downscale both dimensions", ResolutionConfig{Width: 32, Height: 32}, 32, 32},
		{
			"only one dimension needs downscaling",
			ResolutionConfig{Width: 32, Height: step}, 32, step,
		},
		{
			"force interpolation upsizes past native size",
			ResolutionConfig{Width: 200, Height: 200, ForceInterpolation: true}, 200, 200,
		},
		{
			"ignore when original is smaller keeps native size",
			ResolutionConfig{Width: 200, Height: 200, IgnoreWhenOriginalImageIsSmaller: true},
			step, step,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b, err := gen.CreateHeightMapImage(27.687397, 86.731814, side, tt.conf)
			if err != nil {
				t.Fatalf("cannot create heightmap image. Cause: %s", err)
			}

			img, err := png.Decode(bytes.NewReader(b))
			if err != nil {
				t.Fatalf("response is not a single valid PNG image. Cause: %s", err)
			}

			bounds := img.Bounds()
			if bounds.Dx() != tt.wantWidth || bounds.Dy() != tt.wantHeight {
				t.Errorf("expected image %dx%d, got %dx%d",
					tt.wantWidth, tt.wantHeight, bounds.Dx(), bounds.Dy())
			}
		})
	}
}

func TestGenerateAllTilesInZoomLevelRejectsZoomAboveLimit(t *testing.T) {
	t.Parallel()

	gen := Generator{}

	if err := gen.GenerateAllTilesInZoomLevel(maxBatchZoomLevel + 1); err == nil {
		t.Error("expected an error for a zoom level above the batch limit, got none")
	}
}

func TestGenerateAllTilesInZoomLevelRejectsNegativeZoom(t *testing.T) {
	t.Parallel()

	gen := Generator{}

	if err := gen.GenerateAllTilesInZoomLevel(-1); err == nil {
		t.Error("expected an error for a negative zoom level, got none")
	}
}

// TestGenerateAllTilesInZoomLevelGeneratesAllTiles exercises the incremental,
// concurrency-limited enumeration added to replace the full in-memory tile
// materialization. It pre-seeds the tile cache for every tile of the zoom
// level so GetTileHeightmap short-circuits through getTileFromDisk: actually
// generating a real height profile for a whole zoom level's tiles hits a
// pre-existing, out-of-scope cost problem (see final report) unrelated to
// this fix, since createHeightProfile always samples at a fixed 30m step
// regardless of the tile's real-world size.
func TestGenerateAllTilesInZoomLevelGeneratesAllTiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const zoom = 1
	const numTiles = 2 // 2^zoom

	for x := 0; x < numTiles; x++ {
		for y := 0; y < numTiles; y++ {
			tileDir := formatTileDirPath(dir, x, zoom, 256)

			if err := os.MkdirAll(tileDir, os.ModePerm); err != nil {
				t.Fatalf("cannot create tile dir. Cause: %s", err)
			}

			path := formatTilePath(dir, x, y, zoom, 256)

			if err := os.WriteFile(path, []byte{byte(x), byte(y)}, 0644); err != nil {
				t.Fatalf("cannot seed cached tile. Cause: %s", err)
			}
		}
	}

	gen := Generator{Dir: dir}

	if err := gen.GenerateAllTilesInZoomLevel(zoom); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

func TestGetTileFromDiskNotCached(t *testing.T) {
	t.Parallel()

	gen := Generator{Dir: t.TempDir()}

	_, err := gen.getTileFromDisk(0, 0, 0, 256)

	if !errors.Is(err, ErrTileNotCached) {
		t.Errorf("expected ErrTileNotCached, got: %v", err)
	}
}

func TestSaveTileConcurrentWritesProduceValidFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	gen := Generator{Dir: dir}

	payload := bytes.Repeat([]byte{0xAB}, 4096)

	const concurrency = 10
	var wg sync.WaitGroup
	paths := make([]string, concurrency)
	errs := make([]error, concurrency)

	for i := 0; i < concurrency; i++ {
		i := i

		wg.Add(1)
		go func() {
			defer wg.Done()
			paths[i], errs[i] = gen.saveTile(1, 1, 1, 256, payload)
		}()
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("saveTile call #%d failed: %s", i, err)
		}
	}

	b, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatalf("cannot read saved tile. Cause: %s", err)
	}

	if !bytes.Equal(b, payload) {
		t.Error("saved tile content does not match the input; concurrent writes may have torn the file")
	}

	entries, err := os.ReadDir(filepath.Dir(paths[0]))
	if err != nil {
		t.Fatalf("cannot list tile directory. Cause: %s", err)
	}

	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("leftover temporary file was not cleaned up: %s", e.Name())
		}
	}
}
