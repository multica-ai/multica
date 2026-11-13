package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestGC_LocalCodexHomeRetentionAndResume(t *testing.T) {
	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)
	d := newGCTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"issues":[{"id":"issue","found":true,"status":"done","updated_at":"2026-01-01T00:00:00Z"}]}`))
	}))
	d.cfg.GCCompletedTaskTTL = DefaultGCCompletedTaskTTL
	params := execenv.PrepareParams{
		WorkspacesRoot: d.cfg.WorkspacesRoot, WorkspaceID: "ws", TaskID: "task-first",
		Provider: "codex", LocalWorkDir: t.TempDir(),
		Task: execenv.TaskContextForEnv{AgentID: "agent", IssueID: "issue"},
	}
	env, err := execenv.Prepare(params, d.logger)
	if err != nil {
		t.Fatal(err)
	}
	const sessionID = "019f59d9-a6aa-7a53-b173-1eccc4b4c873"
	rolloutRel := filepath.Join("sessions", "2026", "07", "13", "rollout-2026-07-13T00-00-00-"+sessionID+".jsonl")
	rollout := []byte(`{"timestamp":"2026-07-13T00:00:00Z","type":"session_meta","payload":{"id":"` + sessionID + `","timestamp":"2026-07-13T00:00:00Z","cwd":"/project","originator":"codex_cli_rs","cli_version":"0.143.0","source":"cli","model_provider":"openai"}}` + "\n")
	writeFile(t, filepath.Join(env.CodexHome, rolloutRel), 0)
	if err := os.WriteFile(filepath.Join(env.CodexHome, rolloutRel), rollout, 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.CodexHome, "state_5.sqlite"), 64)
	writeFile(t, filepath.Join(env.CodexHome, "logs_1.sqlite-wal"), 32)
	writeFile(t, filepath.Join(env.RootDir, "logs", "run.log"), 16)
	writeFile(t, filepath.Join(env.RootDir, "output", "result.md"), 8)
	writeFile(t, filepath.Join(params.LocalWorkDir, "node_modules", "keep"), 4)
	meta := execenv.GCMeta{Kind: execenv.GCKindIssue, IssueID: "issue", WorkspaceID: "ws", TaskID: params.TaskID,
		LocalDirectory: true, CompletedAt: time.Now().Add(-15 * 24 * time.Hour)}
	mustWriteMeta(t, env.RootDir, meta)
	wantBytes := dirSize(env.CodexHome)
	stats := &gcStats{}
	d.gcWorkspace(context.Background(), filepath.Dir(env.RootDir), stats)
	assertGone(t, env.RootDir, "codex-home")
	assertKept(t, env.RootDir, "logs/run.log", "output/result.md", ".gc_meta.json", ".task_owner")
	assertKept(t, params.LocalWorkDir, "node_modules/keep")
	if stats.codexHomesReclaimed != 1 || stats.localDirectoriesRetained != 1 || stats.bytesReclaimed != wantBytes {
		t.Fatalf("stats = %+v, want one reclaimed home and %d bytes excluding linked session store", stats, wantBytes)
	}

	// Local-directory follow-ups get a new task root. Preparing it must restore
	// the old rollout from the conversation store after GC removed the old home.
	params.TaskID = "task-second"
	next, err := execenv.Prepare(params, d.logger)
	if err != nil {
		t.Fatal(err)
	}
	if !execenv.CodexResumeRolloutPresent(next.CodexHome, sessionID) {
		t.Fatal("follow-up cannot resume the retained rollout after codex-home GC")
	}
	data, err := os.ReadFile(filepath.Join(next.CodexHome, rolloutRel))
	if err != nil || !bytes.Equal(data, rollout) {
		t.Fatalf("rollout changed after GC: size=%d err=%v", len(data), err)
	}
}

func TestGC_LocalCodexHomeRetentionGuards(t *testing.T) {
	for _, name := range []string{"expired", "recent", "disabled", "no completion", "active", "unowned", "linked home", "local sessions", "archived sessions", "sessions inside root"} {
		t.Run(name, func(t *testing.T) {
			d := newGCTestDaemon(t, chatGCMux("chat", "active"))
			d.cfg.GCCompletedTaskTTL = DefaultGCCompletedTaskTTL
			// Home retention is independent of the shorter artifact TTL.
			d.cfg.GCArtifactTTL = 0
			meta := execenv.GCMeta{Kind: execenv.GCKindChat, ChatSessionID: "chat", WorkspaceID: "ws",
				LocalDirectory: true, CompletedAt: time.Now().Add(-15 * 24 * time.Hour)}
			if name == "recent" {
				meta.CompletedAt = time.Now().Add(-13 * 24 * time.Hour)
			}
			if name == "no completion" {
				meta.CompletedAt = time.Time{}
			}
			if name == "disabled" {
				d.cfg.GCCompletedTaskTTL = 0
			}
			taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "local-task", &meta)
			writeFile(t, filepath.Join(taskDir, "codex-home", "state_5.sqlite"), 32)
			if name == "active" {
				defer d.markActiveEnvRoot(taskDir)()
			}
			if name == "unowned" {
				if err := os.Remove(filepath.Join(taskDir, ".task_owner")); err != nil {
					t.Fatal(err)
				}
			}
			if name == "local sessions" {
				writeFile(t, filepath.Join(taskDir, "codex-home/sessions/rollout.jsonl"), 8)
			}
			if name == "archived sessions" {
				writeFile(t, filepath.Join(taskDir, "codex-home/archived_sessions/rollout.jsonl"), 8)
			}
			if name == "sessions inside root" {
				store := filepath.Join(taskDir, "conversation")
				writeFile(t, filepath.Join(store, "rollout.jsonl"), 8)
				if err := os.Symlink(store, filepath.Join(taskDir, "codex-home/sessions")); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			if name == "linked home" {
				outside := t.TempDir()
				if err := os.Rename(filepath.Join(taskDir, "codex-home"), filepath.Join(outside, "home")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(outside, "home"), filepath.Join(taskDir, "codex-home")); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			stats := &gcStats{}
			d.gcWorkspace(context.Background(), filepath.Dir(taskDir), stats)
			if name == "expired" {
				assertGone(t, taskDir, "codex-home")
			} else {
				assertKept(t, taskDir, "codex-home/state_5.sqlite")
			}
			assertKept(t, taskDir, ".gc_meta.json")
		})
	}
}

func TestGC_LocalRetentionLogsActualCleanup(t *testing.T) {
	d := newGCTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"issues":[{"id":"issue","found":true,"status":"done","updated_at":"2026-01-01T00:00:00Z"}]}`))
	}))
	var logs bytes.Buffer
	d.logger = slog.New(slog.NewTextHandler(&logs, nil))
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "local-task", &execenv.GCMeta{
		Kind: execenv.GCKindIssue, IssueID: "issue", WorkspaceID: "ws", LocalDirectory: true,
		CompletedAt: time.Now().Add(-24 * time.Hour),
	})
	writeFile(t, filepath.Join(taskDir, "codex-home/cache/remote_plugin_catalog/catalog.json"), 64)
	d.runGC(context.Background())
	assertGone(t, taskDir, "codex-home/cache/remote_plugin_catalog")
	if strings.Contains(logs.String(), "eligible for cleanup") {
		t.Fatalf("misleading full cleanup log: %s", &logs)
	}
	if !strings.Contains(logs.String(), "local_directories_retained=1") {
		t.Fatalf("missing retention summary: %s", &logs)
	}
	logs.Reset()
	d.runGC(context.Background())
	if strings.Contains(logs.String(), "eligible for") || strings.Contains(logs.String(), "artifact removed") {
		t.Fatalf("idle cycle repeated cleanup logs: %s", &logs)
	}
}

