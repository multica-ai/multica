package skill

import (
	"path/filepath"
	"strings"
)

// IsLikelyBinaryFilePath reports whether the file's extension indicates a
// non-text payload. Conservative blacklist — extensions not on the list are
// assumed text and pass through.
//
// Both skill ingestion paths need this, for the same reason from two
// directions: an archive/URL import cannot put these bytes in a PG TEXT
// column (SQLSTATE 22021), and runtime-local sync cannot carry them through
// SkillFileData.Content, because encoding/json rewrites every invalid UTF-8
// byte to U+FFFD on the daemon/server hop. Either way the bytes that come
// back out are not the bytes that went in, so both paths skip the file
// instead of storing a payload they would later hand back corrupted.
func IsLikelyBinaryFilePath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case
		// images
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tiff", ".ico", ".heic",
		// fonts
		".ttf", ".otf", ".woff", ".woff2", ".eot",
		// archives
		".zip", ".gz", ".tar", ".bz2", ".7z", ".rar",
		// documents (binary office)
		".pdf", ".docx", ".xlsx", ".pptx", ".doc", ".xls", ".ppt",
		// media
		".mp3", ".mp4", ".wav", ".avi", ".mov", ".webm", ".m4a", ".flac",
		// compiled / executable
		".exe", ".dll", ".so", ".dylib", ".class", ".jar", ".wasm",
		// db / cache
		".db", ".sqlite", ".sqlite3", ".pyc":
		return true
	}
	return false
}
