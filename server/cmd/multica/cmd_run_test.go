package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/issue-1":
			json.NewEncoder(w).Encode(map[string]any{
				"id":           "issue-1",
				"identifier":   "MUL-1",
				"title":        "Fake issue",
				"status":       "todo",
				"priority":     "medium",
				"description":  "Do it",
				"workspace_id": "ws-1",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/issue-1/comments":
			json.NewEncoder(w).Encode([]map[string]any{{"author_type": "member", "author_id": "user-1", "content": "start"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/ws-1":
			json.NewEncoder(w).Encode(map[string]any{"id": "ws-1", "name": "Workspace"})
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

	err := runLocalCLI(cmd, []string{"MUL-1", "/bin/sh", "-c", `printf '{"type":"result","result":"done"}\n'`})
	if err != nil {
		t.Fatalf("runLocalCLI: %v", err)
	}

	if createBody["cli_name"] != "sh" || createBody["comments_mode"] != "off" || createBody["work_dir"] != tmp {
		t.Fatalf("unexpected create body: %+v", createBody)
	}
	if len(patches) < 2 {
		t.Fatalf("patches = %+v, want context and terminal status updates", patches)
	}
	contextDir := filepath.Join(tmp, ".multica", "runs", "run-1")
	if patches[0]["status"] != "running" || patches[0]["context_dir"] != contextDir {
		t.Fatalf("first patch = %+v, want running context update", patches[0])
	}
	lastPatch := patches[len(patches)-1]
	if lastPatch["status"] != "completed" || int(lastPatch["exit_code"].(float64)) != 0 {
		t.Fatalf("last patch = %+v, want completed exit 0", lastPatch)
	}
	if len(messages) == 0 {
		t.Fatalf("expected streamed messages")
	}
	lastMsg := messages[len(messages)-1]
	if lastMsg["type"] != "final" || lastMsg["content"] != "done" {
		t.Fatalf("last message = %+v, want final done", lastMsg)
	}
	if _, err := os.Stat(filepath.Join(contextDir, "issue.md")); err != nil {
		t.Fatalf("issue context not written: %v", err)
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

func TestInitialLocalRunPromptIncludesContextDir(t *testing.T) {
	dir := "/tmp/project/.multica/runs/run-1"
	got := initialLocalRunPrompt(dir)
	if got == "" || !containsAll(got, []string{"Multica issue context", dir}) {
		t.Fatalf("prompt %q does not include context directory", got)
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

func TestTerminalTurnCaptureExtractsVisibleAssistantText(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCapture(reporter, nil)

	capture.AfterUserSubmit("question")
	capture.Write([]byte("\x1b[31mFinal answer\x1b[0m\n"))
	capture.Finalize()
	reporter.Close()

	finals := finalMessages(poster.messages())
	if len(finals) != 1 || finals[0].Content != "Final answer" {
		t.Fatalf("finals = %+v, want final visible answer", finals)
	}
}

func TestTerminalTurnCaptureKeepsOnlyRedrawnVisibleText(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCapture(reporter, nil)

	capture.AfterUserSubmit("question")
	capture.Write([]byte("Working 10%\r\x1b[KFinal answer\n"))
	capture.Finalize()
	reporter.Close()

	finals := finalMessages(poster.messages())
	if len(finals) != 1 || finals[0].Content != "Final answer" {
		t.Fatalf("finals = %+v, want only final redrawn text", finals)
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

func TestProviderTranscriptLateOldTurnIsDiscarded(t *testing.T) {
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

	if finals := finalMessages(poster.messages()); len(finals) != 0 {
		t.Fatalf("finals = %+v, want stale first turn discarded", finals)
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

func TestProviderTranscriptMissWaitsForTerminalFallbackUntilNextInput(t *testing.T) {
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
	if len(finals) != 1 || finals[0].Content != "Terminal fallback answer" {
		t.Fatalf("finals = %+v, want terminal fallback on next input", finals)
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
	capture := newTerminalTurnCaptureWithPollInterval(reporter, fakeProviderTranscript{answer: "provider answer", ok: true, userMissing: true}, 10*time.Millisecond)

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

func TestProviderTranscriptMissAllowsTerminalFallback(t *testing.T) {
	poster := &fakeLocalRunPoster{}
	reporter := newLocalRunReporter(poster, "run-1")
	capture := newTerminalTurnCapture(reporter, fakeProviderTranscript{ok: false})

	capture.AfterUserSubmit("question")
	capture.Write([]byte("Terminal fallback answer\n"))
	capture.Finalize()
	reporter.Close()

	finals := finalMessages(poster.messages())
	if len(finals) != 1 || finals[0].Content != "Terminal fallback answer" {
		t.Fatalf("finals = %+v, want terminal fallback", finals)
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
		"run-1",
		"issue-1",
		filepath.Join(tmp, ".multica", "runs", "run-1"),
		"http://127.0.0.1:8080",
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

func TestWriteLocalRunContextWritesExpectedFiles(t *testing.T) {
	tmp := t.TempDir()
	contextDir := filepath.Join(tmp, ".multica", "runs", "run-1")
	client := fakeContextClient{
		"/api/issues/issue-1": map[string]any{
			"id":           "issue-1",
			"identifier":   "MUL-1",
			"title":        "Test issue",
			"status":       "todo",
			"priority":     "medium",
			"description":  "Do the work",
			"workspace_id": "workspace-1",
			"project_id":   "project-1",
		},
		"/api/issues/issue-1/comments": []map[string]any{
			{"author_type": "member", "author_id": "user-1", "content": "hello", "created_at": "2026-01-01T00:00:00Z"},
		},
		"/api/workspaces/workspace-1": map[string]any{"id": "workspace-1", "name": "Workspace"},
		"/api/projects/project-1":     map[string]any{"id": "project-1", "name": "Project"},
		"/api/projects/project-1/resources": map[string]any{
			"items": []any{"resource"},
		},
	}

	if err := writeLocalRunContext(context.Background(), client, "issue-1", "run-1", "codex", tmp, contextDir); err != nil {
		t.Fatalf("writeLocalRunContext: %v", err)
	}

	runJSON := readFileForTest(t, filepath.Join(contextDir, "run.json"))
	if !containsAll(runJSON, []string{`"id": "run-1"`, `"cli_name": "codex"`, tmp}) {
		t.Fatalf("run.json missing expected fields:\n%s", runJSON)
	}
	issueMD := readFileForTest(t, filepath.Join(contextDir, "issue.md"))
	if !containsAll(issueMD, []string{"# Test issue", "- Identifier: `MUL-1`", "Do the work"}) {
		t.Fatalf("issue.md missing expected fields:\n%s", issueMD)
	}
	commentsMD := readFileForTest(t, filepath.Join(contextDir, "comments.md"))
	if !containsAll(commentsMD, []string{"# Comments", "hello"}) {
		t.Fatalf("comments.md missing expected fields:\n%s", commentsMD)
	}
	resourcesJSON := readFileForTest(t, filepath.Join(contextDir, "resources.json"))
	if !containsAll(resourcesJSON, []string{`"workspace"`, `"project"`, `"project_resources"`}) {
		t.Fatalf("resources.json missing expected fields:\n%s", resourcesJSON)
	}
}

func TestAddMulticaToGitExcludeIsIdempotent(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".git", "info"), 0o755); err != nil {
		t.Fatalf("mkdir git info: %v", err)
	}

	if err := addMulticaToGitExclude(tmp); err != nil {
		t.Fatalf("addMulticaToGitExclude first: %v", err)
	}
	if err := addMulticaToGitExclude(tmp); err != nil {
		t.Fatalf("addMulticaToGitExclude second: %v", err)
	}

	exclude := readFileForTest(t, filepath.Join(tmp, ".git", "info", "exclude"))
	if got := strings.Count(exclude, ".multica/"); got != 1 {
		t.Fatalf(".multica/ entries = %d, want 1 in:\n%s", got, exclude)
	}
}

type fakeLocalRunPoster struct {
	mu   sync.Mutex
	msgs []localCLIMessage
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

type fakeContextClient map[string]any

func (f fakeContextClient) GetJSON(_ context.Context, path string, out any) error {
	if decoded, err := url.PathUnescape(path); err == nil {
		path = decoded
	}
	data, err := json.Marshal(f[path])
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
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

func writeJSONLForTest(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
	return path
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

func containsAll(s string, needles []string) bool {
	for _, needle := range needles {
		if !strings.Contains(s, needle) {
			return false
		}
	}
	return true
}
