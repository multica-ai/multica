package imagemetadata

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestDimensions(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 80, 40))
	for name, encode := range map[string]func(*bytes.Buffer) error{
		"png":  func(b *bytes.Buffer) error { return png.Encode(b, img) },
		"jpeg": func(b *bytes.Buffer) error { return jpeg.Encode(b, img, nil) },
		"gif": func(b *bytes.Buffer) error {
			return gif.Encode(b, image.NewPaletted(img.Rect, color.Palette{color.White}), nil)
		},
	} {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			if err := encode(&b); err != nil {
				t.Fatal(err)
			}
			if size, ok := Dimensions(b.Bytes()); !ok || size != (Size{80, 40}) {
				t.Fatalf("Dimensions = %+v, %v", size, ok)
			}
		})
	}
	for _, data := range [][]byte{nil, []byte("not an image"), []byte("\x89PNG\r\n\x1a\n")} {
		if size, ok := Dimensions(data); ok || size != (Size{}) {
			t.Fatalf("invalid file returned %+v, %v", size, ok)
		}
	}
}

func TestDimensionsReadsHeaderWithoutPixelDecode(t *testing.T) {
	// A valid IHDR with no pixel data is sufficient, even for huge images.
	header := make([]byte, 13)
	binary.BigEndian.PutUint32(header, 100000)
	binary.BigEndian.PutUint32(header[4:], 50000)
	header[8], header[9] = 8, 2
	chunk := append([]byte("IHDR"), header...)
	data := append([]byte("\x89PNG\r\n\x1a\n"), 0, 0, 0, 13)
	data = append(data, chunk...)
	data = binary.BigEndian.AppendUint32(data, crc32.ChecksumIEEE(chunk))
	if _, err := png.Decode(bytes.NewReader(data)); err == nil {
		t.Fatal("fixture must not contain decodable pixels")
	}
	if size, ok := Dimensions(data); !ok || size != (Size{100000, 50000}) {
		t.Fatalf("Dimensions = %+v, %v", size, ok)
	}
}

func TestDimensionsJPEGOrientation(t *testing.T) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 80, 40)), nil); err != nil {
		t.Fatal(err)
	}
	for _, order := range []binary.ByteOrder{binary.BigEndian, binary.LittleEndian} {
		for orientation := uint16(1); orientation <= 8; orientation++ {
			tiff := make([]byte, 26)
			copy(tiff, "MM")
			if order == binary.LittleEndian {
				copy(tiff, "II")
			}
			order.PutUint16(tiff[2:], 42)
			order.PutUint32(tiff[4:], 8)
			order.PutUint16(tiff[8:], 1)
			order.PutUint16(tiff[10:], 0x0112)
			order.PutUint16(tiff[12:], 3)
			order.PutUint32(tiff[14:], 1)
			order.PutUint16(tiff[18:], orientation)
			payload := append([]byte("Exif\x00\x00"), tiff...)
			data := append([]byte{0xff, 0xd8, 0xff, 0xe1}, byte((len(payload)+2)>>8), byte(len(payload)+2))
			data = append(data, payload...)
			data = append(data, b.Bytes()[2:]...)
			want := Size{80, 40}
			if orientation >= 5 {
				want = Size{40, 80}
			}
			if size, ok := Dimensions(data); !ok || size != want {
				t.Fatalf("orientation %d: got %+v, %v; want %+v", orientation, size, ok, want)
			}
		}
	}
}

func TestMalformedEXIFFallsBack(t *testing.T) {
	for _, data := range [][]byte{
		nil, []byte("Exif"),
		[]byte("MM\x00\x2a\xff\xff\xff\xff"),
		[]byte("II\x2a\x00\x08\x00\x00\x00\xff\xff"),
	} {
		if got := tiffOrientation(data); got != 1 {
			t.Fatalf("orientation = %d", got)
		}
	}
}
