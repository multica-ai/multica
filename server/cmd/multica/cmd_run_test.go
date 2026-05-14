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

	"github.com/multica-ai/multica/server/pkg/redact"
	"github.com/spf13/cobra"
)

func TestRunLocalCLIEndToEndWithFakeAPI(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh unavailable")
	}
	tmp := t.TempDir()
	isolateCodexSessions(t)
	codexPath := filepath.Join(tmp, "codex")
	if err := os.Symlink("/bin/sh", codexPath); err != nil {
		t.Fatalf("symlink codex shim: %v", err)
	}
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

func TestParseStructuredTranscript(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"thinking","content":"checking"}`,
		`plain terminal output`,
		`{"type":"tool","name":"shell","input":{"cmd":"date"}}`,
		`{"type":"tool_result","tool":"shell","output":"ok"}`,
		`{"type":"result","result":"done"}`,
	}, "\n")

	messages := parseStructuredTranscript(raw)
	if len(messages) != 4 {
		t.Fatalf("expected 4 structured messages, got %d", len(messages))
	}
	if messages[1].Type != "tool_use" || messages[1].Tool != "shell" {
		t.Fatalf("unexpected tool message: %+v", messages[1])
	}
	if messages[3].Type != "final" || messages[3].Content != "done" {
		t.Fatalf("unexpected final message: %+v", messages[3])
	}
}

func TestTranscriptStreamReportsRawAndStructuredMessages(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	stream := newTranscriptStream(reporter, nil)

	_, _ = stream.Write([]byte("plain OPENAI_API_KEY=sk-proj-abc123def456ghi789jkl012mno345\n"))
	_, _ = stream.Write([]byte(`{"type":"result","result":"done"}` + "\n"))
	stream.Flush()
	reporter.Close()

	messages := poster.messages()
	if len(messages) < 2 {
		t.Fatalf("expected raw and structured messages, got %+v", messages)
	}
	if messages[0].Type != "raw" {
		t.Fatalf("first message type = %q, want raw", messages[0].Type)
	}
	if strings.Contains(messages[0].Content, "sk-proj-abc123") {
		t.Fatalf("raw message was not redacted: %q", messages[0].Content)
	}
	last := messages[len(messages)-1]
	if last.Type != "final" || last.Content != "done" {
		t.Fatalf("last message = %+v, want final done", last)
	}
}

func TestTranscriptStreamCleansANSIRawOutput(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	stream := newTranscriptStream(reporter, nil)

	_, _ = stream.Write([]byte("\x1b[31mred text\x1b[0m\n"))
	stream.Flush()
	reporter.Close()

	raw := rawMessageContent(poster.messages())
	if raw != "red text" {
		t.Fatalf("raw = %q, want visible text only", raw)
	}
	if strings.Contains(raw, "\x1b") {
		t.Fatalf("raw contains escape sequence: %q", raw)
	}
}

func TestTranscriptStreamKeepsOnlyVisibleRedraw(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	stream := newTranscriptStream(reporter, nil)

	_, _ = stream.Write([]byte("Working 10%\r\x1b[KFinal answer\n"))
	stream.Flush()
	reporter.Close()

	raw := rawMessageContent(poster.messages())
	if raw != "Final answer" {
		t.Fatalf("raw = %q, want final visible redraw", raw)
	}
}

func TestTranscriptStreamSkipsPureControlAndStatusRawOutput(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	stream := newTranscriptStream(reporter, nil)

	_, _ = stream.Write([]byte("\x1b[31m\x1b[0m\r\x1b[KThinking...\r\x1b[K"))
	stream.Flush()
	reporter.Close()

	if raw := rawMessageContent(poster.messages()); raw != "" {
		t.Fatalf("raw = %q, want no pure control/status message", raw)
	}
}

func TestTranscriptStreamDoesNotDuplicateStructuredAsRaw(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	stream := newTranscriptStream(reporter, nil)

	_, _ = stream.Write([]byte("plain output\n"))
	_, _ = stream.Write([]byte(`{"type":"result","result":"done"}` + "\n"))
	stream.Flush()
	reporter.Close()

	messages := poster.messages()
	raw := rawMessageContent(messages)
	if raw != "plain output" {
		t.Fatalf("raw = %q, want only plain output", raw)
	}
	finals := finalMessages(messages)
	if len(finals) != 1 || finals[0].Content != "done" {
		t.Fatalf("finals = %+v, want structured final", finals)
	}
}

func TestTranscriptStreamReportsRawBeforeClose(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	defer reporter.Close()
	stream := newTranscriptStream(reporter, nil)

	_, _ = stream.Write([]byte(strings.Repeat("x", 4096)))

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if messages := poster.messages(); len(messages) > 0 && messages[0].Type == "raw" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("raw transcript was not reported before reporter close")
}

func TestStdinCaptureReportsCompleteUserInputLines(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := &stdinCapture{reporter: reporter}

	_, _ = capture.Write([]byte("/status"))
	if got := len(poster.messages()); got != 0 {
		t.Fatalf("messages before newline = %d, want 0", got)
	}
	_, _ = capture.Write([]byte("\r"))
	reporter.Close()

	messages := poster.messages()
	if len(messages) != 1 {
		t.Fatalf("messages = %+v, want one user input", messages)
	}
	if messages[0].Type != "user_input" || messages[0].Content != "/status" {
		t.Fatalf("message = %+v, want /status user_input", messages[0])
	}
}

func TestStdinCaptureUsesLineEditingState(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "backspace", in: "abc\x7fd\r", want: "abd"},
		{name: "delete", in: "abc\x1b[D\x1b[3~\r", want: "ab"},
		{name: "middle insert", in: "ac\x1b[Db\r", want: "abc"},
		{name: "ctrl u", in: "old\x15new\r", want: "new"},
		{name: "ctrl w", in: "hello world\x17gopher\r", want: "hello gopher"},
		{name: "utf8 backspace", in: "你好\x7f啊\r", want: "你啊"},
		{name: "terminal query response ignored", in: "hi\x1b[?2004;1$y\r", want: "hi"},
		{name: "osc ignored", in: "hi\x1b]0;title\x07!\r", want: "hi!"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			poster := &fakeLocalRunPoster{}
			reporter := newLocalRunReporter(poster, "run-1")
			capture := &stdinCapture{reporter: reporter}

			_, _ = capture.Write([]byte(tt.in))
			reporter.Close()

			messages := poster.messages()
			if len(messages) != 1 {
				t.Fatalf("messages = %+v, want one user input", messages)
			}
			if messages[0].Type != "user_input" || messages[0].Content != tt.want {
				t.Fatalf("message = %+v, want %q user_input", messages[0], tt.want)
			}
		})
	}
}

func TestStdinCaptureDoesNotCommitControlInterrupts(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := &stdinCapture{reporter: reporter}

	_, _ = capture.Write([]byte("partial\x03"))
	reporter.Close()

	if got := len(poster.messages()); got != 0 {
		t.Fatalf("messages = %d, want 0", got)
	}
}

func TestTerminalTurnCaptureDoesNotCreateFinalFromVisibleTerminalText(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCapture(reporter, nil)

	capture.AfterUserSubmit("question")
	capture.Write([]byte("\x1b[31mFinal answer\x1b[0m\n"))
	capture.Finalize()
	reporter.Close()

	finals := finalMessages(poster.messages())
	if len(finals) != 0 {
		t.Fatalf("finals = %+v, want no terminal fallback final", finals)
	}
}

func TestTerminalTurnCaptureDoesNotCreateFinalFromRedrawnVisibleText(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCapture(reporter, nil)

	capture.AfterUserSubmit("question")
	capture.Write([]byte("Working 10%\r\x1b[KFinal answer\n"))
	capture.Finalize()
	reporter.Close()

	finals := finalMessages(poster.messages())
	if len(finals) != 0 {
		t.Fatalf("finals = %+v, want no terminal fallback final", finals)
	}
}

func TestTerminalTurnCaptureSkipsStatusOnlyOutput(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCapture(reporter, nil)

	capture.AfterUserSubmit("question")
	capture.Write([]byte("Thinking...\nWorking 20%\n"))
	capture.Finalize()
	reporter.Close()

	if finals := finalMessages(poster.messages()); len(finals) != 0 {
		t.Fatalf("finals = %+v, want no assistant comment", finals)
	}
}

func TestTerminalTurnCaptureDoesNotDuplicateStructuredFinal(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCapture(reporter, nil)
	stream := newTranscriptStream(reporter, capture)

	capture.AfterUserSubmit("question")
	_, _ = stream.Write([]byte(`{"type":"result","result":"structured done"}` + "\n"))
	stream.Flush()
	capture.Write([]byte("Fallback visible answer\n"))
	capture.Finalize()
	reporter.Close()

	messages := poster.messages()
	var finals []localCLIMessage
	for _, msg := range messages {
		if msg.Type == "final" {
			finals = append(finals, msg)
		}
	}
	if len(finals) != 1 || finals[0].Content != "structured done" {
		t.Fatalf("finals = %+v, want only structured final", finals)
	}
}

func TestTerminalTurnCaptureInitialPromptProviderFinalIsSilent(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCapture(reporter, staticProviderTranscript{turns: []providerTranscriptTurn{
		{Key: "turn:1", UserInput: "issue context prompt", Final: "provider answer"},
	}})

	capture.StartInitialPrompt("issue context prompt")
	capture.Finalize()
	reporter.Close()

	messages := poster.messages()
	if inputs := userInputMessages(messages); len(inputs) != 0 {
		t.Fatalf("inputs = %+v, want no initial prompt user_input", inputs)
	}
	if finals := finalMessages(messages); len(finals) != 0 {
		t.Fatalf("finals = %+v, want no bootstrap final", finals)
	}
}

func TestTerminalTurnCaptureInitialPromptTerminalFallbackIsSilent(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCapture(reporter, nil)

	capture.StartInitialPrompt("issue context prompt")
	capture.Write([]byte("Initial answer\n"))
	capture.Finalize()
	reporter.Close()

	messages := poster.messages()
	if inputs := userInputMessages(messages); len(inputs) != 0 {
		t.Fatalf("inputs = %+v, want no initial prompt user_input", inputs)
	}
	if finals := finalMessages(messages); len(finals) != 0 {
		t.Fatalf("finals = %+v, want no bootstrap final", finals)
	}
}

func TestTerminalTurnCaptureInitialPromptStructuredFinalIsSilent(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCapture(reporter, nil)
	stream := newTranscriptStream(reporter, capture)

	capture.StartInitialPrompt("issue context prompt")
	_, _ = stream.Write([]byte(`{"type":"result","result":"structured bootstrap"}` + "\n"))
	stream.Flush()
	capture.Finalize()
	reporter.Close()

	if finals := finalMessages(poster.messages()); len(finals) != 0 {
		t.Fatalf("finals = %+v, want no bootstrap final", finals)
	}
}

func TestTerminalTurnCaptureAfterInitialPromptIgnoresTerminalFallbackTurn(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCapture(reporter, nil)

	capture.StartInitialPrompt("issue context prompt")
	capture.BeforeUserSubmit()
	capture.AfterUserSubmit("real question")
	capture.Write([]byte("Real answer\n"))
	capture.Finalize()
	reporter.Close()

	messages := poster.messages()
	inputs := userInputMessages(messages)
	finals := finalMessages(messages)
	if len(inputs) != 0 {
		t.Fatalf("inputs = %+v, want no terminal fallback user input", inputs)
	}
	if len(finals) != 0 {
		t.Fatalf("finals = %+v, want no terminal fallback final", finals)
	}
}

func TestTerminalTurnCaptureDoesNotSyncPendingInputWithoutProviderOrAnswer(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCaptureWithPollInterval(reporter, fakeProviderTranscript{ok: false, userMissing: true}, 0)

	capture.AfterUserSubmit("question before jsonl exists")
	capture.Finalize()
	reporter.Close()

	if inputs := userInputMessages(poster.messages()); len(inputs) != 0 {
		t.Fatalf("inputs = %+v, want pending input not synced without provider or answer", inputs)
	}
}

func TestTerminalTurnCaptureSyncsProviderUserInputWithoutStdinPrompt(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCaptureWithPollInterval(reporter, staticProviderTranscript{turns: []providerTranscriptTurn{
		{Key: "turn:1", UserInput: "same prompt"},
	}}, 0)

	capture.Finalize()
	reporter.Close()

	inputs := userInputMessages(poster.messages())
	if len(inputs) != 1 || inputs[0].Content != "same prompt" {
		t.Fatalf("inputs = %+v, want one provider user_input", inputs)
	}
}

func TestTerminalTurnCaptureAbsolutePathInputIsNotSlashCommand(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	input := "/Users/airthor/WebProjects/multica/server/go.mod what is this"
	capture := newTerminalTurnCaptureWithPollInterval(reporter, staticProviderTranscript{turns: []providerTranscriptTurn{
		{Key: "turn:1", UserInput: input},
	}}, 0)

	capture.Finalize()
	reporter.Close()

	inputs := userInputMessages(poster.messages())
	if len(inputs) != 1 || inputs[0].Content != redact.Text(input) || inputs[0].Input != nil {
		t.Fatalf("inputs = %+v, want absolute path as normal user_input", inputs)
	}
}

func TestTerminalTurnCaptureSyncsFileSelectionStyleProviderInput(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCaptureWithPollInterval(reporter, staticProviderTranscript{turns: []providerTranscriptTurn{
		{Key: "turn:1", UserInput: "internal/util/text.go 这是 go文件吗"},
	}}, 0)

	capture.Finalize()
	reporter.Close()

	inputs := userInputMessages(poster.messages())
	if len(inputs) != 1 || inputs[0].Content != "internal/util/text.go 这是 go文件吗" || inputs[0].Input != nil {
		t.Fatalf("inputs = %+v, want normalized file prompt", inputs)
	}
}

func TestTerminalTurnCaptureDoesNotSyncAtMenuSelectionEnter(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCaptureWithPollInterval(reporter, promptProviderTranscript{
		"hello.go 这个文件做什么": "它定义了 hello 逻辑。",
	}, 0)

	capture.AfterUserSubmit("@h")
	capture.Write([]byte("> @hello\n"))
	capture.BeforeUserSubmit()
	capture.AfterUserSubmit("hello.go 这个文件做什么")
	capture.Finalize()
	reporter.Close()

	inputs := userInputMessages(poster.messages())
	if len(inputs) != 1 || inputs[0].Content != "hello.go 这个文件做什么" {
		t.Fatalf("inputs = %+v, want only final accepted prompt", inputs)
	}
}

func TestTerminalTurnCapturePurePathWaitsForProviderConfirmation(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCaptureWithPollInterval(reporter, fakeProviderTranscript{ok: false, userMissing: true}, 0)

	capture.AfterUserSubmit("internal/util/text.go")
	capture.Finalize()
	reporter.Close()

	if inputs := userInputMessages(poster.messages()); len(inputs) != 0 {
		t.Fatalf("inputs = %+v, want pure path pending input dropped without provider confirmation", inputs)
	}
}

func TestTerminalTurnCaptureSyncsProviderConfirmedPathInputs(t *testing.T) {
	for _, input := range []string{
		"internal/util/text.go",
		"/Users/airthor/WebProjects/multica/server/cmd/multica/cmd_attachment.go",
		"/Users/airthor/WebProjects/multica/server/cmd/multica/cmd_attachment.go那这个文件呢",
		"那这个文件呢/Users/airthor/WebProjects/multica/server/cmd/multica/cmd_attachment.go那这个文件呢",
	} {
		t.Run(input, func(t *testing.T) {
			poster := &fakeLocalRunPoster{}
			reporter := newLocalRunReporter(poster, "run-1")
			capture := newTerminalTurnCaptureWithPollInterval(reporter, staticProviderTranscript{turns: []providerTranscriptTurn{
				{Key: "turn:1", UserInput: input},
			}}, 0)

			capture.Finalize()
			reporter.Close()

			inputs := userInputMessages(poster.messages())
			if len(inputs) != 1 || inputs[0].Content != redact.Text(input) || inputs[0].Input != nil {
				t.Fatalf("inputs = %+v, want provider-confirmed path input %q", inputs, input)
			}
		})
	}
}

func TestTerminalTurnCaptureSkipsCapturedBootstrapAndStatusInput(t *testing.T) {
	for _, input := range []string{
		"--output json",
		"status, add comments to report progress",
		"only. Do not add comments after you are done",
		"ready, and wait for the user's next request",
		"✓ You approved codex to run `multica issue get TES-10 --output json`",
		"• Explored",
		"└ Read server/go.mod",
		"└ Listed packages/views/foo.tsx",
	} {
		t.Run(input, func(t *testing.T) {
			poster := &fakeLocalRunPoster{}
			reporter := newLocalRunReporter(poster, "run-1")
			capture := newTerminalTurnCaptureWithPollInterval(reporter, fakeProviderTranscript{ok: false, userMissing: true}, 0)

			capture.AfterUserSubmit(input)
			capture.Finalize()
			reporter.Close()

			if inputs := userInputMessages(poster.messages()); len(inputs) != 0 {
				t.Fatalf("inputs = %+v, want captured noise skipped", inputs)
			}
		})
	}
}

func TestTerminalTurnCaptureSlashCommandsAreMarkedAsCommands(t *testing.T) {
	for _, input := range []string{"/status", " /help"} {
		t.Run(input, func(t *testing.T) {
			poster := &fakeLocalRunPoster{}
			reporter := newLocalRunReporter(poster, "run-1")
			capture := newTerminalTurnCaptureWithPollInterval(reporter, fakeProviderTranscript{ok: false, userMissing: true}, 0)

			capture.AfterUserSubmit(input)
			capture.Finalize()
			reporter.Close()

			inputs := userInputMessages(poster.messages())
			if len(inputs) != 1 || inputs[0].Input == nil || inputs[0].Input["command"] != true {
				t.Fatalf("inputs = %+v, want slash command metadata", inputs)
			}
		})
	}
}

func TestTerminalTurnCaptureSkipsApprovalAndBootstrapFallbackText(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCapture(reporter, nil)

	capture.AfterUserSubmit("question")
	capture.Write([]byte("\u2714 You approved codex to run `multica issue get TES-10 --output json`\n"))
	capture.Write([]byte("Assigned issue ID: TES-10\n"))
	capture.Write([]byte("- `multica issue get TES-10 --output json`\n"))
	capture.Write([]byte("- `multica issue comment list TES-10 --output json`\n"))
	capture.Write([]byte("status, add comments to report progress\n"))
	capture.Write([]byte("only. Do not add comments after you are done\n"))
	capture.Write([]byte("ready, and wait for the user's next request\n"))
	capture.Write([]byte("• Explored\n"))
	capture.Write([]byte("└ Read server/go.mod\n"))
	capture.Write([]byte("└ Listed packages/views/foo.tsx\n"))
	capture.Write([]byte("internal/util/text.go\n"))
	capture.Write([]byte("cmd/multica/help.go server/go.mod\n"))
	capture.Finalize()
	reporter.Close()

	if finals := finalMessages(poster.messages()); len(finals) != 0 {
		t.Fatalf("finals = %+v, want no approval/bootstrap fallback comment", finals)
	}
}

func TestCodexTranscriptExtractsFinalAnswerAndIgnoresCommentary(t *testing.T) {
	path := writeJSONLForTest(t, []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"please summarize"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","phase":"commentary","message":"I am checking files."}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","phase":"final_answer","message":"Final paragraph."}}`,
	})

	got, ok := extractCodexAnswerFromJSONL(path, "please summarize")
	if !ok || got != "Final paragraph." {
		t.Fatalf("answer = %q, %v; want final_answer only", got, ok)
	}
}

