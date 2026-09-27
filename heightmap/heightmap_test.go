package heightmap

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geovannyAvelar/lukla/srtm"
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

	_, err = heightmapGen.createHeightProfile(context.Background(), 27.687397, 86.731814, side, heightDataResolution, nil,
		func(p *Point, i1 interface{}, i2 int) error {
			return nil
		})

	if err != nil {
		t.Errorf("cannot create height profile. cause: %s", err)
	}
}

func TestCreateHeightProfileCountsFailedPoints(t *testing.T) {
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

	failedPoints, err := heightmapGen.createHeightProfile(context.Background(), 27.687397, 86.731814, side, heightDataResolution, nil,
		func(p *Point, i1 interface{}, i2 int) error {
			if i2%2 == 0 {
				return errors.New("simulated processing failure")
			}
			return nil
		})

	if err != nil {
		t.Fatalf("cannot create height profile. cause: %s", err)
	}

	if failedPoints == 0 {
		t.Error("expected at least one failed point to be counted, got 0")
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

			b, err := gen.CreateHeightMapImage(context.Background(), 27.687397, 86.731814, side, tt.conf)
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

	if _, err := gen.GenerateAllTilesInZoomLevel(context.Background(), maxBatchZoomLevel+1); err == nil {
		t.Error("expected an error for a zoom level above the batch limit, got none")
	}
}

func TestGenerateAllTilesInZoomLevelRejectsNegativeZoom(t *testing.T) {
	t.Parallel()

	gen := Generator{}

	if _, err := gen.GenerateAllTilesInZoomLevel(context.Background(), -1); err == nil {
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

	type outcome struct {
		result TileGenerationResult
		err    error
	}

	done := make(chan outcome, 1)

	go func() {
		result, err := gen.GenerateAllTilesInZoomLevel(context.Background(), zoom)
		done <- outcome{result, err}
	}()

	var o outcome

	select {
	case o = <-done:
		if o.err != nil {
			t.Fatalf("unexpected error: %s", o.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("GenerateAllTilesInZoomLevel did not return in time; the fixed 30m " +
			"sampling stride regression may have reappeared")
	}

	wantTotal := numTiles * numTiles

	if o.result.Total != wantTotal {
		t.Errorf("expected Total %d, got %d", wantTotal, o.result.Total)
	}

	if o.result.Succeeded != wantTotal {
		t.Errorf("expected all %d tiles to succeed, got Succeeded=%d, Failed=%d",
			wantTotal, o.result.Succeeded, o.result.Failed)
	}

	// Tile saves are now synchronous (see the GetTileHeightmap dedup fix), so
	// by the time GenerateAllTilesInZoomLevel returns every successful tile is
	// already on disk - no polling needed.
	for x := 0; x < numTiles; x++ {
		for y := 0; y < numTiles; y++ {
			if _, err := os.Stat(formatTilePath(dir, x, y, zoom, 256)); err != nil {
				t.Errorf("tile (%d, %d, %d) was not saved to disk. Cause: %s", x, y, zoom, err)
			}
		}
	}
}

// TestGenerateAllTilesInZoomLevelReportsPartialFailure is a regression test
// for GenerateAllTilesInZoomLevel always returning a nil error regardless of
// how many individual tiles failed, which a caller could misread as full
// success.
func TestGenerateAllTilesInZoomLevelReportsPartialFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	h, err := hgt.OpenDataDir(demDatasetDir, nil)
	if err != nil {
		t.Fatalf("cannot open DEM dir. Cause: %s", err)
	}
	defer h.Close()

	failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "downloader down", http.StatusInternalServerError)
	}))
	defer failingServer.Close()

	gen := Generator{
		ElevationDataset: h,
		Dir:              dir,
		SrtmDownloader: &srtm.Downloader{
			BasePath:   failingServer.URL,
			Dir:        t.TempDir(),
			Api:        &srtm.EarthdataApi{BaseUrl: failingServer.URL, HttpClient: http.DefaultClient},
			HttpClient: http.DefaultClient,
		},
	}

	const zoom = 1 // 4 tiles

	result, err := gen.GenerateAllTilesInZoomLevel(context.Background(), zoom)

	if err != nil {
		t.Fatalf("unexpected structural error: %s", err)
	}

	if result.Failed == 0 {
		t.Fatal("expected at least one tile to fail with a broken SrtmDownloader, got Failed=0")
	}

	if result.Succeeded+result.Failed != result.Total {
		t.Errorf("expected Succeeded+Failed (%d+%d) to equal Total (%d)",
			result.Succeeded, result.Failed, result.Total)
	}
}

