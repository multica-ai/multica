package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/codechanges"
)

func codeSnapshotFixture() *codechanges.Snapshot {
	return &codechanges.Snapshot{Version: 1, BaseRef: strings.Repeat("a", 40), HeadRef: strings.Repeat("b", 40), Files: []codechanges.File{{Path: "src/file.go", Status: "M", Diff: "@@ -1 +1 @@\n-old\n+private-source", Additions: 1, Deletions: 1}}}
}
func TestCodeChangesTerminalOutboxSurvivesRestart(t *testing.T) {
	for _, kind := range []terminalTaskReportKind{terminalTaskReportComplete, terminalTaskReportFail} {
		var payload struct {
			CodeChanges *codechanges.Snapshot `json:"code_changes"`
			Output      string                `json:"output"`
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			encoded, err := codechanges.Encode(payload.CodeChanges)
			if err != nil {
				t.Error(err)
			}
			w.Header().Set(codechanges.ReceiptHeader, "saved:"+codechanges.DigestEncoded(encoded))
			w.WriteHeader(200)
		}))
		cfg := Config{ServerBaseURL: srv.URL, WorkspacesRoot: t.TempDir(), DaemonID: "code-changes"}
		d := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
		report := terminalTaskReport{kind: kind, taskID: "task", codeChangesRef: publicCodeChangesRef(), output: "done", errorMessage: "failed", codeChanges: codeSnapshotFixture()}
		d.terminalReportSend = func(context.Context, terminalTaskReport, []time.Duration) error { return errors.New("offline") }
		if err := d.reportTerminalTask(context.Background(), report); err == nil {
			t.Fatal("offline report succeeded")
		}
		record, err := persistedTerminalReport(report, time.Now())
		if err != nil || record.Version != 2 {
			t.Fatalf("snapshot version=%d err=%v", record.Version, err)
		}
		record.Version = 1
		if _, err = record.terminalReport(); err == nil {
			t.Fatal("old version silently carried snapshot")
		}
		after := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
		pending, delivered := after.replayPendingTerminalReports(context.Background())
		if pending != 0 || delivered != 1 || payload.CodeChanges == nil || payload.CodeChanges.Files[0].Diff != report.codeChanges.Files[0].Diff {
			t.Fatalf("replay pending=%d delivered=%d snapshot=%+v", pending, delivered, payload.CodeChanges)
		}
		srv.Close()
	}
}
func TestCodeChangesCaptureAfterWorktreeRemoval(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init")
	git("commit", "--allow-empty", "-m", "base")
	worktree := filepath.Join(t.TempDir(), "task")
	git("worktree", "add", "-b", "task-changes", worktree)
	env := &execenv.Environment{WorkDir: worktree, LocalWorktree: &execenv.LocalWorktree{GitRoot: root, Branch: "task-changes", WorkDir: worktree}}
	if prepareTaskCodeChanges(context.Background(), Task{IssueID: "issue"}, env) != nil {
		t.Fatal("disabled capture started")
	}
	capture := prepareTaskCodeChanges(context.Background(), Task{IssueID: "issue", CodeChangesEnabled: true, RuntimeID: publicCodeChangesRef().RuntimeID, DispatchedAt: publicCodeChangesRef().DispatchedAt}, env)
	if err := os.WriteFile(filepath.Join(worktree, "app.txt"), []byte("delivered\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("-C", worktree, "add", ".")
	git("-C", worktree, "commit", "-m", "delivered")
	git("worktree", "remove", worktree)
	result := TaskResult{DurableWorkDir: root, BranchName: "task-changes"}
	capture.finish(&result)
	if result.CodeChanges == nil || result.CodeChanges.Reason != "" || len(result.CodeChanges.Files) != 1 || !strings.Contains(result.CodeChanges.Files[0].Diff, "+delivered") {
		t.Fatalf("removed-worktree snapshot=%+v", result.CodeChanges)
	}
}

func TestCodeChangesOutboxComparesSnapshotContent(t *testing.T) {
	store := newTerminalReportStore(Config{WorkspacesRoot: t.TempDir(), ServerBaseURL: "https://api.example.test", DaemonID: "snapshot-equality"})
	report := terminalTaskReport{kind: terminalTaskReportComplete, taskID: "snapshot", codeChangesRef: publicCodeChangesRef(), codeChanges: codeSnapshotFixture()}
	if err := store.enqueue(report); err != nil {
		t.Fatal(err)
	}
	same := report
	same.codeChanges = codeSnapshotFixture()
	if err := store.enqueue(same); err != nil {
		t.Fatalf("equal snapshot conflict: %v", err)
	}
	items, err := store.list()
	if err != nil || len(items) != 1 {
		t.Fatalf("list=%d err=%v", len(items), err)
	}
	now := time.Now()
	for i := range 3 {
		quarantined, err := store.recordPermanentRejection(items[0], 400, now.Add(time.Duration(i)*11*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if quarantined != (i == 2) {
			t.Fatalf("rejection %d quarantined=%v", i, quarantined)
		}
	}
}

func TestCodeChangesLazyCheckoutCapturesOnlyCurrentExecution(t *testing.T) {
	workdir := t.TempDir()
	repo := filepath.Join(workdir, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	capture := prepareTaskCodeChanges(context.Background(), Task{IssueID: "issue", CodeChangesEnabled: true, RuntimeID: publicCodeChangesRef().RuntimeID, DispatchedAt: publicCodeChangesRef().DispatchedAt}, &execenv.Environment{WorkDir: workdir})
	git("init")
	git("commit", "--allow-empty", "-m", "base")
	capture.observeCheckout(repo, git("rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(repo, "app.go"), []byte("change\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "task")
	// Repeated checkout must not advance the current execution's start commit.
	capture.observeCheckout(repo, git("rev-parse", "HEAD"))
	result := TaskResult{}
	capture.finish(&result)
	if result.CodeChanges == nil || result.CodeChanges.Reason != "" || len(result.CodeChanges.Files) != 1 || !strings.Contains(result.CodeChanges.Files[0].Diff, "+change") {
		t.Fatalf("lazy snapshot=%+v", result.CodeChanges)
	}
}

func TestCodeChangesReadOnlyRemovedBranchIsEmpty(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v %v %s", args, err, out)
		}
	}
	git("init")
	git("commit", "--allow-empty", "-m", "base")
	worktree := filepath.Join(t.TempDir(), "task")
	git("worktree", "add", "-b", "read-only", worktree)
	env := &execenv.Environment{WorkDir: worktree, LocalWorktree: &execenv.LocalWorktree{GitRoot: root, Branch: "read-only"}}
	capture := prepareTaskCodeChanges(context.Background(), Task{IssueID: "issue", CodeChangesEnabled: true, RuntimeID: publicCodeChangesRef().RuntimeID, DispatchedAt: publicCodeChangesRef().DispatchedAt}, env)
	git("worktree", "remove", worktree)
	git("branch", "-D", "read-only")
	result := TaskResult{DurableWorkDir: root}
	capture.finish(&result)
	if result.CodeChanges == nil || result.CodeChanges.Reason != "" || len(result.CodeChanges.Files) != 0 {
		t.Fatalf("read-only snapshot=%+v", result.CodeChanges)
	}
}

func TestCodeChangesMissingServerReceiptRetainsOutbox(t *testing.T) {
	var acknowledge atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if acknowledge.Load() {
			w.Header().Set(codechanges.ReceiptHeader, "discarded")
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	cfg := Config{ServerBaseURL: srv.URL, WorkspacesRoot: t.TempDir(), DaemonID: "old-server-receipt"}
	d := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.terminalReportSend = func(ctx context.Context, report terminalTaskReport, _ []time.Duration) error {
		return d.client.postTerminalWithCodeChanges(ctx, "/terminal", map[string]any{"code_changes": report.codeChanges}, report.codeChanges, nil)
	}

	report := terminalTaskReport{kind: terminalTaskReportComplete, taskID: "snapshot", codeChangesRef: publicCodeChangesRef(), output: "done", codeChanges: codeSnapshotFixture()}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := d.reportTerminalTask(ctx, report); err == nil {
		t.Fatal("unacknowledged snapshot treated as delivered")
	}
	items, err := d.terminalReports.list()
	if err != nil || len(items) != 1 {
		t.Fatalf("missing receipt dropped outbox: %d %v", len(items), err)
	}
	if err := d.client.postTerminalWithCodeChanges(ctx, "/terminal", map[string]any{"code_changes": report.codeChanges}, report.codeChanges, []time.Duration{time.Hour}); err == nil || isTransientError(err) {
		t.Fatalf("missing receipt should be a non-transient response error: %v", err)
	}
	acknowledge.Store(true)
	pending, delivered := d.replayPendingTerminalReports(context.Background())
	if pending != 0 || delivered != 1 {
		t.Fatalf("explicit discard receipt pending=%d delivered=%d", pending, delivered)
	}
}

func publicCodeChangesRef() codechanges.ExecutionRef {
	return codechanges.ExecutionRef{RuntimeID: "734055d4-d462-421c-b218-324b1a3660de", DispatchedAt: "2026-10-25T01:02:03.123456Z"}
}

func TestCodeChangesOutboxSeparatesExecutionGenerations(t *testing.T) {
	store := newTerminalReportStore(Config{WorkspacesRoot: t.TempDir(), ServerBaseURL: "https://api.example.test", DaemonID: "multi-generation"})
	first := terminalTaskReport{kind: terminalTaskReportComplete, taskID: "same-task", codeChanges: codeSnapshotFixture(), codeChangesRef: publicCodeChangesRef(), output: "first"}
	next := first
	next.output = "second"
	next.codeChangesRef.DispatchedAt = "2026-10-25T01:02:03.123457Z"
	legacy := terminalTaskReport{kind: terminalTaskReportComplete, taskID: "same-task", output: "legacy"}
	for _, report := range []terminalTaskReport{first, next, legacy} {
		if err := store.enqueue(report); err != nil {
			t.Fatal(err)
		}
	}
	items, err := store.list()
	if err != nil || len(items) != 3 {
		t.Fatalf("generation records=%d err=%v", len(items), err)
	}
	if err := store.acknowledge(pendingTerminalTaskReport{fileName: terminalReportFileNameForReport(first), report: first}); err != nil {
		t.Fatal(err)
	}
	items, err = store.list()
	if err != nil || len(items) != 2 {
		t.Fatalf("independent ack records=%d err=%v", len(items), err)
	}
	for _, item := range items {
		if item.report.output == "first" {
			t.Fatal("ack left old generation")
		}
	}
}
func TestCodeChangesOutboxConflictDoesNotSendReplacement(t *testing.T) {
	cfg := Config{WorkspacesRoot: t.TempDir(), ServerBaseURL: "https://api.example.test", DaemonID: "conflicting-generation"}
	d := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	original := terminalTaskReport{kind: terminalTaskReportComplete, taskID: "task", output: "original", codeChanges: codeSnapshotFixture(), codeChangesRef: publicCodeChangesRef()}
	if err := d.terminalReports.enqueue(original); err != nil {
		t.Fatal(err)
	}
	sent := false
	d.terminalReportSend = func(context.Context, terminalTaskReport, []time.Duration) error { sent = true; return nil }
	replacement := original
	replacement.output = "replacement"
	if err := d.reportTerminalTask(context.Background(), replacement); !errors.Is(err, errTerminalReportConflict) {
		t.Fatalf("conflict=%v", err)
	}
	if sent {
		t.Fatal("conflicting artifact bypassed durable ownership")
	}
}
