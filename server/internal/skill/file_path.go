package skill

import (
	"path"
	"strings"
)

// IsSafeFilePath reports whether filePath is a canonical, slash-delimited
// supporting-file path accepted consistently by the server and daemon.
func IsSafeFilePath(filePath string) bool {
	if filePath == "" || strings.Contains(filePath, "\x00") || strings.HasPrefix(filePath, "/") || strings.Contains(filePath, "\\") {
		return false
	}
	if len(filePath) >= 2 && filePath[1] == ':' && isASCIILetter(filePath[0]) {
		return false
	}

	clean := path.Clean(filePath)
	return clean != "." && clean == filePath && clean != ".." && !strings.HasPrefix(clean, "../")
}

func isASCIILetter(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}
