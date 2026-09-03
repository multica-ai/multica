//go:build !windows

package daemon

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestRunTaskPiBusyRetryDoesNotRetireHealthySession pins the distinction
// between a permanently rejected resume and a healthy transcript that another
// execution owns temporarily. The busy attempt must still fall back to a fresh
// session for this turn, but that fallback must not remove the original JSONL
// from every future session lookup.
func TestRunTaskPiBusyRetryDoesNotRetireHealthySession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	priorSessionID := filepath.Join(t.TempDir(), "busy-session.jsonl")
	claim, err := os.OpenFile(priorSessionID, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open prior session: %v", err)
	}
	defer claim.Close()
	if _, err := claim.WriteString("{}\n"); err != nil {
		t.Fatalf("seed prior session: %v", err)
	}
	if err := unix.Flock(int(claim.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatalf("lock prior session: %v", err)
	}
	defer unix.Flock(int(claim.Fd()), unix.LOCK_UN) //nolint:errcheck -- best-effort test cleanup

	fakeBin := filepath.Join(t.TempDir(), "pi")
	script := `#!/bin/sh
cat > /dev/null
printf '%s\n' '{"type":"agent_start"}'
printf '%s\n' '{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"done"}}'
printf '%s\n' '{"type":"turn_end","message":{"role":"assistant","model":"test","usage":{"input":1,"output":1}}}'
`
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake pi: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := &Daemon{
		client:         NewClient(srv.URL),
		logger:         logger,
		workspaces:     make(map[string]*workspaceState),
		runtimeIndex:   map[string]Runtime{"rt-pi": {ID: "rt-pi", Provider: "pi"}},
		activeEnvRoots: make(map[string]int),
		cfg: Config{
			WorkspacesRoot: t.TempDir(),
			AgentTimeout:   5 * time.Second,
			ServerBaseURL:  srv.URL,
			Agents: map[string]AgentEntry{
				"pi": {Path: fakeBin},
			},
		},
	}
	task := Task{
		ID:             "task-pi-busy",
		WorkspaceID:    "ws-pi",
		RuntimeID:      "rt-pi",
		IssueID:        "issue-pi",
		AgentID:        "agent-pi",
		AuthToken:      "mat_pi_busy",
		PriorSessionID: priorSessionID,
		Agent: &AgentData{
			ID:   "agent-pi",
			Name: "pi-agent",
		},
	}

	result, err := d.runTask(context.Background(), task, "pi", 0, logger)
	if err != nil {
		t.Fatalf("runTask: %v", err)
	}
	if result.Status != "completed" || result.Comment != "done" {
		t.Fatalf("result = %+v, want successful fresh-session retry", result)
	}
	if result.SessionID == "" || result.SessionID == priorSessionID {
		t.Fatalf("SessionID = %q, want a new session", result.SessionID)
	}
	if result.RetiredSessionID != "" {
		t.Fatalf("RetiredSessionID = %q, want empty for a temporarily busy healthy session", result.RetiredSessionID)
	}
}
