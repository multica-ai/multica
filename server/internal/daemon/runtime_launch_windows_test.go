//go:build windows

package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHandleTaskReportsWindowsCodexProcessStartFailure(t *testing.T) {
	invalidExecutable := filepath.Join(t.TempDir(), "codex.exe")
	if err := os.WriteFile(invalidExecutable, []byte("not a Windows executable"), 0o755); err != nil {
		t.Fatal(err)
	}

	var failBody atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/fail"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode failure body: %v", err)
			} else {
				failBody.Store(body)
			}
		case strings.HasSuffix(r.URL.Path, "/status"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"status":"running"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := New(Config{
		ServerBaseURL:  srv.URL,
		WorkspacesRoot: t.TempDir(),
		AgentTimeout:   5 * time.Second,
	}, logger)
	d.executionEnvironmentCommand = nil
	d.client = NewClient(srv.URL)
	d.cancelPollInterval = time.Hour
	d.runtimeIndex["rt-custom-codex"] = Runtime{
		ID:        "rt-custom-codex",
		Provider:  "codex",
		ProfileID: "profile-invalid-codex",
	}
	d.profileLaunchSpecs["profile-invalid-codex"] = profileLaunchSpec{
		path:    invalidExecutable,
		version: "0.147.0",
	}

	d.handleTask(context.Background(), Task{
		ID:          "task-windows-launch-failure",
		WorkspaceID: "workspace-windows-launch-failure",
		RuntimeID:   "rt-custom-codex",
		IssueID:     "issue-windows-launch-failure",
		AgentID:     "agent-windows-launch-failure",
		AuthToken:   "mat_windows_launch_failure",
		Agent: &AgentData{
			ID:   "agent-windows-launch-failure",
			Name: "Windows launch failure agent",
		},
	}, 0)

	body, _ := failBody.Load().(map[string]any)
	if body == nil {
		t.Fatal("runtime CreateProcess failure was not reported through the task /fail endpoint")
	}
	if got := body["error"]; got == nil || !strings.Contains(strings.ToLower(got.(string)), "start codex") {
		t.Fatalf("task failure error = %v, want visible Codex process-start diagnostic", got)
	}
	if got := body["failure_reason"]; got != "agent_error.process_failure" {
		t.Fatalf("task failure reason = %v, want agent_error.process_failure", got)
	}
}
