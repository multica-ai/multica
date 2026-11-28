// Package codechange records what a task run changed in the git checkouts
// under its working directory, so the run's page can show the changes after
// the daemon has removed the working directory.
//
// For each checkout the snapshot is the diff from where the run started to the
// working tree at the end of the run: commits the agent made, uncommitted
// edits and new untracked files (.gitignore respected). Everything is computed
// through a throwaway copy of the checkout's index, so its real index, files
// and refs are never touched.
package codechange

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// MaxPatchBytes caps one checkout's patch. Past it the file list still
	// says what changed, and the patch is cut at a file boundary.
	MaxPatchBytes = 512 << 10
	// MaxFiles caps one checkout's file list.
	MaxFiles = 1000
	// MaxReportRepos and MaxReportBytes bound one upload below the server's
	// limits (20 repos, 4 MiB body). Fit drops patches before file lists, and
	// file lists before whole checkouts.
	MaxReportRepos = 20
	MaxReportBytes = 3 << 20
	// maxRepos and maxDepth bound the checkout scan. Submodules count as
	// checkouts: in a superproject the agent's edits live inside them.
	maxRepos = 60
	maxDepth = 3
	// gitTimeout bounds every git subprocess, and captureTimeout one whole
	// Capture, which runs before the agent starts. A checkout left without a
	// baseline falls back to defaultBase in Snapshot.
	gitTimeout     = 30 * time.Second
	captureTimeout = 30 * time.Second
)

// File is one changed path.
type File struct {
	Path      string `json:"path"`
	OldPath   string `json:"old_path,omitempty"`
	Status    string `json:"status"` // added | modified | deleted | renamed
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary,omitempty"`
}

// Repo is one checkout's changes. Path is relative to the scanned directory
// ("." when the directory is the checkout itself). Base is the commit the run
// started from.
type Repo struct {
	Path           string `json:"path"`
	Branch         string `json:"branch,omitempty"`
	Base           string `json:"base"`
	Head           string `json:"head,omitempty"`
	Files          []File `json:"files"`
	FilesTruncated bool   `json:"files_truncated,omitempty"`
	Patch          string `json:"patch"`
	PatchTruncated bool   `json:"patch_truncated,omitempty"`
}

// baseline is where one checkout started. Tree, when set, is the working tree
// at that moment (uncommitted and untracked files included), so work that was
// already there before the run is not reported as the run's.
type baseline struct {
	head string
	tree string
}

// Baselines remembers where each checkout started. It is safe for concurrent
// use: checkouts the agent creates mid-run are recorded from the daemon's
// repo-checkout handler while the run is in progress.
type Baselines struct {
	mu sync.Mutex
	m  map[string]baseline
}

func NewBaselines() *Baselines {
	return &Baselines{m: map[string]baseline{}}
}

// Capture records a baseline for every checkout at or below dir that has none
// yet. With workingTree, the baseline includes the checkout's current
// uncommitted and untracked files; without it, only HEAD (for a checkout whose
// working tree already holds files the daemon wrote, which it removes again
// before the snapshot).
func (b *Baselines) Capture(ctx context.Context, dir string, workingTree bool) {
	if b == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, captureTimeout)
	defer cancel()
	for _, repo := range Find(ctx, dir) {
		key := pathKey(repo)
		b.mu.Lock()
		_, seen := b.m[key]
		b.mu.Unlock()
		if seen {
			continue
		}
		bl := baseline{}
		if head, err := git(ctx, repo, nil, "rev-parse", "--verify", "-q", "HEAD"); err == nil {
			bl.head = strings.TrimSpace(head)
		}
		if workingTree {
			if tree, err := workingTreeID(ctx, repo, bl.head); err == nil {
				bl.tree = tree
			}
		}
		b.mu.Lock()
		if _, seen := b.m[key]; !seen {
			b.m[key] = bl
		}
		b.mu.Unlock()
	}
}