func TestGC_LocalCodexHomeRechecksActivity(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"active after decision", "completed again", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			d := newGCTestDaemon(t, chatGCMux("chat", "active"))
			d.cfg.GCCompletedTaskTTL = DefaultGCCompletedTaskTTL
			meta := execenv.GCMeta{Kind: execenv.GCKindChat, ChatSessionID: "chat", WorkspaceID: "ws",
				LocalDirectory: true, CompletedAt: time.Now().Add(-15 * 24 * time.Hour)}
			taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "local-task", &meta)
			writeFile(t, filepath.Join(taskDir, "codex-home/state_5.sqlite"), 32)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			action := d.shouldCleanTaskDir(ctx, taskDir)
			if action != gcActionCleanCodexHome {
				t.Fatalf("action = %v, want home cleanup", action)
			}
			switch name {
			case "active after decision":
				defer d.markActiveEnvRoot(taskDir)()
			case "completed again":
				meta.CompletedAt = time.Now()
				mustWriteMeta(t, taskDir, meta)
			case "cancelled":
				cancel()
			}
			stats := &gcStats{}
			d.applyGCAction(ctx, taskDir, action, stats)
			assertKept(t, taskDir, "codex-home/state_5.sqlite")
			if stats.codexHomesReclaimed != 0 {
				t.Fatalf("unexpected cleanup: %+v", stats)
			}
		})
	}
}

