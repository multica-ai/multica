package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func commitTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_COUNT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
func commitTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	commitTestGit(t, dir, "init", "-b", "main")
	for k, v := range map[string]string{"user.name": "Test", "user.email": "test@example.com", "commit.gpgsign": "false", "gc.auto": "0", "maintenance.auto": "false"} {
		commitTestGit(t, dir, "config", k, v)
	}
	commitTestWrite(t, dir, "file.txt", "original\n")
	commitTestCommit(t, dir)
	return dir
}
func commitTestWrite(t *testing.T, dir, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func commitTestCommit(t *testing.T, dir string) string {
	t.Helper()
	commitTestGit(t, dir, "add", "-A")
	commitTestGit(t, dir, "commit", "-m", "test")
	return commitTestGit(t, dir, "rev-parse", "HEAD")
}
func TestCommitDiffCapturesOnlyThisRunsFileChanges(t *testing.T) {
	dir := commitTestRepo(t)
	r := newCommitDiffRecorder()
	t.Cleanup(r.close)
	r.record(dir)
	commitTestWrite(t, dir, "committed.txt", "committed change\n")
	head := commitTestCommit(t, dir)
	commitTestWrite(t, dir, "file.txt", "uncommitted change\n")
	commitTestWrite(t, dir, "not-committed.txt", "new file\n")
	before := commitTestGit(t, dir, "status", "--porcelain")
	var repo commitRepo
	for _, v := range r.repos {
		repo = v
	}
	diffs := collectRunDiffs(context.Background(), []commitRepo{repo}, true)
	if len(diffs) != 2 || diffs[0]["kind"] != diffCommits || diffs[1]["kind"] != diffWorktree {
		t.Fatalf("expected independent commit and working-tree comparisons, got %v", diffs)
	}
	commits, _ := json.Marshal(diffs[0])
	worktree, _ := json.Marshal(diffs[1])
	if diffs[0]["head"] != head || strings.Contains(string(commits), "not-committed.txt") ||
		!strings.Contains(string(worktree), "+uncommitted change") || !strings.Contains(string(worktree), "not-committed.txt") ||
		!strings.Contains(string(worktree), "+committed change") {
		t.Fatalf("unexpected comparisons: %s / %s", commits, worktree)
	}
	if after := commitTestGit(t, dir, "status", "--porcelain"); after != before {
		t.Fatalf("collector mutated repository: %q -> %q", before, after)
	}
	next := newCommitDiffRecorder()
	t.Cleanup(next.close)
	next.record(dir)
	for _, v := range next.repos {
		diffs := collectRunDiffs(context.Background(), []commitRepo{v}, true)
		if len(diffs) != 1 || diffs[0]["kind"] != diffWorktree || diffs[0]["unavailable"] == true ||
			len(diffs[0]["changes"].([]map[string]any)) != 0 {
			t.Fatalf("unchanged drafts must not reappear in a later run: %v", diffs)
		}
	}
}
func TestCommitDiffUsesFinalizationReceiptAfterWorktreeRemoval(t *testing.T) {
	dir := commitTestRepo(t)
	base := commitTestGit(t, dir, "rev-parse", "HEAD")
	commitTestWrite(t, dir, "file.txt", "first turn\n")
	head := commitTestCommit(t, dir)
	commitTestWrite(t, dir, "file.txt", "later turn\n")
	commitTestCommit(t, dir)
	diffs := collectRunDiffs(context.Background(), []commitRepo{{root: dir, base: base, head: head}}, true)
	if len(diffs) != 1 || diffs[0]["kind"] != diffCommits {
		t.Fatalf("expected the exact delivered range: %v", diffs)
	}
	got := diffs[0]
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "later turn") || !strings.Contains(string(raw), "first turn") {
		t.Fatalf("read mutable HEAD instead of receipt: %s", raw)
	}
}
func TestCommitDiffHandlesPathsAndBinaryFiles(t *testing.T) {
	dir := commitTestRepo(t)
	base := commitTestGit(t, dir, "rev-parse", "HEAD")
	name := "中文 name[1].txt"
	commitTestWrite(t, dir, name, "hello\n")
	commitTestWrite(t, dir, "binary.dat", "\x00\x01\x02")
	commitTestCommit(t, dir)
	budget := commitDiffMaxBytes
	got := collectDiff(context.Background(), commitRepo{root: dir, base: base}, diffCommits, base, "HEAD", nil, &budget)
	files := got["changes"].([]map[string]any)
	found, binary := false, false
	for _, file := range files {
		if file["path"] == name {
			found = strings.Contains(file["diff"].(string), "hello")
		}
		if file["path"] == "binary.dat" {
			binary = file["binary"] == true
		}
	}
	if !found || !binary {
		t.Fatalf("lost paths or binary state: %v", files)
	}
}
func TestCommitDiffStopsOversizedPatches(t *testing.T) {
	dir := commitTestRepo(t)
	base := commitTestGit(t, dir, "rev-parse", "HEAD")
	commitTestWrite(t, dir, "file.txt", strings.Repeat("changed line\n", 20000))
	commitTestCommit(t, dir)
	budget := 2048
	got := collectDiff(context.Background(), commitRepo{root: dir, base: base}, diffCommits, base, "HEAD", nil, &budget)
	file := got["changes"].([]map[string]any)[0]
	if got["truncated"] != true || file["truncated"] != true || file["diff"] != nil || budget < 0 {
		t.Fatalf("unbounded/partial patch: truncated=%v file-truncated=%v patch-bytes=%d budget=%d", got["truncated"], file["truncated"], len(fmt.Sprint(file["diff"])), budget)
	}
}

