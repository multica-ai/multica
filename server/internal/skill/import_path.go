package skill

import (
	"fmt"
	"path"
	"strings"
)

// CanonicalRuntimeLocalFilePath converts cross-platform separators to the
// slash form used by skill bundles and rejects unsafe or ambiguous paths.
func CanonicalRuntimeLocalFilePath(filePath string) (string, bool) {
	canonical := strings.ReplaceAll(filePath, `\`, "/")
	if canonical == "" || strings.Contains(canonical, "\x00") || strings.HasPrefix(canonical, "/") {
		return "", false
	}
	if len(canonical) >= 2 && canonical[1] == ':' && isASCIILetter(canonical[0]) {
		return "", false
	}
	clean := path.Clean(canonical)
	if clean == "." || clean != canonical || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return canonical, true
}

// CanonicalRuntimeLocalFilePaths canonicalizes a daemon-reported bundle while
// preserving input indexes. Invalid entries are empty for callers to omit.
// Paths that become duplicates after normalization reject the bundle.
func CanonicalRuntimeLocalFilePaths(filePaths []string) ([]string, error) {
	canonicalPaths := make([]string, len(filePaths))
	seen := make(map[string]struct{}, len(filePaths))
	for i, filePath := range filePaths {
		canonical, ok := CanonicalRuntimeLocalFilePath(filePath)
		if !ok {
			continue
		}
		if _, exists := seen[canonical]; exists {
			return nil, fmt.Errorf("duplicate local skill file path after normalization: %s", canonical)
		}
		seen[canonical] = struct{}{}
		canonicalPaths[i] = canonical
	}
	return canonicalPaths, nil
}

func isASCIILetter(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}
