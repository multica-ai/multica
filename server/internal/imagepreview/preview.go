// Package imagepreview measures uploaded images and produces the downscaled
// copy that comment and description bodies render inline (MUL-8182). The
// original stays the file of record: the viewer, downloads and links use it.
package imagepreview

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"path"
	"strings"

	_ "image/gif"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	// MaxWidth is about twice the widest column an inline image renders in, so
	// the preview stays sharp on a 2x display.
	MaxWidth = 1600
	// MaxPixels bounds very tall images, whose width alone says little about
	// what decoding them costs.
	MaxPixels = MaxWidth * 5000
	// A preview is only worth a second object when it is clearly smaller.
	maxUsefulScale = 0.8
	// Decoding allocates 4 bytes per pixel; larger sources keep the original.
	maxSourcePixels = 50_000_000
	jpegQuality     = 85
	previewInfix    = ".preview"
)

// Only a couple of decodes run at once, so a burst of large uploads cannot
// multiply the transient memory each decode needs.
var decodeSlots = make(chan struct{}, 2)

// Result describes an image as browsers display it, EXIF orientation applied.
type Result struct {
	Width  int
	Height int
	// Preview is the downscaled encoding, nil when the original is small
	// enough or could not be processed. Store it under PreviewKey.
	Preview            []byte
	PreviewContentType string
}

// Supports reports whether Process can measure a file of this content type.
func Supports(contentType string) bool {
	switch strings.ToLower(contentType) {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}

// PreviewKey is the storage key of the preview for the original stored at key,
// or "" when no preview is ever written for that key. It is a pure function of
// the original key so every path that deletes an original can delete its
// preview without reading the attachment row.
func PreviewKey(key string) string {
	switch strings.ToLower(path.Ext(key)) {
	case ".png", ".webp":
		return key + previewInfix + ".png"
	case ".jpg", ".jpeg":
		return key + previewInfix + ".jpg"
	}
	return ""
}

// ContentTypeForPreviewKey is the content type of the object PreviewKey names.
func ContentTypeForPreviewKey(previewKey string) string {
	if strings.HasSuffix(previewKey, ".jpg") {
		return "image/jpeg"
	}
	return "image/png"
}

// Process measures data and, when it is larger than an inline image needs,
// encodes a preview in the format PreviewKey(key) implies. ok is false when
// data is not an image this package can read; a failure past measuring still
// returns the dimensions, without a preview.
func Process(ctx context.Context, data []byte, key string) (res Result, ok bool) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return Result{}, false
	}
	orientation := 1
	if format == "jpeg" {
		orientation = jpegOrientation(data)
	}
	res.Width, res.Height = cfg.Width, cfg.Height
	if orientation >= 5 {
		res.Width, res.Height = cfg.Height, cfg.Width
	}

	previewKey := PreviewKey(key)
	if previewKey == "" || format == "gif" {
		return res, true
	}
	outType := ContentTypeForPreviewKey(previewKey)
	// JPEG has no alpha, so only a JPEG source may become a JPEG preview.
	if outType == "image/jpeg" && format != "jpeg" {
		return res, true
	}
	var colorChunks []byte
	if format == "png" {
		var animated bool
		colorChunks, animated = pngColorChunks(data)
		if animated {
			return res, true
		}
	}
	scale := previewScale(res.Width, res.Height)
	if scale > maxUsefulScale || cfg.Width*cfg.Height > maxSourcePixels {
		return res, true
	}

	select {
	case decodeSlots <- struct{}{}:
		defer func() { <-decodeSlots }()
	case <-ctx.Done():
		return res, true
	}

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return res, true
	}
	w := max(1, int(math.Round(float64(cfg.Width)*scale)))
	h := max(1, int(math.Round(float64(cfg.Height)*scale)))
	scaled := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.BiLinear.Scale(scaled, scaled.Bounds(), src, src.Bounds(), draw.Src, nil)
	oriented := orient(scaled, orientation)

	var buf bytes.Buffer
	if outType == "image/jpeg" {
		if err := jpeg.Encode(&buf, oriented, &jpeg.Options{Quality: jpegQuality}); err != nil {
			return res, true
		}
		res.Preview = withJPEGSegments(buf.Bytes(), jpegICCSegments(data))
	} else {
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		if err := enc.Encode(&buf, oriented); err != nil {
			return res, true
		}
		res.Preview = withPNGChunks(buf.Bytes(), colorChunks)
	}
	// Resampling can cost a flat, highly compressible image more bytes than
	// it saves pixels; such a preview would only make the page slower to load.
	if len(res.Preview) >= len(data) {
		res.Preview = nil
		return res, true
	}
	res.PreviewContentType = outType
	return res, true
}

// previewScale is the factor that fits an image of this size within MaxWidth
// and MaxPixels, 1 when it already fits.
func previewScale(width, height int) float64 {
	scale := 1.0
	if width > MaxWidth {
		scale = float64(MaxWidth) / float64(width)
	}
	if pixels := float64(width) * float64(height) * scale * scale; pixels > MaxPixels {
		scale *= math.Sqrt(MaxPixels / pixels)
	}
	return scale
}

// orient applies an EXIF orientation (1-8) to img, which is in stored order.
func orient(img *image.RGBA, orientation int) *image.RGBA {
	if orientation < 2 || orientation > 8 {
		return img
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
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
			si := img.PixOffset(sx, sy)
			di := dst.PixOffset(x, y)
			copy(dst.Pix[di:di+4], img.Pix[si:si+4])
		}
	}
	return dst
}
