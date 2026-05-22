package storage

import (
	"net/url"
	"path"
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

func contentDisposition(disposition, filename string) string {
	safe := sanitizeFilename(filename)
	fallback := asciiFilenameFallback(safe)
	if fallback == "" {
		fallback = "download"
	}
	if fallback == safe {
		return disposition + `; filename="` + fallback + `"`
	}
	encoded := strings.ReplaceAll(url.PathEscape(safe), "+", "%20")
	return disposition + `; filename="` + fallback + `"; filename*=UTF-8''` + encoded
}

func asciiFilenameFallback(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if r >= 0x20 && r <= 0x7e && r != '"' && r != ';' && r != '\\' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	fallback := strings.TrimSpace(b.String())
	if strings.Trim(fallback, "._") != "" {
		return fallback
	}
	ext := path.Ext(fallback)
	if ext != "" && strings.Trim(ext, "._") != "" {
		return "download" + ext
	}
	return "download"
}

// isInlineContentType returns true for media types that browsers should
// display inline (images, video, audio, PDF). Everything else triggers a
// download via Content-Disposition: attachment.
func isInlineContentType(ct string) bool {
	return strings.HasPrefix(ct, "image/") ||
		strings.HasPrefix(ct, "video/") ||
		strings.HasPrefix(ct, "audio/") ||
		ct == "application/pdf"
}