func TestCodexTranscriptTaskCompleteLastAgentMessageWins(t *testing.T) {
	path := writeJSONLForTest(t, []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"please summarize"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","phase":"final_answer","message":"Final from event."}}`,
		`{"type":"event_msg","payload":{"type":"task_complete","last_agent_message":"Final from task complete."}}`,
	})

	got, ok := extractCodexAnswerFromJSONL(path, "please summarize")
	if !ok || got != "Final from task complete." {
		t.Fatalf("answer = %q, %v; want task_complete.last_agent_message", got, ok)
	}
}

func TestCodexTranscriptExtractsMatchingTurnOnly(t *testing.T) {
	path := writeJSONLForTest(t, []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"first prompt"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","phase":"final_answer","message":"first answer"}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"second prompt"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","phase":"commentary","message":"second progress"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","phase":"final_answer","message":"second answer"}}`,
	})

	got, ok := extractCodexAnswerFromJSONL(path, "second prompt")
	if !ok || got != "second answer" {
		t.Fatalf("answer = %q, %v; want second answer", got, ok)
	}
}

func TestCodexTranscriptIgnoresNonAnswerEvents(t *testing.T) {
	path := writeJSONLForTest(t, []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"question"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","message":"100"}}`,
		`{"type":"event_msg","payload":{"type":"reasoning","message":"hidden reasoning"}}`,
		`{"type":"event_msg","payload":{"type":"tool_output","message":"tool output"}}`,
		`{"type":"event_msg","payload":{"type":"status","message":"working"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","phase":"final_answer","message":"final answer"}}`,
	})

	got, ok := extractCodexAnswerFromJSONL(path, "question")
	if !ok || got != "final answer" {
		t.Fatalf("answer = %q, %v; want only agent message", got, ok)
	}
}

