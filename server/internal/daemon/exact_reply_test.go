package daemon

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestRunTaskDeclaredExactReplyOverridesProviderOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script agent fixture is POSIX-only")
	}

	workspacesRoot := t.TempDir()
	workspaceID := "ws-exact-reply"
	taskID := "task-exact-reply"
	envRoot := execenv.PredictRootDir(workspacesRoot, workspaceID, taskID)
	want := "Authoritative reply\nwith exact formatting.\n"
	overriddenPath := filepath.Join(t.TempDir(), "attacker-selected.md")

	fakeBin := filepath.Join(t.TempDir(), "claude")
	script := `#!/bin/sh
test "$(dirname "$MULTICA_EXACT_REPLY_FILE")" = "$TMPDIR" || exit 12
printf '%s' 'Authoritative reply
with exact formatting.
' > "$MULTICA_EXACT_REPLY_FILE"
IFS= read -r _
printf '%s\n' '{"type":"system","session_id":"session-exact-reply"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"session-exact-reply","result":"I reached the iteration limit and could not finish."}'
`
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	d := &Daemon{
		client:         NewClient(srv.URL),
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:     make(map[string]*workspaceState),
		runtimeIndex:   map[string]Runtime{"rt-exact-reply": {ID: "rt-exact-reply", Provider: "claude"}},
		activeEnvRoots: make(map[string]int),
		cfg: Config{
			WorkspacesRoot: workspacesRoot,
			AgentTimeout:   5 * time.Second,
			ServerBaseURL:  srv.URL,
			Agents: map[string]AgentEntry{
				"claude": {Path: fakeBin},
			},
		},
	}
	task := Task{
		ID:          taskID,
		WorkspaceID: workspaceID,
		RuntimeID:   "rt-exact-reply",
		IssueID:     "issue-exact-reply",
		AgentID:     "agent-exact-reply",
		AuthToken:   "mat_exact_reply",
		Agent: &AgentData{
			ID:   "agent-exact-reply",
			Name: "exact-reply-agent",
			CustomEnv: map[string]string{
				exactReplyRequiredEnv:      "1",
				"MULTICA_EXACT_REPLY_FILE": overriddenPath,
			},
		},
	}

	result, err := d.runTask(context.Background(), task, "claude", 0, d.logger)
	if err != nil {
		t.Fatalf("runTask: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("runTask status = %q, want completed (comment=%q)", result.Status, result.Comment)
	}
	if result.Comment != want {
		t.Fatalf("runTask comment = %q, want exact declared reply %q", result.Comment, want)
	}
	if result.EnvRoot != envRoot {
		t.Fatalf("runTask env root = %q, want %q", result.EnvRoot, envRoot)
	}
	if _, err := os.Stat(overriddenPath); !os.IsNotExist(err) {
		t.Fatalf("custom exact-reply path %q was used; stat err=%v", overriddenPath, err)
	}
}

