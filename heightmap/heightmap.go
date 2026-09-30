package heightmap

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

// Approximate meters per degree of latitude, used to derive GeoTIFF pixel sizes
const metersPerDegreeLat = 111320.0

// Azimuth angle pointing to the east
const eastAzimuth = 90

// NoElevationData marks a Point.Elevation whose value could not be read (missing
// DEM file, coordinate outside coverage, decode error, etc). It reuses the void
// value SRTM HGT files already use for "no data" so it can never be confused
// with a valid elevation, including sea level (0).
const NoElevationData int16 = -32768

// Maximum zoom level accepted by GenerateAllTilesInZoomLevel. At zoom z the
// number of tiles grows as 4^z:
//
//	z=0: 1              z=5: 1,024          z=9:  262,144
//	z=1: 4               z=6: 4,096         z=10: 1,048,576
//	z=2: 16              z=7: 16,384        z=11: 4,194,304
//	z=3: 64              z=8: 65,536        z=12: 16,777,216
//	z=4: 256
//
// z=12 (16,777,216 tiles) is treated as the practical ceiling for a single
// batch request. This cap is not the primary defense against a runaway batch
// any more, though - a caller can also cancel the request's context to stop
// an in-progress batch (see GenerateAllTilesInZoomLevel), which is the more
// useful control for a batch that turns out to be too large or too slow.
const maxBatchZoomLevel = 12

// Number of tiles generated concurrently by GenerateAllTilesInZoomLevel.
const maxConcurrentTileGeneration = 100

// maxSamplingDimension caps the native (pre-resize) sampling grid regardless of the
// requested resolution or the real-world size of `side` - defense in depth for a
// caller that requests no resolution, or an excessive one, over a very large area.
const maxSamplingDimension = 4096

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

	tileLocks      map[string]*tileLock
	tileLocksMutex sync.Mutex
}

// tileLock is a reference-counted mutex: refs tracks how many goroutines
// currently hold or are waiting for it, so the owning Generator can safely
// delete its map entry once the count drops back to zero - and only then,
// never while a waiter still holds a reference to it.
type tileLock struct {
	mu   sync.Mutex
	refs int
}

// acquireTileLock locks (creating it on first use) the mutex guarding a
// specific tile's generation and returns it for release via releaseTileLock.
// Concurrent requests for the same tile key block on this mutex instead of
// independently repeating the expensive generation work; requests for
// different keys are unaffected by each other. Unlike a plain
// map[string]*sync.Mutex, entries here don't accumulate forever: see
// releaseTileLock.
func (t *Generator) acquireTileLock(key string) *tileLock {
	t.tileLocksMutex.Lock()

	if t.tileLocks == nil {
		t.tileLocks = make(map[string]*tileLock)
	}

	lock, ok := t.tileLocks[key]

	if !ok {
		lock = &tileLock{}
		t.tileLocks[key] = lock
	}

	lock.refs++

	t.tileLocksMutex.Unlock()

	lock.mu.Lock()

	return lock
}

// releaseTileLock unlocks a tileLock obtained from acquireTileLock and removes
// its map entry once no goroutine still holds or is waiting for it (refs == 0),
// bounding tileLocks' size to the number of tiles currently in flight rather
// than every distinct tile ever requested over the process's lifetime.
func (t *Generator) releaseTileLock(key string, lock *tileLock) {
	lock.mu.Unlock()

	t.tileLocksMutex.Lock()

	lock.refs--

	if lock.refs == 0 {
		delete(t.tileLocks, key)
	}

	t.tileLocksMutex.Unlock()
}

type ResolutionConfig struct {
	Width                            int
	Height                           int
	ForceInterpolation               bool
	IgnoreWhenOriginalImageIsSmaller bool
	// Format is the output encoding. Zero value is PNG.
	Format Format
}