func TestCodexTranscriptLegacyAgentMessageIsCompatibilityFallback(t *testing.T) {
	path := writeJSONLForTest(t, []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"question"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","message":"legacy answer"}}`,
	})

	got, ok := extractCodexAnswerFromJSONL(path, "question")
	if !ok || got != "legacy answer" {
		t.Fatalf("answer = %q, %v; want legacy agent message fallback", got, ok)
	}
}

func TestCodexTranscriptLegacyAgentMessageDoesNotOverrideFinalAnswer(t *testing.T) {
	path := writeJSONLForTest(t, []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"question"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","message":"legacy answer"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","phase":"final_answer","message":"final answer"}}`,
	})

	got, ok := extractCodexAnswerFromJSONL(path, "question")
	if !ok || got != "final answer" {
		t.Fatalf("answer = %q, %v; want final_answer over legacy fallback", got, ok)
	}
}

func TestCodexTranscriptOnlyCommentaryMissesProviderFinal(t *testing.T) {
	path := writeJSONLForTest(t, []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"question"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","phase":"commentary","message":"working"}}`,
	})

	if got, ok := extractCodexAnswerFromJSONL(path, "question"); ok || got != "" {
		t.Fatalf("answer = %q, %v; want no provider extraction", got, ok)
	}
}

