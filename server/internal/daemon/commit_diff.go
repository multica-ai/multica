package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/multica-ai/multica/server/pkg/redact"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

const commitDiffTool = "multica_commit_diff"
const commitDiffMaxFiles = 50
const commitDiffMaxBytes = 256 << 10
const commitDiffMaxRepos = 8

type commitRepo struct {
	root, base, head, leftoverRef  string
	snapshotDir, baseTree, objects string
	scope                          string
}

// commitDiffRecorder observes only configured directories and authenticated
// checkout results. It never discovers repositories from prompts or tool text.
type commitDiffRecorder struct {
	mu                  sync.Mutex
	repos               map[string]commitRepo
	registeredRepos     map[string]struct{}
	registryPath        string
	workDir             string
	repositoriesOmitted bool
}

func newCommitDiffRecorder() *commitDiffRecorder {
	return &commitDiffRecorder{
		repos:           make(map[string]commitRepo),
		registeredRepos: make(map[string]struct{}),
	}
}

// restoreCheckouts retains checkout locations across environment reuse, but
// captures a fresh HEAD for each run rather than carrying the previous baseline.
func (r *commitDiffRecorder) restoreCheckouts(rootDir, workDir string) {
	if rootDir == "" || workDir == "" {
		return
	}
	canonical, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		return
	}
	r.workDir = canonical
	r.registryPath = filepath.Join(rootDir, "commit-repositories.json")
	data, err := os.ReadFile(r.registryPath)
	if err != nil {
		return
	}
	var paths []string
	if json.Unmarshal(data, &paths) != nil {
		return
	}
	var restored []string
	for _, relative := range paths {
		if filepath.IsLocal(relative) {
			path := filepath.Join(canonical, relative)
			r.registeredRepos[path] = struct{}{}
			restored = append(restored, path)
		}
	}
	for index, path := range restored {
		r.record(path)
		if len(r.repos) == commitDiffMaxRepos {
			if index+1 < len(restored) {
				r.mu.Lock()
				r.repositoriesOmitted = true
				r.mu.Unlock()
			}
			break
		}
	}
}

func (r *commitDiffRecorder) persistCheckoutsLocked() error {
	if r.registryPath == "" {
		return nil
	}
	paths := make([]string, 0, len(r.registeredRepos))
	for root := range r.registeredRepos {
		relative, err := filepath.Rel(r.workDir, root)
		if err == nil && filepath.IsLocal(relative) {
			paths = append(paths, relative)
		}
	}
	sort.Strings(paths)
	data, err := json.Marshal(paths)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(r.registryPath), ".commit-repositories-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), r.registryPath)
}

func (r *commitDiffRecorder) record(dir string) {
	r.recordBaseline(dir, false)
}

// Replace a restored baseline only after checkout actually recreated or reset
// the directory. A kept checkout may already contain this run's agent work.
func (r *commitDiffRecorder) recordBaseline(dir string, replace bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	root, err, _ := commitGit(ctx, dir, 4096, "rev-parse", "--show-toplevel")
	if err != nil {
		return
	}
	path := strings.TrimSpace(string(root))
	canonicalDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return
	}
	canonicalDir, err = filepath.Abs(canonicalDir)
	if err != nil {
		return
	}
	scope, err := filepath.Rel(path, canonicalDir)
	if err != nil || !filepath.IsLocal(scope) {
		return
	}
	if r.workDir != "" {
		relative, err := filepath.Rel(r.workDir, path)
		if err != nil || !filepath.IsLocal(relative) {
			return
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	previous, exists := r.repos[path]
	if r.repos == nil || (exists && !replace) {
		return
	}
	if _, registered := r.registeredRepos[path]; !registered {
		r.registeredRepos[path] = struct{}{}
		if err := r.persistCheckoutsLocked(); err != nil {
			slog.Warn("persist commit diff checkout registry failed", "error", err)
		}
	}
	if !exists && len(r.repos) >= commitDiffMaxRepos {
		r.repositoriesOmitted = true
		return
	}
	repo := commitRepo{root: path, scope: filepath.ToSlash(scope)}
	head, err, _ := commitGit(ctx, path, 128, "rev-parse", "--verify", "HEAD^{commit}")
	// Keep confirmed repositories even when HEAD cannot supply a baseline.
	if err == nil {
		repo.base = strings.TrimSpace(string(head))
		if repo.openSnapshot(ctx) {
			if tree, err := repo.snapshot(ctx, repo.base); err == nil {
				repo.baseTree = tree
			}
		}
	}
	r.repos[path] = repo
	if previous.snapshotDir != "" {
		_ = os.RemoveAll(previous.snapshotDir)
	}
}
func (r *commitDiffRecorder) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, repo := range r.repos {
		if repo.snapshotDir != "" {
			_ = os.RemoveAll(repo.snapshotDir)
		}
	}
	r.repos = nil
}

