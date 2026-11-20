//go:build !windows

package daemon

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

const traeBriefCanary = "TRAE_BRIEF_CANARY"
const traePublicVersionReceipt = "traecli 0.209.1\ninstallation: public"
const traeLegacyVersionReceipt = "trae-cli version 0.121.3"

func writeTraeBriefTestCLI(t *testing.T, path, version string) {
	t.Helper()
	writeTestExecutable(t, path, []byte(`#!/bin/sh
version='`+version+`'
if [ "$1" = --version ]; then
  printf '%s\n' "$version"
  exit 0
fi
phase=initial
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}\n' "$id";;
    *'"method":"session/new"'*)
      if [ -f "$TRAE_BRIEF_CAPTURE/rejected" ]; then phase=retry; fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"test-session"}}\n' "$id";;
    *'"method":"session/load"'*missing-session*)
      touch "$TRAE_BRIEF_CAPTURE/rejected"
      printf '{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"session not found"}}\n' "$id";;
    *'"method":"session/load"'*)
      phase=resume
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id";;
    *'"method":"session/prompt"'*)
      printf '%s\n' "$line" > "$TRAE_BRIEF_CAPTURE/$phase.json"
      printf '%s\n%s' "$0" "$version" > "$TRAE_BRIEF_CAPTURE/$phase.launch"
      cat AGENTS.md > "$TRAE_BRIEF_CAPTURE/$phase.md"
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0;;
  esac
done
`))
}

func newTraeBriefTest(t *testing.T) (*Daemon, Task, string) {
	t.Helper()
	capture := t.TempDir()
	t.Setenv("TRAE_BRIEF_CAPTURE", capture)
	bin := t.TempDir()
	target := filepath.Join(bin, "traex")
	alias := filepath.Join(bin, "traecli")
	writeTraeBriefTestCLI(t, target, traePublicVersionReceipt)
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	version, err := agent.DetectVersion(t.Context(), agent.NewCommand(alias, nil))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)
	d := &Daemon{
		client: NewClient(srv.URL), logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		activeEnvRoots: make(map[string]int),
		agentVersions:  map[string]string{"traecli": version},
		cfg: Config{
			DaemonID: "test-daemon", WorkspacesRoot: t.TempDir(), AgentTimeout: 5 * time.Second, ServerBaseURL: srv.URL,
			Agents: map[string]AgentEntry{"traecli": {Path: alias}},
		},
	}
	task := Task{
		ID: "test-first", WorkspaceID: "test-workspace", RuntimeID: "test-runtime",
		AgentID: "test-agent", AuthToken: "mat_test_trae_brief", ChatSessionID: "test-chat", ChatMessage: "FIRST_TURN",
		Agent: &AgentData{ID: "test-agent", Name: "test", Instructions: traeBriefCanary},
	}
	return d, task, capture
}