func TestCodexTranscriptNoPromptMatch(t *testing.T) {
	path := writeJSONLForTest(t, []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"different prompt"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","phase":"final_answer","message":"answer"}}`,
	})

	if got, ok := extractCodexAnswerFromJSONL(path, "question"); ok || got != "" {
		t.Fatalf("answer = %q, %v; want no extraction", got, ok)
	}
}

func TestCodexTranscriptExtractsUserInputForAbsolutePathPrompt(t *testing.T) {
	path := writeJSONLForTest(t, []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"/Users/me/project fix this bug"}}`,
	})

	got, ok := extractCodexUserInputFromJSONL(path, "/Users/me/project fix this bug")
	if !ok || got != "/Users/me/project fix this bug" {
		t.Fatalf("user input = %q, %v; want absolute path prompt", got, ok)
	}
}

func TestCodexTranscriptExtractsSessionTurns(t *testing.T) {
	path := writeJSONLForTest(t, []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"bootstrap prompt"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","phase":"final_answer","message":"bootstrap answer"}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"/Users/me/project/cmd_autopilot.go这个文件大小多少"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","phase":"commentary","message":"checking"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","phase":"final_answer","message":"20,701 字节"}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"[Image #1] 这个图片多大","local_images":["1.png"]}}`,
		`{"type":"event_msg","payload":{"type":"task_complete","last_agent_message":"500 × 277 像素"}}`,
	})

	turns := extractCodexTurnsFromJSONL(path)
	if len(turns) != 3 {
		t.Fatalf("turns = %+v, want 3", turns)
	}
	if turns[1].UserInput != "/Users/me/project/cmd_autopilot.go这个文件大小多少" || turns[1].Final != "20,701 字节" {
		t.Fatalf("file turn = %+v, want file user/final", turns[1])
	}
	if turns[2].UserInput != "[Image #1] 这个图片多大" || turns[2].Final != "500 × 277 像素" {
		t.Fatalf("image turn = %+v, want image user/final", turns[2])
	}
}

