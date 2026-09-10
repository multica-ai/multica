package agent

import (
	"log/slog"
	"strings"
	"testing"
)

// The fixtures below are transcript shapes captured from Command Code 1.44.0
// (`commandcode -p --output-format json`). Every line the CLI writes is one of
// two envelopes: {"type":"event","event":{...}} or the single trailing
// {"type":"result",...}.

// drainCommandcodeMessages closes ch and returns everything buffered in it, so
// a test can assert on the whole emitted sequence at once.
func drainCommandcodeMessages(ch chan Message) []Message {
	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	return msgs
}

func newCommandcodeBackendForTest() *commandcodeBackend {
	return &commandcodeBackend{cfg: Config{Logger: slog.Default()}}
}

func TestCommandcodeProcessEventsHappyPath(t *testing.T) {
	t.Parallel()

	b := newCommandcodeBackendForTest()
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"event","event":{"type":"run_start","sessionId":"58a325ce-9290-49cd-9f0d-7e9d64855a7e"}}`,
		`{"type":"event","event":{"type":"turn_start","turnNumber":1}}`,
		`{"type":"event","event":{"type":"message_start"}}`,
		`{"type":"event","event":{"type":"model_request_start","model":"deepseek/deepseek-v4-flash"}}`,
		`{"type":"event","event":{"type":"tool_running","toolCallId":"call_1","toolName":"bash","description":"ls"}}`,
		`{"type":"event","event":{"type":"tool_completed","toolCallId":"call_1","toolName":"bash","result":"file1.go\nfile2.go\n"}}`,
		`{"type":"event","event":{"type":"message_end","content":[{"type":"text","text":"Listed the files."}]}}`,
		`{"type":"event","event":{"type":"model_request_end","model":"deepseek/deepseek-v4-flash","stopReason":"end_turn","usage":{"inputTokens":120,"outputTokens":45,"cacheReadTokens":10,"cacheWriteTokens":5}}}`,
		`{"type":"event","event":{"type":"run_end"}}`,
		`{"type":"result","subtype":"success","sessionId":"58a325ce-9290-49cd-9f0d-7e9d64855a7e","stopReason":"end_turn","usage":{"inputTokens":120,"outputTokens":45,"cacheReadTokens":10,"cacheWriteTokens":5},"durationMs":1834,"finalText":"Listed the files."}`,
	}, "\n")

	res := b.processEvents(strings.NewReader(lines), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if !res.sawResultLine {
		t.Error("sawResultLine: got false, want true — the result line is the only proof the run finished")
	}
	if res.sessionID != "58a325ce-9290-49cd-9f0d-7e9d64855a7e" {
		t.Errorf("sessionID: got %q", res.sessionID)
	}
	if res.output != "Listed the files." {
		t.Errorf("output: got %q", res.output)
	}
	if res.errMsg != "" {
		t.Errorf("errMsg: got %q, want empty", res.errMsg)
	}

	usage, ok := res.usage["deepseek/deepseek-v4-flash"]
	if !ok {
		t.Fatalf("usage: no entry for the requesting model, got %v", res.usage)
	}
	if usage.InputTokens != 120 || usage.OutputTokens != 45 || usage.CacheReadTokens != 10 || usage.CacheWriteTokens != 5 {
		t.Errorf("usage: got %+v", usage)
	}

	msgs := drainCommandcodeMessages(ch)

	var sawStatus, sawToolUse, sawToolResult, sawText bool
	for _, m := range msgs {
		switch m.Type {
		case MessageStatus:
			sawStatus = true
			if m.SessionID != "58a325ce-9290-49cd-9f0d-7e9d64855a7e" {
				t.Errorf("status message SessionID: got %q", m.SessionID)
			}
		case MessageToolUse:
			sawToolUse = true
			if m.Tool != "bash" || m.CallID != "call_1" {
				t.Errorf("tool-use: got tool=%q callID=%q", m.Tool, m.CallID)
			}
		case MessageToolResult:
			sawToolResult = true
			if m.Output != "file1.go\nfile2.go\n" {
				t.Errorf("tool-result output: got %q", m.Output)
			}
		case MessageText:
			sawText = true
			if m.Content != "Listed the files." {
				t.Errorf("text: got %q", m.Content)
			}
		}
	}
	if !sawStatus {
		t.Error("no status message — the session id must be pinned as soon as run_start arrives")
	}
	if !sawToolUse || !sawToolResult || !sawText {
		t.Errorf("missing messages: toolUse=%v toolResult=%v text=%v", sawToolUse, sawToolResult, sawText)
	}
}

// A run that dies before the result line must not pass as a success. This is
// the shape captured when the account is out of credits.
func TestCommandcodeProcessEventsRunErrorFails(t *testing.T) {
	t.Parallel()

	b := newCommandcodeBackendForTest()
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"event","event":{"type":"run_start","sessionId":"ses_err"}}`,
		`{"type":"event","event":{"type":"model_request_start","model":"stealth/ox-alpha"}}`,
		`{"type":"event","event":{"type":"run_error","error":{"name":"TransportError","message":"POST /alpha/generate → 400 error: You have insufficient credits to make this request."}}}`,
	}, "\n")

	res := b.processEvents(strings.NewReader(lines), ch)

	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}
	if !strings.Contains(res.errMsg, "insufficient credits") {
		t.Errorf("errMsg does not carry the provider message: %q", res.errMsg)
	}
	if !strings.Contains(res.errMsg, "TransportError") {
		t.Errorf("errMsg drops the error name: %q", res.errMsg)
	}
	if res.sawResultLine {
		t.Error("sawResultLine: got true, want false — no result line was emitted")
	}
	if res.sessionID != "ses_err" {
		t.Errorf("sessionID: got %q — a failed run still yields a resumable id", res.sessionID)
	}

	msgs := drainCommandcodeMessages(ch)
	var sawError bool
	for _, m := range msgs {
		if m.Type == MessageError {
			sawError = true
		}
	}
	if !sawError {
		t.Error("run_error must surface as an error message")
	}
}

