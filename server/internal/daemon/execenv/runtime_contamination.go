package execenv

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// RuntimeContamination identifies an affected commit without rewriting it.
type RuntimeContamination struct {
	Commit string
	Path   string
}

func (w *LocalWorktree) runtimeContamination(tip string) (found []RuntimeContamination, err error) {
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
	// One cat-file process serves every blob: a task commit can touch
	// thousands of files, and a process per blob would stall Finalize.
	blobs, err := startRuntimeBlobReader(w.GitRoot)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := blobs.close(err != nil); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
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
			marker, hash, err := blobs.inspect(metadata[3])
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

// runtimeBlobReader streams blobs from a long-lived `git cat-file --batch`.
// Blobs are never buffered whole: a task may commit large binaries, which must
// not be held in memory just to search for a short marker or hash a sidecar.
type runtimeBlobReader struct {
	cancel context.CancelFunc
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
}

func startRuntimeBlobReader(gitRoot string) (*runtimeBlobReader, error) {
	// The whole scan shares one deadline; it runs once, after the agent exits.
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	cmd := exec.CommandContext(ctx, "git", "-C", gitRoot, "cat-file", "--batch")
	cmd.WaitDelay = 5 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	return &runtimeBlobReader{cancel: cancel, cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}, nil
}

func (r *runtimeBlobReader) inspect(object string) (bool, string, error) {
	if _, err := io.WriteString(r.stdin, object+"\n"); err != nil {
		return false, "", fmt.Errorf("git cat-file: %w", err)
	}
	header, err := r.stdout.ReadString('\n')
	if err != nil {
		return false, "", fmt.Errorf("git cat-file %s: %w", object, err)
	}
	fields := strings.Fields(header)
	if len(fields) != 3 || fields[1] != "blob" {
		return false, "", fmt.Errorf("git cat-file %s: unexpected header %q", object, strings.TrimSpace(header))
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return false, "", fmt.Errorf("git cat-file %s: %w", object, err)
	}
	hash := sha256.New()
	probe := &runtimeBlobProbe{tail: []byte("\n")}
	if _, err := io.CopyN(io.MultiWriter(hash, probe), r.stdout, size); err != nil {
		return false, "", fmt.Errorf("git cat-file %s: %w", object, err)
	}
	if b, err := r.stdout.ReadByte(); err != nil || b != '\n' {
		return false, "", fmt.Errorf("git cat-file %s: missing object terminator", object)
	}
	probe.Write([]byte("\n"))
	var marker taskContextMarkerFile
	isTaskMarker := !probe.large && json.Unmarshal(probe.jsonPrefix, &marker) == nil && marker.ManagedBy == TaskContextMarkerManagedBy
	return probe.marked || isTaskMarker, fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// close ends the batch. After a failed read git may still be blocked writing
// the unread remainder of a blob, so an aborted scan kills it rather than
// waiting out the deadline.
func (r *runtimeBlobReader) close(aborted bool) error {
	defer r.cancel()
	_ = r.stdin.Close()
	if aborted {
		r.cancel()
		_ = r.cmd.Wait()
		return nil
	}
	return r.cmd.Wait()
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