func TestCodexTranscriptExtractorBindsToBootstrapSession(t *testing.T) {
	isolateCodexSessions(t)
	root := filepath.Join(os.Getenv("CODEX_HOME"), "sessions", "2026", "05", "14")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	bootstrap := "bootstrap prompt for issue"
	unrelated := filepath.Join(root, "unrelated.jsonl")
	bound := filepath.Join(root, "bound.jsonl")
	if err := os.WriteFile(unrelated, []byte(strings.Join([]string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"unrelated prompt"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","phase":"final_answer","message":"wrong answer"}}`,
	}, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write unrelated session: %v", err)
	}
	if err := os.WriteFile(bound, []byte(strings.Join([]string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"` + bootstrap + `"}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"real prompt"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","phase":"final_answer","message":"right answer"}}`,
	}, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write bound session: %v", err)
	}
	now := time.Now()
	if err := os.Chtimes(bound, now.Add(-time.Second), now.Add(-time.Second)); err != nil {
		t.Fatalf("chtime bound: %v", err)
	}
	if err := os.Chtimes(unrelated, now, now); err != nil {
		t.Fatalf("chtime unrelated: %v", err)
	}

	extractor := &codexTranscriptExtractor{runStart: now.Add(-time.Minute), bootstrapPrompt: bootstrap}
	turns, ok := extractor.ExtractTurns()
	if !ok {
		t.Fatal("ExtractTurns did not find bound session")
	}
	var users []string
	for _, turn := range turns {
		users = append(users, turn.UserInput)
	}
	if strings.Join(users, "|") != bootstrap+"|real prompt" {
		t.Fatalf("turn users = %+v, want bound session only", users)
	}
}

func TestCodexTranscriptMatchesFileSelectionPromptWithoutPathAndChineseSpacing(t *testing.T) {
	path := writeJSONLForTest(t, []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"这是 go文件吗"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","phase":"final_answer","message":"是，这是 Go 文件。"}}`,
	})

	got, ok := extractCodexAnswerFromJSONL(path, "internal/util/text.go 这 是 go文 件 吗")
	if !ok || got != "是，这是 Go 文件。" {
		t.Fatalf("answer = %q, %v; want file-selection prompt to match JSONL prompt", got, ok)
	}
}

func TestCodexTranscriptExtractsCanonicalUserInputForFileSelectionPrompt(t *testing.T) {
	path := writeJSONLForTest(t, []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"这是 go文件吗"}}`,
	})

	got, ok := extractCodexUserInputFromJSONL(path, "internal/util/text.go 这 是 go文 件 吗")
	if !ok || got != "这是 go文件吗" {
		t.Fatalf("user input = %q, %v; want canonical JSONL prompt", got, ok)
	}
}

func TestCodexTranscriptMatchesWrappedAbsolutePathPromptByFileName(t *testing.T) {
	path := writeJSONLForTest(t, []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"/Users/airthor/WebProjects/multica/server/cmd/multica/cmd_autopilot.go这个文件大小多少"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","phase":"final_answer","message":"` + "`/Users/airthor/WebProjects/multica/server/cmd/multica/cmd_autopilot.go` 大小是 `20,701` 字节。" + `"}}`,
	})

	got, ok := extractCodexAnswerFromJSONL(path, "cmd_autopilot.go这个文件大小多少")
	if !ok || got != "`/Users/airthor/WebProjects/multica/server/cmd/multica/cmd_autopilot.go` 大小是 `20,701` 字节。" {
		t.Fatalf("answer = %q, %v; want filename-fragment prompt to match full JSONL path", got, ok)
	}
}

func TestCodexTranscriptMatchesImagePlaceholderPrompt(t *testing.T) {
	path := writeJSONLForTest(t, []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"[Image #1] 这个图片多大","images":[],"local_images":["1.png"],"text_elements":[{"placeholder":"[Image #1]"}]}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","phase":"final_answer","message":"这张图片尺寸是 ` + "`500 × 277`" + ` 像素。"}}`,
	})

	got, ok := extractCodexAnswerFromJSONL(path, "这个图片多大")
	if !ok || got != "这张图片尺寸是 `500 × 277` 像素。" {
		t.Fatalf("answer = %q, %v; want image-placeholder prompt to match text-only stdin", got, ok)
	}

	user, ok := extractCodexUserInputFromJSONL(path, "这个图片多大")
	if !ok || user != "[Image #1] 这个图片多大" {
		t.Fatalf("user input = %q, %v; want JSONL image placeholder prompt", user, ok)
	}
}