func TestGC_UnownedDirectoryWarnsOnce(t *testing.T) {
	t.Parallel()
	d := newGCTestDaemon(t, http.NewServeMux())
	var logs bytes.Buffer
	d.logger = slog.New(slog.NewTextHandler(&logs, nil))
	taskDir := filepath.Join(d.cfg.WorkspacesRoot, "ws", "user-directory")
	writeFile(t, filepath.Join(taskDir, "keep"), 8)
	for i := 0; i < 2; i++ {
		d.gcWorkspace(context.Background(), filepath.Dir(taskDir), &gcStats{})
	}
	assertKept(t, taskDir, "keep")
	if got := strings.Count(logs.String(), "level=WARN"); got != 1 {
		t.Fatalf("warnings = %d, want 1: %s", got, &logs)
	}
}

func TestGC_LocalCodexHomeAfterSessionStoreRemoval(t *testing.T) {
	t.Parallel()
	for _, sessionDir := range []string{"sessions", "archived_sessions"} {
		t.Run(sessionDir, func(t *testing.T) {
			d := newGCTestDaemon(t, chatGCMux("chat", "active"))
			d.cfg.GCCompletedTaskTTL = DefaultGCCompletedTaskTTL
			d.cfg.GCArtifactTTL = 0
			taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "local-task", &execenv.GCMeta{
				Kind: execenv.GCKindChat, ChatSessionID: "chat", WorkspaceID: "ws",
				LocalDirectory: true, CompletedAt: time.Now().Add(-15 * 24 * time.Hour),
			})
			writeFile(t, filepath.Join(taskDir, "codex-home/state_5.sqlite"), 32)
			store := filepath.Join(t.TempDir(), "conversation")
			writeFile(t, filepath.Join(store, "rollout.jsonl"), 64)
			if err := os.Symlink(store, filepath.Join(taskDir, "codex-home", sessionDir)); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			// The independent session TTL expires before home retention.
			if err := os.RemoveAll(store); err != nil {
				t.Fatal(err)
			}
			stats := &gcStats{}
			d.gcWorkspace(context.Background(), filepath.Dir(taskDir), stats)
			assertGone(t, taskDir, "codex-home")
			if stats.codexHomesReclaimed != 1 || stats.bytesReclaimed != 32 {
				t.Fatalf("expired session store blocked home reclamation: %+v", stats)
			}
		})
	}
}

func TestGC_LocalCodexHomeKeepsUnresolvableSessionLink(t *testing.T) {
	t.Parallel()
	d := newGCTestDaemon(t, chatGCMux("chat", "active"))
	d.cfg.GCCompletedTaskTTL = DefaultGCCompletedTaskTTL
	taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "local-task", &execenv.GCMeta{
		Kind: execenv.GCKindChat, ChatSessionID: "chat", WorkspaceID: "ws",
		LocalDirectory: true, CompletedAt: time.Now().Add(-15 * 24 * time.Hour),
	})
	writeFile(t, filepath.Join(taskDir, "codex-home/state_5.sqlite"), 32)
	if err := os.Symlink("sessions", filepath.Join(taskDir, "codex-home/sessions")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	d.gcWorkspace(context.Background(), filepath.Dir(taskDir), &gcStats{})
	assertKept(t, taskDir, "codex-home/state_5.sqlite")
}
