package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/pkg/rundiff"
)

func runGitQuiet(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRunDiffTestDaemon(t testing.TB, srvURL string) *Daemon {
	t.Helper()
	return &Daemon{cfg: Config{WorkspacesRoot: t.TempDir(), ServerBaseURL: srvURL}, client: NewClient(srvURL), logger: slog.Default()}
}

func TestRunDiffFinalizesCancelledContextAndUploadsManifestWithoutBodies(t *testing.T) {
	var mu sync.Mutex
	fail := false
	var received []rundiff.Collection
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			w.WriteHeader(503)
			return
		}
		var c rundiff.Collection
		if e := json.NewDecoder(r.Body).Decode(&c); e != nil {
			t.Error(e)
			return
		}
		received = append(received, c)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	d := newRunDiffTestDaemon(t, srv.URL)
	work := t.TempDir()
	if e := os.WriteFile(filepath.Join(work, "a"), []byte("before"), 0600); e != nil {
		t.Fatal(e)
	}
	task := Task{ID: uuid.NewString()}
	ctx, cancel := context.WithCancel(context.Background())
	c := d.startRunDiff(ctx, task, work, slog.Default())
	if c == nil {
		t.Fatal("baseline missing")
	}
	if e := os.WriteFile(filepath.Join(work, "a"), []byte("after"), 0600); e != nil {
		t.Fatal(e)
	}
	mu.Lock()
	fail = true
	mu.Unlock()
	cancel()
	d.finishRunDiff(ctx, task, c, slog.Default())
	path := d.runDiffIndexPath(task.ID)
	// The failed upload must be durable for the retry loop.
	if _, e := os.Stat(path); e != nil {
		t.Fatal("failed upload not durable", e)
	}
	mu.Lock()
	fail = false
	mu.Unlock()
	d.flushRunDiffSpool(context.Background(), true)
	mu.Lock()
	defer mu.Unlock()
	// The workdir fixture has no repository, so it is a directory root and
	// keeps the inline JSON body path (Q10): before/after travel with it.
	if len(received) != 2 || received[1].Status != "ready" || len(received[1].Files) != 1 ||
		received[1].Files[0].Before == nil || *received[1].Files[0].Before != "before" ||
		received[1].Files[0].After == nil || *received[1].Files[0].After != "after" {
		t.Fatalf("%+v", received)
	}
	// The index stays resident after a successful upload: it anchors the refs
	// the relay serves from until task GC.
	if _, e := os.Stat(path); e != nil {
		t.Fatal("resident index removed after upload", e)
	}
}

