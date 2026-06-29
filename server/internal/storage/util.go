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
	return dispositionHeader(disposition, filename)
}

func AttachmentContentDisposition(filename string) string {
	return dispositionHeader("attachment", filename)
}

// dispositionHeader builds a Content-Disposition value. The name is sanitized
// against header injection, then emitted as an ASCII `filename="…"` plus — when
// the name contains non-ASCII characters — an RFC 5987 `filename*=UTF-8”…` so
// compliant clients (all modern browsers) render the real Unicode name instead
// of the mojibake-prone ASCII fallback (RFC 6266 §4.3). Pure-ASCII names keep
// the legacy single-parameter form.
func dispositionHeader(disposition, filename string) string {
	sanitized := sanitizeFilename(filename)
	ascii := asciiFallbackFilename(sanitized)
	header := disposition + `; filename="` + ascii + `"`
	if ascii != sanitized {
		header += `; filename*=UTF-8''` + rfc5987ValueEncode(sanitized)
	}
	return header
}

// asciiFallbackFilename replaces every non-ASCII rune with '_' so the value is
// safe for the ASCII-only `filename=` parameter.
func asciiFallbackFilename(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if r > 0x7f {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// rfc5987ValueEncode percent-encodes a UTF-8 string per RFC 5987 ext-value:
// attr-char bytes stay literal, every other byte becomes %XX (uppercase hex).
func rfc5987ValueEncode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isAttrChar(c) {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}

// isAttrChar reports whether c is an RFC 5987 attr-char (left literal in an
// ext-value).
func isAttrChar(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '!', '#', '$', '&', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}
	return false
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
