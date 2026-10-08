package execenv

import "path/filepath"

const (
	codexHomeDirName       = "codex-home"
	codexSandboxBinDirName = ".sandbox-bin"
)

// ManagedReclaimableArtifactSubpaths returns daemon-owned, regenerable
// directories inside a task env root. Callers must match these as exact
// relative paths rather than basenames: a repository may legitimately contain
// a directory with the same leaf name.
func ManagedReclaimableArtifactSubpaths() []string {
	return []string{filepath.Join(codexHomeDirName, codexSandboxBinDirName)}
}
