package imagepreview

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"testing"
)

func TestDimensions(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 20))
	for _, format := range []string{"png", "jpeg", "gif"} {
		t.Run(format, func(t *testing.T) {
			var data bytes.Buffer
			var err error
			switch format {
			case "png":
				err = png.Encode(&data, img)
			case "jpeg":
				err = jpeg.Encode(&data, img, nil)
			case "gif":
				err = gif.Encode(&data, img, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			w, h := Dimensions(bytes.NewReader(data.Bytes()))
			if w != 32 || h != 20 {
				t.Fatalf("dimensions = %dx%d", w, h)
			}
		})
	}
}

// A lossless WebP header is sufficient: no pixel stream is needed to measure it.
func TestDimensionsWebPHeader(t *testing.T) {
	data := append([]byte("RIFF"), 18, 0, 0, 0)
	data = append(data, []byte("WEBPVP8L")...)
	data = append(data, 5, 0, 0, 0, 0x2f)
	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], uint32(31|(19<<14)))
	data = append(data, size[:]...)
	data = append(data, 0)
	w, h := Dimensions(bytes.NewReader(data))
	if w != 32 || h != 20 {
		t.Fatalf("dimensions = %dx%d", w, h)
	}
}

func TestDimensionsJPEGOrientation(t *testing.T) {
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 32, 20)), nil); err != nil {
		t.Fatal(err)
	}
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for orientation := 1; orientation <= 8; orientation++ {
			payload := make([]byte, 32)
			copy(payload, "Exif\x00\x00")
			tiff := payload[6:]
			if order == binary.LittleEndian {
				copy(tiff, "II")
			} else {
				copy(tiff, "MM")
			}
			order.PutUint16(tiff[2:], 42)
			order.PutUint32(tiff[4:], 8)
			order.PutUint16(tiff[8:], 1)
			order.PutUint16(tiff[10:], 0x112)
			order.PutUint16(tiff[12:], 3)
			order.PutUint32(tiff[14:], 1)
			order.PutUint16(tiff[18:], uint16(orientation))
			data := append([]byte{0xff, 0xd8, 0xff, 0xe1, 0, 34}, payload...)
			data = append(data, jpg.Bytes()[2:]...)
			w, h := Dimensions(bytes.NewReader(data))
			wantW, wantH := 32, 20
			if orientation >= 5 {
				wantW, wantH = wantH, wantW
			}
			if w != wantW || h != wantH {
				t.Fatalf("%v orientation %d: %dx%d", order, orientation, w, h)
			}
		}
	}
}

type countedReader struct {
	*bytes.Reader
	read int
}

func (r *countedReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += n
	return n, err
}

func TestDimensionsReadHeadersOnly(t *testing.T) {
	var pngFile bytes.Buffer
	if err := png.Encode(&pngFile, image.NewRGBA(image.Rect(0, 0, 32, 20))); err != nil {
		t.Fatal(err)
	}
	// PNG DecodeConfig needs only IHDR, even when the uploaded object is 100 MiB.
	data := make([]byte, 100<<20)
	copy(data, pngFile.Bytes())
	r := &countedReader{Reader: bytes.NewReader(data)}
	w, h := Dimensions(r)
	if w != 32 || h != 20 || r.read > 4096 {
		t.Fatalf("%dx%d, read %d bytes", w, h, r.read)
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	var prefix [8]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil || !bytes.Equal(prefix[:], data[:8]) {
		t.Fatal("rewind lost bytes")
	}
}

func TestDimensionsInvalidAndBounded(t *testing.T) {
	// Repeated JPEG APP segments never reach a frame header. Stop at the cap.
	data := []byte{0xff, 0xd8}
	segment := append([]byte{0xff, 0xe2, 0xff, 0xff}, make([]byte, 65533)...)
	for len(data) < 2*maxHeaderBytes {
		data = append(data, segment...)
	}
	for _, input := range [][]byte{nil, []byte("not an image"), {0xff, 0xd8, 0xff, 0xe1, 0, 1}, data} {
		r := &countedReader{Reader: bytes.NewReader(input)}
		w, h := Dimensions(r)
		if w != 0 || h != 0 || r.read > maxHeaderBytes {
			t.Fatalf("%dx%d, read %d bytes", w, h, r.read)
		}
	}
}