// The result line can report failure on its own, without a preceding
// run_error.
func TestCommandcodeProcessEventsResultLineErrorFails(t *testing.T) {
	t.Parallel()

	b := newCommandcodeBackendForTest()
	ch := make(chan Message, 256)

	line := `{"type":"result","subtype":"error","sessionId":"ses_x","usage":{"inputTokens":1,"outputTokens":0,"cacheReadTokens":0,"cacheWriteTokens":0},"durationMs":12,"finalText":"","error":{"name":"AuthError","message":"not logged in"}}`

	res := b.processEvents(strings.NewReader(line), ch)

	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}
	if !strings.Contains(res.errMsg, "not logged in") {
		t.Errorf("errMsg: got %q", res.errMsg)
	}
	if !res.sawResultLine {
		t.Error("sawResultLine: got false, want true")
	}
	drainCommandcodeMessages(ch)
}

func TestCommandcodeProcessEventsInterruptedIsAborted(t *testing.T) {
	t.Parallel()

	b := newCommandcodeBackendForTest()
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"event","event":{"type":"run_start","sessionId":"ses_int"}}`,
		`{"type":"event","event":{"type":"interrupted"}}`,
		`{"type":"result","subtype":"interrupted","sessionId":"ses_int","usage":{"inputTokens":5,"outputTokens":2,"cacheReadTokens":0,"cacheWriteTokens":0},"durationMs":40,"finalText":"partial"}`,
	}, "\n")

	res := b.processEvents(strings.NewReader(lines), ch)

	if res.status != "aborted" {
		t.Errorf("status: got %q, want %q", res.status, "aborted")
	}
	drainCommandcodeMessages(ch)
}

// Assistant prose and reasoning arrive together at message_end, since Command
// Code has no token-level delta event.
func TestCommandcodeProcessEventsMessageEndBlocks(t *testing.T) {
	t.Parallel()

	b := newCommandcodeBackendForTest()
	ch := make(chan Message, 256)

	line := `{"type":"event","event":{"type":"message_end","content":[` +
		`{"type":"reasoning","text":"Thinking it through."},` +
		`{"type":"text","text":"Here is the answer."},` +
		`{"type":"tool_use","id":"call_9","name":"edit","input":{"path":"a.go"}},` +
		`{"type":"tool_use","id":"srv_1","name":"web_search","input":{},"providerExecuted":true}` +
		`]}}`

	b.processEvents(strings.NewReader(line), ch)
	msgs := drainCommandcodeMessages(ch)

	var thinking, text, toolUse int
	for _, m := range msgs {
		switch m.Type {
		case MessageThinking:
			thinking++
			if m.Content != "Thinking it through." {
				t.Errorf("thinking content: got %q", m.Content)
			}
		case MessageText:
			text++
		case MessageToolUse:
			toolUse++
			if m.CallID != "call_9" {
				t.Errorf("provider-executed tool leaked through: callID=%q", m.CallID)
			}
			if m.Input["path"] != "a.go" {
				t.Errorf("tool input lost: %v", m.Input)
			}
		}
	}
	if thinking != 1 || text != 1 {
		t.Errorf("blocks: thinking=%d text=%d, want 1 and 1", thinking, text)
	}
	// A provider-executed tool never runs on this machine and never gets a
	// matching client-side result, so emitting it would leave a tool call that
	// never closes.
	if toolUse != 1 {
		t.Errorf("tool-use count: got %d, want 1 (provider-executed must be skipped)", toolUse)
	}
}

// Usage is attributed to the model that actually incurred it, so a run that
// switches models bills each one separately instead of folding everything
// under the configured model.
func TestCommandcodeProcessEventsUsagePerModel(t *testing.T) {
	t.Parallel()

	b := newCommandcodeBackendForTest()
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"event","event":{"type":"model_request_start","model":"deepseek/deepseek-v4-flash"}}`,
		`{"type":"event","event":{"type":"model_request_end","model":"deepseek/deepseek-v4-flash","usage":{"inputTokens":10,"outputTokens":1,"cacheReadTokens":0,"cacheWriteTokens":0}}}`,
		`{"type":"event","event":{"type":"model_request_start","model":"zai-org/glm-5.3"}}`,
		`{"type":"event","event":{"type":"model_request_end","model":"zai-org/glm-5.3","usage":{"inputTokens":7,"outputTokens":3,"cacheReadTokens":2,"cacheWriteTokens":1}}}`,
		`{"type":"event","event":{"type":"model_request_end","model":"zai-org/glm-5.3","usage":{"inputTokens":1,"outputTokens":1,"cacheReadTokens":0,"cacheWriteTokens":0}}}`,
	}, "\n")

	res := b.processEvents(strings.NewReader(lines), ch)
	drainCommandcodeMessages(ch)

	if len(res.usage) != 2 {
		t.Fatalf("usage models: got %d entries (%v), want 2", len(res.usage), res.usage)
	}
	if got := res.usage["deepseek/deepseek-v4-flash"].InputTokens; got != 10 {
		t.Errorf("deepseek input tokens: got %d, want 10", got)
	}
	// The two glm requests accumulate rather than overwrite.
	if got := res.usage["zai-org/glm-5.3"].InputTokens; got != 8 {
		t.Errorf("glm input tokens: got %d, want 8", got)
	}
	if got := res.usage["zai-org/glm-5.3"].OutputTokens; got != 4 {
		t.Errorf("glm output tokens: got %d, want 4", got)
	}
}

