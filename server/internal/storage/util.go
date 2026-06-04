package storage

import (
	"strings"
)

// sanitizeFilename removes characters that could cause header injection in Content-Disposition.
func sanitizeFilename(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		// Strip control chars, newlines, null bytes, quotes, semicolons, backslashes
		if r < 0x20 || r == 0x7f || r == '"' || r == ';' || r == '\\' || r == '\x00' {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func ContentDisposition(contentType, filename string) string {
	disposition := "attachment"
	if isInlineContentType(contentType) {
		disposition = "inline"
	}
	return disposition + `; filename="` + sanitizeFilename(filename) + `"`
}

func AttachmentContentDisposition(filename string) string {
	return `attachment; filename="` + sanitizeFilename(filename) + `"`
}

// isInlineContentType returns true for media types that browsers should
// display inline (images, video, audio, PDF). Everything else triggers a
// download via Content-Disposition: attachment.
//
// SVG is excluded even though its MIME type is image/svg+xml: SVG is XML
// and can carry <script>, <foreignObject>, or onload= attributes that
// execute in the document's origin when rendered inline. Forcing
// attachment disposition prevents stored-XSS via uploaded .svg files.
//
// Input is normalized (trim, lowercase, strip parameters) before matching
// so that values like "image/svg+xml; charset=utf-8" or "IMAGE/SVG+XML"
// can't slip past the SVG carve-out. RFC 2045 §5.1 defines MIME type
// matching as case-insensitive with optional parameters; this is the
// security boundary, so normalize here instead of trusting callers.
func isInlineContentType(ct string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(ct))
	if i := strings.IndexByte(mediaType, ';'); i >= 0 {
		mediaType = strings.TrimSpace(mediaType[:i])
	}
	if mediaType == "image/svg+xml" {
		return false
	}
	return strings.HasPrefix(mediaType, "image/") ||
		strings.HasPrefix(mediaType, "video/") ||
		strings.HasPrefix(mediaType, "audio/") ||
		mediaType == "application/pdf"
}
