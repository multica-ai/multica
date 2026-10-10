package execenv

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// UntrackedReplayCheck describes the same payload PrepareLocalWorktree will
// snapshot. Paths are repo-relative; diagnostics never disclose the home path.
type UntrackedReplayCheck struct {
	Files    int
	Bytes    int64
	Symlinks int
	Largest  []UntrackedReplayPath
}

type UntrackedReplayPath struct {
	Path  string
	Files int
	Bytes int64
}

func (c UntrackedReplayCheck) Err() error {
	if c.Files <= maxUntrackedFiles && c.Bytes <= maxUntrackedBytes && c.Symlinks == 0 {
		return nil
	}
	var paths []string
	for _, p := range c.Largest {
		paths = append(paths, fmt.Sprintf("%q: %d files / %d bytes", p.Path, p.Files, p.Bytes))
	}
	return fmt.Errorf("cannot replay every untracked file: %d regular files / %d bytes (%.1f MiB); limits %d files / %d bytes (200 MiB); %d symlinks cannot be replayed; largest top-level paths: %s. Gitignore or remove the offending files to resume, or cancel and switch the resource to in_place",
		c.Files, c.Bytes, float64(c.Bytes)/(1<<20), maxUntrackedFiles, maxUntrackedBytes, c.Symlinks, strings.Join(paths, "; "))
}

// InspectUntrackedReplay reads metadata only, without creating a snapshot or
// following symlinks. Use Git's ignore rules and the replay sidecar exclusions.
// A resource may point below the repo root; the snapshot still covers the repo.
func InspectUntrackedReplay(ctx context.Context, localPath string) (UntrackedReplayCheck, error) {
	var check UntrackedReplayCheck
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	git := func(dir string, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		cmd.WaitDelay = 5 * time.Second
		out, err := cmd.Output()
		return string(out), err
	}
	root, err := git(localPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return check, fmt.Errorf("worktree preflight: locate repository: %w", err)
	}
	root = strings.TrimSpace(root)
	out, err := git(root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return check, fmt.Errorf("worktree preflight: list untracked files: %w", err)
	}
	paths := make(map[string]UntrackedReplayPath)
	for _, rel := range strings.Split(out, "\x00") {
		if rel == "" || isMulticaSidecarPath(rel) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return check, err
		}
		info, err := os.Lstat(filepath.Join(root, rel))
		if err != nil {
			continue // Match replay: a listed file may disappear during inspection.
		}
		if info.Mode()&os.ModeSymlink != 0 {
			check.Symlinks++
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		check.Files++
		check.Bytes += info.Size()
		top, _, _ := strings.Cut(rel, "/")
		p := paths[top]
		p.Path = top
		p.Files++
		p.Bytes += info.Size()
		paths[top] = p
	}
	for _, p := range paths {
		check.Largest = append(check.Largest, p)
	}
	sort.Slice(check.Largest, func(i, j int) bool {
		a, b := check.Largest[i], check.Largest[j]
		if a.Bytes != b.Bytes {
			return a.Bytes > b.Bytes
		}
		if a.Files != b.Files {
			return a.Files > b.Files
		}
		return a.Path < b.Path
	})
	if len(check.Largest) > 3 {
		check.Largest = check.Largest[:3]
	}
	return check, nil
}
