package heightmap

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"strings"
)

// Format is an output image encoding. The zero value is PNG, so callers that
// do not set a format keep the original behavior.
type Format string

const (
	FormatPNG     Format = ""
	FormatJPEG    Format = "jpg"
	FormatTIFF    Format = "tiff"
	FormatGeoTIFF Format = "geotiff"
	// FormatPNG16 is a 16-bit grayscale PNG holding elevation in whole meters
	// (1 unit = 1 m). Negative elevations clamp to 0, no-data becomes 0.
	FormatPNG16 Format = "png16"
	// FormatDEM is a single-band Float32 GeoTIFF holding elevation in meters,
	// with no-data set to NoElevationData (-32768).
	FormatDEM Format = "dem"
)

// IsElevation reports whether the format encodes elevation values instead of
// the colored gradient image.
func (f Format) IsElevation() bool {
	return f == FormatPNG16 || f == FormatDEM
}

const jpegQuality = 95

// ParseFormat maps a file extension (with or without a leading dot) or a
// format name to a Format. Returns false for unsupported values.
func ParseFormat(s string) (Format, bool) {
	switch strings.ToLower(strings.TrimPrefix(s, ".")) {
	case "png", "":
		return FormatPNG, true
	case "jpg", "jpeg":
		return FormatJPEG, true
	case "tif", "tiff":
		return FormatTIFF, true
	case "geotiff", "gtiff":
		return FormatGeoTIFF, true
	case "png16":
		return FormatPNG16, true
	case "dem", "dem.tif", "float32":
		return FormatDEM, true
	}

	return FormatPNG, false
}

// Extension returns the file extension, without dot.
func (f Format) Extension() string {
	switch f {
	case FormatJPEG:
		return "jpg"
	case FormatTIFF:
		return "tif"
	case FormatGeoTIFF:
		return "geotiff"
	case FormatPNG16:
		return "png16"
	case FormatDEM:
		return "dem.tif"
	}

	return "png"
}

// ContentType returns the HTTP media type.
func (f Format) ContentType() string {
	switch f {
	case FormatJPEG:
		return "image/jpeg"
	case FormatTIFF, FormatGeoTIFF, FormatDEM:
		return "image/tiff"
	}

	return "image/png"
}

// GeoReference places an image on the WGS84 (EPSG:4326) grid. Lat/Lon are the
// upper-left corner; PixelSizeLat/PixelSizeLon are degrees per pixel.
type GeoReference struct {
	Lat, Lon                   float64
	PixelSizeLat, PixelSizeLon float64
}

func encodeImage(img image.Image, f Format, geo *GeoReference) ([]byte, error) {
	var b bytes.Buffer

	switch f {
	case FormatPNG:
		if err := png.Encode(&b, img); err != nil {
			return nil, fmt.Errorf("cannot encode PNG image. Cause: %w", err)
		}
	case FormatJPEG:
		if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
			return nil, fmt.Errorf("cannot encode JPEG image. Cause: %w", err)
		}
	case FormatTIFF:
		return encodeTIFF(img, nil), nil
	case FormatGeoTIFF:
		return encodeTIFF(img, geo), nil
	default:
		return nil, fmt.Errorf("unsupported image format %q", string(f))
	}

	return b.Bytes(), nil
}

// TIFF tag ids and types used by the writer.
const (
	tagImageWidth      = 256
	tagImageLength     = 257
	tagBitsPerSample   = 258
	tagCompression     = 259
	tagPhotometric     = 262
	tagStripOffsets    = 273
	tagSamplesPerPixel = 277
	tagRowsPerStrip    = 278
	tagStripByteCounts = 279
	tagPlanarConfig    = 284
	tagExtraSamples    = 338
	tagSampleFormat    = 339

	tagModelPixelScale = 33550
	tagModelTiepoint   = 33922
	tagGeoKeyDirectory = 34735
	tagGDALNoData      = 42113

	typeASCII  = 2
	typeShort  = 3
	typeLong   = 4
	typeDouble = 12
)

type ifdEntry struct {
	tag, typ uint16
	count    uint32
	data     []byte // little-endian encoded values
}

func shorts(v ...uint16) []byte {
	b := make([]byte, 2*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint16(b[2*i:], x)
	}
	return b
}

func doubles(v ...float64) []byte {
	b := make([]byte, 8*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint64(b[8*i:], math.Float64bits(x))
	}
	return b
}