// Snapshots use a disposable index and object database. The repository's
// object database supplies existing objects through an alternate.
func (repo *commitRepo) openSnapshot(ctx context.Context) bool {
	out, err, _ := commitGit(ctx, repo.root, 4096, "rev-parse", "--path-format=absolute", "--git-path", "objects")
	if err != nil {
		return false
	}
	dir, err := os.MkdirTemp("", "multica-run-diff-")
	if err != nil {
		return false
	}
	if err := os.Mkdir(filepath.Join(dir, "objects"), 0700); err != nil {
		_ = os.RemoveAll(dir)
		return false
	}
	repo.objects = strings.TrimSuffix(strings.TrimSuffix(string(out), "\n"), "\r")
	repo.snapshotDir = dir
	return true
}

func (repo commitRepo) snapshotEnv() []string {
	return []string{
		"GIT_INDEX_FILE=" + filepath.Join(repo.snapshotDir, "index"),
		"GIT_OBJECT_DIRECTORY=" + filepath.Join(repo.snapshotDir, "objects"),
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=" + strconv.Quote(repo.objects),
		"GIT_OPTIONAL_LOCKS=0",
	}
}

func (repo commitRepo) snapshot(ctx context.Context, head string) (string, error) {
	env := repo.snapshotEnv()
	if _, err, _ := runDiffGit(ctx, repo.root, env, false, 128, "read-tree", "--reset", head); err != nil {
		return "", err
	}
	// Capture working copies without running repository clean filters such as
	// Git LFS, which can write to their own caches outside the scratch store.
	keys, err, _ := runDiffGit(ctx, repo.root, env, false, 16<<10, "config", "--null", "--name-only", "--get-regexp", `^filter\..*\.(clean|process|required)$`)
	var exitErr *exec.ExitError
	if err != nil && !(errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && len(keys) == 0) {
		return "", err
	}
	var args []string
	for _, key := range strings.Split(strings.TrimSuffix(string(keys), "\x00"), "\x00") {
		if key == "" {
			continue
		}
		value := ""
		if strings.HasSuffix(key, ".required") {
			value = "false"
		}
		args = append(args, "-c", key+"="+value)
	}
	scope := "."
	if repo.scope != "" && repo.scope != "." {
		scope = ":(top,literal)" + repo.scope
	}
	args = append(args, "add", "-A", "--", scope, ":(exclude,glob)**/.agent_context/**", ":(exclude,glob)**/.multica/**")
	if _, err, _ := runDiffGit(ctx, repo.root, env, false, 128, args...); err != nil {
		return "", err
	}
	out, err, _ := runDiffGit(ctx, repo.root, env, false, 128, "write-tree")
	return strings.TrimSpace(string(out)), err
}

func (r *commitDiffRecorder) preserved(root, base string) {
	if base == "" {
		return
	}
	repo := commitRepo{root: root, base: base}
	// Only the starting commit is available here. It may contain clean-filtered
	// blobs, whereas snapshots disable clean/process filters. Without an actual
	// starting snapshot these representations cannot safely be compared.
	r.mu.Lock()
	defer r.mu.Unlock()
	r.repos[root] = repo
}

func (r *commitDiffRecorder) finalized(root, base, head, leftoverRef string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.repos[root] = commitRepo{root: root, base: base, head: head, leftoverRef: leftoverRef}
}

type commitOutput struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
	cancel    context.CancelFunc
}

func (b *commitOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - b.buffer.Len()
	if n > remaining {
		b.buffer.Write(p[:remaining])
		b.truncated = true
		b.cancel()
	} else {
		b.buffer.Write(p)
	}
	return n, nil
}