func (b *Baselines) get(dir string) (baseline, bool) {
	if b == nil {
		return baseline{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	bl, ok := b.m[pathKey(dir)]
	return bl, ok
}

func pathKey(dir string) string {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return real
	}
	return filepath.Clean(dir)
}

// Find returns the top-level directories of the git checkouts at or below
// root (depth-bounded, skipping dependency and runtime directories), followed
// by their initialized submodules.
func Find(ctx context.Context, root string) []string {
	if root == "" {
		return nil
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil
	}
	if isToplevel(ctx, root) {
		return withSubmodules(ctx, []string{root})
	}
	var found []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || path == root {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if strings.Count(rel, string(filepath.Separator))+1 > maxDepth || skipDir(d.Name()) {
			return filepath.SkipDir
		}
		if _, statErr := os.Lstat(filepath.Join(path, ".git")); statErr == nil {
			found = append(found, path)
			if len(found) >= maxRepos {
				return fs.SkipAll
			}
			return filepath.SkipDir
		}
		return nil
	})
	sort.Strings(found)
	return withSubmodules(ctx, found)
}

// withSubmodules appends every initialized submodule (recursively) of each
// checkout, capped at maxRepos in total.
func withSubmodules(ctx context.Context, repos []string) []string {
	out := append([]string{}, repos...)
	for _, repo := range repos {
		status, err := git(ctx, repo, nil, "submodule", "status", "--recursive")
		if err != nil {
			continue
		}
		for _, line := range strings.Split(status, "\n") {
			// " <sha> <path> (<describe>)"; a leading "-" marks one that is
			// not initialized.
			if len(line) < 2 || line[0] == '-' {
				continue
			}
			fields := strings.Fields(line[1:])
			if len(fields) < 2 {
				continue
			}
			out = append(out, filepath.Join(repo, filepath.FromSlash(fields[1])))
			if len(out) >= maxRepos {
				return out
			}
		}
	}
	return out
}

func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".venv", "venv", "__pycache__", ".multica", ".agent_context", ".claude":
		return true
	}
	return false
}

// Result is a snapshot of every checkout under the scanned directory.
type Result struct {
	// Repos lists the checkouts that changed; unchanged ones are left out.
	Repos []Repo
	// Checkouts is how many checkouts were inspected, changed or not.
	Checkouts int
	// ScanCapped reports that the scan stopped at its cap, so some checkouts
	// may be missing.
	ScanCapped bool
}

// Snapshot computes the changes of every checkout under root against its
// baseline. A checkout with no baseline (cloned mid-run without going through
// the daemon) is measured from where it forked from its upstream, else from
// HEAD. An error in one checkout skips just that checkout.
func Snapshot(ctx context.Context, root string, baselines *Baselines) Result {
	root, err := filepath.Abs(root)
	if err != nil {
		return Result{}
	}
	dirs := Find(ctx, root)
	res := Result{Repos: []Repo{}, Checkouts: len(dirs), ScanCapped: len(dirs) >= maxRepos}
	for _, dir := range dirs {
		bl, _ := baselines.get(dir)
		repo, err := snapshotRepo(ctx, root, dir, bl)
		if err != nil || len(repo.Files) == 0 {
			continue
		}
		res.Repos = append(res.Repos, repo)
	}
	return res
}

// excludeRuntimeDirs keeps the daemon's own per-task directories out of every
// `git add`, in case a checkout does not ignore them.
var excludeRuntimeDirs = []string{"--", ".", ":(exclude).multica", ":(exclude).agent_context"}

