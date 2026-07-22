package daemon

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const managedArtifactPatternPrefix = "managed:"

// artifactMatcher combines operator-configured basename matches with exact
// daemon-managed paths. Exact paths take precedence so a broad basename such
// as .sandbox-bin cannot double-count a managed directory.
type artifactMatcher struct {
	basenames      map[string]struct{}
	exactPaths     map[string]string
	exactLeafNames map[string]struct{}
}

func newArtifactMatcher(patterns, managedSubpaths []string) artifactMatcher {
	m := artifactMatcher{
		basenames:      buildPatternSet(patterns),
		exactPaths:     make(map[string]string, len(managedSubpaths)),
		exactLeafNames: make(map[string]struct{}, len(managedSubpaths)),
	}
	for _, subpath := range managedSubpaths {
		cleaned, ok := safeRelativePath(subpath)
		if !ok {
			continue
		}
		display := filepath.ToSlash(cleaned)
		m.exactPaths[cleaned] = managedArtifactPatternPrefix + display
		m.exactLeafNames[filepath.Base(cleaned)] = struct{}{}
	}
	return m
}

func (m artifactMatcher) matchDirectory(absRoot, path string, entry os.DirEntry) (string, bool) {
	_, exactCandidate := m.exactLeafNames[entry.Name()]
	_, basenameMatch := m.basenames[entry.Name()]
	if !exactCandidate && !basenameMatch {
		return "", false
	}

	// Rel and containment validation are only needed for a directory whose
	// leaf could actually match. Most workdir entries avoid this path entirely.
	rel, err := filepath.Rel(absRoot, path)
	if err != nil {
		return "", false
	}
	rel, ok := safeRelativePath(rel)
	if !ok {
		return "", false
	}
	if label, ok := m.exactPaths[rel]; ok {
		return label, true
	}
	if basenameMatch {
		return entry.Name(), true
	}
	return "", false
}

func (m artifactMatcher) managedSubpaths() []string {
	out := make([]string, 0, len(m.exactPaths))
	for rel := range m.exactPaths {
		out = append(out, filepath.ToSlash(rel))
	}
	sort.Strings(out)
	return out
}

func safeRelativePath(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return "", false
	}
	cleaned := filepath.Clean(path)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", false
	}
	return cleaned, true
}