func TestCodexTranscriptDoesNotExtractUserInputForLocalCommand(t *testing.T) {
	path := writeJSONLForTest(t, []string{
		`{"type":"event_msg","payload":{"type":"agent_message","phase":"final_answer","message":"status output"}}`,
	})

	if got, ok := extractCodexUserInputFromJSONL(path, "/status"); ok || got != "" {
		t.Fatalf("user input = %q, %v; want no session user_message match", got, ok)
	}
}

func TestStructuredFinalWinsOverProviderTranscript(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCapture(reporter, fakeProviderTranscript{answer: "provider answer", ok: true})
	stream := newTranscriptStream(reporter, capture)

	capture.AfterUserSubmit("question")
	_, _ = stream.Write([]byte(`{"type":"result","result":"structured done"}` + "\n"))
	stream.Flush()
	capture.Write([]byte("Terminal fallback\n"))
	capture.Finalize()
	reporter.Close()

	finals := finalMessages(poster.messages())
	if len(finals) != 1 || finals[0].Content != "structured done" {
		t.Fatalf("finals = %+v, want structured final only", finals)
	}
}

func TestStructuredTextDoesNotSuppressProviderFinal(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCaptureWithPollInterval(reporter, fakeProviderTranscript{answer: "provider answer", ok: true}, 10*time.Millisecond)
	stream := newTranscriptStream(reporter, capture)

	capture.AfterUserSubmit("question")
	_, _ = stream.Write([]byte(`{"type":"agent_message","message":"progress update"}` + "\n"))
	stream.Flush()
	waitForFinals(t, poster, 1)
	capture.Finalize()
	reporter.Close()

	finals := finalMessages(poster.messages())
	if len(finals) != 1 || finals[0].Content != "provider answer" {
		t.Fatalf("finals = %+v, want provider final after structured text", finals)
	}
}

func TestProviderTranscriptPollSyncsFinalWithoutNextInput(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCaptureWithPollInterval(reporter, fakeProviderTranscript{answer: "provider answer", ok: true}, 10*time.Millisecond)

	capture.AfterUserSubmit("question")
	waitForFinals(t, poster, 1)
	capture.Finalize()
	reporter.Close()

	finals := finalMessages(poster.messages())
	if len(finals) != 1 || finals[0].Content != "provider answer" {
		t.Fatalf("finals = %+v, want one provider answer", finals)
	}
}

func TestProviderTranscriptPollDoesNotDuplicateOnNextInput(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCaptureWithPollInterval(reporter, promptProviderTranscript{"question": "provider answer"}, 10*time.Millisecond)

	capture.AfterUserSubmit("question")
	waitForFinals(t, poster, 1)
	capture.BeforeUserSubmit()
	capture.AfterUserSubmit("next question")
	capture.Finalize()
	reporter.Close()

	finals := finalMessages(poster.messages())
	if len(finals) != 1 || finals[0].Content != "provider answer" {
		t.Fatalf("finals = %+v, want provider answer only once", finals)
	}
}

func TestProviderTranscriptLateTurnSyncsBySessionKey(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	provider := newBlockingFirstProvider("old answer")
	capture := newTerminalTurnCaptureWithPollInterval(reporter, provider, 10*time.Millisecond)

	capture.AfterUserSubmit("first question")
	provider.waitForFirstPrompt(t)
	capture.AfterUserSubmit("second question")
	provider.releaseFirst()
	time.Sleep(30 * time.Millisecond)
	capture.Finalize()
	reporter.Close()

	finals := finalMessages(poster.messages())
	if len(finals) != 1 || finals[0].Content != "old answer" {
		t.Fatalf("finals = %+v, want late provider answer once", finals)
	}
}

func TestStructuredFinalSuppressesProviderPollAndTerminalFallback(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCaptureWithPollInterval(reporter, fakeProviderTranscript{answer: "provider answer", ok: true}, 100*time.Millisecond)
	stream := newTranscriptStream(reporter, capture)

	capture.AfterUserSubmit("question")
	_, _ = stream.Write([]byte(`{"type":"result","result":"structured done"}` + "\n"))
	stream.Flush()
	capture.Write([]byte("Terminal fallback\n"))
	capture.Finalize()
	reporter.Close()

	finals := finalMessages(poster.messages())
	if len(finals) != 1 || finals[0].Content != "structured done" {
		t.Fatalf("finals = %+v, want structured final only", finals)
	}
}

func TestProviderTranscriptMissDoesNotUseTerminalFallbackOnNextInput(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCaptureWithPollInterval(reporter, fakeProviderTranscript{ok: false}, 10*time.Millisecond)

	capture.AfterUserSubmit("question")
	capture.Write([]byte("Terminal fallback answer\n"))
	time.Sleep(30 * time.Millisecond)
	if finals := finalMessages(poster.messages()); len(finals) != 0 {
		t.Fatalf("finals before next input = %+v, want no silent terminal fallback", finals)
	}
	capture.BeforeUserSubmit()
	capture.AfterUserSubmit("next question")
	capture.Finalize()
	reporter.Close()

	finals := finalMessages(poster.messages())
	if len(finals) != 0 {
		t.Fatalf("finals = %+v, want no terminal fallback", finals)
	}
}

func TestProviderTranscriptWinsOverTerminalCapture(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCapture(reporter, fakeProviderTranscript{answer: "provider answer", ok: true})

	capture.AfterUserSubmit("question")
	capture.Write([]byte("TUI status\nTerminal fallback answer\n"))
	capture.Finalize()
	reporter.Close()

	finals := finalMessages(poster.messages())
	if len(finals) != 1 || finals[0].Content != "provider answer" {
		t.Fatalf("finals = %+v, want provider answer only", finals)
	}
}

