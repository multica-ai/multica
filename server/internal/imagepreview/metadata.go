// Package imagepreview reads displayed image dimensions without decoding pixels.
package imagepreview

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"

	_ "golang.org/x/image/webp"
)

// Bound work on uploads with unusually large or malformed metadata. Such
// images remain valid uploads, but use the client's unknown-size placeholder.
const maxHeaderBytes = 1 << 20

// Dimensions reads only image headers. The caller must rewind before uploading.
// Unsupported or malformed headers return zero dimensions.
func Dimensions(file io.ReadSeeker) (width, height int) {
	cfg, format, err := image.DecodeConfig(io.LimitReader(file, maxHeaderBytes))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > math.MaxInt32 || cfg.Height > math.MaxInt32 {
		return 0, 0
	}
	width, height = cfg.Width, cfg.Height
	if format == "jpeg" {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return 0, 0
		}
		orientation := jpegOrientation(bufio.NewReader(io.LimitReader(file, maxHeaderBytes)))
		if orientation == 0 {
			return 0, 0
		}
		if orientation >= 5 {
			width, height = height, width
		}
	}
	return width, height
}

// Read APP1 segments up to SOS, skipping other header segments without keeping
// them in memory. Zero means the header could not be fully inspected.
func jpegOrientation(r *bufio.Reader) int {
	var soi [2]byte
	if _, err := io.ReadFull(r, soi[:]); err != nil || soi != [2]byte{0xff, 0xd8} {
		return 0
	}
	for {
		prefix, err := r.ReadByte()
		if err != nil || prefix != 0xff {
			return 0
		}
		marker, err := r.ReadByte()
		for marker == 0xff && err == nil {
			marker, err = r.ReadByte()
		}
		if err != nil {
			return 0
		}
		if marker == 0xda || marker == 0xd9 {
			return 1
		}
		if marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) {
			continue
		}
		var size [2]byte
		if _, err := io.ReadFull(r, size[:]); err != nil {
			return 0
		}
		length := int(binary.BigEndian.Uint16(size[:])) - 2
		if length < 0 {
			return 0
		}
		if marker != 0xe1 {
			if _, err := io.CopyN(io.Discard, r, int64(length)); err != nil {
				return 0
			}
			continue
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0
		}
		if !bytes.HasPrefix(payload, []byte("Exif\x00\x00")) {
			continue
		}
		tiff := payload[6:]
		if len(tiff) < 8 {
			return 1
		}
		var order binary.ByteOrder
		switch string(tiff[:2]) {
		case "II":
			order = binary.LittleEndian
		case "MM":
			order = binary.BigEndian
		default:
			return 1
		}
		ifd := int(order.Uint32(tiff[4:8]))
		if ifd < 8 || ifd+2 > len(tiff) {
			return 1
		}
		count := int(order.Uint16(tiff[ifd:]))
		for i := 0; i < count; i++ {
			entry := ifd + 2 + i*12
			if entry+12 > len(tiff) {
				return 1
			}
			if order.Uint16(tiff[entry:]) == 0x0112 && order.Uint16(tiff[entry+2:]) == 3 && order.Uint32(tiff[entry+4:]) == 1 {
				if v := int(order.Uint16(tiff[entry+8:])); v >= 1 && v <= 8 {
					return v
				}
				return 1
			}
		}
		return 1
	}
}