// Cancel the process when its output exceeds the budget, rather than reading
// an unbounded patch into memory and clipping it afterwards.
func commitGit(parent context.Context, dir string, limit int, args ...string) ([]byte, error, bool) {
	return runDiffGit(parent, dir, nil, true, limit, args...)
}

func runDiffGit(parent context.Context, dir string, env []string, literal bool, limit int, args ...string) ([]byte, error, bool) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	argv := []string{"-C", dir, "-c", "core.quotePath=false", "-c", "diff.noprefix=false", "-c", "diff.mnemonicPrefix=false"}
	if literal {
		argv = append(argv, "--literal-pathspecs")
	}
	if env != nil {
		argv = append(argv, "-c", "core.fsmonitor=false", "-c", "core.splitIndex=false", "-c", "core.untrackedCache=false", "-c", "core.hooksPath="+os.DevNull)
	}
	cmd := exec.CommandContext(ctx, "git", append(argv, args...)...)
	cmd.Env = append(os.Environ(), env...)
	out := &commitOutput{limit: limit, cancel: cancel}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	err := cmd.Run()
	return out.buffer.Bytes(), err, out.truncated
}

// Git's binary marker is an unprefixed metadata line. Hunk contents always
// have a space, + or - prefix, even when their text mentions "Binary files".
func isBinaryCommitPatch(patch []byte) bool {
	for _, line := range bytes.Split(patch, []byte{'\n'}) {
		if bytes.HasPrefix(line, []byte("Binary files ")) && bytes.HasSuffix(line, []byte(" differ")) {
			return true
		}
	}
	return false
}

const (
	diffCommits  = "commits"
	diffWorktree = "worktree"
	diffDraft    = "draft"
)

func repositoryDiff(repo commitRepo, kind string) map[string]any {
	identity := sha256.Sum256([]byte(repo.root))
	result := map[string]any{
		"repository": filepath.Base(repo.root), "repository_id": fmt.Sprintf("%x", identity[:8]),
		"kind": kind, "worktree": kind == diffWorktree, "changes": []map[string]any{},
	}
	if repo.base != "" {
		result["base"] = repo.base
	}
	if repo.head != "" {
		result["head"] = repo.head
	}
	return result
}

func unavailableDiff(repo commitRepo, kind string) map[string]any {
	result := repositoryDiff(repo, kind)
	result["unavailable"] = true
	return result
}

// A successful comparison can legitimately have zero changed files. Keep
// that result separate from an unavailable comparison and from other kinds.
func collectDiff(ctx context.Context, repo commitRepo, kind, from, to string, env []string, budget *int) map[string]any {
	if from == to {
		return repositoryDiff(repo, kind)
	}

	scope := repo.scope
	if scope == "" {
		scope = "."
	}
	raw, err, overflow := runDiffGit(ctx, repo.root, env, true, 64<<10, "diff", "--name-status", "-z", "--no-renames", from, to, "--", scope)
	if err != nil && !overflow {
		return unavailableDiff(repo, kind)
	}
	changes := make([]map[string]any, 0)
	// Exclude a final partial path if Git was stopped by the output limit.
	complete := bytes.LastIndexByte(raw, 0) + 1
	fields := bytes.Split(raw[:complete], []byte{0})
	truncated := overflow
	for i := 0; i+1 < len(fields) && len(fields[i]) > 0; i += 2 {
		if len(changes) == commitDiffMaxFiles {
			truncated = true
			break
		}
		status, path := string(fields[i]), string(fields[i+1])
		if path == "" {
			break
		}
		kind := "modify"
		if status == "A" {
			kind = "add"
		} else if status == "D" {
			kind = "delete"
		}
		change := map[string]any{"path": path, "kind": kind}
		limit := min(*budget, 64<<10)
		if limit <= 0 {
			change["truncated"] = true
			truncated = true
		} else {
			patch, patchErr, cut := runDiffGit(ctx, repo.root, env, true, limit, "diff", "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames", "--unified=3", from, to, "--", path)
			*budget -= len(patch)
			switch {
			case cut:
				change["truncated"] = true
				truncated = true
			case patchErr != nil:
				change["unavailable"] = true
			case isBinaryCommitPatch(patch):
				change["binary"] = true
			default:
				// Oversized patches are discarded above. Scrub complete patch
				// bodies here so older servers never receive raw credentials.
				change["diff"] = redact.Text(string(patch))
			}
		}
		changes = append(changes, change)
	}
	result := repositoryDiff(repo, kind)
	result["changes"] = changes
	result["truncated"] = truncated
	return result
}