// tiffLayout describes the pixel data handed to writeTIFF.
type tiffLayout struct {
	width, height int
	pixels        []byte // little-endian, row-major, chunky
	bitsPerSample []uint16
	photometric   uint16 // 1 = BlackIsZero, 2 = RGB
	extraSamples  bool   // last sample is unassociated alpha
	sampleFormat  uint16 // 0 = omit (unsigned int), 3 = IEEE float
	noData        string // GDAL_NODATA value, empty to omit
}

// encodeTIFF writes an uncompressed 8-bit RGBA TIFF. When geo is not nil,
// GeoTIFF tags are added (see writeTIFF).
func encodeTIFF(img image.Image, geo *GeoReference) []byte {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(nrgba, nrgba.Bounds(), img, bounds.Min, draw.Src)

	// nrgba was just allocated, so its stride is exactly w*4.
	return writeTIFF(tiffLayout{width: w, height: h, pixels: nrgba.Pix,
		bitsPerSample: []uint16{8, 8, 8, 8}, photometric: 2, extraSamples: true}, geo)
}

// encodeElevationTIFF writes a single-band Float32 GeoTIFF from a row-major
// elevation grid in meters.
func encodeElevationTIFF(grid []float32, w, h int, geo *GeoReference) []byte {
	pixels := make([]byte, 4*len(grid))
	for i, v := range grid {
		binary.LittleEndian.PutUint32(pixels[4*i:], math.Float32bits(v))
	}

	return writeTIFF(tiffLayout{width: w, height: h, pixels: pixels, bitsPerSample: []uint16{32},
		photometric: 1, sampleFormat: 3, noData: fmt.Sprintf("%d", NoElevationData)}, geo)
}

// encodeElevationPNG16 writes a 16-bit grayscale PNG, 1 unit = 1 meter.
func encodeElevationPNG16(grid []float32, w, h int) ([]byte, error) {
	img := image.NewGray16(image.Rect(0, 0, w, h))

	for i, v := range grid {
		var u uint16
		if v > 0 { // also false for no-data (negative)
			u = uint16(math.Min(float64(v)+0.5, math.MaxUint16))
		}
		img.SetGray16(i%w, i/w, color.Gray16{Y: u})
	}

	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil, fmt.Errorf("cannot encode PNG image. Cause: %w", err)
	}

	return b.Bytes(), nil
}

// writeTIFF writes a little-endian, uncompressed, single-strip TIFF. When geo
// is not nil, GeoTIFF tags (EPSG:4326, pixel-is-area) are added so GIS
// software can place the image. IFD entries must be sorted by tag.
func writeTIFF(l tiffLayout, geo *GeoReference) []byte {
	long := func(v uint32) []byte {
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, v)
		return b
	}

	pixels := l.pixels
	samples := uint32(len(l.bitsPerSample))

	// Layout: header (8) | pixel data | IFD | out-of-line values.
	const headerSize = 8

	entries := []ifdEntry{
		{tagImageWidth, typeLong, 1, long(uint32(l.width))},
		{tagImageLength, typeLong, 1, long(uint32(l.height))},
		{tagBitsPerSample, typeShort, samples, shorts(l.bitsPerSample...)},
		{tagCompression, typeShort, 1, shorts(1)},
		{tagPhotometric, typeShort, 1, shorts(l.photometric)},
		{tagStripOffsets, typeLong, 1, long(headerSize)},
		{tagSamplesPerPixel, typeShort, 1, shorts(uint16(samples))},
		{tagRowsPerStrip, typeLong, 1, long(uint32(l.height))},
		{tagStripByteCounts, typeLong, 1, long(uint32(len(pixels)))},
		{tagPlanarConfig, typeShort, 1, shorts(1)},
	}

	if l.extraSamples {
		entries = append(entries, ifdEntry{tagExtraSamples, typeShort, 1, shorts(2)})
	}

	if l.sampleFormat != 0 {
		sf := make([]uint16, samples)
		for i := range sf {
			sf[i] = l.sampleFormat
		}
		entries = append(entries, ifdEntry{tagSampleFormat, typeShort, samples, shorts(sf...)})
	}

	if geo != nil {
		entries = append(entries,
			ifdEntry{tagModelPixelScale, typeDouble, 3, doubles(geo.PixelSizeLon, geo.PixelSizeLat, 0)},
			ifdEntry{tagModelTiepoint, typeDouble, 6, doubles(0, 0, 0, geo.Lon, geo.Lat, 0)},
			// Header(1,1,0,3 keys); GTModelType=Geographic(2), GTRasterType=PixelIsArea(1),
			// GeographicType=EPSG:4326.
			ifdEntry{tagGeoKeyDirectory, typeShort, 16, shorts(
				1, 1, 0, 3,
				1024, 0, 1, 2,
				1025, 0, 1, 1,
				2048, 0, 1, 4326,
			)},
		)
	}

	if l.noData != "" {
		entries = append(entries, ifdEntry{tagGDALNoData, typeASCII, uint32(len(l.noData) + 1),
			append([]byte(l.noData), 0)})
	}

	pixelEnd := headerSize + len(pixels)
	pad := pixelEnd % 2 // IFD must start on a word boundary
	ifdOffset := pixelEnd + pad
	ifdSize := 2 + 12*len(entries) + 4
	extraOffset := ifdOffset + ifdSize

	var ifd, extra bytes.Buffer

	binary.Write(&ifd, binary.LittleEndian, uint16(len(entries)))

	for _, e := range entries {
		binary.Write(&ifd, binary.LittleEndian, e.tag)
		binary.Write(&ifd, binary.LittleEndian, e.typ)
		binary.Write(&ifd, binary.LittleEndian, e.count)

		if len(e.data) <= 4 {
			v := make([]byte, 4)
			copy(v, e.data)
			ifd.Write(v)
			continue
		}

		binary.Write(&ifd, binary.LittleEndian, uint32(extraOffset+extra.Len()))
		extra.Write(e.data)
		if extra.Len()%2 == 1 { // keep values word-aligned
			extra.WriteByte(0)
		}
	}

	binary.Write(&ifd, binary.LittleEndian, uint32(0)) // no next IFD

	var out bytes.Buffer
	out.Grow(extraOffset + extra.Len())
	out.WriteString("II")
	binary.Write(&out, binary.LittleEndian, uint16(42))
	binary.Write(&out, binary.LittleEndian, uint32(ifdOffset))
	out.Write(pixels)
	out.Write(make([]byte, pad))
	out.Write(ifd.Bytes())
	out.Write(extra.Bytes())

	return out.Bytes()
}

