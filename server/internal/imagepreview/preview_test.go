package imagepreview

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func solid(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func pngChunk(typ string, data []byte) []byte {
	out := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	out = append(out, typ...)
	out = append(out, data...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(append([]byte(typ), data...)))
}

// exifOrientationSegment is a minimal APP1 segment carrying one IFD0 entry.
func exifOrientationSegment(orientation uint16) []byte {
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08")
	tiff = binary.BigEndian.AppendUint16(tiff, 1)
	tiff = binary.BigEndian.AppendUint16(tiff, 0x0112)
	tiff = binary.BigEndian.AppendUint16(tiff, 3)
	tiff = binary.BigEndian.AppendUint32(tiff, 1)
	tiff = binary.BigEndian.AppendUint16(tiff, orientation)
	tiff = append(tiff, 0, 0, 0, 0, 0, 0)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1}
	seg = binary.BigEndian.AppendUint16(seg, uint16(len(payload)+2))
	return append(seg, payload...)
}

func decode(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("preview does not decode: %v", err)
	}
	return img
}

func TestPreviewKey(t *testing.T) {
	cases := map[string]string{
		"workspaces/w/a.png":  "workspaces/w/a.png.preview.png",
		"workspaces/w/a.PNG":  "workspaces/w/a.PNG.preview.png",
		"workspaces/w/a.webp": "workspaces/w/a.webp.preview.png",
		"workspaces/w/a.jpg":  "workspaces/w/a.jpg.preview.jpg",
		"workspaces/w/a.jpeg": "workspaces/w/a.jpeg.preview.jpg",
		"workspaces/w/a.gif":  "",
		"workspaces/w/a.pdf":  "",
		"workspaces/w/a":      "",
	}
	for key, want := range cases {
		if got := PreviewKey(key); got != want {
			t.Errorf("PreviewKey(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestProcessSmallImageKeepsOriginal(t *testing.T) {
	res, ok := Process(context.Background(), encodePNG(t, solid(800, 600, color.White)), "a.png")
	if !ok || res.Width != 800 || res.Height != 600 || res.Preview != nil {
		t.Fatalf("got ok=%v %dx%d preview=%d bytes", ok, res.Width, res.Height, len(res.Preview))
	}
}

func TestProcessDownscalesWideScreenshot(t *testing.T) {
	res, ok := Process(context.Background(), encodePNG(t, solid(3200, 2000, color.RGBA{10, 20, 30, 255})), "a.png")
	if !ok || res.Width != 3200 || res.Height != 2000 {
		t.Fatalf("got ok=%v %dx%d", ok, res.Width, res.Height)
	}
	if res.PreviewContentType != "image/png" {
		t.Fatalf("content type %q", res.PreviewContentType)
	}
	if b := decode(t, res.Preview).Bounds(); b.Dx() != 1600 || b.Dy() != 1000 {
		t.Fatalf("preview is %dx%d, want 1600x1000", b.Dx(), b.Dy())
	}
}

func TestProcessDropsPreviewThatIsNotSmaller(t *testing.T) {
	// Scattered black pixels on white take two values and compress well;
	// resampled, they turn into shades of gray that compress far worse.
	img := solid(3000, 400, color.White)
	seed := uint32(1)
	for y := 0; y < 400; y++ {
		for x := 0; x < 3000; x++ {
			seed = seed*1664525 + 1013904223
			if seed>>30 == 0 {
				img.Set(x, y, color.Black)
			}
		}
	}
	data := encodePNG(t, img)
	res, ok := Process(context.Background(), data, "a.png")
	if !ok || res.Width != 3000 || res.Height != 400 {
		t.Fatalf("got ok=%v %dx%d", ok, res.Width, res.Height)
	}
	if res.Preview != nil || res.PreviewContentType != "" {
		t.Fatalf("kept a %d-byte preview of a %d-byte original", len(res.Preview), len(data))
	}
}

func TestProcessBoundsVeryTallImages(t *testing.T) {
	res, _ := Process(context.Background(), encodePNG(t, solid(1000, 20000, color.White)), "a.png")
	b := decode(t, res.Preview).Bounds()
	if b.Dx()*b.Dy() > MaxPixels || b.Dx() >= 1000 {
		t.Fatalf("preview is %dx%d", b.Dx(), b.Dy())
	}
}

func TestProcessJPEGAppliesOrientation(t *testing.T) {
	// Stored landscape with a red left half; EXIF 6 displays it rotated 90°
	// clockwise, so the red half ends up on top.
	src := solid(4000, 2000, color.RGBA{0, 0, 255, 255})
	for y := 0; y < 2000; y++ {
		for x := 0; x < 2000; x++ {
			src.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	plain := encodeJPEG(t, src)
	data := append(append([]byte{0xFF, 0xD8}, exifOrientationSegment(6)...), plain[2:]...)

	res, ok := Process(context.Background(), data, "photo.jpg")
	if !ok || res.Width != 2000 || res.Height != 4000 {
		t.Fatalf("got ok=%v %dx%d, want 2000x4000", ok, res.Width, res.Height)
	}
	if res.PreviewContentType != "image/jpeg" {
		t.Fatalf("content type %q", res.PreviewContentType)
	}
	img := decode(t, res.Preview)
	b := img.Bounds()
	if b.Dx() != 1600 || b.Dy() != 3200 {
		t.Fatalf("preview is %dx%d, want 1600x3200", b.Dx(), b.Dy())
	}
	if r, _, bl, _ := img.At(800, 400).RGBA(); r>>8 < 200 || bl>>8 > 60 {
		t.Fatalf("top of preview is not red: r=%d b=%d", r>>8, bl>>8)
	}
	if r, _, bl, _ := img.At(800, 2800).RGBA(); bl>>8 < 200 || r>>8 > 60 {
		t.Fatalf("bottom of preview is not blue: r=%d b=%d", r>>8, bl>>8)
	}
	if jpegOrientation(res.Preview) != 1 {
		t.Fatal("preview must not carry the orientation it already applied")
	}
}

func TestProcessKeepsPNGColorProfile(t *testing.T) {
	plain := encodePNG(t, solid(3200, 2000, color.White))
	iccp := pngChunk("iCCP", append([]byte("Display P3\x00\x00"), 0x78, 0x9c, 0x03, 0x00, 0x00, 0x00, 0x00, 0x01))
	data := append(append(append([]byte{}, plain[:33]...), iccp...), plain[33:]...)

	res, _ := Process(context.Background(), data, "shot.png")
	if !bytes.Contains(res.Preview, iccp) {
		t.Fatal("preview dropped the iCCP chunk")
	}
	if !bytes.Equal(res.Preview[12:16], []byte("IHDR")) {
		t.Fatal("IHDR must stay the first chunk")
	}
}

func TestProcessKeepsJPEGColorProfile(t *testing.T) {
	plain := encodeJPEG(t, solid(3200, 2000, color.White))
	payload := append([]byte("ICC_PROFILE\x00\x01\x01"), bytes.Repeat([]byte{7}, 32)...)
	seg := binary.BigEndian.AppendUint16([]byte{0xFF, 0xE2}, uint16(len(payload)+2))
	seg = append(seg, payload...)
	data := append(append([]byte{0xFF, 0xD8}, seg...), plain[2:]...)

	res, _ := Process(context.Background(), data, "photo.jpg")
	if !bytes.Equal(jpegICCSegments(res.Preview), seg) {
		t.Fatal("preview dropped the ICC profile")
	}
	decode(t, res.Preview)
}

func TestProcessSkipsPreviewItCannotRepresent(t *testing.T) {
	big := solid(3200, 2000, color.White)

	t.Run("gif keeps its animation", func(t *testing.T) {
		var buf bytes.Buffer
		frame := image.NewPaletted(big.Rect, color.Palette{color.White, color.Black})
		if err := gif.Encode(&buf, frame, nil); err != nil {
			t.Fatal(err)
		}
		res, ok := Process(context.Background(), buf.Bytes(), "a.gif")
		if !ok || res.Width != 3200 || res.Preview != nil {
			t.Fatalf("got ok=%v width=%d preview=%d bytes", ok, res.Width, len(res.Preview))
		}
	})
	t.Run("animated png keeps its animation", func(t *testing.T) {
		plain := encodePNG(t, big)
		actl := pngChunk("acTL", []byte{0, 0, 0, 2, 0, 0, 0, 0})
		data := append(append(append([]byte{}, plain[:33]...), actl...), plain[33:]...)
		res, ok := Process(context.Background(), data, "a.png")
		if !ok || res.Width != 3200 || res.Preview != nil {
			t.Fatalf("got ok=%v width=%d preview=%d bytes", ok, res.Width, len(res.Preview))
		}
	})
	t.Run("png content under a jpeg key would lose its alpha", func(t *testing.T) {
		res, ok := Process(context.Background(), encodePNG(t, big), "a.jpg")
		if !ok || res.Width != 3200 || res.Preview != nil {
			t.Fatalf("got ok=%v width=%d preview=%d bytes", ok, res.Width, len(res.Preview))
		}
	})
	t.Run("no preview format for the key", func(t *testing.T) {
		res, ok := Process(context.Background(), encodePNG(t, big), "a")
		if !ok || res.Width != 3200 || res.Preview != nil {
			t.Fatalf("got ok=%v width=%d preview=%d bytes", ok, res.Width, len(res.Preview))
		}
	})
	t.Run("cancelled request still measures", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		for i := 0; i < cap(decodeSlots); i++ {
			decodeSlots <- struct{}{}
		}
		defer func() {
			for i := 0; i < cap(decodeSlots); i++ {
				<-decodeSlots
			}
		}()
		res, ok := Process(ctx, encodePNG(t, big), "a.png")
		if !ok || res.Width != 3200 || res.Preview != nil {
			t.Fatalf("got ok=%v width=%d preview=%d bytes", ok, res.Width, len(res.Preview))
		}
	})
}

func TestProcessRejectsNonImages(t *testing.T) {
	if _, ok := Process(context.Background(), []byte("%PDF-1.7 not an image"), "a.png"); ok {
		t.Fatal("non-image measured as an image")
	}
}

func TestOrientMapsEveryCorner(t *testing.T) {
	// 3x2 source with a distinct value per pixel; each orientation's expected
	// top-left / top-right / bottom-left source pixels follow the EXIF spec.
	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			src.Pix[src.PixOffset(x, y)] = uint8(y*3 + x)
		}
	}
	at := func(img *image.RGBA, x, y int) uint8 { return img.Pix[img.PixOffset(x, y)] }
	// [topLeft, topRight, bottomLeft] as source pixel ids.
	want := map[int][3]uint8{
		1: {0, 2, 3},
		2: {2, 0, 5},
		3: {5, 3, 2},
		4: {3, 5, 0},
		5: {0, 3, 2},
		6: {3, 0, 5},
		7: {5, 2, 3},
		8: {2, 5, 0},
	}
	for o, corners := range want {
		got := orient(src, o)
		w, h := got.Rect.Dx(), got.Rect.Dy()
		if o >= 5 && (w != 2 || h != 3) || o < 5 && (w != 3 || h != 2) {
			t.Fatalf("orientation %d: size %dx%d", o, w, h)
		}
		if g := [3]uint8{at(got, 0, 0), at(got, w-1, 0), at(got, 0, h-1)}; g != corners {
			t.Errorf("orientation %d: corners %v, want %v", o, g, corners)
		}
	}
}
