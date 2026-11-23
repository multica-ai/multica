package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// Exercise the complete claim -> environment -> prompt -> backend path. The
// fixture rejects a resumed session only after capturing its native input, so
// the next process must receive a freshly rebuilt invocation and continuity
// notice, not the preceding attempt's warm prompt.
func TestRunTaskExplicitSkillFirstTurnAndFreshRetry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script agent fixture is POSIX-only")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	capture := t.TempDir()
	t.Setenv("SKILL_CAPTURE", capture)
	fake := filepath.Join(t.TempDir(), "claude-fixture")
	writeTestExecutable(t, fake, []byte(`#!/bin/sh
case "$*" in *--version*) echo '2.1.292'; exit 0;; esac
attempt=1
if [ -f "$SKILL_CAPTURE/count" ]; then
  attempt=$(cat "$SKILL_CAPTURE/count")
  attempt=$((attempt + 1))
fi
printf '%s' "$attempt" > "$SKILL_CAPTURE/count"
printf '%s\n' "$@" > "$SKILL_CAPTURE/args-$attempt"
IFS= read -r input
case "$input" in
  *'"type":"control_request"'*)
    printf '%s\n' '{"type":"control_response","response":{"subtype":"success","request_id":"multica-supplement-initialize","response":{"commands":[{"name":"review-code","description":"Bound skill (project)"}]}}}'
    IFS= read -r input;;
esac
printf '%s\n' "$input" > "$SKILL_CAPTURE/input-$attempt.json"
case "$*" in
  *--resume*) printf '%s\n' 'No conversation found with session ID: skill-session-1' >&2
    printf '%s\n' '{"type":"result","subtype":"error_during_execution","is_error":true}';;
  *) printf '{"type":"result","subtype":"success","session_id":"skill-session-%s","result":"done"}\n' "$attempt";;
esac
cat >/dev/null
`))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	d := &Daemon{
		client: NewClient(srv.URL), logger: logger,
		workspaces: make(map[string]*workspaceState), activeEnvRoots: make(map[string]int),
		runtimeIndex: map[string]Runtime{"test-rt": {ID: "test-rt", Provider: "claude"}},
		cfg: Config{
			WorkspacesRoot: t.TempDir(), AgentTimeout: 5 * time.Second, ServerBaseURL: srv.URL,
			Agents: map[string]AgentEntry{"claude": {Path: fake}},
		},
	}
	message := "[/untrusted label](slash://skill/selected-id) USER_REQUEST"
	task := Task{
		ID: "first-task", WorkspaceID: "test-ws", RuntimeID: "test-rt",
		AgentID: "test-agent", AuthToken: "mat_test_skills", ChatSessionID: "test-chat",
		ChatMessage: message, ChatSkillMessage: &message,
		Agent: &AgentData{ID: "test-agent", Name: "test", Skills: []SkillData{
			{ID: "selected-id", Name: "Review Code", Content: "---\ndisable-model-invocation: true\n---\nSELECTED_BODY"},
		}},
	}
	first, err := d.runTask(t.Context(), task, "claude", 0, logger)
	if err != nil || first.Status != "completed" || first.SessionID != "skill-session-1" {
		t.Fatalf("first run=%+v, err=%v", first, err)
	}
	task.ID = "followup-task"
	task.PriorWorkDir, task.PriorSessionID = first.WorkDir, first.SessionID
	retried, err := d.runTask(t.Context(), task, "claude", 0, logger)
	if err != nil || retried.Status != "completed" || retried.SessionID != "skill-session-3" || retried.RetiredSessionID != first.SessionID {
		t.Fatalf("resumed run did not retry fresh: %+v, err=%v", retried, err)
	}
	for attempt := 1; attempt <= 3; attempt++ {
		wire, err := os.ReadFile(filepath.Join(capture, fmt.Sprintf("input-%d.json", attempt)))
		if err != nil {
			t.Fatal(err)
		}
		var input struct {
			Message struct{ Content []struct{ Text string } }
		}
		if err := json.Unmarshal(wire, &input); err != nil {
			t.Fatal(err)
		}
		if len(input.Message.Content) != 2 {
			t.Fatalf("attempt %d did not receive native skill blocks: %s", attempt, wire)
		}
		platform, user := input.Message.Content[0].Text, input.Message.Content[1].Text
		if !strings.HasPrefix(user, "/review-code ") || !strings.Contains(user, "USER_REQUEST") || strings.Contains(user, "slash://") {
			t.Errorf("attempt %d lost the ID-resolved native command/user input: %q", attempt, user)
		}
		if !strings.Contains(platform, "Explicitly selected skills:\n- review-code\n") || strings.Contains(platform, "SELECTED_BODY") {
			t.Errorf("attempt %d used fallback instead of the installed skill: %q", attempt, platform)
		}
		wantNotices := 0
		if attempt == 3 {
			wantNotices = 1
		}
		if got := strings.Count(platform+user, execenv.SessionContinuityNoticeChatTranscript); got != wantNotices {
			t.Errorf("attempt %d continuity notice count=%d, want %d", attempt, got, wantNotices)
		}
		args, err := os.ReadFile(filepath.Join(capture, fmt.Sprintf("args-%d", attempt)))
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(string(args), "--resume\n"); got != (attempt == 2) {
			t.Errorf("attempt %d resume flag=%v", attempt, got)
		}
	}
}