func snapshotRepo(ctx context.Context, root, dir string, bl baseline) (Repo, error) {
	rel, _ := filepath.Rel(root, dir)
	repo := Repo{Path: filepath.ToSlash(rel), Files: []File{}}
	head, _ := git(ctx, dir, nil, "rev-parse", "--verify", "-q", "HEAD")
	repo.Head = strings.TrimSpace(head)
	branch, _ := git(ctx, dir, nil, "symbolic-ref", "--short", "-q", "HEAD")
	repo.Branch = strings.TrimSpace(branch)
	repo.Base = bl.head
	if repo.Base == "" && bl.tree == "" {
		repo.Base = defaultBase(ctx, dir, repo.Head)
	}
	from := bl.tree
	if from == "" {
		from = repo.Base
	}
	if from == "" {
		// Unborn branch: everything in the working tree is new.
		from = emptyTree
	}

	// Nothing committed since a clean start and nothing in the working tree:
	// skip the index work. This is the common case for most submodules.
	if bl.tree == "" && repo.Base != "" && repo.Base == repo.Head {
		status, err := git(ctx, dir, []string{"GIT_OPTIONAL_LOCKS=0"}, "status", "--porcelain", "--untracked-files=normal", "--ignore-submodules=dirty")
		if err == nil && strings.TrimSpace(status) == "" {
			return repo, nil
		}
	}

	tmpIndex, err := tempIndex(ctx, dir, repo.Head)
	if err != nil {
		return repo, err
	}
	defer os.Remove(tmpIndex)
	env := []string{"GIT_INDEX_FILE=" + tmpIndex, "GIT_OPTIONAL_LOCKS=0"}
	if _, err := git(ctx, dir, env, append([]string{"add", "-A"}, excludeRuntimeDirs...)...); err != nil {
		return repo, err
	}
	diff := []string{"diff", "--cached", "-M", "--no-color", "--no-ext-diff", "--no-textconv", from}
	numstat, err := git(ctx, dir, env, append(append([]string{}, diff...), "--numstat", "-z")...)
	if err != nil {
		return repo, err
	}
	nameStatus, err := git(ctx, dir, env, append(append([]string{}, diff...), "--name-status", "-z")...)
	if err != nil {
		return repo, err
	}
	repo.Files, repo.FilesTruncated = parseFiles(nameStatus, numstat)
	if len(repo.Files) == 0 {
		return repo, nil
	}
	patch, err := git(ctx, dir, env, diff...)
	if err != nil {
		return repo, err
	}
	repo.Patch, repo.PatchTruncated = capPatch(patch, MaxPatchBytes)
	return repo, nil
}

// workingTreeID writes the checkout's current working tree (tracked changes
// and untracked files, .gitignore respected) as a tree object and returns its
// id. Only the object database is written to.
func workingTreeID(ctx context.Context, dir, head string) (string, error) {
	tmpIndex, err := tempIndex(ctx, dir, head)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpIndex)
	env := []string{"GIT_INDEX_FILE=" + tmpIndex, "GIT_OPTIONAL_LOCKS=0"}
	if _, err := git(ctx, dir, env, append([]string{"add", "-A"}, excludeRuntimeDirs...)...); err != nil {
		return "", err
	}
	tree, err := git(ctx, dir, env, "write-tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(tree), nil
}