// TestGenerateAllTilesInZoomLevelCancellation is a regression test for the
// batch having no way to stop in-progress work: it starts a real (uncached)
// zoom-4 batch (256 tiles, above maxConcurrentTileGeneration), cancels the
// context shortly after starting, and checks that the batch stops promptly
// instead of running to completion, that the result honestly reflects only
// part of the batch was attempted, and that no goroutines are left running.
func TestGenerateAllTilesInZoomLevelCancellation(t *testing.T) {
	t.Parallel()

	h, err := hgt.OpenDataDir(demDatasetDir, nil)
	if err != nil {
		t.Fatalf("cannot open DEM dir. Cause: %s", err)
	}
	defer h.Close()

	gen := Generator{ElevationDataset: h, Dir: t.TempDir()}

	const zoom = 4 // 256 tiles

	ctx, cancel := context.WithCancel(context.Background())

	baselineGoroutines := runtime.NumGoroutine()

	type outcome struct {
		result TileGenerationResult
		err    error
	}

	done := make(chan outcome, 1)

	go func() {
		result, err := gen.GenerateAllTilesInZoomLevel(ctx, zoom)
		done <- outcome{result, err}
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	var o outcome

	select {
	case o = <-done:
		if o.err != nil {
			t.Fatalf("unexpected structural error: %s", o.err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("GenerateAllTilesInZoomLevel did not return promptly after cancellation")
	}

	const wantTotal = 256

	if o.result.Total != wantTotal {
		t.Errorf("expected Total %d, got %d", wantTotal, o.result.Total)
	}

	if !o.result.Canceled {
		t.Error("expected Canceled to be true")
	}

	if o.result.Succeeded+o.result.Failed > o.result.Total {
		t.Errorf("Succeeded+Failed (%d+%d) exceeds Total (%d)",
			o.result.Succeeded, o.result.Failed, o.result.Total)
	}

	if o.result.Succeeded == o.result.Total {
		t.Error("expected cancellation to prevent at least some tiles from completing successfully")
	}

	deadline := time.Now().Add(5 * time.Second)

	for {
		current := runtime.NumGoroutine()

		if current <= baselineGoroutines+5 { // generous slack for unrelated runtime goroutines
			break
		}

		if time.Now().After(deadline) {
			t.Errorf("goroutine count did not settle after cancellation: baseline=%d, current=%d",
				baselineGoroutines, current)
			break
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

// writeAlternatingHgtFile writes a synthetic SRTM3-shaped (1201x1201) .hgt
// file where adjacent samples alternate between two elevations, so that any
// window of more than one native sample is guaranteed to contain both
// values - useful for proving a sampling stride isn't skipping real detail.
func writeAlternatingHgtFile(t *testing.T, dir, name string, a, b int16) {
	t.Helper()

	const samples = 1201
	data := make([]byte, samples*samples*2)

	for row := 0; row < samples; row++ {
		for col := 0; col < samples; col++ {
			e := a
			if (row+col)%2 == 1 {
				e = b
			}

			idx := (row*samples + col) * 2
			binary.BigEndian.PutUint16(data[idx:], uint16(e))
		}
	}

	if err := os.WriteFile(filepath.Join(dir, name), data, 0644); err != nil {
		t.Fatalf("cannot write fixture hgt file %s. Cause: %s", name, err)
	}
}

// TestSamplingStrideNativeResolutionPreservesDetail documents and verifies
// samplingStride's central quality claim: a small-area request with a normal
// resolution stays at the native 30m stride and loses no real elevation
// detail. It uses a checkerboard elevation fixture (see
// writeAlternatingHgtFile) so that flattening the sampling would be visible
// as a single-color output image.
func TestSamplingStrideNativeResolutionPreservesDetail(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeAlternatingHgtFile(t, dir, "N10E010.hgt", 0, 1000)

	h, err := hgt.OpenDataDir(dir, nil)
	if err != nil {
		t.Fatalf("cannot open DEM dir. Cause: %s", err)
	}
	defer h.Close()

	gen := Generator{ElevationDataset: h}

	// A small area with a normal resolution: side/max(Width,Height) = 500/64
	// = 7, well under heightDataResolution (30), so samplingStride keeps the
	// native 30m stride (see TestSamplingStride's "small area" case).
	const side = 500.0
	conf := ResolutionConfig{Width: 64, Height: 64}

	b, err := gen.CreateHeightMapImage(context.Background(), 10.5, 10.5, side, conf)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("response is not a valid PNG. Cause: %s", err)
	}

	colors := map[color.RGBA]bool{}
	bounds := img.Bounds()

	for y := bounds.Min.Y; y < bounds.Max.Y && len(colors) < 2; y++ {
		for x := bounds.Min.X; x < bounds.Max.X && len(colors) < 2; x++ {
			r, g, bl, al := img.At(x, y).RGBA()
			colors[color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bl >> 8), A: uint8(al >> 8)}] = true
		}
	}

	if len(colors) < 2 {
		t.Error("expected more than one distinct pixel color for a small-area, native-stride " +
			"request over varying elevation data; a flat image would mean detail is being lost " +
			"even in the regime that should preserve it")
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
		b, err := gen.CreateHeightMapImage(context.Background(), 27.687397, 86.731814, zoom1TileSideMeters, conf)
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

// TestGetTileHeightmapDeduplicatesConcurrentRequests is a regression test for
// GetTileHeightmap doing no coordination between concurrent requests for the
// same tile: it fires many concurrent identical requests and checks the whole
// batch finishes close to the cost of a single generation, not N of them.
func TestGetTileHeightmapDeduplicatesConcurrentRequests(t *testing.T) {
	t.Parallel()

	h, err := hgt.OpenDataDir(demDatasetDir, nil)
	if err != nil {
		t.Fatalf("cannot open DEM dir. Cause: %s", err)
	}
	defer h.Close()

	const zoom = 10
	const resolution = 256

	baseline := Generator{ElevationDataset: h, Dir: t.TempDir()}

	start := time.Now()
	if _, err := baseline.GetTileHeightmap(context.Background(), zoom, 0, 0, resolution); err != nil {
		t.Fatalf("unexpected error generating baseline tile: %s", err)
	}
	singleDuration := time.Since(start)

	gen := Generator{ElevationDataset: h, Dir: t.TempDir()}

	const concurrency = 20
	var wg sync.WaitGroup
	errs := make([]error, concurrency)

	batchStart := time.Now()

	for i := 0; i < concurrency; i++ {
		i := i

		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = gen.GetTileHeightmap(context.Background(), zoom, 1, 1, resolution)
		}()
	}

	wg.Wait()
	batchDuration := time.Since(batchStart)

	for i, err := range errs {
		if err != nil {
			t.Fatalf("call #%d failed: %s", i, err)
		}
	}

	maxExpected := singleDuration * 5
	if maxExpected < 50*time.Millisecond {
		maxExpected = 50 * time.Millisecond
	}

	if batchDuration > maxExpected {
		t.Errorf("%d concurrent identical requests took %s (one generation took %s); expected "+
			"roughly one generation's worth of time (up to %s) if the requests were deduplicated",
			concurrency, batchDuration, singleDuration, maxExpected)
	}
}

// TestGetTileHeightmapDifferentTilesRunConcurrently proves the per-tile lock
// added for deduplication is scoped per key, not a single global lock that
// would serialize unrelated tiles too.
func TestGetTileHeightmapDifferentTilesRunConcurrently(t *testing.T) {
	t.Parallel()

	h, err := hgt.OpenDataDir(demDatasetDir, nil)
	if err != nil {
		t.Fatalf("cannot open DEM dir. Cause: %s", err)
	}
	defer h.Close()

	const zoom = 10
	const resolution = 256

	baseline := Generator{ElevationDataset: h, Dir: t.TempDir()}

	start := time.Now()
	if _, err := baseline.GetTileHeightmap(context.Background(), zoom, 0, 0, resolution); err != nil {
		t.Fatalf("unexpected error generating baseline tile: %s", err)
	}
	singleDuration := time.Since(start)

	gen := Generator{ElevationDataset: h, Dir: t.TempDir()}

	const concurrency = 8
	var wg sync.WaitGroup
	errs := make([]error, concurrency)

	batchStart := time.Now()

	for i := 0; i < concurrency; i++ {
		i := i

		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = gen.GetTileHeightmap(context.Background(), zoom, i+1, i+1, resolution)
		}()
	}

	wg.Wait()
	batchDuration := time.Since(batchStart)

	for i, err := range errs {
		if err != nil {
			t.Fatalf("call #%d failed: %s", i, err)
		}
	}

	maxExpected := singleDuration * 5
	if maxExpected < 50*time.Millisecond {
		maxExpected = 50 * time.Millisecond
	}

	if batchDuration > maxExpected {
		t.Errorf("%d concurrent requests for DIFFERENT tiles took %s (one generation took %s); "+
			"expected them to run in parallel (up to %s), not be serialized by a shared lock",
			concurrency, batchDuration, singleDuration, maxExpected)
	}
}

// TestGetTileHeightmapRetriesAfterFailedGeneration is a regression test for
// the per-tile lock potentially being left held after a failed generation: a
// second request for the same tile, after a first one failed, must still be
// able to acquire the lock and try again.
func TestGetTileHeightmapRetriesAfterFailedGeneration(t *testing.T) {
	t.Parallel()

	h, err := hgt.OpenDataDir(demDatasetDir, nil)
	if err != nil {
		t.Fatalf("cannot open DEM dir. Cause: %s", err)
	}
	defer h.Close()

	brokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "downloader down", http.StatusInternalServerError)
	}))
	defer brokenServer.Close()

	gen := &Generator{
		ElevationDataset: h,
		Dir:              t.TempDir(),
		SrtmDownloader: &srtm.Downloader{
			BasePath:   brokenServer.URL,
			Dir:        t.TempDir(),
			Api:        &srtm.EarthdataApi{BaseUrl: brokenServer.URL, HttpClient: http.DefaultClient},
			HttpClient: http.DefaultClient,
		},
	}

	const zoom, x, y, resolution = 10, 2, 2, 256

	if _, err := gen.GetTileHeightmap(context.Background(), zoom, x, y, resolution); err == nil {
		t.Fatal("expected the first call to fail against a broken SRTM downloader, got success")
	}

	gen.SrtmDownloader = nil // second attempt falls back to the local DEM dataset only

	done := make(chan error, 1)

	go func() {
		_, err := gen.GetTileHeightmap(context.Background(), zoom, x, y, resolution)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second call for the same tile failed: %s", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second call for the same tile did not return: the per-tile lock is likely still " +
			"held from the first call's failure")
	}
}