// TileGenerationResult summarizes the outcome of a GenerateAllTilesInZoomLevel
// batch. Total is the intended batch size (4^zoomLevel), known upfront.
// Succeeded and Failed only count tiles actually attempted, so
// Succeeded+Failed <= Total; when the batch was canceled partway through
// (Canceled == true), the gap between Total and Succeeded+Failed is the
// number of tiles that were never even dispatched.
type TileGenerationResult struct {
	Total     int  `json:"total"`
	Succeeded int  `json:"succeeded"`
	Failed    int  `json:"failed"`
	Canceled  bool `json:"canceled,omitempty"`
}

// GetTileHeightmap Generate a heightmap with the same size of an OpenStreetMap (OSM) tile.
// Concurrent requests for the same (x, y, z, resolution) tile are serialized so the
// expensive generation only happens once: the second request blocks on the per-tile
// lock, then finds the first request's result already on disk.
func (t *Generator) GetTileHeightmap(ctx context.Context, z, x, y, resolution int) ([]byte, error) {
	return t.GetTileHeightmapFormat(ctx, z, x, y, resolution, FormatPNG)
}

// GetTileHeightmapFormat is GetTileHeightmap with a selectable output format.
// Each format is cached separately on disk.
func (t *Generator) GetTileHeightmapFormat(ctx context.Context, z, x, y, resolution int, format Format) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	key := formatTilePathFormat(t.Dir, x, y, z, resolution, format)

	lock := t.acquireTileLock(key)
	defer t.releaseTileLock(key, lock)

	byteArray, err := t.getTileFromDiskFormat(x, y, z, resolution, format)

	if err == nil {
		return byteArray, nil
	}

	osmTile := gosm.NewTileWithXY(x, y, z)
	lat, lon := osmTile.Num2deg()

	tileSide := calculateTileSizeKm(z) * 1000

	byteArray, err = t.CreateHeightMapImage(ctx, lat, lon, tileSide,
		ResolutionConfig{Width: resolution, Height: resolution, ForceInterpolation: true,
			IgnoreWhenOriginalImageIsSmaller: false, Format: format})

	if err != nil {
		return []byte{}, err
	}

	// Saved synchronously (unlike the earlier fire-and-forget goroutine) so
	// that by the time the lock above is released, a waiting concurrent
	// request for the same tile reliably finds it cached on disk instead of
	// racing to regenerate it too. A save failure is still non-fatal: the
	// image was already generated successfully, so it's returned regardless.
	if _, err := t.saveTileFormat(x, y, z, resolution, format, byteArray); err != nil {
		log.Errorf("cannot save tile (%d, %d, %d) to disk. Cause: %s", x, y, z, err)
	}

	return byteArray, nil
}

// samplingStride picks the DEM sampling stride, in meters, for an area `side` meters
// wide. It never samples finer than the native heightDataResolution grid, and coarsens
// the stride so the native grid never needs more samples per axis than the larger of
// the requested Width/Height (or maxSamplingDimension, whichever is smaller) - avoiding
// O((side/heightDataResolution)^2) sampling cost and memory for real-world-sized
// low-zoom tiles that only need a small output image.
//
// Effective ground resolution: the returned stride *is* the effective ground
// sample distance (GSD) of the resulting image, in meters/pixel:
//
//	stride = max(heightDataResolution, side / max(Width, Height))
//
// Two regimes fall out of that formula:
//
//   - Small area, normal resolution (the common case: a CLI heightmap, the
//     /heightmap API endpoint, or a high-zoom OSM tile where `side` is on the
//     order of a few km). `side / max(Width, Height)` comes out below the
//     native 30m grid, so stride stays at 30m and every sample the DEM can
//     offer is used - no detail is discarded. See
//     TestSamplingStrideNativeResolutionPreservesDetail.
//   - Huge area, normal resolution (a real-world OSM tile at low zoom, where
//     `side` can be tens of thousands of km - the scenario that used to OOM,
//     see TestCreateHeightMapImageBoundedForLargeArea). Here the stride
//     coarsens well past 30m. This is not a quality regression: at that zoom,
//     each output pixel already represents many kilometers of real terrain,
//     so no combination of Width/Height could show finer relief than the
//     coarsened stride already captures - sampling any more finely would just
//     spend time and memory on data the image can never display.
//
// Only a caller that asks for no resolution at all (Width or Height <= 0)
// over a huge area is capped by maxSamplingDimension instead of a requested
// size; no current caller does this (every real caller - the API handlers,
// the CLI, and GetTileHeightmap - always supplies a concrete resolution), so
// this is a defensive fallback rather than a documented feature with its own
// quality guarantee.
func samplingStride(side float64, conf ResolutionConfig) int {
	stride := int(heightDataResolution)

	target := maxSamplingDimension
	if conf.Width > 0 && conf.Height > 0 {
		target = conf.Width
		if conf.Height > target {
			target = conf.Height
		}
		if target > maxSamplingDimension {
			target = maxSamplingDimension
		}
	}

	if s := int(side) / target; s > stride {
		stride = s
	}

	return stride
}

