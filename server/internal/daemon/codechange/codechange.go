// Package codechange reads what a run changed out of a git repository: the
// files between two commits, their line counts, and the patch, bounded so a
// run that rewrote a vendored tree or checked in a build output cannot turn
// into a multi-megabyte upload (MUL-7651).
//
// Everything here is read-only git plumbing against commits that already
// exist. Nothing touches an index, a working tree or a ref.
package codechange

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// MaxPatchBytes is the largest patch uploaded. Past it only the file list
	// travels, and the viewer says the change was too large to show.
	MaxPatchBytes = 1 << 20
	// MaxFiles caps the file list, matching GitHub's own ceiling for a pull
	// request.
	MaxFiles = 3000
	// gitTimeout bounds each git call. Diffing two local commits is fast; a
	// call that takes this long is a wedged repository, not a big change.
	gitTimeout = 60 * time.Second
)

// File is one changed file.
type File struct {
	Path    string `json:"path"`
	OldPath string `json:"old_path,omitempty"`
	// Status is added, modified, deleted, renamed, copied or type_changed.
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary,omitempty"`
}

// Diff is the change between two commits.
type Diff struct {
	BaseCommit string
	HeadCommit string
	// FileCount counts every changed file, including any past MaxFiles.
	FileCount      int
	Additions      int
	Deletions      int
	Files          []File
	FilesTruncated bool
	// Patch is the unified diff, empty when PatchOmitted says why.
	Patch string
	// PatchOmitted is "too_large" when the patch passed MaxPatchBytes.
	PatchOmitted string
}

// Empty reports that the two commits carry the same tree.
func (d Diff) Empty() bool { return d.FileCount == 0 }

// Collect diffs base..head in the repository at dir. Rename detection is on;
// user diff configuration that would change the patch's shape (external diff
// drivers, textconv, prefixes, color) is overridden.
func Collect(ctx context.Context, dir, base, head string) (Diff, error) {
	diff := Diff{BaseCommit: base, HeadCommit: head}

	numstat, err := gitOutput(ctx, dir, append(diffArgs(), "--numstat", "-z", base, head)...)
	if err != nil {
		return Diff{}, fmt.Errorf("diff --numstat: %w", err)
	}
	nameStatus, err := gitOutput(ctx, dir, append(diffArgs(), "--name-status", "-z", base, head)...)
	if err != nil {
		return Diff{}, fmt.Errorf("diff --name-status: %w", err)
	}
	files, err := mergeFileLists(numstat, nameStatus)
	if err != nil {
		return Diff{}, err
	}
	diff.FileCount = len(files)
	for _, f := range files {
		diff.Additions += f.Additions
		diff.Deletions += f.Deletions
	}
	if len(files) > MaxFiles {
		files = files[:MaxFiles]
		diff.FilesTruncated = true
	}
	diff.Files = files
	if diff.FileCount == 0 {
		return diff, nil
	}

	patch, complete, err := gitOutputCapped(ctx, dir, MaxPatchBytes, append(diffArgs(), "--patch", base, head)...)
	if err != nil {
		return Diff{}, fmt.Errorf("diff --patch: %w", err)
	}
	if !complete {
		diff.PatchOmitted = "too_large"
	} else {
		diff.Patch = string(patch)
	}
	return diff, nil
}

// diffArgs pins the diff's shape against user configuration: no colour, no
// external drivers or textconv filters, standard a/ b/ prefixes, and unquoted
// non-ASCII paths so the viewer can show them as written.
func diffArgs() []string {
	return []string{
		"-c", "core.quotePath=false",
		"-c", "diff.noprefix=false",
		"-c", "diff.mnemonicPrefix=false",
		"diff",
		"--no-color", "--no-ext-diff", "--no-textconv",
		"--src-prefix=a/", "--dst-prefix=b/",
		"-M",
	}
}

// mergeFileLists joins `--numstat -z` (line counts) with `--name-status -z`
// (status letters). Both list the same files in the same order.
func mergeFileLists(numstat, nameStatus []byte) ([]File, error) {
	type counts struct {
		additions, deletions int
		binary               bool
	}
	byPath := map[string]counts{}
	fields := splitNUL(numstat)
	for i := 0; i < len(fields); i++ {
		parts := strings.SplitN(fields[i], "\t", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("unexpected numstat record %q", fields[i])
		}
		c := counts{}
		if parts[0] == "-" && parts[1] == "-" {
			c.binary = true
		} else {
			c.additions, _ = strconv.Atoi(parts[0])
			c.deletions, _ = strconv.Atoi(parts[1])
		}
		path := parts[2]
		if path == "" {
			// A rename or copy: the old and new paths follow as their own
			// NUL-terminated fields.
			if i+2 >= len(fields) {
				return nil, errors.New("truncated numstat rename record")
			}
			path = fields[i+2]
			i += 2
		}
		byPath[path] = c
	}

	var files []File
	fields = splitNUL(nameStatus)
	for i := 0; i < len(fields); i++ {
		code := fields[i]
		if code == "" {
			continue
		}
		f := File{}
		switch code[0] {
		case 'R', 'C':
			if i+2 >= len(fields) {
				return nil, errors.New("truncated name-status rename record")
			}
			f.OldPath, f.Path = fields[i+1], fields[i+2]
			f.Status = "renamed"
			if code[0] == 'C' {
				f.Status = "copied"
			}
			i += 2
		default:
			if i+1 >= len(fields) {
				return nil, errors.New("truncated name-status record")
			}
			f.Path = fields[i+1]
			i++
			switch code[0] {
			case 'A':
				f.Status = "added"
			case 'D':
				f.Status = "deleted"
			case 'T':
				f.Status = "type_changed"
			default:
				f.Status = "modified"
			}
		}
		c := byPath[f.Path]
		f.Additions, f.Deletions, f.Binary = c.additions, c.deletions, c.binary
		files = append(files, f)
	}
	return files, nil
}

