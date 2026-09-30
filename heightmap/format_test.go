package heightmap

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"testing"
)

func testImage() image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x * 50), G: uint8(y * 100), B: 7, A: 255})
		}
	}
	return img
}

func TestParseFormat(t *testing.T) {
	for in, want := range map[string]Format{"png": FormatPNG, ".PNG": FormatPNG, "": FormatPNG,
		"jpg": FormatJPEG, "jpeg": FormatJPEG, "tif": FormatTIFF, "TIFF": FormatTIFF, "geotiff": FormatGeoTIFF} {
		got, ok := ParseFormat(in)
		if !ok || got != want {
			t.Errorf("ParseFormat(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}

	if _, ok := ParseFormat("bmp"); ok {
		t.Error("bmp must be unsupported")
	}
}

func TestEncodeImageDecodable(t *testing.T) {
	img := testImage()

	b, err := encodeImage(img, FormatPNG, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(b)); err != nil {
		t.Errorf("png: %s", err)
	}

	b, err = encodeImage(img, FormatJPEG, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(b)); err != nil {
		t.Errorf("jpeg: %s", err)
	}
}

// readIFD parses the first IFD of a little-endian TIFF into tag -> raw value bytes.
func readIFD(t *testing.T, b []byte) map[uint16][]byte {
	t.Helper()

	if string(b[:2]) != "II" || binary.LittleEndian.Uint16(b[2:]) != 42 {
		t.Fatal("bad TIFF header")
	}

	off := int(binary.LittleEndian.Uint32(b[4:]))
	n := int(binary.LittleEndian.Uint16(b[off:]))
	sizes := map[uint16]int{2: 1, 3: 2, 4: 4, 12: 8}
	out := map[uint16][]byte{}

	for i := 0; i < n; i++ {
		e := b[off+2+12*i:]
		tag := binary.LittleEndian.Uint16(e)
		size := sizes[binary.LittleEndian.Uint16(e[2:])] * int(binary.LittleEndian.Uint32(e[4:]))
		if size <= 4 {
			out[tag] = e[8 : 8+size]
		} else {
			p := int(binary.LittleEndian.Uint32(e[8:]))
			out[tag] = b[p : p+size]
		}
	}

	return out
}

func TestEncodeTIFF(t *testing.T) {
	b, err := encodeImage(testImage(), FormatTIFF, nil)
	if err != nil {
		t.Fatal(err)
	}

	tags := readIFD(t, b)

	if binary.LittleEndian.Uint32(tags[tagImageWidth]) != 3 || binary.LittleEndian.Uint32(tags[tagImageLength]) != 2 {
		t.Error("wrong dimensions")
	}
	if _, ok := tags[tagModelTiepoint]; ok {
		t.Error("plain TIFF must not carry geo tags")
	}

	start := int(binary.LittleEndian.Uint32(tags[tagStripOffsets]))
	if got := b[start : start+4]; !bytes.Equal(got, []byte{0, 0, 7, 255}) {
		t.Errorf("first pixel = %v", got)
	}
	if n := binary.LittleEndian.Uint32(tags[tagStripByteCounts]); n != 3*2*4 {
		t.Errorf("strip byte count = %d", n)
	}
}

func TestEncodeGeoTIFF(t *testing.T) {
	geo := &GeoReference{Lat: 27.5, Lon: 86.5, PixelSizeLat: 0.001, PixelSizeLon: 0.002}

	b, err := encodeImage(testImage(), FormatGeoTIFF, geo)
	if err != nil {
		t.Fatal(err)
	}

	tags := readIFD(t, b)
	f := func(raw []byte, i int) float64 {
		return math.Float64frombits(binary.LittleEndian.Uint64(raw[8*i:]))
	}

	scale, tie := tags[tagModelPixelScale], tags[tagModelTiepoint]
	if f(scale, 0) != 0.002 || f(scale, 1) != 0.001 {
		t.Errorf("bad pixel scale")
	}
	if f(tie, 3) != 86.5 || f(tie, 4) != 27.5 {
		t.Errorf("bad tiepoint")
	}

	keys := tags[tagGeoKeyDirectory]
	if len(keys) != 32 || binary.LittleEndian.Uint16(keys[30:]) != 4326 {
		t.Errorf("bad GeoKeyDirectory")
	}
}

func TestTilesCachedPerFormat(t *testing.T) {
	dir := t.TempDir()
	g := &Generator{Dir: dir}

	if _, err := g.saveTileFormat(1, 1, 1, 256, FormatGeoTIFF, []byte{1}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.getTileFromDiskFormat(1, 1, 1, 256, FormatPNG); err != ErrTileNotCached {
		t.Errorf("png must not see geotiff tile, got %v", err)
	}
	if b, err := g.getTileFromDiskFormat(1, 1, 1, 256, FormatGeoTIFF); err != nil || len(b) != 1 {
		t.Errorf("geotiff tile not found: %v", err)
	}
}

func TestElevationFormats(t *testing.T) {
	nd := float32(NoElevationData)
	grid := []float32{100, 200, 300, nd, 8848.4, -50}

	b, err := encodeElevation(grid, 3, 2, FormatPNG16, nil)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	g16, ok := img.(*image.Gray16)
	if !ok {
		t.Fatalf("want Gray16, got %T", img)
	}
	for i, want := range []uint16{100, 200, 300, 0, 8848, 0} {
		if got := g16.Gray16At(i%3, i/3).Y; got != want {
			t.Errorf("png16 pixel %d = %d, want %d", i, got, want)
		}
	}

	geo := &GeoReference{Lat: 1, Lon: 2, PixelSizeLat: .1, PixelSizeLon: .1}
	b, err = encodeElevation(grid, 3, 2, FormatDEM, geo)
	if err != nil {
		t.Fatal(err)
	}
	tags := readIFD(t, b)
	if binary.LittleEndian.Uint16(tags[tagBitsPerSample]) != 32 || binary.LittleEndian.Uint16(tags[tagSampleFormat]) != 3 {
		t.Error("want 32-bit float samples")
	}
	if string(tags[tagGDALNoData]) != "-32768\x00" {
		t.Errorf("nodata tag = %q", tags[tagGDALNoData])
	}
	start := int(binary.LittleEndian.Uint32(tags[tagStripOffsets]))
	if v := math.Float32frombits(binary.LittleEndian.Uint32(b[start+16:])); v != 8848.4 {
		t.Errorf("pixel 4 = %v", v)
	}
}

func TestResampleElevationNoDataDoesNotBleed(t *testing.T) {
	nd := float32(NoElevationData)
	src := []float32{
		10, 10, nd, nd,
		10, 10, nd, nd,
		nd, nd, nd, nd,
		nd, nd, nd, nd,
	}

	down := resampleElevation(src, 4, 4, 2, 2)
	if down[0] != 10 || down[1] != nd || down[3] != nd {
		t.Errorf("downsample = %v", down)
	}

	up := resampleElevation([]float32{10, 20, 30, 40}, 2, 2, 4, 4)
	if up[0] != 10 || up[15] != 40 {
		t.Errorf("upsample corners = %v %v", up[0], up[15])
	}
	for _, v := range up {
		if v < 10 || v > 40 {
			t.Fatalf("interpolated value %v out of range", v)
		}
	}
}
