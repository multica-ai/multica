package execenv

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

// RuntimeContamination identifies an affected commit without rewriting it.
type RuntimeContamination struct {
	Commit string
	Path   string
}

func (w *LocalWorktree) runtimeContamination(tip string) ([]RuntimeContamination, error) {
	commits, err := runGitTrimmed(w.GitRoot, "rev-list", w.BaseCommit+".."+tip)
	if err != nil {
		return nil, err
	}
	hashes := make(map[string]string)
	for path, hash := range w.RuntimeSidecarHashes {
		rel, err := filepath.Rel(w.Path, path)
		if err == nil && filepath.IsLocal(rel) {
			hashes[filepath.ToSlash(rel)] = hash
		}
	}
	var found []RuntimeContamination
	for _, commit := range strings.Fields(commits) {
		paths, err := runGitStdout(w.GitRoot, "diff-tree", "--root", "-r", "-m", "--no-commit-id", "--no-renames", "--raw", "--diff-filter=ACMT", "-z", commit)
		if err != nil {
			return found, err
		}
		seen := make(map[string]bool)
		entries := strings.Split(paths, "\x00")
		for i := 0; i+1 < len(entries); i += 2 {
			metadata, path := strings.Fields(entries[i]), entries[i+1]
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			if len(metadata) != 5 {
				return found, fmt.Errorf("unexpected git diff-tree entry for %s", commit)
			}
			if metadata[1] == "160000" {
				continue
			} // Submodules contain commits, not blobs.
			marker, hash, err := inspectRuntimeBlob(w.GitRoot, metadata[3])
			if err != nil {
				return found, err
			}
			if expected := hashes[path]; marker || (expected != "" && expected == hash) {
				found = append(found, RuntimeContamination{Commit: commit, Path: path})
			}
		}
	}
	return found, nil
}

// Stream blobs: a task may commit large binaries, which must not be buffered
// whole just to search for a short marker or hash an owned sidecar.
func inspectRuntimeBlob(gitRoot, object string) (bool, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", gitRoot, "cat-file", "blob", object)
	hash := sha256.New()
	probe := &runtimeBlobProbe{tail: []byte("\n")}
	cmd.Stdout = io.MultiWriter(hash, probe)
	if err := cmd.Run(); err != nil {
		return false, "", err
	}
	probe.Write([]byte("\n"))
	var marker taskContextMarkerFile
	isTaskMarker := !probe.large && json.Unmarshal(probe.jsonPrefix, &marker) == nil && marker.ManagedBy == TaskContextMarkerManagedBy
	return probe.marked || isTaskMarker, fmt.Sprintf("%x", hash.Sum(nil)), nil
}

type runtimeBlobProbe struct {
	tail          []byte
	jsonPrefix    []byte
	marked, large bool
}

func (p *runtimeBlobProbe) Write(data []byte) (int, error) {
	const maxMarkerBytes = 64 << 10 // Generated task markers contain only four short fields.
	if !p.large {
		if len(p.jsonPrefix)+len(data) > maxMarkerBytes {
			p.large = true
			p.jsonPrefix = nil
		} else {
			p.jsonPrefix = append(p.jsonPrefix, data...)
		}
	}
	window := append(p.tail, data...)
	for _, ending := range []string{"\n", "\r\n"} {
		if bytes.Contains(window, []byte("\n"+runtimeMarkerBegin+ending)) {
			p.marked = true
		}
	}
	keep := len(runtimeMarkerBegin) + 3
	if len(window) > keep {
		window = window[len(window)-keep:]
	}
	p.tail = append(p.tail[:0], window...)
	return len(data), nil
}
