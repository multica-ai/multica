package skillbundle

import (
	pathpkg "path"
	"strings"
)

const (
	MaxFileSize   = 1 << 20
	MaxBundleSize = 8 << 20
	MaxFileCount  = 128
)

func NormalizePath(p string) (string, bool) {
	normalized := strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	cleaned := pathpkg.Clean(normalized)
	if cleaned == "." || strings.HasPrefix(cleaned, "/") || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}

func ShouldSkipDir(name string) bool {
	trimmed := strings.TrimSpace(name)
	switch strings.ToLower(trimmed) {
	case "", ".git", ".hg", ".svn", ".venv", "venv", "env", ".env",
		"__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache",
		"node_modules", "bower_components",
		"dist", "build", "out", "target", "coverage",
		".next", ".nuxt", ".turbo", ".cache":
		return true
	default:
		return strings.HasPrefix(trimmed, ".")
	}
}

func ShouldSkipFile(path string) bool {
	if ShouldSkipPath(path) {
		return true
	}
	base := strings.ToLower(pathpkg.Base(path))
	switch base {
	case "", "skill.md", "license", "license.md", "license.txt", ".ds_store":
		return true
	}
	if strings.HasPrefix(base, ".") {
		return true
	}
	if IsLikelyBinaryPath(path) {
		return true
	}
	return false
}

func ShouldSkipPath(path string) bool {
	normalized, ok := NormalizePath(path)
	if !ok {
		return true
	}
	parts := strings.Split(normalized, "/")
	for _, part := range parts[:len(parts)-1] {
		if ShouldSkipDir(part) {
			return true
		}
	}
	return false
}

func IsLikelyBinaryPath(path string) bool {
	ext := strings.ToLower(pathpkg.Ext(path))
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
		".exe", ".dll", ".so", ".dylib", ".class", ".jar", ".wasm", ".pyc",
		// db / cache
		".db", ".sqlite", ".sqlite3":
		return true
	}
	return false
}
