package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestRunLocalCLIEndToEndWithFakeAPI(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh unavailable")
	}
	tmp := t.TempDir()
	codexPath := filepath.Join(tmp, "codex")
	if err := os.Symlink("/bin/sh", codexPath); err != nil {
		t.Fatalf("symlink codex shim: %v", err)
	}
	origExecute := executeLocalCLIForRun
	executeLocalCLIForRun = func(args []string, cwd, cliName string, env localCLIEnv, initialPrompt string, reporter *localRunReporter) (int, error) {
		if cliName != "codex" {
			t.Fatalf("cliName = %q, want codex", cliName)
		}
		if len(args) == 0 || args[0] != codexPath {
			t.Fatalf("args = %v, want codex path first", args)
		}
		if env.RunID != "run-1" || env.IssueID != "issue-1" {
			t.Fatalf("env = %+v, want run and issue metadata", env)
		}
		if !strings.Contains(initialPrompt, "Assigned issue ID: issue-1") {
			t.Fatalf("initialPrompt missing issue context: %q", initialPrompt)
		}
		return 0, nil
	}
	defer func() { executeLocalCLIForRun = origExecute }()
	var (
		createBody map[string]any
		patches    []map[string]any
		messages   []map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/MUL-1":
			json.NewEncoder(w).Encode(map[string]any{
				"id":           "issue-1",
				"identifier":   "MUL-1",
				"title":        "Fake issue",
				"status":       "todo",
				"priority":     "medium",
				"description":  "Do it",
				"workspace_id": "ws-1",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/issues/issue-1/local-runs":
			json.NewDecoder(r.Body).Decode(&createBody)
			json.NewEncoder(w).Encode(map[string]any{
				"id":          "run-1",
				"issue_id":    "issue-1",
				"cli_name":    "sh",
				"context_dir": "",
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/local-runs/run-1":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			patches = append(patches, body)
			json.NewEncoder(w).Encode(map[string]any{"id": "run-1"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/local-runs/run-1/messages":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			messages = append(messages, body)
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cmd := newRunCommandForTest()
	if err := cmd.Flags().Set("server-url", srv.URL); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("workspace-id", "ws-1"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("cwd", tmp); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("comments", "off"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MULTICA_TOKEN", "token-1")
	t.Setenv("MULTICA_SERVER_URL", "")
	t.Setenv("MULTICA_WORKSPACE_ID", "")

	err := runLocalCLI(cmd, []string{"MUL-1", codexPath, "-c", `printf '{"type":"result","result":"done"}\n'`})
	if err != nil {
		t.Fatalf("runLocalCLI: %v", err)
	}

	if createBody["cli_name"] != "codex" || createBody["comments_mode"] != "off" || createBody["work_dir"] != tmp {
		t.Fatalf("unexpected create body: %+v", createBody)
	}
	if len(patches) < 2 {
		t.Fatalf("patches = %+v, want running and terminal status updates", patches)
	}
	if patches[0]["status"] != "running" {
		t.Fatalf("first patch = %+v, want running status update", patches[0])
	}
	if _, ok := patches[0]["context_dir"]; ok {
		t.Fatalf("first patch = %+v, did not want context_dir", patches[0])
	}
	lastPatch := patches[len(patches)-1]
	if lastPatch["status"] != "completed" || int(lastPatch["exit_code"].(float64)) != 0 {
		t.Fatalf("last patch = %+v, want completed exit 0", lastPatch)
	}
	if finals := mapMessagesByType(messages, "final"); len(finals) != 0 {
		t.Fatalf("final messages = %+v, want no bootstrap final", finals)
	}
	if _, err := os.Stat(filepath.Join(tmp, ".multica", "runs", "run-1", "issue.md")); !os.IsNotExist(err) {
		t.Fatalf("issue context file exists or stat failed unexpectedly: %v", err)
	}
}

func TestRunLocalCLIRejectsUnsupportedLocalAgent(t *testing.T) {
	cmd := newRunCommandForTest()

	err := runLocalCLI(cmd, []string{"MUL-1", "/bin/sh", "-c", "true"})
	if err == nil || !strings.Contains(err.Error(), "当前 Agent 尚未支持，敬请期待") {
		t.Fatalf("runLocalCLI error = %v, want unsupported agent message", err)
	}
}

func TestSupportsLocalRunAgentIncludesProviderRegistry(t *testing.T) {
	if !supportsLocalRunAgent("codex") || !supportsLocalRunAgent("claude") {
		t.Fatalf("expected codex and claude providers to be supported")
	}
	if supportsLocalRunAgent("sh") {
		t.Fatalf("unexpected support for shell without a provider")
	}
}

func newRunCommandForTest() *cobra.Command {
	cmd := &cobra.Command{Use: "run"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("cwd", "", "")
	cmd.Flags().Bool("no-status-update", false, "")
	cmd.Flags().String("comments", "thread", "")
	return cmd
}

func TestInferCLIName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "codex", want: "codex"},
		{in: "/usr/local/bin/claude", want: "claude"},
		{in: `C:\Tools\codex.exe`, want: `C:\Tools\codex`},
	}

	for _, tt := range tests {
		if got := inferCLIName(tt.in); got != tt.want {
			t.Fatalf("inferCLIName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLocalRunPromptUsesPlatformContextCommandsAndSilence(t *testing.T) {
	got := localRunPrompt("issue-1")
	if got == "" || !containsAll(got, []string{
		"Multica issue issue-1",
		"Assigned issue ID: issue-1",
		"`multica issue get issue-1 --output json`",
		"`multica issue comment list issue-1 --output json`",
		"Do not use any other `multica` command during bootstrap",
		"read the assigned issue and its comments only",
		"Do not proactively fetch parent issues, child issues, or issues mentioned in text",
		"After loading context, produce no output",
		"Wait silently for the user's next input",
	}) {
		t.Fatalf("prompt %q does not include platform context command instructions", got)
	}
	for _, forbidden := range []string{
		".multica",
		"runs",
		"context directory",
		"Issue JSON:",
		"Comments JSON:",
		`"title": "Fake issue"`,
		`"content": "Prior decision"`,
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("prompt %q contains forbidden reference %q", got, forbidden)
		}
	}
}

func TestReporterIgnoresPostsAfterClose(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	reporter.Close()

	reporter.Post(localCLIMessage{Type: "raw", Content: "late"})
	if got := len(poster.messages()); got != 0 {
		t.Fatalf("messages after close = %d, want 0", got)
	}
}

func TestLocalRunHeartbeatPatchesRunningUntilStopped(t *testing.T) {
	patcher := &fakeLocalRunPatcher{}
	stop := startLocalRunHeartbeat(patcher, "run-1", 10*time.Millisecond)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if patcher.count() > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	stop()
	got := patcher.count()
	if got == 0 {
		t.Fatalf("heartbeat did not patch running status")
	}
	time.Sleep(30 * time.Millisecond)
	if after := patcher.count(); after != got {
		t.Fatalf("heartbeat patched after stop: before=%d after=%d", got, after)
	}
	if path, status := patcher.last(); path != "/api/local-runs/run-1" || status != "running" {
		t.Fatalf("last patch = %q/%q, want local run running", path, status)
	}
}

func TestRunLocalRunPTYReportsExitCode(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh unavailable")
	}
	tmp := t.TempDir()

	exitCode, err := runLocalRunPTY(localRunPTYOptions{
		Args: []string{"/bin/sh", "-c", `printf '{"type":"result","result":"done"}\n'; exit 7`},
		Cwd:  tmp,
		Env:  os.Environ(),
	})

	if exitCode != 7 {
		t.Fatalf("exitCode = %d, want 7", exitCode)
	}
	if err == nil {
		t.Fatalf("expected non-nil error for exit 7")
	}
}

func TestRunLocalRunPTYWritesInitialStdin(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh unavailable")
	}
	tmp := t.TempDir()

	exitCode, err := runLocalRunPTY(localRunPTYOptions{
		Args:         []string{"/bin/sh", "-c", `read line; test "$line" = "embedded issue context prompt"`},
		Cwd:          tmp,
		Env:          os.Environ(),
		InitialStdin: "embedded issue context prompt\n",
	})

	if exitCode != 0 || err != nil {
		t.Fatalf("runLocalRunPTY exitCode=%d err=%v", exitCode, err)
	}
}

func TestRunLocalRunPTYReturnsWhenChildExitsWithoutOutput(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh unavailable")
	}
	tmp := t.TempDir()
	done := make(chan struct {
		exitCode int
		err      error
	}, 1)
	go func() {
		exitCode, err := runLocalRunPTY(localRunPTYOptions{
			Args: []string{"/bin/sh", "-c", "exit 0"},
			Cwd:  tmp,
			Env:  os.Environ(),
		})
		done <- struct {
			exitCode int
			err      error
		}{exitCode: exitCode, err: err}
	}()

	select {
	case result := <-done:
		if result.exitCode != 0 || result.err != nil {
			t.Fatalf("runLocalRunPTY exitCode=%d err=%v", result.exitCode, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("runLocalRunPTY did not return after child exited")
	}
}

func TestValidateCodexRemoteArgsRejectsManagedFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--remote", "ws://127.0.0.1:1"},
		{"--remote=ws://127.0.0.1:1"},
		{"app-server"},
		{"exec"},
		{"review"},
	} {
		if err := validateCodexRemoteArgs(args); err == nil {
			t.Fatalf("validateCodexRemoteArgs(%v) = nil, want error", args)
		}
	}
	if err := validateCodexRemoteArgs([]string{"--model", "gpt-5.5"}); err != nil {
		t.Fatalf("validateCodexRemoteArgs ordinary args: %v", err)
	}
}

func TestCodexAppServerMapperMapsUserAndFinal(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	mapper := newCodexAppServerMapper(reporter, "bootstrap prompt")

	mapper.Observe(false, []byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"user-1","type":"userMessage","content":[{"type":"text","text":"你好"}]}}}`))
	mapper.Observe(false, []byte(`{"method":"item/agentMessage/delta","params":{"threadId":"thread-1","turnId":"turn-1","itemId":"agent-1","delta":"你好"}}`))
	mapper.Observe(false, []byte(`{"method":"item/agentMessage/delta","params":{"threadId":"thread-1","turnId":"turn-1","itemId":"agent-1","delta":"。"}}`))
	mapper.Observe(false, []byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"agent-1","type":"agentMessage","phase":"final_answer","text":""}}}`))
	reporter.Close()

	messages := poster.messages()
	inputs := userInputMessages(messages)
	if len(inputs) != 1 || inputs[0].Content != "你好" || inputs[0].Source != codexAppServerSource {
		t.Fatalf("inputs = %+v, want codex app-server user input", inputs)
	}
	finals := finalMessages(messages)
	if len(finals) != 1 || finals[0].Content != "你好。" || finals[0].SourceKey == "" {
		t.Fatalf("finals = %+v, want accumulated final", finals)
	}
	texts := localMessagesByType(messages, "text")
	if len(texts) != 1 || texts[0].Content != "你好。" {
		t.Fatalf("texts = %+v, want accumulated text", texts)
	}
}

func TestCodexAppServerMapperSkipsBootstrapComments(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	mapper := newCodexAppServerMapper(reporter, "bootstrap prompt")

	mapper.Observe(false, []byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"user-1","type":"userMessage","text":"bootstrap prompt"}}}`))
	mapper.Observe(false, []byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"agent-1","type":"agentMessage","phase":"final_answer","text":"should stay silent"}}}`))
	reporter.Close()

	if inputs := userInputMessages(poster.messages()); len(inputs) != 0 {
		t.Fatalf("inputs = %+v, want bootstrap user skipped", inputs)
	}
	if finals := finalMessages(poster.messages()); len(finals) != 0 {
		t.Fatalf("finals = %+v, want bootstrap final skipped", finals)
	}
}

