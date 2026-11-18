//go:build unix

package daemon

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestHandleTaskAgentDefaultDirectoryPreparesAndPreservesUserContext(t *testing.T) {
	redirectTestHome(t, t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	for _, kind := range []string{"issue", "chat", "autopilot"} {
		t.Run(kind, func(t *testing.T) {
			directory, capture := t.TempDir(), t.TempDir()
			const userRules = "# User context\nUSER_DIRECTORY_RULES\n"
			if err := os.WriteFile(filepath.Join(directory, "CLAUDE.md"), []byte(userRules), 0o600); err != nil {
				t.Fatal(err)
			}
			settings := []byte(`{"env":{"LOCAL_SETTINGS_SENTINEL":"preserved"}}`)
			if err := os.Mkdir(filepath.Join(directory, ".claude"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, ".claude", "settings.local.json"), settings, 0o600); err != nil {
				t.Fatal(err)
			}
			fake := filepath.Join(t.TempDir(), "claude-fixture")
			writeTestExecutable(t, fake, []byte(`#!/bin/sh
set -e
case "$*" in *--version*) echo '2.1.292'; exit 0;; esac
pwd -P > "$DIRECTORY_CAPTURE/cwd"
if [ -f CLAUDE.md ]; then cp CLAUDE.md "$DIRECTORY_CAPTURE/rules"; fi
if [ -f .claude/skills/directory-skill/SKILL.md ]; then cp .claude/skills/directory-skill/SKILL.md "$DIRECTORY_CAPTURE/skill"; fi
if [ -f .claude/settings.local.json ]; then cp .claude/settings.local.json "$DIRECTORY_CAPTURE/settings"; fi
IFS= read -r input
case "$input" in
 *'"type":"control_request"'*)
 printf '%s\n' '{"type":"control_response","response":{"subtype":"success","request_id":"multica-supplement-initialize","response":{"commands":[]}}}'
 IFS= read -r input;;
esac
printf '%s\n' "$input" > "$DIRECTORY_CAPTURE/input"
printf '%s\n' '{"type":"result","subtype":"success","session_id":"directory-session","result":"done"}'
cat >/dev/null
`))
			reports := make(chan string, 2)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/complete") || strings.HasSuffix(r.URL.Path, "/fail") {
					reports <- r.URL.Path
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()
			logs := &lockedBuffer{}
			logger := slog.New(slog.NewTextHandler(logs, nil))
			d := New(Config{DaemonID: "directory-daemon", ServerBaseURL: srv.URL, WorkspacesRoot: t.TempDir(), AgentTimeout: 20 * time.Second, AgentLocalDirectories: map[string]string{directoryTestAgentID: directory}, Agents: map[string]AgentEntry{"claude": {Path: fake}}}, logger)
			d.executionEnvironmentCommand = nil
			d.cancelPollInterval = time.Hour
			d.runtimeIndex["directory-runtime"] = Runtime{ID: "directory-runtime", Provider: "claude"}
			d.setAgentVersion("claude", "2.1.292")
			task := Task{ID: "directory-" + kind, WorkspaceID: "directory-workspace", RuntimeID: "directory-runtime", AgentID: directoryTestAgentID, AuthToken: "mat_directory_fixture", Agent: &AgentData{ID: directoryTestAgentID, Name: "Directory agent", Instructions: "AGENT_DIRECTORY_RULES", CustomEnv: map[string]string{"DIRECTORY_CAPTURE": capture}, Skills: []SkillData{{ID: "directory-skill", Name: "Directory skill", Content: "DIRECTORY_BOUND_SKILL"}}}}
			switch kind {
			case "issue":
				task.IssueID = "directory-issue"
			case "chat":
				task.ChatSessionID = "directory-chat"
				task.ChatMessage = "Check local context"
			case "autopilot":
				task.AutopilotRunID = "directory-autopilot"
			}
			configured := d.cfg.AgentLocalDirectories
			d.cfg.AgentLocalDirectories = nil
			prior := task
			prior.ID = task.ID + "-prior"
			priorAgent := *task.Agent
			priorAgent.CustomEnv = map[string]string{"DIRECTORY_CAPTURE": t.TempDir()}
			prior.Agent = &priorAgent
			previous, err := d.runTask(t.Context(), prior, "claude", 0, logger)
			if err != nil || previous.Status != "completed" {
				t.Fatalf("prior managed run: %+v, %v", previous, err)
			}
			d.cfg.AgentLocalDirectories = configured
			task.PriorWorkDir = previous.WorkDir
			d.handleTask(t.Context(), task, newTestTaskSlot(0))
			read := func(name string) string {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(capture, name))
				if err != nil {
					t.Fatal(err)
				}
				return string(data)
			}
			canonical, err := filepath.EvalSymlinks(directory)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(read("cwd")); got != canonical {
				t.Fatalf("provider cwd = %q, want agent directory %q; logs: %s", got, canonical, logs.String())
			}
			select {
			case endpoint := <-reports:
				if !strings.HasSuffix(endpoint, "/complete") {
					t.Fatalf("run reported failure: %s", endpoint)
				}
			default:
				t.Fatal("run did not report successful completion")
			}
			if got := read("rules"); !strings.Contains(got, "USER_DIRECTORY_RULES") {
				t.Fatal("user context did not reach the provider directory")
			}
			if got := read("rules") + read("input"); !strings.Contains(got, "AGENT_DIRECTORY_RULES") {
				t.Fatal("managed agent instructions missing from prepared context")
			}
			if got := read("skill"); !strings.Contains(got, "DIRECTORY_BOUND_SKILL") {
				t.Fatal("bound skill missing from prepared directory")
			}
			if got := read("settings"); got != string(settings) {
				t.Fatal("provider did not see original project settings")
			}
			after, err := os.ReadFile(filepath.Join(directory, ".claude", "settings.local.json"))
			if err != nil || string(after) != string(settings) {
				t.Fatalf("user settings changed after finalization: %v", err)
			}
			after, err = os.ReadFile(filepath.Join(directory, "CLAUDE.md"))
			if err != nil || !strings.Contains(string(after), "USER_DIRECTORY_RULES") {
				t.Fatalf("user rules lost after finalization: %v", err)
			}
			root, err := execenv.ResolveRootDir(taskRootDirParams(d.cfg.WorkspacesRoot, task))
			if err != nil {
				t.Fatal(err)
			}
			meta, err := execenv.ReadGCMeta(root)
			if err != nil || meta == nil || !meta.LocalDirectory {
				t.Fatalf("user directory missing GC protection: %+v, %v", meta, err)
			}
		})
	}
}