func TestRunTaskFreshSessionRetryMustReplacePriorAttemptExactReply(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script agent fixture is POSIX-only")
	}

	testDir := t.TempDir()
	attemptsFile := filepath.Join(testDir, "exact-reply-attempts.txt")
	fakeBin := filepath.Join(testDir, "claude")
	script := `#!/bin/sh
if [ -z "$MULTICA_EXACT_REPLY_FILE" ]; then
  IFS= read -r _
  printf '%s\n' '{"type":"system","session_id":"session-stale"}'
  printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"session-stale","result":"seed reusable session"}'
  exit 0
fi
case " $* " in
  *" --resume "*)
    printf '%s\n' 'resumed' >> "` + attemptsFile + `"
    printf '%s\n' 'STALE EXACT REPLY FROM ABANDONED ATTEMPT' > "$MULTICA_EXACT_REPLY_FILE"
    IFS= read -r _
    printf '%s\n' '{"type":"system","session_id":"session-stale"}'
    printf '%s\n' '{"type":"result","subtype":"error","is_error":true,"session_id":"session-stale","result":"no conversation found"}'
    ;;
  *)
    printf '%s\n' 'fresh' >> "` + attemptsFile + `"
    IFS= read -r _
    printf '%s\n' '{"type":"system","session_id":"session-fresh"}'
    printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"session-fresh","result":"PROVIDER FALLBACK FROM FRESH ATTEMPT"}'
    ;;
esac
`
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	d := &Daemon{
		client:         NewClient(srv.URL),
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:     make(map[string]*workspaceState),
		runtimeIndex:   map[string]Runtime{"rt-exact-retry": {ID: "rt-exact-retry", Provider: "claude"}},
		activeEnvRoots: make(map[string]int),
		cfg: Config{
			WorkspacesRoot: t.TempDir(),
			AgentTimeout:   5 * time.Second,
			ServerBaseURL:  srv.URL,
			Agents:         map[string]AgentEntry{"claude": {Path: fakeBin}},
		},
	}
	seed := Task{
		ID:           "task-exact-retry-seed",
		WorkspaceID:  "ws-exact-retry",
		RuntimeID:    "rt-exact-retry",
		IssueID:      "issue-exact-retry",
		AgentID:      "agent-exact-retry",
		AuthToken:    "mat_exact_retry_seed",
		IsLeaderTask: true,
		Agent: &AgentData{
			ID:   "agent-exact-retry",
			Name: "exact-retry-agent",
		},
	}
	seedResult, err := d.runTask(context.Background(), seed, "claude", 0, d.logger)
	if err != nil {
		t.Fatalf("seed runTask: %v", err)
	}
	if seedResult.Status != "completed" || seedResult.SessionID != "session-stale" {
		t.Fatalf("seed runTask result = %+v, want completed reusable session", seedResult)
	}

	task := seed
	task.ID = "task-exact-retry"
	task.AuthToken = "mat_exact_retry"
	task.PriorSessionID = seedResult.SessionID
	task.PriorWorkDir = seedResult.WorkDir
	task.Agent.CustomEnv = map[string]string{exactReplyRequiredEnv: "1"}
	result, err := d.runTask(context.Background(), task, "claude", 0, d.logger)
	if err != nil {
		t.Fatalf("runTask: %v", err)
	}
	attempts, err := os.ReadFile(attemptsFile)
	if err != nil {
		t.Fatalf("read attempts: %v", err)
	}
	if string(attempts) != "resumed\nfresh\n" {
		t.Fatalf("agent attempts = %q, want resumed then fresh", attempts)
	}
	if result.Status != "blocked" {
		t.Fatalf("runTask result = status %q, comment %q; want blocked", result.Status, result.Comment)
	}
	if result.FailureReason != "agent_error.empty_or_unparseable_output" {
		t.Fatalf("failure reason = %q, want agent_error.empty_or_unparseable_output", result.FailureReason)
	}
	if !strings.Contains(result.Comment, "required declaration was not written") {
		t.Fatalf("runTask comment = %q, want missing fresh declaration detail", result.Comment)
	}
	if strings.Contains(result.Comment, "STALE EXACT REPLY") || strings.Contains(result.Comment, "PROVIDER FALLBACK") {
		t.Fatalf("runTask published abandoned-attempt content: %q", result.Comment)
	}
}

func TestRunTaskCodexCanDeclareExactReplyInTaskTempDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script Codex fixture is POSIX-only")
	}

	want := "Codex authoritative reply\n"
	fakeBin := filepath.Join(t.TempDir(), "codex")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then echo "codex-cli 0.149.0"; exit 0; fi