func TestCodexAppServerMapperMapsCommandExecution(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	mapper := newCodexAppServerMapper(reporter, "")

	mapper.Observe(false, []byte(`{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"cmd-1","type":"commandExecution","command":"go test ./cmd/multica"}}}`))
	mapper.Observe(false, []byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"cmd-1","type":"commandExecution","aggregatedOutput":"ok\n"}}}`))
	reporter.Close()

	messages := poster.messages()
	uses := localMessagesByType(messages, "tool_use")
	if len(uses) != 1 || uses[0].Tool != "exec_command" || uses[0].Input["command"] != "go test ./cmd/multica" {
		t.Fatalf("tool uses = %+v, want exec_command with command input", uses)
	}
	results := localMessagesByType(messages, "tool_result")
	if len(results) != 1 || results[0].Tool != "exec_command" || results[0].Output != "ok" {
		t.Fatalf("tool results = %+v, want exec_command aggregated output", results)
	}
}

func TestCodexAppServerMapperMapsFileChange(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	mapper := newCodexAppServerMapper(reporter, "")

	mapper.Observe(false, []byte(`{"method":"item/started","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"patch-1","type":"fileChange"}}}`))
	mapper.Observe(false, []byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"patch-1","type":"fileChange"}}}`))
	reporter.Close()

	messages := poster.messages()
	uses := localMessagesByType(messages, "tool_use")
	if len(uses) != 1 || uses[0].Tool != "patch_apply" {
		t.Fatalf("tool uses = %+v, want patch_apply", uses)
	}
	results := localMessagesByType(messages, "tool_result")
	if len(results) != 1 || results[0].Tool != "patch_apply" {
		t.Fatalf("tool results = %+v, want patch_apply", results)
	}
}

func TestCodexAppServerMapperMapsErrors(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	mapper := newCodexAppServerMapper(reporter, "")

	mapper.Observe(false, []byte(`{"method":"turn/completed","params":{"threadId":"thread-1","turn":{"id":"turn-1","status":"failed","error":{"message":"unexpected status 401 Unauthorized"}}}}`))
	mapper.Observe(false, []byte(`{"method":"error","params":{"error":{"message":"websocket closed"}}}`))
	reporter.Close()

	errors := localMessagesByType(poster.messages(), "error")
	if len(errors) != 2 || errors[0].Content != "unexpected status 401 Unauthorized" || errors[1].Content != "websocket closed" {
		t.Fatalf("errors = %+v, want failed turn and top-level error messages", errors)
	}
}

func TestCodexAppServerMapperSkipsLifecycleMessages(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	mapper := newCodexAppServerMapper(reporter, "")

	mapper.Observe(true, []byte(`{"id":9,"method":"thread/start","params":{"sessionStartSource":"clear"}}`))
	mapper.Observe(false, []byte(`{"id":9,"result":{"thread":{"id":"thread-clear","sessionId":"thread-clear","path":"/tmp/clear.jsonl","cwd":"/tmp"}}}`))
	mapper.Observe(false, []byte(`{"method":"thread/started","params":{"thread":{"id":"thread-clear","sessionId":"thread-clear","path":"/tmp/clear.jsonl","cwd":"/tmp"}}}`))
	mapper.Observe(false, []byte(`{"method":"turn/started","params":{"threadId":"thread-clear","turn":{"id":"turn-clear"}}}`))
	mapper.Observe(false, []byte(`{"method":"turn/completed","params":{"threadId":"thread-clear","turn":{"id":"turn-clear","status":"completed"}}}`))
	reporter.Close()

	if messages := poster.messages(); len(messages) != 0 {
		t.Fatalf("messages = %+v, want no lifecycle transcript messages", messages)
	}
}

func TestCodexAppServerMapperTracksClearAndResumeThreadsForComments(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	mapper := newCodexAppServerMapper(reporter, "")

	mapper.Observe(true, []byte(`{"id":9,"method":"thread/start","params":{"sessionStartSource":"clear"}}`))
	mapper.Observe(false, []byte(`{"id":9,"result":{"thread":{"id":"thread-clear","sessionId":"thread-clear","path":"/tmp/clear.jsonl","cwd":"/tmp"}}}`))
	mapper.Observe(false, []byte(`{"method":"turn/started","params":{"turn":{"id":"turn-clear"}}}`))
	mapper.Observe(false, []byte(`{"method":"item/completed","params":{"item":{"id":"user-clear","type":"userMessage","text":"clear question"}}}`))
	mapper.Observe(false, []byte(`{"method":"item/completed","params":{"item":{"id":"agent-clear","type":"agentMessage","phase":"final_answer","text":"clear answer"}}}`))
	mapper.Observe(true, []byte(`{"id":13,"method":"thread/resume","params":{"threadId":"thread-old"}}`))
	mapper.Observe(false, []byte(`{"id":13,"result":{"thread":{"id":"thread-old","sessionId":"thread-old","path":"/tmp/old.jsonl","cwd":"/tmp"}}}`))
	mapper.Observe(false, []byte(`{"method":"turn/started","params":{"turn":{"id":"turn-old"}}}`))
	mapper.Observe(false, []byte(`{"method":"item/completed","params":{"item":{"id":"user-old","type":"userMessage","text":"resume question"}}}`))
	mapper.Observe(false, []byte(`{"method":"item/completed","params":{"item":{"id":"agent-old","type":"agentMessage","phase":"final_answer","text":"resume answer"}}}`))
	reporter.Close()

	inputs := userInputMessages(poster.messages())
	if len(inputs) != 2 || inputs[0].Content != "clear question" || inputs[1].Content != "resume question" {
		t.Fatalf("inputs = %+v, want clear and resume user inputs", inputs)
	}
	finals := finalMessages(poster.messages())
	if len(finals) != 2 || finals[0].Content != "clear answer" || finals[1].Content != "resume answer" {
		t.Fatalf("finals = %+v, want clear and resume finals", finals)
	}
}

func TestValidateClaudeLocalRunArgsRejectsManagedSettings(t *testing.T) {
	for _, args := range [][]string{
		{"--settings", "/tmp/settings.json"},
		{"--settings=/tmp/settings.json"},
	} {
		if err := validateClaudeLocalRunArgs(args); err == nil {
			t.Fatalf("validateClaudeLocalRunArgs(%v) = nil, want error", args)
		}
	}
	if err := validateClaudeLocalRunArgs([]string{"--model", "sonnet"}); err != nil {
		t.Fatalf("validateClaudeLocalRunArgs ordinary args: %v", err)
	}
}

func TestClaudeHookSettingsIncludesSessionStartForwarder(t *testing.T) {
	path, cleanup, err := writeClaudeHookSettings(43210)
	if err != nil {
		t.Fatalf("writeClaudeHookSettings: %v", err)
	}
	defer cleanup()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read hook settings: %v", err)
	}
	text := string(data)
	if !containsAll(text, []string{
		`"SessionStart"`,
		`"type": "command"`,
		`__claude-session-hook --port 43210`,
	}) {
		t.Fatalf("settings = %s, want SessionStart command hook", text)
	}
}

func TestClaudeHookForwarderReportsSessionToServer(t *testing.T) {
	var got claudeSessionHookPayload
	done := make(chan struct{})
	server, err := startClaudeSessionHookServer(func(payload claudeSessionHookPayload) {
		got = payload
		close(done)
	})
	if err != nil {
		t.Fatalf("startClaudeSessionHookServer: %v", err)
	}
	defer server.Close(context.Background())

	body := `{"session_id":"sess-1","transcript_path":"/tmp/sess-1.jsonl","cwd":"/tmp/project"}`
	if err := runClaudeSessionHookForwarder(context.Background(), server.Port(), strings.NewReader(body)); err != nil {
		t.Fatalf("runClaudeSessionHookForwarder: %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("hook server did not receive payload")
	}
	if got.SessionID != "sess-1" || got.TranscriptPath != "/tmp/sess-1.jsonl" || got.Cwd != "/tmp/project" {
		t.Fatalf("payload = %+v, want parsed Claude hook payload", got)
	}
}

func TestClaudeTranscriptTrackerMapsUserToolResultAndFinal(t *testing.T) {
	tmp := t.TempDir()
	sessionPath := filepath.Join(tmp, "sess-1.jsonl")
	if err := os.WriteFile(sessionPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	tracker := newClaudeTranscriptTracker(reporter, tmp, "bootstrap prompt", time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC))
	tracker.ObserveSessionHook(claudeSessionHookPayload{SessionID: "sess-1", TranscriptPath: sessionPath, Cwd: tmp})

	writeClaudeJSONLLines(t, sessionPath, []string{
		`{"type":"user","uuid":"u1","timestamp":"2026-05-14T12:00:01Z","message":{"role":"user","content":"帮我运行测试"}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-05-14T12:00:02Z","message":{"role":"assistant","content":[{"type":"thinking","text":"I should run tests"},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"go test ./cmd/multica"}}]}}`,
		`{"type":"user","uuid":"tr1","timestamp":"2026-05-14T12:00:03Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok\n"}]}}`,
		`{"type":"assistant","uuid":"a2","timestamp":"2026-05-14T12:00:04Z","message":{"role":"assistant","content":[{"type":"text","text":"完成"}]}}`,
	})
	tracker.Sync()
	reporter.Close()

	messages := poster.messages()
	inputs := userInputMessages(messages)
	if len(inputs) != 1 || inputs[0].Content != "帮我运行测试" || inputs[0].Source != claudeJSONLSource {
		t.Fatalf("inputs = %+v, want Claude user input", inputs)
	}
	thinking := localMessagesByType(messages, "thinking")
	if len(thinking) != 1 || thinking[0].Content != "I should run tests" {
		t.Fatalf("thinking = %+v, want Claude thinking block", thinking)
	}
	uses := localMessagesByType(messages, "tool_use")
	if len(uses) != 1 || uses[0].Tool != "Bash" || uses[0].Input["command"] != "go test ./cmd/multica" {
		t.Fatalf("tool uses = %+v, want raw Claude Bash tool", uses)
	}
	results := localMessagesByType(messages, "tool_result")
	if len(results) != 1 || results[0].Tool != "Bash" || results[0].Output != "ok" {
		t.Fatalf("tool results = %+v, want raw Claude tool result", results)
	}
	finals := finalMessages(messages)
	if len(finals) != 1 || finals[0].Content != "完成" {
		t.Fatalf("finals = %+v, want Claude final reply", finals)
	}
}