func TestSlashCommandSuppressesProviderAndTerminalFinals(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCaptureWithPollInterval(reporter, promptProviderTranscript{
		" /status":        "provider answer",
		"normal question": "Normal answer",
	}, 10*time.Millisecond)

	capture.AfterUserSubmit(" /status")
	capture.Write([]byte("Terminal command output\n"))
	time.Sleep(30 * time.Millisecond)
	capture.BeforeUserSubmit()
	capture.AfterUserSubmit("normal question")
	capture.Write([]byte("Normal answer\n"))
	capture.Finalize()
	reporter.Close()

	finals := finalMessages(poster.messages())
	if len(finals) != 1 || finals[0].Content != "Normal answer" {
		t.Fatalf("finals = %+v, want only normal prompt final", finals)
	}
}

func TestSlashPrefixedSessionUserInputIsNotCommand(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCaptureWithPollInterval(reporter, promptProviderTranscript{"/Users/me/project fix it": "provider answer"}, 10*time.Millisecond)

	capture.AfterUserSubmit("/Users/me/project fix it")
	waitForFinals(t, poster, 1)
	capture.Finalize()
	reporter.Close()

	messages := poster.messages()
	var inputs []localCLIMessage
	for _, msg := range messages {
		if msg.Type == "user_input" {
			inputs = append(inputs, msg)
		}
	}
	finals := finalMessages(messages)
	if len(inputs) != 1 || inputs[0].Content != "/Users/me/project fix it" || inputs[0].Input != nil {
		t.Fatalf("inputs = %+v, want normal absolute-path user_input", inputs)
	}
	if len(finals) != 1 || finals[0].Content != "provider answer" {
		t.Fatalf("finals = %+v, want provider answer", finals)
	}
}

func TestProviderSessionSlashCommandIsNotSyncedAsComment(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCaptureWithPollInterval(reporter, staticProviderTranscript{turns: []providerTranscriptTurn{
		{Key: "turn:1", UserInput: "/help", Final: "help output"},
	}}, 0)

	capture.Finalize()
	reporter.Close()

	messages := poster.messages()
	if inputs := userInputMessages(messages); len(inputs) != 0 {
		t.Fatalf("inputs = %+v, want no slash command user comment", inputs)
	}
	if finals := finalMessages(messages); len(finals) != 0 {
		t.Fatalf("finals = %+v, want no slash command final comment", finals)
	}
}

func TestSlashCommandSuppressesStructuredFinal(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCapture(reporter, nil)
	stream := newTranscriptStream(reporter, capture)

	capture.AfterUserSubmit("/help")
	_, _ = stream.Write([]byte(`{"type":"result","result":"command result"}` + "\n"))
	stream.Flush()
	capture.Finalize()
	reporter.Close()

	if finals := finalMessages(poster.messages()); len(finals) != 0 {
		t.Fatalf("finals = %+v, want no slash command structured final", finals)
	}
}

func TestProviderBackedSlashCommandSuppressesStructuredFinalWhenSessionMisses(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCaptureWithPollInterval(reporter, fakeProviderTranscript{userMissing: true}, 10*time.Millisecond)
	stream := newTranscriptStream(reporter, capture)

	capture.AfterUserSubmit("/help")
	_, _ = stream.Write([]byte(`{"type":"result","result":"command result"}` + "\n"))
	stream.Flush()
	capture.Finalize()
	reporter.Close()

	if finals := finalMessages(poster.messages()); len(finals) != 0 {
		t.Fatalf("finals = %+v, want no provider-backed slash command structured final", finals)
	}
}

func TestProviderTranscriptMissDoesNotAllowTerminalFallback(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCapture(reporter, fakeProviderTranscript{ok: false})

	capture.AfterUserSubmit("question")
	capture.Write([]byte("Terminal fallback answer\n"))
	capture.Finalize()
	reporter.Close()

	finals := finalMessages(poster.messages())
	if len(finals) != 0 {
		t.Fatalf("finals = %+v, want no terminal fallback", finals)
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

func TestExecuteLocalCLIReportsPTYOutputAndExitCode(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh unavailable")
	}
	tmp := t.TempDir()
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")

	exitCode, err := executeLocalCLI(
		[]string{"/bin/sh", "-c", `printf '{"type":"result","result":"done"}\n'; exit 7`},
		tmp,
		"sh",
		localCLIEnv{
			RunID:     "run-1",
			IssueID:   "issue-1",
			ServerURL: "http://127.0.0.1:8080",
		},
		"",
		reporter,
	)
	reporter.Close()

	if exitCode != 7 {
		t.Fatalf("exitCode = %d, want 7", exitCode)
	}
	if err == nil {
		t.Fatalf("expected non-nil error for exit 7")
	}
	messages := poster.messages()
	if len(messages) == 0 {
		t.Fatalf("expected transcript messages")
	}
	if last := messages[len(messages)-1]; last.Type != "final" || last.Content != "done" {
		t.Fatalf("last message = %+v, want final done", last)
	}
}

func TestExecuteLocalCLIPassesPromptAsCodexArg(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh unavailable")
	}
	tmp := t.TempDir()
	isolateCodexSessions(t)
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	prompt := "embedded issue context prompt"

	exitCode, err := executeLocalCLI(
		[]string{"/bin/sh", "-c", `printf 'arg:%s\n' "$0"`},
		tmp,
		"codex",
		localCLIEnv{RunID: "run-1", IssueID: "issue-1"},
		prompt,
		reporter,
	)
	reporter.Close()

	if exitCode != 0 || err != nil {
		t.Fatalf("executeLocalCLI exitCode=%d err=%v", exitCode, err)
	}
	messages := poster.messages()
	if len(messages) == 0 {
		t.Fatalf("expected transcript messages")
	}
	if inputs := userInputMessages(messages); len(inputs) != 0 {
		t.Fatalf("inputs = %+v, want no initial prompt user_input", inputs)
	}
	if finals := finalMessages(messages); len(finals) != 0 {
		t.Fatalf("finals = %+v, want no bootstrap final", finals)
	}
	if raw := rawMessageContent(messages); !strings.Contains(raw, "arg:"+prompt) {
		t.Fatalf("raw messages = %q, want prompt as argv", raw)
	}
}

