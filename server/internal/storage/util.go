package storage

import (
	"net/http"
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

// detectContentType determines the content type for a file.
// Uses http.DetectContentType as base, with overrides for common extensions.
func detectContentType(data []byte, filename string) string {
	contentType := http.DetectContentType(data)
	ext := strings.ToLower(path.Ext(filename))
	if ct := overrideContentType(ext); ct != "" {
		return ct
	}
	return contentType
}

// overrideContentType returns content type overrides for extensions that http.DetectContentType gets wrong.
func overrideContentType(ext string) string {
	switch ext {
	case ".svg":
		return "image/svg+xml"
	case ".css":
		return "text/css"
	case ".js", ".mjs":
		return "application/javascript"
	case ".json":
		return "application/json"
	case ".wasm":
		return "application/wasm"
	}
	return ""
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
