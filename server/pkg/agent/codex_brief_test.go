package agent

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// writeBriefRecordingCodex returns a fake app-server that answers thread/start
// and thread/resume with threadID and saves the thread-setup and turn/start
// requests next to itself.
func writeBriefRecordingCodex(t *testing.T, threadID string) string {
	t.Helper()
	return writeFakeCodexAppServer(t, `
dir="$(dirname "$0")"
IFS= read -r line
echo '{"jsonrpc":"2.0","id":1,"result":{}}'
IFS= read -r line
IFS= read -r line
printf '%s\n' "$line" > "$dir/thread-setup.json"
echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"`+threadID+`"}}}'
IFS= read -r line
printf '%s\n' "$line" > "$dir/turn-start.json"
echo '{"jsonrpc":"2.0","id":3,"result":{}}'
echo '{"jsonrpc":"2.0","method":"turn/started","params":{"threadId":"`+threadID+`","turn":{"id":"turn-1"}}}'
echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"`+threadID+`","turnId":"turn-1","item":{"type":"agentMessage","id":"msg-1","text":"done"}}}'
echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"`+threadID+`","turn":{"id":"turn-1","status":"completed"}}}'
`)
}

type briefRun struct {
	method                string
	developerInstructions any
	turnText              string
}

func runBriefRecordingCodex(t *testing.T, codexHome, threadID string, opts ExecOptions) briefRun {
	t.Helper()
	fakePath := writeBriefRecordingCodex(t, threadID)
	opts.Timeout = 5 * time.Second
	opts.SemanticInactivityTimeout = 5 * time.Second
	cfg := Config{Logger: slog.Default(), Env: map[string]string{"CODEX_HOME": codexHome}}
	result, _ := executeFakeCodexCollectingMessagesWithConfig(t, fakePath, cfg, opts, 10*time.Second)
	if result.Status != "completed" {
		t.Fatalf("expected completed run, got %+v", result)
	}

	var setup struct {
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	readJSON(t, filepath.Join(filepath.Dir(fakePath), "thread-setup.json"), &setup)
	var turn struct {
		Params struct {
			Input []struct {
				Text string `json:"text"`
			} `json:"input"`
		} `json:"params"`
	}
	readJSON(t, filepath.Join(filepath.Dir(fakePath), "turn-start.json"), &turn)
	if len(turn.Params.Input) != 1 {
		t.Fatalf("expected one turn input block, got %d", len(turn.Params.Input))
	}
	return briefRun{
		method:                setup.Method,
		developerInstructions: setup.Params["developerInstructions"],
		turnText:              turn.Params.Input[0].Text,
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

// A resumed thread keeps the developerInstructions it started with, so the
// backend resends the brief in the turn exactly when it changed.
func TestCodexResumeResendsBriefOnlyWhenChanged(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	codexHome := t.TempDir()
	const threadID = "thr_brief"
	const briefV1 = "BRIEF-V1 workflow rules"
	const briefV2 = "BRIEF-V2 workflow rules"

	fresh := runBriefRecordingCodex(t, codexHome, threadID, ExecOptions{Cwd: t.TempDir(), SystemPrompt: briefV1})
	if fresh.method != "thread/start" || fresh.developerInstructions != briefV1 {
		t.Fatalf("fresh thread must start with the brief as developerInstructions, got %s %v", fresh.method, fresh.developerInstructions)
	}
	if strings.Contains(fresh.turnText, "BRIEF-V1") {
		t.Fatalf("fresh thread already holds the brief; turn must not repeat it: %q", fresh.turnText)
	}

	resumeOpts := func(brief string) ExecOptions {
		return ExecOptions{Cwd: t.TempDir(), ResumeSessionID: threadID, ResumeExpected: true, SystemPrompt: brief}
	}

	same := runBriefRecordingCodex(t, codexHome, threadID, resumeOpts(briefV1))
	if same.method != "thread/resume" || same.developerInstructions != nil {
		t.Fatalf("resume must not send developerInstructions, got %s %v", same.method, same.developerInstructions)
	}
	if strings.Contains(same.turnText, "BRIEF-V1") || strings.Contains(same.turnText, "multica-runtime-brief-update") {
		t.Fatalf("unchanged brief must not be resent: %q", same.turnText)
	}

	changed := runBriefRecordingCodex(t, codexHome, threadID, resumeOpts(briefV2))
	if !strings.HasPrefix(changed.turnText, "<multica-runtime-brief-update>") || !strings.Contains(changed.turnText, briefV2) {
		t.Fatalf("changed brief must be resent ahead of the prompt: %q", changed.turnText)
	}

	again := runBriefRecordingCodex(t, codexHome, threadID, resumeOpts(briefV2))
	if strings.Contains(again.turnText, "BRIEF-V2") {
		t.Fatalf("brief resent once must not be resent again: %q", again.turnText)
	}
}

// A thread started before the brief moved out of AGENTS.md has no record, so
// its first resume resends the brief.
func TestCodexResumeWithoutRecordResendsBrief(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	got := runBriefRecordingCodex(t, t.TempDir(), "thr_legacy", ExecOptions{
		Cwd: t.TempDir(), ResumeSessionID: "thr_legacy", ResumeExpected: true, SystemPrompt: "BRIEF-LEGACY",
	})
	if !strings.Contains(got.turnText, "BRIEF-LEGACY") {
		t.Fatalf("resume of an unrecorded thread must resend the brief: %q", got.turnText)
	}
}

func TestCodexBriefRecordPathRejectsNonPlainThreadIDs(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	for _, id := range []string{"", ".", "..", "../escape", "a/b", ".hidden"} {
		if got := codexBriefRecordPath(home, id); got != "" {
			t.Errorf("codexBriefRecordPath(%q) = %q, want empty", id, got)
		}
	}
	want := filepath.Join(home, "sessions", codexBriefRecordDir, "thr_1")
	if got := codexBriefRecordPath(home, "thr_1"); got != want {
		t.Errorf("codexBriefRecordPath = %q, want %q", got, want)
	}
}