func TestExecuteLocalCLIInitialPromptTerminalFallbackIsSilent(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh unavailable")
	}
	tmp := t.TempDir()
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")

	exitCode, err := executeLocalCLI(
		[]string{"/bin/sh", "-c", `printf 'Initial result\n'`},
		tmp,
		"sh",
		localCLIEnv{RunID: "run-1", IssueID: "issue-1"},
		"embedded issue context prompt\n",
		reporter,
	)
	reporter.Close()

	if exitCode != 0 || err != nil {
		t.Fatalf("executeLocalCLI exitCode=%d err=%v", exitCode, err)
	}
	messages := poster.messages()
	if inputs := userInputMessages(messages); len(inputs) != 0 {
		t.Fatalf("inputs = %+v, want no initial prompt user_input", inputs)
	}
	if finals := finalMessages(messages); len(finals) != 0 {
		t.Fatalf("finals = %+v, want no bootstrap final", finals)
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

type fakeProviderTranscript struct {
	answer      string
	ok          bool
	userMissing bool
	user        string
}

func (f fakeProviderTranscript) ExtractUserInput(prompt string, _ time.Time) (string, bool) {
	if f.userMissing {
		return "", false
	}
	if f.user != "" {
		return f.user, true
	}
	return prompt, true
}

func (f fakeProviderTranscript) Extract(string, time.Time) (string, bool) {
	return f.answer, f.ok
}

func (f fakeProviderTranscript) ExtractTurns() ([]providerTranscriptTurn, bool) {
	if f.userMissing || !f.ok && f.user == "" {
		return nil, false
	}
	user := f.user
	if user == "" {
		user = "question"
	}
	turn := providerTranscriptTurn{Key: "fake:" + user, UserInput: user}
	if f.ok {
		turn.Final = f.answer
	}
	return []providerTranscriptTurn{turn}, true
}

type promptProviderTranscript map[string]string

func (p promptProviderTranscript) ExtractUserInput(prompt string, _ time.Time) (string, bool) {
	if _, ok := p[prompt]; !ok {
		return "", false
	}
	return prompt, true
}

func (p promptProviderTranscript) Extract(prompt string, _ time.Time) (string, bool) {
	answer, ok := p[prompt]
	return answer, ok
}

func (p promptProviderTranscript) ExtractTurns() ([]providerTranscriptTurn, bool) {
	var turns []providerTranscriptTurn
	for prompt, answer := range p {
		turns = append(turns, providerTranscriptTurn{Key: "prompt:" + prompt, UserInput: prompt, Final: answer})
	}
	return turns, len(turns) > 0
}

type staticProviderTranscript struct {
	turns []providerTranscriptTurn
}

func (p staticProviderTranscript) ExtractTurns() ([]providerTranscriptTurn, bool) {
	return p.turns, len(p.turns) > 0
}

func (p staticProviderTranscript) ExtractUserInput(string, time.Time) (string, bool) {
	return "", false
}

func (p staticProviderTranscript) Extract(string, time.Time) (string, bool) {
	return "", false
}

type blockingFirstProvider struct {
	answer   string
	seen     chan struct{}
	release  chan struct{}
	seenOnce sync.Once
}

func newBlockingFirstProvider(answer string) *blockingFirstProvider {
	return &blockingFirstProvider{
		answer:  answer,
		seen:    make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (p *blockingFirstProvider) Extract(prompt string, _ time.Time) (string, bool) {
	if prompt != "first question" {
		return "", false
	}
	p.seenOnce.Do(func() { close(p.seen) })
	<-p.release
	return p.answer, true
}

func (p *blockingFirstProvider) ExtractUserInput(prompt string, _ time.Time) (string, bool) {
	return prompt, true
}

func (p *blockingFirstProvider) ExtractTurns() ([]providerTranscriptTurn, bool) {
	p.seenOnce.Do(func() { close(p.seen) })
	<-p.release
	return []providerTranscriptTurn{{Key: "blocking:first", UserInput: "first question", Final: p.answer}}, true
}

func (p *blockingFirstProvider) waitForFirstPrompt(t *testing.T) {
	t.Helper()
	select {
	case <-p.seen:
	case <-time.After(time.Second):
		t.Fatal("provider did not receive first prompt")
	}
}

func (p *blockingFirstProvider) releaseFirst() {
	close(p.release)
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

func writeJSONLForTest(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
	return path
}

func isolateCodexSessions(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
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

func rawMessageContent(messages []localCLIMessage) string {
	var parts []string
	for _, msg := range messages {
		if msg.Type == "raw" {
			parts = append(parts, msg.Content)
		}
	}
	return strings.Join(parts, "")
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

func mapMessagesByType(messages []map[string]any, msgType string) []map[string]any {
	var out []map[string]any
	for _, msg := range messages {
		if msg["type"] == msgType {
			out = append(out, msg)
		}
	}
	return out
}

func waitForFinals(t *testing.T, poster *fakeLocalRunPoster, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(finalMessages(poster.messages())) >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("finals = %+v, want at least %d", finalMessages(poster.messages()), want)
}

func waitForUserInputs(t *testing.T, poster *fakeLocalRunPoster, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(userInputMessages(poster.messages())) >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("inputs = %+v, want at least %d", userInputMessages(poster.messages()), want)
}

func containsAll(s string, needles []string) bool {
	for _, needle := range needles {
		if !strings.Contains(s, needle) {
			return false
		}
	}
	return true
}