func collectDraftDiff(ctx context.Context, repo commitRepo, budget *int) map[string]any {
	delivered := repo.head
	repo.base = delivered
	repo.head = ""
	out, err, _ := commitGit(ctx, repo.root, 128, "rev-parse", "--verify", "--end-of-options", repo.leftoverRef+"^{commit}")
	if err != nil {
		return unavailableDiff(repo, diffDraft)
	}
	repo.head = strings.TrimSpace(string(out))
	return collectDiff(ctx, repo, diffDraft, delivered, repo.head, nil, budget)
}

func collectWorktreeDiff(ctx context.Context, repo commitRepo, budget *int) map[string]any {
	if repo.baseTree == "" || repo.head == "" {
		return unavailableDiff(repo, diffWorktree)
	}
	snapshotCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	tree, err := repo.snapshot(snapshotCtx, repo.head)
	cancel()
	if err != nil || tree == "" {
		return unavailableDiff(repo, diffWorktree)
	}
	return collectDiff(ctx, repo, diffWorktree, repo.baseTree, tree, repo.snapshotEnv(), budget)
}

func collectRunDiffs(ctx context.Context, repos []commitRepo, includeWorktree bool) []map[string]any {
	budget := commitDiffMaxBytes
	var results []map[string]any
	var worktrees []commitRepo
	for _, repo := range repos {
		finalized := repo.head != ""
		if !finalized {
			out, err, _ := commitGit(ctx, repo.root, 128, "rev-parse", "--verify", "HEAD^{commit}")
			if err == nil {
				repo.head = strings.TrimSpace(string(out))
			}
			worktrees = append(worktrees, repo)
		}
		if repo.base == "" || repo.head == "" {
			results = append(results, unavailableDiff(repo, diffCommits))
		} else if repo.base != repo.head {
			results = append(results, collectDiff(ctx, repo, diffCommits, repo.base, repo.head, nil, &budget))
		}
		if repo.leftoverRef != "" {
			results = append(results, collectDraftDiff(ctx, repo, &budget))
		}
	}
	// Preserve the commit/draft records within the shared budget even when
	// capturing working trees takes the rest of the collection deadline.
	for _, repo := range worktrees {
		if !includeWorktree {
			repo.baseTree = ""
		}
		results = append(results, collectWorktreeDiff(ctx, repo, &budget))
	}
	return results
}

func (d *Daemon) reportCommitDiff(taskID string, r *commitDiffRecorder, seq *atomic.Int32, logger *slog.Logger, includeWorktree bool, failureReason string) {
	r.mu.Lock()
	repos := make([]commitRepo, 0, len(r.repos))
	for _, repo := range r.repos {
		repos = append(repos, repo)
	}
	repositoriesOmitted := r.repositoriesOmitted
	r.mu.Unlock()
	sort.Slice(repos, func(i, j int) bool { return repos[i].root < repos[j].root })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// A provider may still be writing to a checkout after unconfirmed cleanup.
	inputs := collectRunDiffs(ctx, repos, includeWorktree && failureReason != taskfailure.ReasonProcessCleanupUnconfirmed.String())
	if len(inputs) == 0 {
		return
	}
	messages := make([]TaskMessageData, 0, len(inputs))
	for _, input := range inputs {
		if repositoriesOmitted {
			input["repositories_omitted"] = true
		}
		messages = append(messages, TaskMessageData{Seq: int(seq.Add(1)), Type: "tool_use", Tool: commitDiffTool, Input: input, CreatedAt: time.Now().UTC()})
	}
	// The collector can exhaust its deadline and still have status records to send.
	reportCtx, stopReport := context.WithTimeout(context.Background(), 3*time.Second)
	defer stopReport()
	if err := d.client.ReportTaskMessages(reportCtx, taskID, messages); err != nil {
		logger.Warn("report run diff failed", "error", err)
	}
}