func (t *Generator) CreateHeightMapImage(ctx context.Context, lat, lon float64, side float64,
	conf ResolutionConfig) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	stride := samplingStride(side, conf)

	// step is the number of samples createHeightProfile will actually produce
	// along each axis: ceil(side/stride), matching its loop condition
	// `x < int(side)` (a half-open interval - side itself is excluded, see
	// createHeightProfile's doc comment). Floor division here used to
	// under-count whenever side wasn't an exact multiple of stride, and
	// image.RGBA.Set silently drops any sample landing outside an
	// under-sized buffer instead of erroring, so the overshoot sample(s)
	// were being discarded rather than rendered. The buffer is sized to
	// exactly the samples that will be written - no other adjustment.
	step := (int(side) + stride - 1) / stride

	upLeft := image.Point{}
	lowRight := image.Point{X: step, Y: step}

	imgRgba := image.NewRGBA(image.Rectangle{Min: upLeft, Max: lowRight})
	gradient, _ := colorgrad.NewGradient().Domain(0, 8865).Build()

	// Elevation formats keep the raw values (row-major, rows run south) instead
	// of the colored image. Cells never written stay NoElevationData.
	var elevations []float32

	if conf.Format.IsElevation() {
		elevations = make([]float32, step*step)
		for i := range elevations {
			elevations[i] = float32(NoElevationData)
		}
	}

	failedPoints, err := t.createHeightProfile(ctx, lat, lon, side, stride, imgRgba,
		func(point *Point, i interface{}, index int) error {
			if elevations != nil {
				if point.X < step && point.Y < step && point.Elevation != NoElevationData {
					elevations[point.X*step+point.Y] = float32(point.Elevation)
				}
				return nil
			}

			imgRgba.Set(point.Y, point.X, gradient.At(float64(point.Elevation)))
			return nil
		})

	log.Infof("Height profile created for coordinates (%f, %f)", lat, lon)

	if err != nil {
		return []byte{}, err
	}

	if failedPoints > 0 {
		log.Warnf("%d point(s) failed processing while creating the height profile for "+
			"coordinates (%f, %f)", failedPoints, lat, lon)
	}

	validResolution := conf.Width > 0 && conf.Height > 0

	needsResize := validResolution && (conf.ForceInterpolation ||
		(!conf.IgnoreWhenOriginalImageIsSmaller && (conf.Width < step || conf.Height < step)))

	if elevations != nil {
		outW, outH := step, step

		if needsResize {
			outW, outH = conf.Width, conf.Height
			elevations = resampleElevation(elevations, step, step, outW, outH)
		}

		return encodeElevation(elevations, outW, outH, conf.Format, geoReference(lat, lon, side, outW, outH))
	}

	var outputImg image.Image = imgRgba

	if needsResize {
		log.Infof("Resizing heightmap image for coordinates (%f, %f) to %dx%d",
			lat, lon, conf.Width, conf.Height)

		outputImg = resize.Resize(uint(conf.Width), uint(conf.Height), imgRgba, resize.Lanczos3)
	}

	outW, outH := outputImg.Bounds().Dx(), outputImg.Bounds().Dy()
	geo := geoReference(lat, lon, side, outW, outH)

	encoded, err := encodeImage(outputImg, conf.Format, geo)

	if err != nil {
		return []byte{}, err
	}

	return encoded, nil
}

