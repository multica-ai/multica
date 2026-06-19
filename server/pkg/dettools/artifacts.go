package dettools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// safeArtifactPath resolves name within workDir/artifactDir, rejecting any name
// that would escape the artifact directory (absolute paths, ".." traversal).
// It returns the absolute destination and the path relative to workDir (used in
// the artifact record so consumers see a stable, repo-relative location).
func safeArtifactPath(workDir, artifactDir, name string) (abs, relToWork string, err error) {
	if strings.TrimSpace(name) == "" {
		return "", "", fmt.Errorf("filename is required")
	}
	base := filepath.Join(workDir, artifactDir)
	dest := filepath.Join(base, name)
	rel, err := filepath.Rel(base, dest)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("filename %q escapes the artifact directory", name)
	}
	relToWork, err = filepath.Rel(workDir, dest)
	if err != nil {
		relToWork = dest
	}
	return dest, relToWork, nil
}

// writeArtifact creates the artifact directory if needed and writes content to
// name within it, returning an Artifact record with a workDir-relative path.
func writeArtifact(workDir, artifactDir, name, artifactType string, content []byte) (Artifact, error) {
	abs, rel, err := safeArtifactPath(workDir, artifactDir, name)
	if err != nil {
		return Artifact{}, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return Artifact{}, fmt.Errorf("create artifact dir: %w", err)
	}
	if err := os.WriteFile(abs, content, 0o644); err != nil {
		return Artifact{}, fmt.Errorf("write artifact: %w", err)
	}
	return Artifact{Type: artifactType, Path: rel}, nil
}