func TestRunDiffInterruptedBaselineIsUnavailable(t *testing.T) {
	var got rundiff.Collection
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := json.NewDecoder(r.Body).Decode(&got); e != nil {
			t.Error(e)
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	d := newRunDiffTestDaemon(t, srv.URL)
	s := runDiffIndex{Version: runDiffIndexVersion, TaskID: uuid.NewString(), Collection: rundiff.Empty(uuid.NewString(), "collecting", "")}
	if e := d.saveRunDiff(s); e != nil {
		t.Fatal(e)
	}
	d.flushRunDiffSpool(context.Background(), false)
	if got.Status != "" {
		t.Fatal("live baseline treated as interrupted")
	}
	d.flushRunDiffSpool(context.Background(), true)
	if got.Status != "unavailable" || got.Reason != "interrupted" {
		t.Fatalf("%+v", got)
	}
	// Interrupted collections retire their record entirely.
	if _, err := os.Stat(d.runDiffIndexPath(s.TaskID)); !os.IsNotExist(err) {
		t.Fatalf("interrupted index retained: %v", err)
	}
}

func TestRunDiffControlledCheckoutRegistersBeforeResponse(t *testing.T) {
	root := t.TempDir()
	c := rundiff.New(root)
	c.Start(context.Background())
	defer c.Abort()
	child := filepath.Join(root, "repo")
	if e := os.Mkdir(child, 0700); e != nil {
		t.Fatal(e)
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.name", "fixture"}, {"config", "user.email", "fixture@example.invalid"}} {
		cmd := exec.Command("git", append([]string{"-C", child}, args...)...)
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("%v %s", e, out)
		}
	}
	if e := os.WriteFile(filepath.Join(child, "a"), []byte("before checkout response"), 0600); e != nil {
		t.Fatal(e)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-qm", "base"}} {
		cmd := exec.Command("git", append([]string{"-C", child}, args...)...)
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("%v %s", e, out)
		}
	}
	cache := &recordingRepoCache{lookupPath: "/unused"}
	d := newRepoCheckoutTestDaemon(t, "ws", "https://example.invalid/repo.git", root, cache)
	d.cfg.WorkspacesRoot = t.TempDir()
	id := uuid.NewString()
	d.registerActiveRepoCheckoutTask("mat_repo_checkout_test", activeRepoCheckoutTask{WorkspaceID: "ws", TaskID: id, AgentID: "agent", WorkDir: root, RunDiff: c})
	payload, _ := json.Marshal(map[string]string{"url": "https://example.invalid/repo.git", "workspace_id": "ws", "task_id": id, "workdir": child})
	w := httptest.NewRecorder()
	d.repoCheckoutHandler().ServeHTTP(w, authorizedRepoCheckoutRequest(bytes.NewReader(payload)))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if e := os.WriteFile(filepath.Join(child, "a"), []byte("after response"), 0600); e != nil {
		t.Fatal(e)
	}
	out := c.Finish(context.Background())
	// bodies stay local; the snapshot sizes prove both sides captured
	// the real content ("before checkout response" = 24 bytes).
	if out.Status != "ready" || len(out.Files) != 1 || out.Files[0].Before != nil || out.Files[0].BeforeBytes != 24 {
		t.Fatalf("%+v", out)
	}
}

func TestRunDiffResidentRefsAndTaskGC(t *testing.T) {
	var mu sync.Mutex
	var uploads []rundiff.Collection
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c rundiff.Collection
		if e := json.NewDecoder(r.Body).Decode(&c); e == nil {
			mu.Lock()
			uploads = append(uploads, c)
			mu.Unlock()
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	d := newRunDiffTestDaemon(t, srv.URL)
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		return runGitQuiet(t, dir, args...)
	}
	git("init", "-q")
	git("config", "user.name", "fixture")
	git("config", "user.email", "fixture@example.invalid")
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "fixture")
	task := Task{ID: uuid.NewString()}
	c := d.startRunDiff(context.Background(), task, dir, slog.Default())
	if c == nil {
		t.Fatal("collector missing")
	}
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	d.finishRunDiff(context.Background(), task, c, slog.Default())
	mu.Lock()
	final := uploads[len(uploads)-1]
	mu.Unlock()
	// git roots upload manifests only — no bodies ever travel.
	if final.Status != "ready" || len(final.Files) != 1 || final.Files[0].Before != nil || final.Files[0].After != nil {
		t.Fatalf("git upload: %+v", final)
	}
	// The capture pins the served content at upload time: the git file
	// carries the digest over its sanitized sides, and the collection digest
	// covers it. The relay path recomputes the same hash from the pinned
	// trees on every request, so a drifted tree fails the server-side check.
	if final.Files[0].BodySHA256 == "" {
		t.Fatalf("git capture must pin a body digest: %+v", final.Files[0])
	}
	if want := rundiff.BodyDigest("before", "after"); final.Files[0].BodySHA256 != want {
		t.Fatalf("digest over sanitized sides: got %s want %s", final.Files[0].BodySHA256, want)
	}
	if final.Digest == "" || !final.VerifyDigest() {
		t.Fatalf("collection digest missing or unverifiable: %+v", final)
	}
	refs := git("for-each-ref", "--format=%(refname)", "refs/multica/run-diff/")
	if strings.Count(refs, "\n") != 1 || !strings.Contains(refs, "/before") || !strings.Contains(refs, "/after") {
		t.Fatalf("expected exactly before+after refs: %q", refs)
	}
	// The task GC hook removes both refs and the index together.
	d.releaseRunDiffForTask(task.ID)
	if refs := git("for-each-ref", "--format=%(refname)", "refs/multica/run-diff/"); refs != "" {
		t.Fatalf("refs survived task GC: %s", refs)
	}
	if _, err := os.Stat(d.runDiffIndexPath(task.ID)); !os.IsNotExist(err) {
		t.Fatalf("index survived task GC: %v", err)
	}
}