func (t *Generator) GetPointsElevations(points []Point) []Point {
	for i, p := range points {
		if t.SrtmDownloader != nil {
			// Unrelated to any tile batch, so there's no request-scoped
			// context to propagate here.
			_, err := t.SrtmDownloader.DownloadDemFile(context.Background(), p.Lat, p.Lon)

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
// failure is logged, counted in the returned result, and does not abort the
// rest of the batch. The returned error is only set for a structural
// rejection (an invalid zoom level) that means no work was attempted at all;
// once the batch starts, its outcome - including a partial failure or an
// early stop from ctx being canceled - is fully described by the returned
// TileGenerationResult. Canceling ctx stops new tiles from being dispatched
// and, via GetTileHeightmap -> CreateHeightMapImage -> createHeightProfile,
// interrupts tiles already in progress too.
func (t *Generator) GenerateAllTilesInZoomLevel(ctx context.Context, zoomLevel int) (TileGenerationResult, error) {
	if zoomLevel < 0 || zoomLevel > maxBatchZoomLevel {
		return TileGenerationResult{}, fmt.Errorf(
			"zoom level %d is out of the allowed range [0, %d] for batch tile generation",
			zoomLevel, maxBatchZoomLevel)
	}

	numTiles := int(math.Exp2(float64(zoomLevel)))
	total := numTiles * numTiles

	sem := make(chan struct{}, maxConcurrentTileGeneration)
	var wg sync.WaitGroup

	var succeeded, failed int64

dispatch:
	for x := 0; x < numTiles; x++ {
		for y := 0; y < numTiles; y++ {
			if ctx.Err() != nil {
				break dispatch
			}

			x, y := x, y

			wg.Add(1)
			sem <- struct{}{}

			go func() {
				defer wg.Done()
				defer func() { <-sem }()

				start := time.Now()

				_, err := t.GetTileHeightmap(ctx, zoomLevel, x, y, 256)

				if err != nil {
					log.Warnf("cannot generate heightmap for tile (%d, %d, %d). Cause: %s",
						x, y, zoomLevel, err)
					atomic.AddInt64(&failed, 1)
					return
				}

				atomic.AddInt64(&succeeded, 1)

				log.Infof("Heightmap for tile (%d, %d, %d) generated. Took %s",
					x, y, zoomLevel, time.Since(start))
			}()
		}
	}

	wg.Wait()

	return TileGenerationResult{
		Total:     total,
		Succeeded: int(atomic.LoadInt64(&succeeded)),
		Failed:    int(atomic.LoadInt64(&failed)),
		Canceled:  ctx.Err() != nil,
	}, nil
}

// createHeightProfile samples the DEM over a lat/lon square and invokes
// processFunc once per sample. The sampled square is the half-open interval
// [0, side) in both axes - a sample is taken at offsets 0, stride, 2*stride,
// ... up to but excluding side itself (the loop condition is `x < int(side)`),
// so side is an exclusive geographic bound, not an inclusive one. The caller
// (CreateHeightMapImage) sizes its output buffer to match this exactly via
// ceil(side/stride); see its `step` comment.
//
// It returns the number of samples whose processFunc call returned an error
// (logged individually, but otherwise non-fatal - the profile keeps going) so
// callers can surface that a profile "succeeded" only partially, instead of it
// being silently discarded as it was before. It checks ctx before each sample
// and returns ctx.Err() early if canceled, so an in-progress profile stops
// promptly instead of running to completion after the caller has stopped
// waiting for it.
func (t *Generator) createHeightProfile(ctx context.Context, lat, lon float64, side float64, stride int,
	processFuncParam interface{}, processFunc heightProfileProcessFunc) (failedPoints int, err error) {
	i := 0

	side = math.Ceil(side)

	for x := 0; x < int(side); x = x + stride {
		var newLat, newLon float64
		geodesic.WGS84.Direct(lat, lon, southAzimuth, float64(x), &newLat, &newLon, nil)

		for y := 0; y < int(side); y = y + stride {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return failedPoints, ctxErr
			}

			var pLat, pLon float64
			geodesic.WGS84.Direct(newLat, newLon, eastAzimuth, float64(y), &pLat, &pLon, nil)

			if t.SrtmDownloader != nil {
				_, downloadErr := t.SrtmDownloader.DownloadDemFile(ctx, pLat, pLon)

				if downloadErr != nil {
					if errors.Is(downloadErr, srtm.ErrTileNotInsideSrtmCoverage) {
						continue
					}

					msg := "cannot download digital elevation model file for coordinate %f, %f. Cause: %s"
					log.Debugf(msg, pLat, pLon, downloadErr)
					return failedPoints, downloadErr
				}
			}

			e, _, elevationErr := t.ElevationDataset.ElevationAt(pLat, pLon)

			if elevationErr != nil {
				log.Debugf("cannot read elevation for coordinate %f, %f. Cause: %s", pLat, pLon, elevationErr)
				e = NoElevationData
			}

			point := &Point{x / stride, y / stride, pLat, pLon, e}
			procErr := processFunc(point, processFuncParam, i)

			if procErr != nil {
				log.Errorf("cannot process point (%d, %d). Cause: %s", point.X, point.Y, procErr)
				failedPoints++
			}

			i++
		}
	}

	return failedPoints, nil
}

// ErrTileNotCached is returned by getTileFromDisk when the requested tile has
// not been generated yet, as opposed to some other read failure.
var ErrTileNotCached = errors.New("tile is not cached")

// saveTile writes a tile to a temporary file in the destination directory and
// renames it into place. The rename is atomic, so a concurrent read of the
// same tile (getTileFromDisk) never observes a partially written file, and two
// concurrent writers of the same tile never corrupt each other's output.
func (t *Generator) saveTile(x int, y int, z, resolution int, bytes []byte) (string, error) {
	return t.saveTileFormat(x, y, z, resolution, FormatPNG, bytes)
}

func (t *Generator) saveTileFormat(x int, y int, z, resolution int, format Format, bytes []byte) (string, error) {
	dir := formatTileDirPath(t.Dir, x, z, resolution)
	err := os.MkdirAll(dir, os.ModePerm)

	if err != nil {
		return "", fmt.Errorf("cannot create directories to store tiles. Cause: %w", err)
	}

	path := fmt.Sprintf("%s/%d.%s", dir, y, format.Extension())

	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	log.Infof("Saving tile (%d, %d, %d) to disk", x, y, z)

	tmpFile, err := os.CreateTemp(dir, fmt.Sprintf(".%d.%s.tmp-*", y, format.Extension()))

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

func (t *Generator) getTileFromDisk(x, y, z, resolution int) ([]byte, error) {
	return t.getTileFromDiskFormat(x, y, z, resolution, FormatPNG)
}

func (t *Generator) getTileFromDiskFormat(x, y, z, resolution int, format Format) ([]byte, error) {
	path := formatTilePathFormat(t.Dir, x, y, z, resolution, format)

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
	return formatTilePathFormat(dir, x, y, z, resolution, FormatPNG)
}

func formatTilePathFormat(dir string, x, y, z, resolution int, format Format) string {
	dir = formatTileDirPath(dir, x, z, resolution)
	yStr := fmt.Sprintf("%d", y)

	return dir + filePathSep + yStr + "." + format.Extension()
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

// geoReference derives the WGS84 placement of a w*h image covering `side`
// meters south and east from the upper-left corner (lat, lon). Pixel sizes use
// an approximate meters-per-degree, scaled by latitude for longitude.
func geoReference(lat, lon, side float64, w, h int) *GeoReference {
	sideDegLat := side / metersPerDegreeLat
	sideDegLon := side / (metersPerDegreeLat * math.Max(math.Cos(lat*math.Pi/180), 1e-6))

	return &GeoReference{Lat: lat, Lon: lon,
		PixelSizeLat: sideDegLat / float64(h), PixelSizeLon: sideDegLon / float64(w)}
}
