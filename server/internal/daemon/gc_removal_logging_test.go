package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestGC_RemovalLogsLifecycleReason(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, status, reason               string
		kind                               execenv.GCMetaKind
		retention                          time.Duration
		singleIssue, missingMeta, notFound bool
	}{
		{name: "terminal issue batch", kind: execenv.GCKindIssue, status: "done", reason: "terminal issue retention expired", retention: 24 * time.Hour},
		{name: "terminal issue single", kind: execenv.GCKindIssue, singleIssue: true, status: "done", reason: "terminal issue retention expired", retention: 24 * time.Hour},
		{name: "completed open issue", kind: execenv.GCKindIssue, status: "in_review", reason: "completed task retention expired", retention: DefaultGCCompletedTaskTTL},
		{name: "orphan without metadata", missingMeta: true, reason: "no meta", retention: 72 * time.Hour},
		{name: "inaccessible issue", kind: execenv.GCKindIssue, notFound: true, reason: "issue not accessible", retention: 72 * time.Hour},
		{name: "archived chat", kind: execenv.GCKindChat, status: "archived", reason: "archived chat retention expired", retention: 24 * time.Hour},
		{name: "deleted chat", kind: execenv.GCKindChat, notFound: true, reason: "chat session not accessible (hard-deleted)"},
		{name: "autopilot", kind: execenv.GCKindAutopilotRun, status: "completed", reason: "autopilot run finished"},
		{name: "quick create", kind: execenv.GCKindQuickCreate, status: "completed", reason: "quick-create task finished"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newGCTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.notFound {
					http.NotFound(w, r)
					return
				}
				if strings.Contains(r.URL.Path, "/workspaces/") {
					_ = json.NewEncoder(w).Encode(map[string]any{"issues": []IssueGCCheckResult{{ID: "issue", Found: true, Status: tc.status, UpdatedAt: time.Now().Add(-48 * time.Hour)}}})
				} else {
					_ = json.NewEncoder(w).Encode(map[string]any{"status": tc.status, "updated_at": time.Now().Add(-48 * time.Hour)})
				}
			}))
			d.cfg.GCTTL, d.cfg.GCOrphanTTL, d.cfg.GCCompletedTaskTTL = 24*time.Hour, 72*time.Hour, DefaultGCCompletedTaskTTL
			var logs bytes.Buffer
			d.logger = slog.New(slog.NewJSONHandler(&logs, nil))
			meta := &execenv.GCMeta{Kind: tc.kind, WorkspaceID: "ws", IssueID: "issue", ChatSessionID: "chat", AutopilotRunID: "run", TaskID: "task",
				CompletedAt: time.Now().Add(-15 * 24 * time.Hour)}
			if tc.singleIssue {
				meta.WorkspaceID = ""
			}
			if tc.missingMeta {
				meta = nil
			}
			taskDir := createTaskDir(t, d.cfg.WorkspacesRoot, "ws", "owned-task", meta)
			writeFile(t, filepath.Join(taskDir, "output/result.md"), 8)
			past := time.Now().Add(-73 * time.Hour)
			if err := os.Chtimes(taskDir, past, past); err != nil {
				t.Fatal(err)
			}
			d.gcWorkspace(context.Background(), filepath.Dir(taskDir), &gcStats{})
			if _, err := os.Stat(taskDir); !os.IsNotExist(err) {
				t.Fatalf("task dir not removed: %v", err)
			}
			removed := 0
			for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				var record map[string]any
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatal(err)
				}
				if record["msg"] != "gc: removed" {
					continue
				}
				removed++
				if record["reason"] != tc.reason {
					t.Fatalf("removal reason = %v, want %s", record["reason"], tc.reason)
				}
				if tc.status != "" && record["status"] != tc.status {
					t.Fatalf("missing observed status: %v", record)
				}
				if tc.retention > 0 && record["retention"] != float64(tc.retention) {
					t.Fatalf("missing retention duration: %v", record)
				}
				if tc.kind == execenv.GCKindIssue && record["issue"] != "issue" {
					t.Fatalf("missing issue identity: %v", record)
				}
			}
			if removed != 1 {
				t.Fatalf("removal log count = %d, want 1: %s", removed, &logs)
			}
		})
	}
}
