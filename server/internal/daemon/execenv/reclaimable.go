package execenv

import "path/filepath"

const (
	codexHomeDirName       = "codex-home"
	codexSandboxBinDirName = ".sandbox-bin"
	codexTempDirName       = ".tmp"
)

// ManagedReclaimableArtifactSubpaths returns daemon-owned, regenerable
// directories inside a task env root. Callers must match these as exact
// relative paths rather than basenames: a repository may legitimately contain
// a directory with the same leaf name.
func ManagedReclaimableArtifactSubpaths() []string {
	return []string{filepath.Join(codexHomeDirName, codexSandboxBinDirName)}
}

// CodexTempReclaimableArtifactSubpaths returns the reusable marketplace and
// plugin cache paths governed by the longer Codex-temp TTL. Keep these separate
// from ManagedReclaimableArtifactSubpaths: the general artifact TTL is shorter.
func CodexTempReclaimableArtifactSubpaths() []string {
	return []string{filepath.Join(codexHomeDirName, codexTempDirName)}
}

// AllManagedReclaimableArtifactSubpaths returns every exact daemon-managed
// cache path for read-only accounting. Mutation callers must select the path
// set whose TTL they are enforcing.
func AllManagedReclaimableArtifactSubpaths() []string {
	paths := append([]string{}, ManagedReclaimableArtifactSubpaths()...)
	return append(paths, CodexTempReclaimableArtifactSubpaths()...)
}
