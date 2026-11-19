package thumbnail

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestSize(t *testing.T) {
	for _, tc := range []struct{ w, h, wantW, wantH int }{
		{5472, 3072, 855, 480},
		{3072, 5472, 480, 855},
		{4000, 4000, 480, 480},
		{10000, 1000, 1280, 128},
		{400, 300, 400, 300},
		{1, 1, 1, 1},
	} {
		if w, h := Size(tc.w, tc.h); w != tc.wantW || h != tc.wantH {
			t.Errorf("Size(%d, %d) = %dx%d, want %dx%d", tc.w, tc.h, w, h, tc.wantW, tc.wantH)
		}
	}
}

func TestGenerateScalesLargeJPEG(t *testing.T) {
	src := encodeJPEG(t, image.NewRGBA(image.Rect(0, 0, 5472, 3072)))
	got, err := Generate(src)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContentType != "image/jpeg" {
		t.Fatalf("content type = %q", got.ContentType)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(got.Data))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 855 || cfg.Height != 480 {
		t.Fatalf("thumbnail = %dx%d", cfg.Width, cfg.Height)
	}
	if len(got.Data) > 100<<10 {
		t.Fatalf("thumbnail is %d bytes", len(got.Data))
	}
}

func TestGenerateKeepsTransparencyAsPNG(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 1200, 900))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	got, err := Generate(encodePNG(t, img))
	if err != nil {
		t.Fatal(err)
	}
	if got.ContentType != "image/png" {
		t.Fatalf("content type = %q", got.ContentType)
	}
	decoded, err := png.Decode(bytes.NewReader(got.Data))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := decoded.At(0, 0).RGBA(); a == 0xffff {
		t.Fatal("alpha was flattened")
	}
}

func TestGenerateEncodesOpaquePNGAsJPEG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1200, 900))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	got, err := Generate(encodePNG(t, img))
	if err != nil {
		t.Fatal(err)
	}
	if got.ContentType != "image/jpeg" {
		t.Fatalf("content type = %q", got.ContentType)
	}
}

func TestGenerateAppliesJPEGOrientation(t *testing.T) {
	// The left half is red. EXIF orientation 6 rotates the image 90° clockwise
	// for display, which moves that half to the top.
	img := image.NewRGBA(image.Rect(0, 0, 1600, 1000))
	for y := 0; y < 1000; y++ {
		for x := 0; x < 1600; x++ {
			c := color.RGBA{0, 0, 255, 255}
			if x < 800 {
				c = color.RGBA{255, 0, 0, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	got, err := Generate(withJPEGOrientation(encodeJPEG(t, img), 6))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(got.Data))
	if err != nil {
		t.Fatal(err)
	}
	b := decoded.Bounds()
	if b.Dx() != 480 || b.Dy() != 768 {
		t.Fatalf("rotated thumbnail = %dx%d", b.Dx(), b.Dy())
	}
	if r, _, bl, _ := decoded.At(b.Dx()/2, b.Dy()/8).RGBA(); r < bl {
		t.Fatal("top of the rotated thumbnail is not the original's left half")
	}
	if r, _, bl, _ := decoded.At(b.Dx()/2, b.Dy()*7/8).RGBA(); r > bl {
		t.Fatal("bottom of the rotated thumbnail is not the original's right half")
	}
}

func TestOrientMapsEveryOrientation(t *testing.T) {
	// A 2x1 source: left pixel A, right pixel B. Each EXIF orientation places A
	// at a known spot of the displayed image.
	a, b := color.RGBA{1, 0, 0, 255}, color.RGBA{2, 0, 0, 255}
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.SetRGBA(0, 0, a)
	src.SetRGBA(1, 0, b)
	for orientation, wantA := range map[int]image.Point{
		1: {0, 0}, 2: {1, 0}, 3: {1, 0}, 4: {0, 0},
		5: {0, 0}, 6: {0, 0}, 7: {0, 1}, 8: {0, 1},
	} {
		got := orient(src, orientation)
		if got.RGBAAt(wantA.X, wantA.Y) != a {
			t.Errorf("orientation %d: A not at %v", orientation, wantA)
		}
	}
}

func TestGenerateRejectsUnsupportedSources(t *testing.T) {
	var gifData bytes.Buffer
	if err := gif.Encode(&gifData, image.NewPaletted(image.Rect(0, 0, 10, 10), color.Palette{color.White}), nil); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"empty":     nil,
		"not image": []byte("hello"),
		"gif":       gifData.Bytes(),
		"too large": pngHeaderOnly(10000, 5000),
	} {
		if _, err := Generate(data); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: err = %v, want ErrUnsupported", name, err)
		}
	}
}

func BenchmarkGenerate5472x3072JPEG(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 5472, 3072))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 31)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := Generate(buf.Bytes()); err != nil {
			b.Fatal(err)
		}
	}
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// withJPEGOrientation inserts an EXIF APP1 segment carrying orientation right
// after the JPEG's SOI marker.
func withJPEGOrientation(data []byte, orientation uint16) []byte {
	tiff := make([]byte, 26)
	copy(tiff, "MM")
	binary.BigEndian.PutUint16(tiff[2:], 42)
	binary.BigEndian.PutUint32(tiff[4:], 8)
	binary.BigEndian.PutUint16(tiff[8:], 1)
	binary.BigEndian.PutUint16(tiff[10:], 0x0112)
	binary.BigEndian.PutUint16(tiff[12:], 3)
	binary.BigEndian.PutUint32(tiff[14:], 1)
	binary.BigEndian.PutUint16(tiff[18:], orientation)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	out := append([]byte{0xff, 0xd8, 0xff, 0xe1}, byte((len(payload)+2)>>8), byte(len(payload)+2))
	out = append(out, payload...)
	return append(out, data[2:]...)
}

// pngHeaderOnly is a PNG whose IHDR declares w x h with no pixel data.
func pngHeaderOnly(w, h uint32) []byte {
	header := make([]byte, 13)
	binary.BigEndian.PutUint32(header, w)
	binary.BigEndian.PutUint32(header[4:], h)
	header[8], header[9] = 8, 2
	chunk := append([]byte("IHDR"), header...)
	data := append([]byte("\x89PNG\r\n\x1a\n"), 0, 0, 0, 13)
	data = append(data, chunk...)
	return binary.BigEndian.AppendUint32(data, crc32.ChecksumIEEE(chunk))
}