// resampleElevation resizes a row-major elevation grid (meters, no-data =
// NoElevationData) to w*h. Downsampling averages the valid source cells under
// each output cell; upsampling interpolates bilinearly between valid
// neighbours. Output cells with no valid input stay NoElevationData, so
// no-data never bleeds into real elevations.
func resampleElevation(src []float32, sw, sh, w, h int) []float32 {
	dst := make([]float32, w*h)
	nodata := float32(NoElevationData)
	fx, fy := float64(sw)/float64(w), float64(sh)/float64(h)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var sum, weight float64

			if fx > 1 || fy > 1 { // area average
				x0, x1 := int(float64(x)*fx), int(math.Ceil(float64(x+1)*fx))
				y0, y1 := int(float64(y)*fy), int(math.Ceil(float64(y+1)*fy))
				for sy := y0; sy < y1 && sy < sh; sy++ {
					for sx := x0; sx < x1 && sx < sw; sx++ {
						if v := src[sy*sw+sx]; v != nodata {
							sum += float64(v)
							weight++
						}
					}
				}
			} else { // bilinear around the source position of the pixel center
				cx, cy := (float64(x)+0.5)*fx-0.5, (float64(y)+0.5)*fy-0.5
				ix, iy := int(math.Floor(cx)), int(math.Floor(cy))
				tx, ty := cx-float64(ix), cy-float64(iy)
				for dy := 0; dy <= 1; dy++ {
					for dx := 0; dx <= 1; dx++ {
						sx, sy := clampInt(ix+dx, 0, sw-1), clampInt(iy+dy, 0, sh-1)
						wgt := (1 - math.Abs(float64(dx)-tx)) * (1 - math.Abs(float64(dy)-ty))
						if v := src[sy*sw+sx]; v != nodata && wgt > 0 {
							sum += float64(v) * wgt
							weight += wgt
						}
					}
				}
			}

			if weight == 0 {
				dst[y*w+x] = nodata
			} else {
				dst[y*w+x] = float32(sum / weight)
			}
		}
	}

	return dst
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// encodeElevation encodes an elevation grid in an elevation Format.
func encodeElevation(grid []float32, w, h int, f Format, geo *GeoReference) ([]byte, error) {
	switch f {
	case FormatPNG16:
		return encodeElevationPNG16(grid, w, h)
	case FormatDEM:
		return encodeElevationTIFF(grid, w, h, geo), nil
	}

	return nil, fmt.Errorf("unsupported elevation format %q", string(f))
}
