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
	"time"

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

	err = heightmapGen.createHeightProfile(27.687397, 86.731814, side, heightDataResolution, nil,
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
// materialization, generating real tiles (no cache pre-seeding). This used to
// be infeasible: at zoom 1, GetTileHeightmap covers a ~20,037,500m-wide area
// per tile, and CreateHeightMapImage sampled that at a fixed 30m stride
// regardless of the requested (256px) resolution, which OOM-killed the test
// process. samplingStride (see TestSamplingStride) fixes that by coarsening
// the sampling stride to match the requested resolution.
func TestGenerateAllTilesInZoomLevelGeneratesAllTiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	h, err := hgt.OpenDataDir(demDatasetDir, nil)
	if err != nil {
		t.Fatalf("cannot open DEM dir. Cause: %s", err)
	}
	defer h.Close()

	gen := Generator{ElevationDataset: h, Dir: dir}

	const zoom = 1
	const numTiles = 2 // 2^zoom

	done := make(chan error, 1)

	go func() {
		done <- gen.GenerateAllTilesInZoomLevel(zoom)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("GenerateAllTilesInZoomLevel did not return in time; the fixed 30m " +
			"sampling stride regression may have reappeared")
	}

	// Tiles are saved to disk asynchronously by GetTileHeightmap, so poll for
	// their arrival instead of asserting immediately.
	deadline := time.Now().Add(5 * time.Second)

	for {
		missing := 0

		for x := 0; x < numTiles; x++ {
			for y := 0; y < numTiles; y++ {
				if _, err := os.Stat(formatTilePath(dir, x, y, zoom, 256)); err != nil {
					missing++
				}
			}
		}

		if missing == 0 {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("%d/%d tiles were not saved to disk in time", missing, numTiles*numTiles)
		}

		time.Sleep(50 * time.Millisecond)
	}
}

func TestSamplingStride(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		side float64
		conf ResolutionConfig
		want int
	}{
		{
			"small area with normal resolution keeps the native stride",
			2251.0, ResolutionConfig{Width: 256, Height: 256}, heightDataResolution,
		},
		{
			"huge area with normal resolution coarsens the stride",
			20037500.0, ResolutionConfig{Width: 256, Height: 256}, 20037500 / 256,
		},
		{
			"no resolution over a huge area is bounded by maxSamplingDimension",
			20037500.0, ResolutionConfig{}, 20037500 / maxSamplingDimension,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := samplingStride(tt.side, tt.conf)

			if got != tt.want {
				t.Errorf("expected stride %d, got %d", tt.want, got)
			}
		})
	}
}

// TestCreateHeightMapImageBoundedForLargeArea is a direct regression test for
// the OOM: a ~20,037,500m-wide area (roughly an OSM zoom-1 tile) with a
// normal 256x256 resolution request used to attempt a ~668,000x668,000 native
// RGBA allocation before any resize logic ran. It must now complete quickly
// and return a correctly-sized image.
func TestCreateHeightMapImageBoundedForLargeArea(t *testing.T) {
	t.Parallel()

	h, err := hgt.OpenDataDir(demDatasetDir, nil)
	if err != nil {
		t.Fatalf("cannot open DEM dir. Cause: %s", err)
	}
	defer h.Close()

	gen := Generator{ElevationDataset: h}

	const zoom1TileSideMeters = 20037500.0
	conf := ResolutionConfig{Width: 256, Height: 256, ForceInterpolation: true}

	type result struct {
		b   []byte
		err error
	}

	done := make(chan result, 1)

	go func() {
		b, err := gen.CreateHeightMapImage(27.687397, 86.731814, zoom1TileSideMeters, conf)
		done <- result{b, err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("unexpected error: %s", r.err)
		}

		img, err := png.Decode(bytes.NewReader(r.b))
		if err != nil {
			t.Fatalf("response is not a valid PNG. Cause: %s", err)
		}

		bounds := img.Bounds()
		if bounds.Dx() != 256 || bounds.Dy() != 256 {
			t.Errorf("expected a 256x256 image, got %dx%d", bounds.Dx(), bounds.Dy())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CreateHeightMapImage did not return in time for a zoom-1-scale area; " +
			"the fixed 30m sampling stride regression may have reappeared")
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
