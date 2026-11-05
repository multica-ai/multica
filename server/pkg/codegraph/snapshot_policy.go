package codegraph

import (
	_ "embed"
	"encoding/json"
	"path/filepath"
	"strings"
)

//go:embed snapshot-policy.json
var snapshotPolicyJSON []byte

var snapshotPolicy = func() struct {
	AllowedHiddenDirectories []string `json:"allowed_hidden_directories"`
	DeniedNames              []string `json:"denied_names"`
	DeniedExtensions         []string `json:"denied_extensions"`
} {
	var policy struct {
		AllowedHiddenDirectories []string `json:"allowed_hidden_directories"`
		DeniedNames              []string `json:"denied_names"`
		DeniedExtensions         []string `json:"denied_extensions"`
	}
	if err := json.Unmarshal(snapshotPolicyJSON, &policy); err != nil {
		panic(err)
	}
	return policy
}()

func policyContains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// PrivatePath uses slash-relative repository paths; directory exceptions never admit files.
func PrivatePath(path string, directory bool) bool {
	if path == "." && directory {
		return false
	}
	if !filepath.IsLocal(path) || strings.Contains(path, "\\") {
		return true
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i, part := range parts {
		lower := strings.ToLower(part)
		if part == "" || part == "." || part == ".." || policyContains(snapshotPolicy.DeniedNames, lower) {
			return true
		}
		if strings.HasPrefix(lower, ".") && !(policyContains(snapshotPolicy.AllowedHiddenDirectories, lower) && (i < len(parts)-1 || directory)) {
			return true
		}
		for _, suffix := range snapshotPolicy.DeniedExtensions {
			if strings.HasSuffix(lower, suffix) {
				return true
			}
		}
	}
	return false
}