func TestRunDiffRelayRespondsFromPinnedTrees(t *testing.T) {
	var mu sync.Mutex
	var responses map[string]map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/file-changes") && r.Method == http.MethodPost {
			w.WriteHeader(200)
			return
		}
		if strings.Contains(r.URL.Path, "/file-change-requests/") && r.Method == http.MethodPost {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			responses[strings.TrimPrefix(r.URL.Path, "/api/daemon/file-change-requests/")] = body
			mu.Unlock()
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	d := newRunDiffTestDaemon(t, srv.URL)
	dir := t.TempDir()
	git := func(args ...string) string { return runGitQuiet(t, dir, args...) }
	git("init", "-q")
	git("config", "user.name", "fixture")
	git("config", "user.email", "fixture@example.invalid")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "fixture")
	task := Task{ID: uuid.NewString()}
	c := d.startRunDiff(context.Background(), task, dir, slog.Default())
	if c == nil {
		t.Fatal("collector missing")
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTWO\nthree\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.finishRunDiff(context.Background(), task, c, slog.Default())
	index := d.loadRunDiffIndex(task.ID)
	if index == nil || len(index.Collection.Files) != 1 {
		t.Fatalf("index: %+v", index)
	}
	responses = map[string]map[string]any{}
	requestID := uuid.NewString()
	req := FileChangeRequest{RequestID: requestID, TaskID: task.ID, FileID: index.Collection.Files[0].ID}
	d.serveFileChangeRequest(context.Background(), req)
	mu.Lock()
	resp := responses[requestID]
	mu.Unlock()
	if resp == nil || resp["state"] != "ok" {
		t.Fatalf("response: %+v", resp)
	}
	file, err := json.Marshal(resp["file"])
	if err != nil {
		t.Fatal(err)
	}
	var f rundiff.File
	if err := json.Unmarshal(file, &f); err != nil {
		t.Fatal(err)
	}
	if f.Before == nil || *f.Before != "one\ntwo\n" || f.After == nil || *f.After != "one\nTWO\nthree\n" {
		t.Fatalf("relay body mismatch: %+v", f)
	}
	// A file id the index does not know answers purged, never "ok".
	missing := uuid.NewString()
	d.serveFileChangeRequest(context.Background(), FileChangeRequest{RequestID: missing, TaskID: task.ID, FileID: "nope"})
	mu.Lock()
	resp = responses[missing]
	mu.Unlock()
	if resp == nil || resp["state"] != "purged" {
		t.Fatalf("purged response: %+v", resp)
	}
}

func TestRunDiffExpiredUploadRetainsCleanup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	d := newRunDiffTestDaemon(t, srv.URL)
	id := uuid.NewString()
	s := runDiffIndex{TaskID: uuid.NewString(), Collection: rundiff.Empty(id, "ready", ""), ExpiresAt: time.Now().Add(-time.Hour), Pins: map[string]string{filepath.Join(t.TempDir(), "inaccessible-repo"): "refs/multica/run-diff/" + id}}
	if err := d.saveRunDiff(s); err != nil {
		t.Fatal(err)
	}
	d.flushRunDiffSpool(context.Background(), true)
	raw, err := os.ReadFile(filepath.Join(d.runDiffDir(), s.TaskID+".json"))
	if err != nil {
		t.Fatal("expired upload lost cleanup", err)
	}
	var got runDiffIndex
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !got.UploadComplete || len(got.Pins) != 1 || !got.ExpiresAt.Equal(s.ExpiresAt) {
		t.Fatalf("%+v", got)
	}
}

