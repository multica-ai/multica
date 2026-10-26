package imagepreview

import (
	"bytes"
	"encoding/binary"
)

// Go's decoders drop the metadata browsers apply when they draw the original:
// EXIF orientation and the embedded color profile. A preview without them
// would show a phone photo sideways and a Display P3 screenshot washed out, so
// orientation is applied to the pixels and the color chunks are carried over.

// jpegOrientation reads the EXIF orientation tag (1-8), 1 when absent.
func jpegOrientation(data []byte) int {
	for _, seg := range jpegSegments(data) {
		if seg.marker != 0xE1 || !bytes.HasPrefix(seg.payload, []byte("Exif\x00\x00")) {
			continue
		}
		tiff := seg.payload[6:]
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
			if order.Uint16(tiff[entry:]) == 0x0112 {
				if v := int(order.Uint16(tiff[entry+8:])); v >= 1 && v <= 8 {
					return v
				}
				return 1
			}
		}
		return 1
	}
	return 1
}

// jpegICCSegments returns the raw APP2 ICC_PROFILE segments, marker included.
func jpegICCSegments(data []byte) []byte {
	var out []byte
	for _, seg := range jpegSegments(data) {
		if seg.marker == 0xE2 && bytes.HasPrefix(seg.payload, []byte("ICC_PROFILE\x00")) {
			out = append(out, seg.raw...)
		}
	}
	return out
}

// withJPEGSegments inserts raw segments right after the SOI marker.
func withJPEGSegments(jpg, segments []byte) []byte {
	if len(segments) == 0 || len(jpg) < 2 {
		return jpg
	}
	out := make([]byte, 0, len(jpg)+len(segments))
	out = append(out, jpg[:2]...)
	out = append(out, segments...)
	return append(out, jpg[2:]...)
}

type jpegSegment struct {
	marker  byte
	payload []byte
	raw     []byte
}

// jpegSegments lists the marker segments before the image data.
func jpegSegments(data []byte) []jpegSegment {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return nil
	}
	var segs []jpegSegment
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return segs
		}
		marker := data[i+1]
		if marker == 0xFF {
			i++
			continue
		}
		if marker == 0xDA || marker == 0xD9 {
			return segs
		}
		length := int(binary.BigEndian.Uint16(data[i+2:]))
		end := i + 2 + length
		if length < 2 || end > len(data) {
			return segs
		}
		segs = append(segs, jpegSegment{marker: marker, payload: data[i+4 : end], raw: data[i:end]})
		i = end
	}
	return segs
}

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// pngColorChunks returns the raw color-space chunks of a PNG (iCCP, sRGB, gAMA,
// cHRM), and whether it is an animated PNG, whose preview would freeze it.
func pngColorChunks(data []byte) (chunks []byte, animated bool) {
	if !bytes.HasPrefix(data, pngSignature) {
		return nil, false
	}
	for i := len(pngSignature); i+12 <= len(data); {
		length := int(binary.BigEndian.Uint32(data[i:]))
		end := i + 12 + length
		if end > len(data) {
			return chunks, false
		}
		switch string(data[i+4 : i+8]) {
		case "iCCP", "sRGB", "gAMA", "cHRM":
			chunks = append(chunks, data[i:end]...)
		case "acTL":
			return nil, true
		case "IDAT", "IEND":
			return chunks, false
		}
		i = end
	}
	return chunks, false
}

// withPNGChunks inserts raw chunks right after IHDR, where color-space chunks
// must precede the image data.
func withPNGChunks(img, chunks []byte) []byte {
	const ihdrEnd = 8 + 12 + 13
	if len(chunks) == 0 || len(img) < ihdrEnd || string(img[12:16]) != "IHDR" {
		return img
	}
	out := make([]byte, 0, len(img)+len(chunks))
	out = append(out, img[:ihdrEnd]...)
	out = append(out, chunks...)
	return append(out, img[ihdrEnd:]...)
}