func readTraeBriefCapture(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func readTraeBriefPrompt(t *testing.T, dir, phase string) string {
	t.Helper()
	var request struct {
		Params struct{ Prompt []struct{ Text string } }
	}
	if err := json.Unmarshal([]byte(readTraeBriefCapture(t, dir, phase+".json")), &request); err != nil || len(request.Params.Prompt) != 1 {
		t.Fatalf("%s: invalid ACP prompt: %v", phase, err)
	}
	return request.Params.Prompt[0].Text
}

func TestRunTaskTraeXRuntimeBrief(t *testing.T) {
	d, task, capture := newTraeBriefTest(t)
	first, err := d.runTask(t.Context(), task, "traecli", 0, d.logger)
	if err != nil || first.Status != "completed" || first.SessionID != "test-session" {
		t.Fatalf("first run = %+v, error = %v", first, err)
	}
	task.ID, task.ChatMessage = "test-resume", "SECOND_TURN"
	task.PriorWorkDir, task.PriorSessionID = first.WorkDir, first.SessionID
	task.Agent.Instructions = traeBriefCanary + "_UPDATED"
	resumed, err := d.runTask(t.Context(), task, "traecli", 0, d.logger)
	if err != nil || resumed.Status != "completed" || resumed.SessionID != first.SessionID {
		t.Fatalf("resumed run = %+v, error = %v", resumed, err)
	}
	task.ID, task.ChatMessage = "test-retry", "THIRD_TURN"
	task.PriorWorkDir, task.PriorSessionID = resumed.WorkDir, "missing-session"
	retried, err := d.runTask(t.Context(), task, "traecli", 0, d.logger)
	if err != nil || retried.Status != "completed" || retried.RetiredSessionID != "missing-session" {
		t.Fatalf("rejected resume did not retry fresh: %+v, error = %v", retried, err)
	}

	target, err := canonicalPath(d.cfg.Agents["traecli"].Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, turn := range []struct{ phase, message, rule string }{
		{"initial", "FIRST_TURN", traeBriefCanary},
		{"resume", "SECOND_TURN", traeBriefCanary + "_UPDATED"},
		{"retry", "THIRD_TURN", traeBriefCanary + "_UPDATED"},
	} {
		brief := readTraeBriefCapture(t, capture, turn.phase+".md")
		if strings.Count(brief, traeBriefCanary) != 1 || !strings.Contains(brief, "\n"+turn.rule+"\n") {
			t.Errorf("%s: AGENTS.md must contain one copy of the current rule", turn.phase)
		}
		prompt := readTraeBriefPrompt(t, capture, turn.phase)
		if strings.Contains(prompt, traeBriefCanary) || !strings.HasPrefix(prompt, "Follow the Multica runtime brief in the task's `AGENTS.md`.") || !strings.Contains(prompt, turn.message) {
			t.Errorf("%s: ACP prompt must refer to AGENTS.md and contain the task message without the rule body", turn.phase)
		}
		launched, _, _ := strings.Cut(readTraeBriefCapture(t, capture, turn.phase+".launch"), "\n")
		if launched != target {
			t.Errorf("%s: launch used %q, want verified target %q", turn.phase, launched, target)
		}
	}
}

func TestRunTaskTraeRuntimeBriefInPlaceIssue(t *testing.T) {
	d, task, capture := newTraeBriefTest(t)
	workDir := t.TempDir()
	ref, err := json.Marshal(localDirectoryRef{
		LocalPath: workDir, DaemonID: d.cfg.DaemonID, ExecutionMode: localDirectoryModeInPlace,
	})
	if err != nil {
		t.Fatal(err)
	}
	task.ChatSessionID, task.IssueID = "", "test-issue"
	task.ProjectResources = []ProjectResourceData{{ResourceType: localDirectoryResourceType, ResourceRef: ref}}
	result, err := d.runTask(t.Context(), task, "traecli", 0, d.logger)
	if err != nil || result.Status != "completed" || result.WorkDir != workDir {
		t.Fatalf("in-place issue run = %+v, error = %v", result, err)
	}
	if prompt := readTraeBriefPrompt(t, capture, "initial"); !strings.Contains(prompt, traeBriefCanary) {
		t.Error("in-place Trae issue must receive its rule inline")
	}
}

func TestRunTaskTraeRuntimeBriefRetargetedAlias(t *testing.T) {
	d, task, capture := newTraeBriefTest(t)
	alias := d.cfg.Agents["traecli"].Path
	legacy := filepath.Join(filepath.Dir(alias), "trae-cli")
	writeTraeBriefTestCLI(t, legacy, traeLegacyVersionReceipt)
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(legacy, alias); err != nil {
		t.Fatal(err)
	}
	result, err := d.runTask(t.Context(), task, "traecli", 0, d.logger)
	if err != nil || result.Status != "completed" {
		t.Fatalf("retargeted Trae run = %+v, error = %v", result, err)
	}
	_, version, _ := strings.Cut(readTraeBriefCapture(t, capture, "initial.launch"), "\n")
	if version != traeLegacyVersionReceipt {
		t.Fatalf("launched CLI reported %q, want legacy CLI", version)
	}
	if prompt := readTraeBriefPrompt(t, capture, "initial"); !strings.Contains(prompt, traeBriefCanary) {
		t.Error("legacy Trae must receive its rule inline after the alias is retargeted")
	}
}

func TestTraeRuntimeBriefInlineWhenFileWriteFails(t *testing.T) {
	prompt, inline := traeRuntimeBriefPrompt("task prompt", traeBriefCanary, false)
	if prompt != "task prompt" || inline != traeBriefCanary {
		t.Fatalf("prompt = %q and inline brief = %q, want plain task prompt and complete inline brief", prompt, inline)
	}
}