func TestRunDiffLegacySpoolReleasesPinsAndRemovesFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	d := newRunDiffTestDaemon(t, srv.URL)
	dir := t.TempDir()
	git := func(args ...string) string { return runGitQuiet(t, dir, args...) }
	git("init", "-q")
	git("config", "user.name", "fixture")
	git("config", "user.email", "fixture@example.invalid")
	git("commit", "--allow-empty", "-qm", "fixture")
	id := uuid.NewString()
	pin := "refs/multica/run-diff/" + id
	git("update-ref", pin, "HEAD")
	s := runDiffIndex{TaskID: uuid.NewString(), Collection: rundiff.Empty(id, "ready", ""), Pins: map[string]string{dir: pin}}
	if err := d.saveRunDiff(s); err != nil {
		t.Fatal(err)
	}
	d.flushRunDiffSpool(context.Background(), true)
	if refs := git("for-each-ref", "--format=%(refname)", "refs/multica/run-diff/"); refs != "" {
		t.Fatalf("legacy pin remains: %s", refs)
	}
	if _, err := os.Stat(d.runDiffIndexPath(s.TaskID)); !os.IsNotExist(err) {
		t.Fatalf("legacy spool retained: %v", err)
	}
}

func TestRunDiffSweepRemovesOrphanRefs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	d := newRunDiffTestDaemon(t, srv.URL)
	dir := t.TempDir()
	git := func(args ...string) string { return runGitQuiet(t, dir, args...) }
	git("init", "-q")
	git("config", "user.name", "fixture")
	git("config", "user.email", "fixture@example.invalid")
	git("commit", "--allow-empty", "-qm", "fixture")
	// Two refs in one repository: one referenced by a live index, one orphaned
	// (crash residue). The sweep deletes the orphan and keeps the referenced
	// ref. The sweep respects its daily cadence, so force it for the test.
	orphan := "refs/multica/run-diff/" + uuid.NewString() + "/before"
	live := "refs/multica/run-diff/" + uuid.NewString() + "/before"
	git("update-ref", orphan, "HEAD")
	git("update-ref", live, "HEAD")
	taskID := uuid.NewString()
	s := runDiffIndex{Version: runDiffIndexVersion, TaskID: taskID, TaskDir: t.TempDir(),
		Collection: rundiff.Empty(uuid.NewString(), "collecting", ""),
		Roots:      []rundiff.RootRef{{ID: "r", Dir: dir, GitCommonDir: filepath.Join(dir, ".git"), BeforeRef: live}}}
	if err := d.saveRunDiff(s); err != nil {
		t.Fatal(err)
	}
	d.runDiffSweepAt.Store(0)
	d.sweepOrphanRunDiffRefs(context.Background())
	afterSweep := git("for-each-ref", "--format=%(refname)", "refs/multica/run-diff/")
	if strings.Contains(afterSweep, orphan) {
		t.Fatalf("orphan ref survived sweep: %q", afterSweep)
	}
	if !strings.Contains(afterSweep, live) {
		t.Fatalf("referenced ref swept: %q", afterSweep)
	}
	// Once the task dir is gone, the index's refs go with it on the next sweep.
	if err := os.RemoveAll(s.TaskDir); err != nil {
		t.Fatal(err)
	}
	d.runDiffSweepAt.Store(0)
	d.sweepOrphanRunDiffRefs(context.Background())
	if refs := git("for-each-ref", "--format=%(refname)", "refs/multica/run-diff/"); refs != "" {
		t.Fatalf("ref survived missing-task sweep: %s", refs)
	}
	if _, err := os.Stat(d.runDiffIndexPath(taskID)); !os.IsNotExist(err) {
		t.Fatalf("index survived missing-task sweep: %v", err)
	}
}

func TestRunDiffRelayPullStopsOnOldServer(t *testing.T) {
	// Old server: the relay claim route 404s. The daemon latches "unsupported"
	// instead of hammering a route that will never exist, and never posts.
	var posts int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/daemon/runtimes/rt/file-change-requests", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	})
	mux.HandleFunc("/api/daemon/file-change-requests/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&posts, 1)
		w.WriteHeader(200)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	d := newRunDiffTestDaemon(t, srv.URL)
	d.mu.Lock()
	d.runtimeIndex = map[string]Runtime{"rt": {}}
	d.mu.Unlock()
	for i := 0; i < 3; i++ {
		d.handleFileChangesRequested("rt")
	}
	if !d.fileChangePullUnsupported.Load() {
		t.Fatal("unsupported flag not latched")
	}
	if atomic.LoadInt32(&posts) != 0 {
		t.Fatal("posted to a request that was never claimed")
	}
}