test "$(dirname "$MULTICA_EXACT_REPLY_FILE")" = "$TMPDIR" || exit 12
printf '%s' 'Codex authoritative reply
' > "$MULTICA_EXACT_REPLY_FILE"
read line
printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{}}'
read line
read line
printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thread-exact-reply"}}}'
read line
printf '%s\n' '{"jsonrpc":"2.0","id":3,"result":{}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"thread-exact-reply","turn":{"id":"turn-exact-reply"}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thread-exact-reply","turnId":"turn-exact-reply","item":{"type":"agentMessage","id":"message-exact-reply","text":"MODEL FALLBACK MUST NOT PUBLISH","phase":"final_answer"}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread-exact-reply","turn":{"id":"turn-exact-reply","status":"completed"}}}'
`
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake Codex: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	d := &Daemon{
		client:         NewClient(srv.URL),
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:     make(map[string]*workspaceState),
		runtimeIndex:   map[string]Runtime{"rt-codex-exact-reply": {ID: "rt-codex-exact-reply", Provider: "codex"}},
		activeEnvRoots: make(map[string]int),
		activeStores:   make(map[string]int),
		deletingStores: make(map[string]bool),
		cfg: Config{
			WorkspacesRoot: t.TempDir(),
			AgentTimeout:   5 * time.Second,
			ServerBaseURL:  srv.URL,
			Agents:         map[string]AgentEntry{"codex": {Path: fakeBin}},
		},
	}
	d.activeStoresCond = sync.NewCond(&d.activeStoresMu)
	task := Task{
		ID:          "task-codex-exact-reply",
		WorkspaceID: "ws-codex-exact-reply",
		RuntimeID:   "rt-codex-exact-reply",
		IssueID:     "issue-codex-exact-reply",
		AgentID:     "agent-codex-exact-reply",
		AuthToken:   "mat_codex_exact_reply",
		Agent: &AgentData{
			ID:        "agent-codex-exact-reply",
			Name:      "codex-exact-reply-agent",
			CustomEnv: map[string]string{exactReplyRequiredEnv: "1"},
		},
	}

	result, err := d.runTask(context.Background(), task, "codex", 0, d.logger)
	if err != nil {
		t.Fatalf("runTask: %v", err)
	}
	if result.Status != "completed" || result.Comment != want {
		t.Fatalf("runTask result = status %q, comment %q; want completed exact reply %q", result.Status, result.Comment, want)
	}
}

func TestRunTaskWithoutDeclaredExactReplyPreservesProviderOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script agent fixture is POSIX-only")
	}

	const want = "Provider output remains authoritative when no reply is declared."
	fakeBin := filepath.Join(t.TempDir(), "claude")
	script := `#!/bin/sh
IFS= read -r _
printf '%s\n' '{"type":"system","session_id":"session-no-exact-reply"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"session-no-exact-reply","result":"Provider output remains authoritative when no reply is declared."}'
`
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	d := &Daemon{
		client:         NewClient(srv.URL),
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:     make(map[string]*workspaceState),
		runtimeIndex:   map[string]Runtime{"rt-no-exact-reply": {ID: "rt-no-exact-reply", Provider: "claude"}},
		activeEnvRoots: make(map[string]int),
		cfg: Config{
			WorkspacesRoot: t.TempDir(),
			AgentTimeout:   5 * time.Second,
			ServerBaseURL:  srv.URL,
			Agents:         map[string]AgentEntry{"claude": {Path: fakeBin}},
		},
	}
	task := Task{
		ID:          "task-no-exact-reply",
		WorkspaceID: "ws-no-exact-reply",
		RuntimeID:   "rt-no-exact-reply",
		IssueID:     "issue-no-exact-reply",
		AgentID:     "agent-no-exact-reply",
		AuthToken:   "mat_no_exact_reply",
		Agent:       &AgentData{ID: "agent-no-exact-reply", Name: "no-exact-reply-agent"},
	}

	result, err := d.runTask(context.Background(), task, "claude", 0, d.logger)
	if err != nil {
		t.Fatalf("runTask: %v", err)
	}
	if result.Status != "completed" || result.Comment != want {
		t.Fatalf("runTask result = status %q, comment %q; want completed provider output %q", result.Status, result.Comment, want)
	}
}

func TestRunTaskInvalidDeclaredExactReplyFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script agent fixture is POSIX-only")
	}

	tests := []struct {
		name       string
		kind       string
		wantDetail string
	}{
		{name: "deleted", kind: "deleted", wantDetail: "required declaration was not written"},
		{name: "empty", kind: "empty", wantDetail: "must not be empty"},
		{name: "whitespace only", kind: "whitespace", wantDetail: "must not be empty"},
		{name: "oversized", kind: "oversized", wantDetail: "exceeds 65536 bytes"},
		{name: "symlink", kind: "symlink", wantDetail: "must be a regular file"},
		{name: "directory", kind: "directory", wantDetail: "must be a regular file"},
		{name: "invalid UTF-8", kind: "invalid-utf8", wantDetail: "must contain valid text"},
		{name: "NUL", kind: "nul", wantDetail: "must contain valid text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspacesRoot := t.TempDir()
			fakeBin := filepath.Join(t.TempDir(), "claude")
			script := `#!/bin/sh