// model_request_end without an explicit model falls back to the model the
// in-flight request started with.
func TestCommandcodeProcessEventsUsageFallsBackToCurrentModel(t *testing.T) {
	t.Parallel()

	b := newCommandcodeBackendForTest()
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"event","event":{"type":"model_request_start","model":"moonshotai/kimi-k3"}}`,
		`{"type":"event","event":{"type":"model_request_end","usage":{"inputTokens":4,"outputTokens":2,"cacheReadTokens":0,"cacheWriteTokens":0}}}`,
	}, "\n")

	res := b.processEvents(strings.NewReader(lines), ch)
	drainCommandcodeMessages(ch)

	if _, ok := res.usage["moonshotai/kimi-k3"]; !ok {
		t.Errorf("usage not attributed to the in-flight model: %v", res.usage)
	}
}

func TestCommandcodeProcessEventsDeniedAndErroredTools(t *testing.T) {
	t.Parallel()

	b := newCommandcodeBackendForTest()
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`{"type":"event","event":{"type":"tool_errored","toolCallId":"c1","toolName":"bash","result":"exit status 1"}}`,
		`{"type":"event","event":{"type":"tool_denied","toolCallId":"c2","toolName":"write","reason":"blocked by policy"}}`,
	}, "\n")

	b.processEvents(strings.NewReader(lines), ch)
	msgs := drainCommandcodeMessages(ch)

	if len(msgs) != 2 {
		t.Fatalf("messages: got %d, want 2", len(msgs))
	}
	for _, m := range msgs {
		if m.Type != MessageToolResult {
			t.Errorf("type: got %q, want tool-result", m.Type)
		}
		if m.Output == "" {
			t.Errorf("tool %q lost its outcome text", m.Tool)
		}
	}
}

// A structured tool result is re-encoded rather than dropped, so the transcript
// keeps what the tool actually said.
func TestCommandcodeToolOutputRendersStructuredResults(t *testing.T) {
	t.Parallel()

	if got := commandcodeToolOutput(nil); got != "" {
		t.Errorf("empty payload: got %q", got)
	}
	if got := commandcodeToolOutput([]byte(`"plain string"`)); got != "plain string" {
		t.Errorf("string payload: got %q", got)
	}
	got := commandcodeToolOutput([]byte(`{"files":["a.go"]}`))
	if !strings.Contains(got, "a.go") {
		t.Errorf("structured payload lost its content: %q", got)
	}
}

// Wrapper scripts sometimes print diagnostics on stdout. A non-JSON line is
// noise, not a protocol violation worth failing the run over.
func TestCommandcodeProcessEventsSkipsNonJSONLines(t *testing.T) {
	t.Parallel()

	b := newCommandcodeBackendForTest()
	ch := make(chan Message, 256)

	lines := strings.Join([]string{
		`command-code is managed by your package manager.`,
		``,
		`{"type":"event","event":{"type":"run_start","sessionId":"ses_noise"}}`,
		`{"type":"result","subtype":"success","sessionId":"ses_noise","usage":{"inputTokens":1,"outputTokens":1,"cacheReadTokens":0,"cacheWriteTokens":0},"durationMs":5,"finalText":"ok"}`,
	}, "\n")

	res := b.processEvents(strings.NewReader(lines), ch)
	drainCommandcodeMessages(ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want completed", res.status)
	}
	if res.output != "ok" {
		t.Errorf("output: got %q", res.output)
	}
}

// A stream that simply stops leaves no proof the run finished. Execute keys its
// prompt-write diagnosis on exactly this signal.
func TestCommandcodeProcessEventsTruncatedStreamHasNoResultLine(t *testing.T) {
	t.Parallel()

	b := newCommandcodeBackendForTest()
	ch := make(chan Message, 256)

	res := b.processEvents(strings.NewReader(`{"type":"event","event":{"type":"turn_start","turnNumber":1}}`), ch)
	drainCommandcodeMessages(ch)

	if res.sawResultLine {
		t.Error("sawResultLine: got true, want false")
	}
	// status stays optimistic here on purpose — Execute is what decides,
	// combining this with the process exit status.
	if res.status != "completed" {
		t.Errorf("status: got %q, want completed", res.status)
	}
}

func TestCommandcodeBlockedArgsProtectTheDaemonChannel(t *testing.T) {
	t.Parallel()

	custom := []string{
		"--output-format", "text", // would mute the event stream
		"--session", "someone-elses-session", // would retarget the run
		"--model", "sneaky/model", // owned by agent.model
		"--yolo",
		"--add-dir", "/tmp/extra", // not blocked: must survive
	}
	got := filterCustomArgs(custom, commandcodeBlockedArgs, slog.Default())

	joined := strings.Join(got, " ")
	for _, blocked := range []string{"--output-format", "--session", "--model", "--yolo"} {
		if strings.Contains(joined, blocked) {
			t.Errorf("%s survived filtering: %v", blocked, got)
		}
	}
	if !strings.Contains(joined, "--add-dir") || !strings.Contains(joined, "/tmp/extra") {
		t.Errorf("an unblocked flag was dropped: %v", got)
	}
}

// The catalog fixture is the real shape of `commandcode --list-models` output
// (Command Code 1.44.0): a header, section titles, aligned id/description
// rows, and a trailing help block.
const commandcodeCatalogFixture = `Available models  ·  67 models

