package daemon

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExactReplyReplacesModelTextAndFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name, receipt, want string
		blocked             bool
	}{
		{"exact", "  verified receipt\n", "  verified receipt\n", false},
		{"provider", "MULTICA_EXACT_REPLY/1 state=provider\n", "", false},
		{"idle", "MULTICA_EXACT_REPLY/1 state=idle\n", exactReplyUnavailable, true},
		{"empty", "", exactReplyUnavailable, true},
		{"unknown protocol", "MULTICA_EXACT_REPLY/1 state=other\n", exactReplyUnavailable, true},
		{"oversize", strings.Repeat("x", 65537), exactReplyUnavailable, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "reply")
			if err := os.WriteFile(path, []byte(tc.receipt), 0600); err != nil {
				t.Fatal(err)
			}
			result := enforceExactReply(TaskResult{Status: "completed", Comment: "invented model completion", SessionID: "session"}, path)
			if result.Comment != tc.want {
				t.Fatalf("got %q, want %q", result.Comment, tc.want)
			}
			if (result.Status == "blocked") != tc.blocked {
				t.Fatalf("status %q", result.Status)
			}
		})
	}
}

func TestExactReplyMissingOrSymlinkNeverPublishesModelOrLinkedContent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "private")
	if err := os.WriteFile(target, []byte("private data"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "missing"), link, root} {
		result := enforceExactReply(TaskResult{Status: "completed", Comment: "model output"}, path)
		if result.Status != "blocked" || result.Comment != exactReplyUnavailable {
			t.Fatalf("unsafe result %#v", result)
		}
	}
}

func TestExactReplyPreparationOwnsPathAndDoesNotRelaxCustomEnvBlocklist(t *testing.T) {
	root := t.TempDir()
	env := map[string]string{}
	task := Task{ChatSessionID: "chat", Agent: &AgentData{CustomEnv: map[string]string{
		"MULTICA_EXACT_REPLY_REQUIRED": "1", "MULTICA_EXACT_REPLY_FILE": "/untrusted/path",
	}}}
	path, err := prepareExactReply(task, root, env)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != root || env["MULTICA_EXACT_REPLY_FILE"] != path || env["MULTICA_EXACT_REPLY_REQUIRED"] != "1" {
		t.Fatalf("env %#v", env)
	}
	if !isBlockedEnvKey("MULTICA_EXACT_REPLY_FILE") {
		t.Fatal("custom env may override daemon path")
	}
	if result := enforceExactReply(TaskResult{Status: "completed", Comment: "no handler ran"}, path); result.Status != "blocked" {
		t.Fatal(result)
	}
	next, err := prepareExactReply(task, root, map[string]string{})
	if err != nil || next == path {
		t.Fatalf("%q %v", next, err)
	}
	task.ChatSessionID = ""
	if path, err := prepareExactReply(task, root, map[string]string{}); err != nil || path != "" {
		t.Fatalf("non-chat enabled: %q %v", path, err)
	}
}

func TestExactReplyPreservesDisabledAndCancelledResults(t *testing.T) {
	original := TaskResult{Status: "completed", Comment: "normal output"}
	if got := enforceExactReply(original, ""); got.Comment != original.Comment {
		t.Fatal(got)
	}
	cancelled := TaskResult{Status: "cancelled"}
	if got := enforceExactReply(cancelled, "missing"); got.Status != "cancelled" || got.Comment != "" {
		t.Fatal(got)
	}
}

func TestExactReplyTerminalCallbackNeverCarriesModelFallback(t *testing.T) {
	for _, valid := range []bool{false, true} {
		root := t.TempDir()
		path, err := prepareExactReply(Task{ChatSessionID: "chat", Agent: &AgentData{CustomEnv: map[string]string{"MULTICA_EXACT_REPLY_REQUIRED": "1"}}}, root, map[string]string{})
		if err != nil {
			t.Fatal(err)
		}
		if valid {
			if err := os.WriteFile(path, []byte("verified handler receipt"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		rec := &reportTaskResultRecorder{}
		srv := httptest.NewServer(rec.handler(t))
		d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
		d.reportTaskResult(context.Background(), "receipt-task", enforceExactReply(TaskResult{Status: "completed", Comment: "model fallback"}, path), slog.Default())
		srv.Close()
		rec.mu.Lock()
		if valid {
			if rec.path != "/api/daemon/tasks/receipt-task/complete" || rec.payload["output"] != "verified handler receipt" {
				t.Fatalf("%s %#v", rec.path, rec.payload)
			}
		} else {
			if rec.path != "/api/daemon/tasks/receipt-task/fail" || rec.payload["error"] != exactReplyUnavailable {
				t.Fatalf("%s %#v", rec.path, rec.payload)
			}
		}
		rec.mu.Unlock()
	}
}

func TestExactReplyCannotPromoteFailedTask(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipt")
	if err := os.WriteFile(path, []byte("success"), 0600); err != nil {
		t.Fatal(err)
	}
	got := enforceExactReply(TaskResult{Status: "blocked", Comment: "provider failed"}, path)
	if got.Status != "blocked" || got.Comment != exactReplyUnavailable {
		t.Fatal(got)
	}
}
