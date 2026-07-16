package storage

import (
	"encoding/hex"
	"strings"
	"unicode/utf8"
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

// asciiOnlyFilename returns an ASCII-only variant of name by replacing every
// non-ASCII rune with an underscore. It is used for the legacy filename="..."
// parameter when the original name requires RFC 5987 encoding.
func asciiOnlyFilename(name string) string {
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

// needsRFC5987Encoding returns true when filename contains non-ASCII characters.
func needsRFC5987Encoding(name string) bool {
	for _, r := range name {
		if r > 0x7f {
			return true
		}
	}
	return false
}

// rfc5987Encode percent-encodes name per RFC 5987 §3.2.1 for UTF-8 encoding.
// Only encodes bytes that are not "attr-char" (alphanumeric plus !#$&+-.^_`|~).
func rfc5987Encode(name string) string {
	var b strings.Builder
	b.Grow(len(name) * 3) // worst case: every byte encoded as %XX
	for _, r := range name {
		if r <= 0x7f && isAttrChar(byte(r)) {
			b.WriteByte(byte(r))
		} else {
			// Encode each byte of the UTF-8 representation
			buf := make([]byte, 4)
			n := utf8.EncodeRune(buf, r)
			for i := 0; i < n; i++ {
				b.WriteByte('%')
				b.WriteString(strings.ToUpper(hex.EncodeToString(buf[i : i+1])))
			}
		}
	}
	return b.String()
}

// isAttrChar returns true for chars allowed in RFC 5987 attr-char.
func isAttrChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		strings.ContainsRune("!#$&+-.^_`|~", rune(c))
}

func ContentDisposition(contentType, filename string) string {
	disposition := "attachment"
	if isInlineContentType(contentType) {
		disposition = "inline"
	}
	if !needsRFC5987Encoding(filename) {
		return disposition + `; filename="` + sanitizeFilename(filename) + `"`
	}
	// Provide an ASCII-only fallback filename for legacy clients (RFC 6266)
	// and the RFC 5987 filename* for modern clients.
	asciiFallback := sanitizeFilename(asciiOnlyFilename(filename))
	return disposition + `; filename="` + asciiFallback + `"; filename*=UTF-8''` + rfc5987Encode(filename)
}

func AttachmentContentDisposition(filename string) string {
	if !needsRFC5987Encoding(filename) {
		return `attachment; filename="` + sanitizeFilename(filename) + `"`
	}
	asciiFallback := sanitizeFilename(asciiOnlyFilename(filename))
	return `attachment; filename="` + asciiFallback + `"; filename*=UTF-8''` + rfc5987Encode(filename)
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