func TestClaudeTranscriptTrackerPreservesStructuredToolResultContent(t *testing.T) {
	tmp := t.TempDir()
	sessionPath := filepath.Join(tmp, "sess-1.jsonl")
	if err := os.WriteFile(sessionPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	tracker := newClaudeTranscriptTracker(reporter, tmp, "", time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC))
	tracker.ObserveSessionHook(claudeSessionHookPayload{SessionID: "sess-1", TranscriptPath: sessionPath, Cwd: tmp})

	writeClaudeJSONLLines(t, sessionPath, []string{
		`{"type":"assistant","uuid":"a1","timestamp":"2026-05-14T12:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Read","input":{"file_path":"README.md"}}]}}`,
		`{"type":"user","uuid":"tr1","timestamp":"2026-05-14T12:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":[{"type":"text","text":"hello"}]}]}}`,
	})
	tracker.Sync()
	reporter.Close()

	results := localMessagesByType(poster.messages(), "tool_result")
	if len(results) != 1 || results[0].Tool != "Read" || results[0].Output != `[{"text":"hello","type":"text"}]` {
		t.Fatalf("tool results = %+v, want raw Claude tool and JSON content string", results)
	}
}

func TestClaudeTranscriptTrackerSkipsBootstrapAndHistoricalLines(t *testing.T) {
	tmp := t.TempDir()
	sessionPath := filepath.Join(tmp, "sess-1.jsonl")
	bootstrap := "bootstrap prompt"
	writeClaudeJSONLLines(t, sessionPath, []string{
		`{"type":"user","uuid":"old-u","timestamp":"2026-05-14T11:59:00Z","message":{"role":"user","content":"old question"}}`,
		`{"type":"assistant","uuid":"old-a","timestamp":"2026-05-14T11:59:01Z","message":{"role":"assistant","content":[{"type":"text","text":"old answer"}]}}`,
	})
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	tracker := newClaudeTranscriptTracker(reporter, tmp, bootstrap, time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC))
	tracker.ObserveSessionHook(claudeSessionHookPayload{SessionID: "sess-1", TranscriptPath: sessionPath, Cwd: tmp})

	writeClaudeJSONLLines(t, sessionPath, []string{
		`{"type":"user","uuid":"boot-u","timestamp":"2026-05-14T12:00:01Z","message":{"role":"user","content":"bootstrap prompt"}}`,
		`{"type":"assistant","uuid":"boot-a","timestamp":"2026-05-14T12:00:02Z","message":{"role":"assistant","content":[{"type":"text","text":"should not comment"}]}}`,
	})
	tracker.Sync()
	reporter.Close()

	messages := poster.messages()
	if inputs := userInputMessages(messages); len(inputs) != 0 {
		t.Fatalf("inputs = %+v, want bootstrap and historical user skipped", inputs)
	}
	if finals := finalMessages(messages); len(finals) != 0 {
		t.Fatalf("finals = %+v, want bootstrap and historical final skipped", finals)
	}
	texts := localMessagesByType(messages, "text")
	if len(texts) != 1 || texts[0].Content != "should not comment" {
		t.Fatalf("texts = %+v, want bootstrap assistant text in execution log only", texts)
	}
}

