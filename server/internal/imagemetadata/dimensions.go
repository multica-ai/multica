// Package imagemetadata reads image headers without decoding pixel data.
package imagemetadata

import (
	"bytes"
	"encoding/binary"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
)

type Size struct {
	Width  int32
	Height int32
}

// Dimensions returns the size displayed by browsers, including JPEG orientation.
// Unsupported or malformed files simply retain the unknown-size placeholder.
func Dimensions(data []byte) (Size, bool) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > math.MaxInt32 || cfg.Height > math.MaxInt32 {
		return Size{}, false
	}
	width, height := int32(cfg.Width), int32(cfg.Height)
	if format == "jpeg" && jpegOrientation(data) >= 5 {
		width, height = height, width
	}
	return Size{Width: width, Height: height}, true
}

func jpegOrientation(data []byte) int {
	if len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for offset := 2; offset+4 <= len(data); {
		if data[offset] != 0xff {
			break
		}
		marker := data[offset+1]
		if marker == 0xff {
			offset++
			continue
		}
		if marker == 0xda || marker == 0xd9 {
			break
		}
		length := int(binary.BigEndian.Uint16(data[offset+2:]))
		end := offset + 2 + length
		if length < 2 || end > len(data) {
			break
		}
		payload := data[offset+4 : end]
		if marker == 0xe1 && bytes.HasPrefix(payload, []byte("Exif\x00\x00")) {
			return tiffOrientation(payload[6:])
		}
		offset = end
	}
	return 1
}

func tiffOrientation(data []byte) int {
	if len(data) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(data[2:4]) != 42 {
		return 1
	}
	offset := uint64(order.Uint32(data[4:8]))
	if offset < 8 || offset+2 > uint64(len(data)) {
		return 1
	}
	count := int(order.Uint16(data[offset:]))
	for i := 0; i < count; i++ {
		entry := offset + 2 + uint64(i)*12
		if entry+12 > uint64(len(data)) {
			return 1
		}
		if order.Uint16(data[entry:]) != 0x0112 {
			continue
		}
		if order.Uint16(data[entry+2:]) != 3 || order.Uint32(data[entry+4:]) != 1 {
			return 1
		}
		if value := int(order.Uint16(data[entry+8:])); value >= 1 && value <= 8 {
			return value
		}
		return 1
	}
	return 1
}
