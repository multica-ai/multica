// Package thumbnail renders small display copies of large images, so a grid of
// tiles does not have to download and decode multi-megapixel originals.
package thumbnail

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"path"
	"strings"

	"github.com/multica-ai/multica/server/internal/imagemetadata"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	// ShortSide covers a ~240 CSS px tile edge at 2x density.
	ShortSide = 480
	// MaxLongSide bounds panoramas and tall screenshots.
	MaxLongSide = 1280
	// MaxSourcePixels bounds decode memory: a 40 MP RGBA bitmap is 160 MB.
	MaxSourcePixels = 40_000_000

	jpegQuality = 82
)

// ErrUnsupported reports a source this package will not decode: an unknown or
// excluded format, a malformed header, or an image above MaxSourcePixels.
var ErrUnsupported = errors.New("thumbnail: unsupported image")

// Thumbnail is an encoded thumbnail. Opaque images are JPEG; images with any
// transparency stay PNG so their alpha survives.
type Thumbnail struct {
	Data        []byte
	ContentType string
}

// StorageKey names the stored thumbnail of a storage object. Only keys with a
// raster image extension get one, so a delete path that knows just the object
// URL can remove the thumbnail without a lookup.
func StorageKey(objectKey string) (string, bool) {
	switch strings.ToLower(path.Ext(objectKey)) {
	case ".jpg", ".jpeg", ".png", ".webp":
		return objectKey + ".thumb", true
	}
	return "", false
}

// SupportedContentType reports whether Generate accepts images of this type.
// GIF (animation), SVG (vector) and HEIC (no decoder) are left to the original.
func SupportedContentType(contentType string) bool {
	switch contentType {
	case "image/jpeg", "image/png", "image/webp":
		return true
	}
	return false
}

// Generate decodes a JPEG, PNG or WebP and returns a copy scaled to Size,
// with JPEG EXIF orientation applied so it displays like the original.
func Generate(data []byte) (Thumbnail, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Thumbnail{}, fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	switch format {
	case "jpeg", "png", "webp":
	default:
		return Thumbnail{}, fmt.Errorf("%w: format %s", ErrUnsupported, format)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxSourcePixels {
		return Thumbnail{}, fmt.Errorf("%w: %dx%d", ErrUnsupported, cfg.Width, cfg.Height)
	}

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Thumbnail{}, fmt.Errorf("thumbnail: decode %s: %w", format, err)
	}
	orientation := 1
	if format == "jpeg" {
		orientation = imagemetadata.JPEGOrientation(data)
	}

	srcW, srcH := src.Bounds().Dx(), src.Bounds().Dy()
	transposed := orientation >= 5
	displayW, displayH := srcW, srcH
	if transposed {
		displayW, displayH = srcH, srcW
	}
	outW, outH := Size(displayW, displayH)
	scaledW, scaledH := outW, outH
	if transposed {
		scaledW, scaledH = outH, outW
	}

	scaled := image.NewRGBA(image.Rect(0, 0, scaledW, scaledH))
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), src, src.Bounds(), draw.Src, nil)
	out := orient(scaled, orientation)

	var buf bytes.Buffer
	if out.Opaque() {
		if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: jpegQuality}); err != nil {
			return Thumbnail{}, fmt.Errorf("thumbnail: encode jpeg: %w", err)
		}
		return Thumbnail{Data: buf.Bytes(), ContentType: "image/jpeg"}, nil
	}
	if err := png.Encode(&buf, out); err != nil {
		return Thumbnail{}, fmt.Errorf("thumbnail: encode png: %w", err)
	}
	return Thumbnail{Data: buf.Bytes(), ContentType: "image/png"}, nil
}

// Size returns the thumbnail dimensions for a w x h image: the short side
// shrinks to ShortSide and the long side to at most MaxLongSide, keeping the
// aspect ratio. Images already within both bounds keep their size.
func Size(w, h int) (int, int) {
	short, long := min(w, h), max(w, h)
	scale := min(1, float64(ShortSide)/float64(short), float64(MaxLongSide)/float64(long))
	return max(1, int(math.Round(float64(w)*scale))), max(1, int(math.Round(float64(h)*scale)))
}

// orient applies an EXIF orientation (1-8) to src. Orientations 5-8 swap the
// image's width and height.
func orient(src *image.RGBA, orientation int) *image.RGBA {
	if orientation < 2 || orientation > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dstW, dstH := w, h
	if orientation >= 5 {
		dstW, dstH = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := 0; y < dstH; y++ {
		for x := 0; x < dstW; x++ {
			var sx, sy int
			switch orientation {
			case 2:
				sx, sy = w-1-x, y
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sx, sy = x, h-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			si, di := src.PixOffset(sx, sy), dst.PixOffset(x, y)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}