Open Source

deepseek/deepseek-v4-pro               hybrid-attention long-context reasoning
deepseek/deepseek-v4-flash             fast hybrid-attention reasoning (default)
moonshotai/kimi-k3                     long-horizon coding & knowledge work with 1M context

Anthropic

anthropic/claude-fable-5-1             fast frontier coding

Pass the full id, or just the short name after the last "/":
  cmd -m kimi-k3

Docs:  https://commandcode.ai/docs/reference/cli/models
`

func TestCommandcodeModelLineParsesCatalog(t *testing.T) {
	t.Parallel()

	var got []Model
	for _, line := range strings.Split(commandcodeCatalogFixture, "\n") {
		m := commandcodeModelLine.FindStringSubmatch(strings.TrimRight(line, " \t"))
		if m == nil {
			continue
		}
		id, label := m[1], strings.TrimSpace(m[2])
		isDefault := strings.Contains(label, commandcodeDefaultMarker)
		if isDefault {
			label = strings.TrimSpace(strings.Replace(label, commandcodeDefaultMarker, "", 1))
		}
		model := Model{ID: id, Label: label, Default: isDefault}
		if provider, _, ok := strings.Cut(id, "/"); ok {
			model.Provider = provider
		}
		got = append(got, model)
	}

	if len(got) != 4 {
		t.Fatalf("parsed %d models, want 4: %+v", len(got), got)
	}

	// Section headers ("Open Source", "Anthropic") and the trailing help block
	// must not be mistaken for models. The help text contains a slash and the
	// docs URL contains several, which is exactly what the id shape rules out.
	for _, m := range got {
		if strings.Contains(m.ID, "http") || strings.Contains(m.ID, "\"") {
			t.Errorf("help text parsed as a model: %+v", m)
		}
	}

	if got[0].ID != "deepseek/deepseek-v4-pro" || got[0].Provider != "deepseek" {
		t.Errorf("first model: got %+v", got[0])
	}
	if got[0].Default {
		t.Error("first model must not be flagged default")
	}
	if !got[1].Default {
		t.Errorf("the (default) row was not flagged: %+v", got[1])
	}
	if strings.Contains(got[1].Label, commandcodeDefaultMarker) {
		t.Errorf("the default marker leaked into the label: %q", got[1].Label)
	}
	if got[1].Label != "fast hybrid-attention reasoning" {
		t.Errorf("label: got %q", got[1].Label)
	}
	if got[3].Provider != "anthropic" {
		t.Errorf("provider from a later section: got %+v", got[3])
	}
}

func TestCommandcodeIsWiredIntoTheFactory(t *testing.T) {
	t.Parallel()

	if !IsSupportedType("commandcode") {
		t.Fatal("commandcode missing from SupportedTypes")
	}
	backend, err := New("commandcode", Config{Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(commandcode): %v", err)
	}
	if _, ok := backend.(*commandcodeBackend); !ok {
		t.Fatalf("New(commandcode) returned %T", backend)
	}
	if LaunchHeader("commandcode") == "" {
		t.Error("commandcode has no launch header")
	}
}