func TestLocalCLIProcessEnvInjectsRunMetadataAndToken(t *testing.T) {
	got := localCLIProcessEnv([]string{
		"MULTICA_SERVER_URL=http://old.example",
		"MULTICA_WORKSPACE_ID=old-ws",
		"MULTICA_TOKEN=old-token",
		"OTHER=value",
	}, localCLIEnv{
		RunID:     "run-1",
		IssueID:   "issue-1",
		ServerURL: "http://127.0.0.1:8080",
		Token:     "token-1",
	})
	joined := "\n" + strings.Join(got, "\n") + "\n"
	if !containsAll(joined, []string{
		"\nMULTICA_RUN_ID=run-1\n",
		"\nMULTICA_ISSUE_ID=issue-1\n",
		"\nMULTICA_SERVER_URL=http://127.0.0.1:8080\n",
		"\nMULTICA_TOKEN=token-1\n",
		"\nOTHER=value\n",
	}) {
		t.Fatalf("env missing resolved values: %v", got)
	}
	if strings.Contains(joined, "\nMULTICA_WORKSPACE_ID=") || strings.Contains(joined, "old-token") {
		t.Fatalf("env leaked workspace or real token: %v", got)
	}
}

func TestLocalCLIProcessEnvRemovesParentWorkspaceAndToken(t *testing.T) {
	got := localCLIProcessEnv([]string{
		"MULTICA_SERVER_URL=http://parent.example",
		"MULTICA_WORKSPACE_ID=parent-ws",
		"MULTICA_TOKEN=parent-token",
	}, localCLIEnv{})
	joined := "\n" + strings.Join(got, "\n") + "\n"
	if !containsAll(joined, []string{
		"\nMULTICA_SERVER_URL=http://parent.example\n",
		"\nMULTICA_TOKEN=" + invalidLocalRunMulticaToken + "\n",
	}) {
		t.Fatalf("env missing expected values: %v", got)
	}
	if strings.Contains(joined, "\nMULTICA_WORKSPACE_ID=") || strings.Contains(joined, "parent-token") {
		t.Fatalf("env leaked parent workspace or token: %v", got)
	}
}