func TestCommitDiffReportsThroughTaskMessages(t *testing.T) {
	dir := commitTestRepo(t)
	r := newCommitDiffRecorder()
	t.Cleanup(r.close)
	r.record(dir)
	commitTestWrite(t, dir, "added.txt", "new\n")
	if err := os.Remove(filepath.Join(dir, "file.txt")); err != nil {
		t.Fatal(err)
	}
	commitTestCommit(t, dir)
	reports := make(chan []TaskMessageData, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/daemon/tasks/task-1/messages" {
			t.Errorf("unexpected endpoint: %s", req.URL.Path)
		}
		var body struct {
			Messages []TaskMessageData `json:"messages"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		reports <- body.Messages
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	d := &Daemon{client: NewClient(srv.URL)}
	var seq atomic.Int32
	seq.Store(7)
	report := func(failureReason string) []TaskMessageData {
		t.Helper()
		d.reportCommitDiff("task-1", r, &seq, slog.Default(), true, failureReason)
		select {
		case messages := <-reports:
			return messages
		default:
			t.Fatal("no diff messages reported")
			return nil
		}
	}
	received := report("")
	if len(received) != 2 {
		t.Fatalf("messages=%d", len(received))
	}
	msg := received[0]
	if msg.Input["kind"] != diffCommits || received[1].Input["kind"] != diffWorktree || received[1].Input["unavailable"] == true {
		t.Fatalf("expected both comparisons to be available: %+v", received)
	}
	if msg.Tool != commitDiffTool || msg.Seq != 8 || msg.Type != "tool_use" {
		t.Fatalf("wrong report identity: %+v", msg)
	}
	raw, _ := json.Marshal(msg.Input)
	if strings.Contains(string(raw), dir) {
		t.Fatal("absolute repository path leaked into payload")
	}
	changes := msg.Input["changes"].([]any)
	kinds := map[string]any{}
	for _, value := range changes {
		file := value.(map[string]any)
		kinds[file["path"].(string)] = file["kind"]
	}
	if kinds["added.txt"] != "add" || kinds["file.txt"] != "delete" {
		t.Fatalf("wrong statuses: %v", kinds)
	}

	commitTestWrite(t, dir, "still-changing.txt", "unfinished\n")
	received = report(taskfailure.ReasonProcessCleanupUnconfirmed.String())
	if len(received) != 2 || received[0].Input["kind"] != diffCommits || received[0].Input["unavailable"] == true ||
		received[1].Input["kind"] != diffWorktree || received[1].Input["unavailable"] != true ||
		len(received[1].Input["changes"].([]any)) != 0 {
		t.Fatalf("expected commit changes and unavailable worktree: %+v", received)
	}
}
func TestCommitDiffFileLimitIsExplicit(t *testing.T) {
	dir := commitTestRepo(t)
	base := commitTestGit(t, dir, "rev-parse", "HEAD")
	for i := 0; i < commitDiffMaxFiles+1; i++ {
		commitTestWrite(t, dir, fmt.Sprintf("file-%03d.txt", i), "new\n")
	}
	commitTestCommit(t, dir)
	budget := commitDiffMaxBytes
	got := collectDiff(context.Background(), commitRepo{root: dir, base: base}, diffCommits, base, "HEAD", nil, &budget)
	if got["truncated"] != true || len(got["changes"].([]map[string]any)) != commitDiffMaxFiles {
		t.Fatal("file limit must be visible")
	}
}

func TestCommitDiffRestoresCheckoutWithFreshBaseline(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "workdir")
	if err := os.MkdirAll(workDir, 0700); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(workDir, "project")
	if err := os.Rename(commitTestRepo(t), repo); err != nil {
		t.Fatal(err)
	}
	first := newCommitDiffRecorder()
	t.Cleanup(first.close)
	first.restoreCheckouts(root, workDir)
	first.record(repo)
	commitTestWrite(t, repo, "first-only.txt", "first run\n")
	firstHead := commitTestCommit(t, repo)

	// A new recorder models environment reuse after a daemon restart. This turn
	// does not call repo checkout, and must start from the previous turn's HEAD.
	second := newCommitDiffRecorder()
	t.Cleanup(second.close)
	second.restoreCheckouts(root, workDir)
	if len(second.repos) != 1 {
		t.Fatalf("registered checkout was lost: %v", second.repos)
	}
	commitTestWrite(t, repo, "second-only.txt", "second run\n")
	secondHead := commitTestCommit(t, repo)
	for _, registered := range second.repos {
		diffs := collectRunDiffs(context.Background(), []commitRepo{registered}, true)
		if len(diffs) != 2 || diffs[1]["kind"] != diffWorktree {
			t.Fatalf("expected both comparisons, got %v", diffs)
		}
		diff := diffs[1]
		if diff["base"] != firstHead || diff["head"] != secondHead {
			t.Fatalf("stale range: %v -> %v", diff["base"], diff["head"])
		}
		files := diff["changes"].([]map[string]any)
		if len(files) != 1 || files[0]["path"] != "second-only.txt" {
			t.Fatalf("wrong turn's files: %v", files)
		}
	}
}

func TestCommitDiffKeepsTextMentioningBinaryMarker(t *testing.T) {
	repo := commitTestRepo(t)
	base := commitTestGit(t, repo, "rev-parse", "HEAD")
	commitTestWrite(t, repo, "file.txt", "Binary files a/test and b/test differ\n")
	commitTestCommit(t, repo)
	budget := commitDiffMaxBytes
	diff := collectDiff(context.Background(), commitRepo{root: repo, base: base}, diffCommits, base, "HEAD", nil, &budget)
	file := diff["changes"].([]map[string]any)[0]
	if file["binary"] == true || !strings.Contains(fmt.Sprint(file["diff"]), "+Binary files a/test and b/test differ") {
		t.Fatal("ordinary text was classified as binary")
	}
}

func TestCommitDiffReportsUnavailableWorktreeWithUnchangedHead(t *testing.T) {
	dir := commitTestRepo(t)
	recorder := newCommitDiffRecorder()
	t.Cleanup(recorder.close)
	recorder.record(dir)
	var repo commitRepo
	for _, registered := range recorder.repos {
		repo = registered
	}
	if repo.baseTree == "" {
		t.Fatal("fixture did not capture its starting tree")
	}
	commitTestWrite(t, dir, "file.txt", "uncommitted edit\n")
	// Let Git see a real cancelled snapshot without waiting on a slow disk.
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	withHead := repo
	withHead.head = repo.base
	budget := commitDiffMaxBytes
	if diff := collectWorktreeDiff(ctx, withHead, &budget); diff["unavailable"] != true {
		t.Fatalf("expired capture deadline was not reported: %v", diff)
	}
	// A failed starting snapshot is represented by an absent starting tree.
	repo.baseTree = ""
	diffs := collectRunDiffs(context.Background(), []commitRepo{repo}, true)
	if len(diffs) != 1 || diffs[0]["kind"] != diffWorktree || diffs[0]["unavailable"] != true || diffs[0]["head"] != repo.base {
		t.Fatalf("capture failure must remain visible with no commits: %v", diffs)
	}
}

func TestCommitDiffReportsUnavailableBaselineForFirstCommit(t *testing.T) {
	dir := t.TempDir()
	commitTestGit(t, dir, "init", "-b", "main")
	recorder := newCommitDiffRecorder()
	t.Cleanup(recorder.close)
	recorder.record(dir)

	commitTestWrite(t, dir, "file.txt", "first commit\n")
	commitTestGit(t, dir, "add", "--", "file.txt")
	commitTestGit(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "first")
	head := commitTestGit(t, dir, "rev-parse", "HEAD")

	repos := make([]commitRepo, 0, len(recorder.repos))
	for _, repo := range recorder.repos {
		repos = append(repos, repo)
	}
	diffs := collectRunDiffs(context.Background(), repos, true)
	if len(diffs) != 2 || diffs[0]["kind"] != diffCommits || diffs[1]["kind"] != diffWorktree {
		t.Fatalf("expected status for both comparisons, got %v", diffs)
	}
	for _, diff := range diffs {
		if diff["unavailable"] != true || diff["base"] != nil || diff["head"] != head {
			t.Fatalf("expected an unavailable comparison with no baseline, got %v", diff)
		}
	}
}

func TestCommitDiffPreservedWorktreeWithCleanFilter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("filter fixture uses POSIX sed")
	}
	dir := commitTestRepo(t)
	commitTestGit(t, dir, "config", "filter.fixture.clean", "sed s/WORKING/STORED/g")
	commitTestGit(t, dir, "config", "filter.fixture.smudge", "sed s/STORED/WORKING/g")
	commitTestWrite(t, dir, ".gitattributes", "filtered.txt filter=fixture\n")
	commitTestWrite(t, dir, "filtered.txt", "WORKING\n")
	base := commitTestCommit(t, dir)
	if status := commitTestGit(t, dir, "status", "--porcelain"); status != "" {
		t.Fatalf("filter fixture must be clean, got %q", status)
	}
	recorder := newCommitDiffRecorder()
	t.Cleanup(recorder.close)
	recorder.preserved(dir, base)
	var repo commitRepo
	for _, registered := range recorder.repos {
		repo = registered
	}
	// A clean filtered file must never be reported as a run change.
	diffs := collectRunDiffs(context.Background(), []commitRepo{repo}, true)
	if len(diffs) != 1 || diffs[0]["kind"] != diffWorktree || diffs[0]["unavailable"] != true ||
		len(diffs[0]["changes"].([]map[string]any)) != 0 {
		t.Fatalf("expected unavailable working-tree comparison, got %v", diffs)
	}
	commitTestWrite(t, dir, "agent.txt", "committed output\n")
	head := commitTestCommit(t, dir)
	commitTestWrite(t, dir, "draft.txt", "uncommitted draft\n")
	diffs = collectRunDiffs(context.Background(), []commitRepo{repo}, true)
	if len(diffs) != 2 || diffs[0]["kind"] != diffCommits || diffs[0]["base"] != base || diffs[0]["head"] != head ||
		diffs[1]["kind"] != diffWorktree || diffs[1]["unavailable"] != true {
		t.Fatalf("valid commit range was not retained: %v", diffs)
	}
	files := diffs[0]["changes"].([]map[string]any)
	if len(files) != 1 || files[0]["path"] != "agent.txt" || !strings.Contains(fmt.Sprint(files[0]["diff"]), "+committed output") {
		t.Fatalf("expected only the actual committed change, got %v", files)
	}
}

func TestCommitDiffReportsRepositoryLimit(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "workdir")
	if err := os.Mkdir(workDir, 0700); err != nil {
		t.Fatal(err)
	}
	newRepo := func(index int) string {
		t.Helper()
		path := filepath.Join(workDir, fmt.Sprintf("repo-%02d", index))
		if err := os.Rename(commitTestRepo(t), path); err != nil {
			t.Fatal(err)
		}
		return path
	}
	recorder := newCommitDiffRecorder()
	t.Cleanup(recorder.close)
	recorder.restoreCheckouts(root, workDir)
	for i := 0; i < commitDiffMaxRepos; i++ {
		recorder.record(newRepo(i))
	}
	if recorder.repositoriesOmitted {
		t.Fatal("exactly eight repositories must not report omissions")
	}
	// Refreshing a known checkout at the limit must still be permitted.
	var existing string
	for root := range recorder.repos {
		existing = root
		break
	}
	recorder.recordBaseline(existing, true)
	if recorder.repositoriesOmitted {
		t.Fatal("refreshing a known repository incorrectly reported omissions")
	}
	ignored := newRepo(commitDiffMaxRepos)
	recorder.record(ignored)
	if len(recorder.repos) != commitDiffMaxRepos || !recorder.repositoriesOmitted {
		t.Fatal("repository limit was not preserved or reported")
	}
	if _, exists := recorder.repos[ignored]; exists {
		t.Fatal("ninth repository was captured")
	}
	commitTestWrite(t, ignored, "ignored.txt", "ninth repository result\n")
	received := make(chan []TaskMessageData, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Messages []TaskMessageData `json:"messages"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		received <- body.Messages
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	var seq atomic.Int32
	d := &Daemon{client: NewClient(srv.URL)}
	assertOmitted := func(taskID string, r *commitDiffRecorder) {
		t.Helper()
		d.reportCommitDiff(taskID, r, &seq, slog.Default(), true, "")
		var messages []TaskMessageData
		select {
		case messages = <-received:
		default:
			t.Fatal("no status was reported for unchanged repositories")
		}
		if len(messages) != commitDiffMaxRepos {
			t.Fatalf("expected eight worktree records, got %d", len(messages))
		}
		for _, message := range messages {
			if message.Input["repositories_omitted"] != true || message.Input["kind"] != diffWorktree ||
				len(message.Input["changes"].([]any)) != 0 {
				t.Fatal("repository omission was not reported independently of file changes")
			}
		}
	}
	assertOmitted("repo-limit", recorder)
	registry, err := os.ReadFile(filepath.Join(root, "commit-repositories.json"))
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	if err := json.Unmarshal(registry, &paths); err != nil || len(paths) != commitDiffMaxRepos+1 {
		t.Fatalf("expected all nine repositories in registry: %s, err=%v", registry, err)
	}
	next := newCommitDiffRecorder()
	t.Cleanup(next.close)
	next.restoreCheckouts(root, workDir)
	if len(next.repos) != commitDiffMaxRepos || len(next.registeredRepos) != commitDiffMaxRepos+1 {
		t.Fatal("restoring repositories lost registration or exceeded capture limit")
	}
	// No checkout call this turn: the agent can reuse the ninth directory directly.
	commitTestWrite(t, ignored, "ignored.txt", "second turn result\n")
	assertOmitted("repo-limit-next-turn", next)
}