func splitNUL(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	parts := strings.Split(string(b), "\x00")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// RevParse resolves ref to a full commit id in dir.
func RevParse(ctx context.Context, dir, ref string) (string, error) {
	out, err := gitOutput(ctx, dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// MergeBase returns the best common ancestor of a and b in dir.
func MergeBase(ctx context.Context, dir, a, b string) (string, error) {
	out, err := gitOutput(ctx, dir, "merge-base", a, b)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// CurrentBranch names the branch checked out in dir, or "" on a detached HEAD.
func CurrentBranch(ctx context.Context, dir string) string {
	out, err := gitOutput(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// RemoteDefaultRef resolves the remote-tracking ref a checkout's work is
// measured against: origin's HEAD when it is recorded, else origin/main or
// origin/master. Returns the short ref ("origin/main") and its commit, or
// empty strings when the repository has none of them.
func RemoteDefaultRef(ctx context.Context, dir string) (string, string) {
	candidates := []string{}
	if out, err := gitOutput(ctx, dir, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); err == nil {
		if ref := strings.TrimSpace(string(out)); ref != "" {
			candidates = append(candidates, ref)
		}
	}
	candidates = append(candidates, "refs/remotes/origin/main", "refs/remotes/origin/master")
	for _, ref := range candidates {
		if sha, err := RevParse(ctx, dir, ref); err == nil && sha != "" {
			return strings.TrimPrefix(ref, "refs/remotes/"), sha
		}
	}
	return "", ""
}

// Repository identifies a repository for display and for matching a change
// to its pull request.
type Repository struct {
	// Key stays the same across runs on the same repository.
	Key string
	// Label is what the viewer shows: owner/name for a hosted remote, the
	// directory name otherwise.
	Label string
	// URL is origin's URL with any credentials removed, or "".
	URL string
}

// Identify names the repository at dir. The origin remote wins when there is
// one — the same repository checked out in two places is one repository —
// and the fallback is the repository's own path.
func Identify(ctx context.Context, dir, fallbackPath string) Repository {
	remote := ""
	if out, err := gitOutput(ctx, dir, "config", "--get", "remote.origin.url"); err == nil {
		remote = RedactRemoteURL(strings.TrimSpace(string(out)))
	}
	if remote != "" {
		label := ownerRepoFromRemote(remote)
		if label == "" {
			label = filepath.Base(fallbackPath)
		}
		return Repository{Key: strings.ToLower(strings.TrimSuffix(remote, ".git")), Label: label, URL: remote}
	}
	return Repository{Key: "path:" + fallbackPath, Label: filepath.Base(fallbackPath)}
}

// RedactRemoteURL drops credentials, query and fragment from a remote URL.
// scp-style remotes (git@host:owner/repo) carry no secret and pass through.
func RedactRemoteURL(raw string) string {
	if raw == "" || !strings.Contains(raw, "://") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// ownerRepoFromRemote reads "owner/name" out of a hosted remote URL.
func ownerRepoFromRemote(remote string) string {
	path := remote
	if strings.Contains(remote, "://") {
		if u, err := url.Parse(remote); err == nil {
			path = u.Path
		}
	} else if i := strings.Index(remote, ":"); i >= 0 {
		path = remote[i+1:]
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[len(parts)-2] == "" || parts[len(parts)-1] == "" {
		return ""
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}

func gitCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	// Reading commits needs no lock; never take one a user's own git process
	// might be waiting on.
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

func gitOutput(ctx context.Context, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := gitCommand(ctx, dir, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%s: %w", msg, err)
		}
		return nil, err
	}
	return out, nil
}

// gitOutputCapped reads at most limit bytes of stdout. complete is false when
// there was more, in which case git is stopped rather than left to write the
// rest of a huge patch into a pipe nobody reads.
func gitOutputCapped(ctx context.Context, dir string, limit int, args ...string) (out []byte, complete bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := gitCommand(ctx, dir, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	if err := cmd.Start(); err != nil {
		return nil, false, err
	}
	buf, readErr := io.ReadAll(io.LimitReader(stdout, int64(limit)+1))
	if len(buf) > limit {
		cancel()
		_ = cmd.Wait()
		return nil, false, nil
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, false, readErr
	}
	if waitErr != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, false, fmt.Errorf("%s: %w", msg, waitErr)
		}
		return nil, false, waitErr
	}
	return buf, true, nil
}