case "$DECLARATION_KIND" in
  deleted) rm "$MULTICA_EXACT_REPLY_FILE" ;;
  empty) : > "$MULTICA_EXACT_REPLY_FILE" ;;
  whitespace) printf ' \n\t' > "$MULTICA_EXACT_REPLY_FILE" ;;
  oversized) awk 'BEGIN { for (i = 0; i < 65537; i++) printf "x" }' > "$MULTICA_EXACT_REPLY_FILE" ;;
  symlink) printf '%s' 'valid target contents' > "$DECLARATION_TARGET"; rm "$MULTICA_EXACT_REPLY_FILE"; ln -s "$DECLARATION_TARGET" "$MULTICA_EXACT_REPLY_FILE" ;;
  directory) rm "$MULTICA_EXACT_REPLY_FILE"; mkdir "$MULTICA_EXACT_REPLY_FILE" ;;
  invalid-utf8) printf '\377' > "$MULTICA_EXACT_REPLY_FILE" ;;
  nul) printf '\000' > "$MULTICA_EXACT_REPLY_FILE" ;;
esac
IFS= read -r _
printf '%s\n' '{"type":"system","session_id":"session-invalid-exact-reply"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"session-invalid-exact-reply","result":"MODEL FALLBACK MUST NOT PUBLISH"}'
`
			if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
				t.Fatalf("write fake agent: %v", err)
			}

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(srv.Close)
			d := &Daemon{
				client:         NewClient(srv.URL),
				logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
				workspaces:     make(map[string]*workspaceState),
				runtimeIndex:   map[string]Runtime{"rt-invalid-exact-reply": {ID: "rt-invalid-exact-reply", Provider: "claude"}},
				activeEnvRoots: make(map[string]int),
				cfg: Config{
					WorkspacesRoot: workspacesRoot,
					AgentTimeout:   5 * time.Second,
					ServerBaseURL:  srv.URL,
					Agents:         map[string]AgentEntry{"claude": {Path: fakeBin}},
				},
			}
			task := Task{
				ID:          "task-invalid-exact-reply",
				WorkspaceID: "ws-invalid-exact-reply",
				RuntimeID:   "rt-invalid-exact-reply",
				IssueID:     "issue-invalid-exact-reply",
				AgentID:     "agent-invalid-exact-reply",
				AuthToken:   "mat_invalid_exact_reply",
				Agent: &AgentData{
					ID:   "agent-invalid-exact-reply",
					Name: "invalid-exact-reply-agent",
					CustomEnv: map[string]string{
						exactReplyRequiredEnv: "1",
						"DECLARATION_KIND":    tt.kind,
						"DECLARATION_TARGET":  filepath.Join(t.TempDir(), "symlink-target.md"),
					},
				},
			}

			result, err := d.runTask(context.Background(), task, "claude", 0, d.logger)
			if err != nil {
				t.Fatalf("runTask: %v", err)
			}
			if result.Status != "blocked" {
				t.Fatalf("runTask status = %q, want blocked (comment=%q)", result.Status, result.Comment)
			}
			if result.FailureReason != "agent_error.empty_or_unparseable_output" {
				t.Fatalf("failure reason = %q, want agent_error.empty_or_unparseable_output", result.FailureReason)
			}
			if !strings.Contains(result.Comment, tt.wantDetail) {
				t.Fatalf("comment = %q, want detail %q", result.Comment, tt.wantDetail)
			}
			if strings.Contains(result.Comment, "MODEL FALLBACK MUST NOT PUBLISH") {
				t.Fatalf("invalid declaration published provider fallback: %q", result.Comment)
			}
		})
	}
}

func TestRunTaskRequiredExactReplyMustReplaceArmedMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script agent fixture is POSIX-only")
	}

	d, _, cleanup := newLeaderReuseTestDaemon(t)
	defer cleanup()

	task := leaderReuseTestTask("task-required-exact-reply-idle")
	task.Agent.CustomEnv = map[string]string{exactReplyRequiredEnv: "1"}
	result, err := d.runTask(context.Background(), task, "claude", 0, d.logger)
	if err != nil {
		t.Fatalf("runTask: %v", err)
	}
	if result.Status != "blocked" {
		t.Fatalf("runTask result = status %q, comment %q; want blocked", result.Status, result.Comment)
	}
	if !strings.Contains(result.Comment, "required declaration was not written") {
		t.Fatalf("runTask comment = %q, want untouched-marker detail", result.Comment)
	}
	if strings.Contains(result.Comment, "done") {
		t.Fatalf("untouched required declaration published provider output: %q", result.Comment)
	}
}

func TestExactReplyRequiredAcceptsOnlyCanonicalOptIn(t *testing.T) {
	tests := []struct {
		name    string
		values  map[string]string
		want    bool
		wantErr bool
	}{
		{name: "absent"},
		{name: "enabled", values: map[string]string{exactReplyRequiredEnv: "1"}, want: true},
		{name: "wrong value", values: map[string]string{exactReplyRequiredEnv: "true"}, wantErr: true},
		{name: "wrong case", values: map[string]string{strings.ToLower(exactReplyRequiredEnv): "1"}, wantErr: true},
		{name: "case duplicate", values: map[string]string{
			exactReplyRequiredEnv:                  "1",
			strings.ToLower(exactReplyRequiredEnv): "1",
		}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := exactReplyRequired(tt.values)
			if (err != nil) != tt.wantErr {
				t.Fatalf("exactReplyRequired() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("exactReplyRequired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunTaskRejectsInvalidExactReplyOptInBeforeAgentLaunch(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
	}{
		{name: "wrong value", values: map[string]string{exactReplyRequiredEnv: "true"}},
		{name: "wrong case", values: map[string]string{strings.ToLower(exactReplyRequiredEnv): "1"}},
		{name: "case duplicate", values: map[string]string{
			exactReplyRequiredEnv:                  "1",
			strings.ToLower(exactReplyRequiredEnv): "1",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, launches, cleanup := newLeaderReuseTestDaemon(t)
			defer cleanup()
			task := leaderReuseTestTask("task-invalid-opt-in")
			task.Agent.CustomEnv = tt.values

			_, err := d.runTask(context.Background(), task, "claude", 0, d.logger)
			if err == nil || !strings.Contains(err.Error(), "must be exactly 1 when set") {
				t.Fatalf("runTask() error = %v, want invalid exact-reply opt-in", err)
			}
			if _, statErr := os.Stat(launches); !os.IsNotExist(statErr) {
				t.Fatalf("agent was launched for invalid opt-in; stat error = %v", statErr)
			}
		})
	}
}

func TestExactReplyProviderMarkerKeepsProviderOutput(t *testing.T) {
	dir := t.TempDir()
	if err := prepareExactReplyFile(dir); err != nil {
		t.Fatalf("prepareExactReplyFile(): %v", err)
	}
	path := filepath.Join(dir, exactReplyFileName)
	if err := os.WriteFile(path, []byte(exactReplyProviderOutput), 0o600); err != nil {
		t.Fatalf("write provider marker: %v", err)
	}

	reply, declared, err := readExactReplyFile(dir, true)
	if err != nil {
		t.Fatalf("readExactReplyFile(): %v", err)
	}
	if declared || reply != "" {
		t.Fatalf("provider marker returned reply %q, declared=%v; want provider output unchanged", reply, declared)
	}
}

func TestPrepareExactReplyFileRearmsStaleDeclaration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, exactReplyFileName)
	if err := os.WriteFile(path, []byte("stale task output"), 0o600); err != nil {
		t.Fatalf("seed stale declaration: %v", err)
	}
	if err := prepareExactReplyFile(dir); err != nil {
		t.Fatalf("prepareExactReplyFile(): %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read armed declaration: %v", err)
	}
	if string(got) != exactReplyFileIdle {
		t.Fatalf("armed declaration = %q, want %q", got, exactReplyFileIdle)
	}
}