// tempIndex returns a temp file holding a copy of dir's index, or the head
// tree read into a fresh index when the checkout has none (fresh clone,
// unborn branch). Copying the real index keeps git's stat cache, so `add -A`
// re-reads only files that actually changed. The caller removes the file.
func tempIndex(ctx context.Context, dir, head string) (string, error) {
	tmp, err := os.CreateTemp("", "codechange-index-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	copied := false
	if indexPath, err := git(ctx, dir, nil, "rev-parse", "--path-format=absolute", "--git-path", "index"); err == nil {
		if src, err := os.Open(strings.TrimSpace(indexPath)); err == nil {
			n, copyErr := io.Copy(tmp, src)
			src.Close()
			if copyErr != nil {
				tmp.Close()
				os.Remove(name)
				return "", copyErr
			}
			copied = n > 0
		}
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	if !copied {
		treeish := head
		if treeish == "" {
			treeish = emptyTree
		}
		// read-tree wants to create the index itself.
		os.Remove(name)
		if _, err := git(ctx, dir, []string{"GIT_INDEX_FILE=" + name}, "read-tree", treeish); err != nil {
			os.Remove(name)
			return "", err
		}
	}
	return name, nil
}

// Fit trims a snapshot to one upload's budget: past MaxReportRepos whole
// checkouts are dropped (reported as reposDropped), then the largest patches
// go (each file list still says what changed), then the longest file lists
// are halved. Trimmed parts are flagged on their Repo.
func Fit(repos []Repo) (kept []Repo, reposDropped bool) {
	if len(repos) > MaxReportRepos {
		repos, reposDropped = repos[:MaxReportRepos], true
	}
	size := encodedSize(repos)
	for size > MaxReportBytes {
		i := largest(repos, func(r Repo) int { return len(r.Patch) })
		if i < 0 {
			break
		}
		repos[i].Patch, repos[i].PatchTruncated = "", true
		size = encodedSize(repos)
	}
	for size > MaxReportBytes {
		i := largest(repos, func(r Repo) int { return len(r.Files) })
		if i < 0 || len(repos[i].Files) <= 1 {
			break
		}
		repos[i].Files = repos[i].Files[:len(repos[i].Files)/2]
		repos[i].FilesTruncated = true
		size = encodedSize(repos)
	}
	return repos, reposDropped
}

func encodedSize(repos []Repo) int {
	b, err := json.Marshal(repos)
	if err != nil {
		return 0
	}
	return len(b)
}

func largest(repos []Repo, size func(Repo) int) int {
	best, bestSize := -1, 0
	for i, r := range repos {
		if s := size(r); s > bestSize {
			best, bestSize = i, s
		}
	}
	return best
}

// emptyTree is git's well-known empty tree object.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// defaultBase is the starting point for a checkout with no recorded
// baseline: where it forked from its upstream, else HEAD itself (so only
// uncommitted work shows).
func defaultBase(ctx context.Context, dir, head string) string {
	if head == "" {
		return ""
	}
	if mb, err := git(ctx, dir, nil, "merge-base", "HEAD", "@{upstream}"); err == nil {
		if s := strings.TrimSpace(mb); s != "" {
			return s
		}
	}
	return head
}

// isToplevel reports whether dir is the top level of a git checkout. A
// directory merely inside one (a home directory under version control, say)
// does not count: the run's working directory must not pull in a parent repo.
func isToplevel(ctx context.Context, dir string) bool {
	out, err := git(ctx, dir, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return false
	}
	top, err := filepath.EvalSymlinks(strings.TrimSpace(out))
	if err != nil {
		return false
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	return top == real
}

// parseFiles joins `--name-status -z` and `--numstat -z` output.
func parseFiles(nameStatus, numstat string) ([]File, bool) {
	type counts struct {
		add, del int
		binary   bool
	}
	stats := map[string]counts{}
	fields := strings.Split(numstat, "\x00")
	for i := 0; i < len(fields); i++ {
		parts := strings.SplitN(fields[i], "\t", 3)
		if len(parts) < 3 {
			continue
		}
		path := parts[2]
		if path == "" && i+2 < len(fields) {
			// Rename: "<add>\t<del>\t" then the old path and the new path.
			path = fields[i+2]
			i += 2
		}
		c := counts{}
		if parts[0] == "-" {
			c.binary = true
		} else {
			c.add, _ = strconv.Atoi(parts[0])
			c.del, _ = strconv.Atoi(parts[1])
		}
		stats[path] = c
	}

	files := []File{}
	truncated := false
	ns := strings.Split(nameStatus, "\x00")
	for i := 0; i < len(ns); i++ {
		code := ns[i]
		if code == "" {
			continue
		}
		if len(files) >= MaxFiles {
			truncated = true
			break
		}
		f := File{}
		switch code[0] {
		case 'R', 'C':
			if i+2 >= len(ns) {
				i = len(ns)
				continue
			}
			f.OldPath, f.Path, f.Status = ns[i+1], ns[i+2], "renamed"
			i += 2
		default:
			if i+1 >= len(ns) {
				i = len(ns)
				continue
			}
			f.Path = ns[i+1]
			i++
			switch code[0] {
			case 'A':
				f.Status = "added"
			case 'D':
				f.Status = "deleted"
			default:
				f.Status = "modified"
			}
		}
		c := stats[f.Path]
		f.Additions, f.Deletions, f.Binary = c.add, c.del, c.binary
		files = append(files, f)
	}
	return files, truncated
}

// capPatch cuts a patch at the last file boundary that fits.
func capPatch(patch string, limit int) (string, bool) {
	if len(patch) <= limit {
		return patch, false
	}
	cut := strings.LastIndex(patch[:limit], "\ndiff --git ")
	if cut <= 0 {
		return "", true
	}
	return patch[:cut+1], true
}

func git(ctx context.Context, dir string, extraEnv []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	full := append([]string{"-C", dir, "-c", "core.quotepath=off", "-c", "diff.noprefix=false", "-c", "diff.mnemonicprefix=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	cmd.Env = append(cmd.Env, extraEnv...)
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.String(), fmt.Errorf("git %s: %s: %w", args[0], strings.TrimSpace(stderr.String()), err)
		}
		return stdout.String(), err
	}
	return stdout.String(), nil
}