type fakeLocalRunPoster struct {
	mu   sync.Mutex
	msgs []localCLIMessage
}

type fakeLocalRunPatcher struct {
	mu      sync.Mutex
	patches []localRunPatch
}

type localRunPatch struct {
	path   string
	status string
}

func (f *fakeLocalRunPoster) PostJSON(_ context.Context, _ string, body any, _ any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, body.(localCLIMessage))
	return nil
}

func (f *fakeLocalRunPoster) messages() []localCLIMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]localCLIMessage(nil), f.msgs...)
}

func (f *fakeLocalRunPatcher) PatchJSON(_ context.Context, path string, body any, _ any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	status, _ := body.(map[string]any)["status"].(string)
	f.patches = append(f.patches, localRunPatch{path: path, status: status})
	return nil
}

func (f *fakeLocalRunPatcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.patches)
}

func (f *fakeLocalRunPatcher) last() (string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.patches) == 0 {
		return "", ""
	}
	last := f.patches[len(f.patches)-1]
	return last.path, last.status
}

func finalMessages(messages []localCLIMessage) []localCLIMessage {
	var finals []localCLIMessage
	for _, msg := range messages {
		if msg.Type == "final" {
			finals = append(finals, msg)
		}
	}
	return finals
}

func userInputMessages(messages []localCLIMessage) []localCLIMessage {
	var inputs []localCLIMessage
	for _, msg := range messages {
		if msg.Type == "user_input" {
			inputs = append(inputs, msg)
		}
	}
	return inputs
}

func localMessagesByType(messages []localCLIMessage, msgType string) []localCLIMessage {
	var out []localCLIMessage
	for _, msg := range messages {
		if msg.Type == msgType {
			out = append(out, msg)
		}
	}
	return out
}

func writeClaudeJSONLLines(t *testing.T, path string, lines []string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatalf("open jsonl: %v", err)
	}
	defer file.Close()
	for _, line := range lines {
		if _, err := file.WriteString(line + "\n"); err != nil {
			t.Fatalf("write jsonl: %v", err)
		}
	}
}

func mapMessagesByType(messages []map[string]any, msgType string) []map[string]any {
	var out []map[string]any
	for _, msg := range messages {
		if msg["type"] == msgType {
			out = append(out, msg)
		}
	}
	return out
}

func containsAll(s string, needles []string) bool {
	for _, needle := range needles {
		if !strings.Contains(s, needle) {
			return false
		}
	}
	return true
}
