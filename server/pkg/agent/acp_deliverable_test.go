package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestACPDeliverableTracker(t *testing.T) {
	t.Parallel()

	text := func(content string) Message { return Message{Type: MessageText, Content: content} }
	toolUse := Message{Type: MessageToolUse, Tool: "read_file", CallID: "tc-1"}

	tests := []struct {
		name            string
		messages        []Message
		wantDeliverable string
		wantFull        string
	}{
		{
			name:            "a turn without tool calls delivers everything",
			messages:        []Message{text("The answer "), text("is 42.")},
			wantDeliverable: "The answer is 42.",
			wantFull:        "The answer is 42.",
		},
		{
			name:            "narration before a tool call is excluded",
			messages:        []Message{text("Let me read the file."), toolUse, text("The file is empty.")},
			wantDeliverable: "The file is empty.",
			wantFull:        "Let me read the file.The file is empty.",
		},
		{
			name: "only the block after the last tool call survives",
			messages: []Message{
				text("First I check the logs."), toolUse,
				text("Now the config."), toolUse,
				text("Both look fine."),
			},
			wantDeliverable: "Both look fine.",
			wantFull:        "First I check the logs.Now the config.Both look fine.",
		},
		{
			name:            "a turn ending on a tool call falls back to the last text block",
			messages:        []Message{text("Done. Posted the summary."), toolUse},
			wantDeliverable: "Done. Posted the summary.",
			wantFull:        "Done. Posted the summary.",
		},
		{
			name: "the fallback is the newest displaced block",
			messages: []Message{
				text("Reading the file."), toolUse,
				text("Done. Posted the summary."), toolUse,
			},
			wantDeliverable: "Done. Posted the summary.",
			wantFull:        "Reading the file.Done. Posted the summary.",
		},
		{
			name:            "consecutive tool calls keep the fallback",
			messages:        []Message{text("Done. Posted the summary."), toolUse, toolUse},
			wantDeliverable: "Done. Posted the summary.",
			wantFull:        "Done. Posted the summary.",
		},
		{
			name:            "whitespace after the last tool call falls back",
			messages:        []Message{text("Done. Posted the summary."), toolUse, text("\n  ")},
			wantDeliverable: "Done. Posted the summary.",
			wantFull:        "Done. Posted the summary.\n  ",
		},
		{
			name: "thinking and tool results carry no deliverable signal",
			messages: []Message{
				{Type: MessageThinking, Content: "the user wants the diagram"},
				text("The diagram shows the architecture."),
				{Type: MessageToolResult, CallID: "tc-1", Output: "<svg />", Status: "completed"},
				{Type: MessageStatus, Status: "running"},
			},
			wantDeliverable: "The diagram shows the architecture.",
			wantFull:        "The diagram shows the architecture.",
		},
		{
			name:            "a turn with no text at all delivers nothing",
			messages:        []Message{toolUse},
			wantDeliverable: "",
			wantFull:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var tracker acpDeliverableTracker
			for _, msg := range tt.messages {
				tracker.observe(msg)
			}
			deliverable, full := tracker.result()
			if deliverable != tt.wantDeliverable {
				t.Errorf("deliverable = %q, want %q", deliverable, tt.wantDeliverable)
			}
			if full != tt.wantFull {
				t.Errorf("full = %q, want %q", full, tt.wantFull)
			}
		})
	}
}

// TestACPDeliverableTrackerIsConcurrencySafe exercises the real access pattern:
// ACP backends call observe from the stdout reader goroutine while the session
// goroutine reads the result. Meaningful under -race.
func TestACPDeliverableTrackerIsConcurrencySafe(t *testing.T) {
	t.Parallel()

	const writers, chunksPerWriter = 4, 200
	var tracker acpDeliverableTracker
	var wg sync.WaitGroup

	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range chunksPerWriter {
				tracker.observe(Message{Type: MessageText, Content: "x"})
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range chunksPerWriter {
			tracker.observe(Message{Type: MessageToolUse, Tool: "read_file"})
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range chunksPerWriter {
			tracker.result()
		}
	}()
	wg.Wait()

	if _, full := tracker.result(); len(full) != writers*chunksPerWriter {
		t.Fatalf("full text length = %d, want %d — chunks were lost", len(full), writers*chunksPerWriter)
	}
}

// acpDeliverableCase describes one ACP backend for the shared boundary
// regression test: how New names it, what its executable is called, and which
// session-update dialect its runtime speaks.
type acpDeliverableCase struct {
	backend string
	binary  string
	// notification is the JSON-RPC method the runtime pushes updates on.
	notification string
	// camelUpdates selects the CamelCase discriminator (`update.type:
	// "AgentMessageChunk"`) kiro and qoder emit, instead of the snake_case
	// `update.sessionUpdate` form hermes/kimi/traecli/grok emit.
	camelUpdates bool
}

// acpDeliverableCases covers every backend sharing acpDeliverableTracker.
var acpDeliverableCases = []acpDeliverableCase{
	{backend: "hermes", binary: "hermes", notification: "session/update"},
	{backend: "kimi", binary: "kimi", notification: "session/update"},
	{backend: "reasonix", binary: "reasonix", notification: "session/update"},
	{backend: "traecli", binary: "traecli", notification: "session/update"},
	{backend: "grok", binary: "grok", notification: "session/update"},
	{backend: "kiro", binary: "kiro-cli", notification: "session/notification", camelUpdates: true},
	{backend: "qoder", binary: "qodercli", notification: "session/notification", camelUpdates: true},
	{backend: "qoderclicn", binary: "qoderclicn", notification: "session/notification", camelUpdates: true},
}

const (
	acpDeliverableNarration  = "I will read the diagram first."
	acpDeliverableAnswerHead = "The diagram shows "
	acpDeliverableAnswerTail = "the service architecture."
	acpDeliverableAnswer     = acpDeliverableAnswerHead + acpDeliverableAnswerTail
)

// fakeACPDeliverableScript returns a POSIX-sh ACP runtime that plays one turn
// in the shape that leaked process narration into channel replies (#6006):
// interim narration, a tool call, then the user-facing answer.
func fakeACPDeliverableScript(c acpDeliverableCase) string {
	notify := func(update string) string {
		return `      printf '{"jsonrpc":"2.0","method":"` + c.notification +
			`","params":{"sessionId":"ses_deliverable","update":` + update + `}}\n'`
	}
	discriminator := func(snake, camel string) string {
		if c.camelUpdates {
			return `"type":"` + camel + `"`
		}
		return `"sessionUpdate":"` + snake + `"`
	}
	chunk := func(text string) string {
		return `{` + discriminator("agent_message_chunk", "AgentMessageChunk") +
			`,"content":{"type":"text","text":"` + text + `"}}`
	}
	toolCall := func(withArgs bool) string {
		args := ""
		if withArgs {
			args = `,"parameters":{"path":"diagram.svg"}`
		}
		return `{` + discriminator("tool_call", "ToolCall") +
			`,"toolCallId":"tc-read","name":"read_file","status":"pending"` + args + `}`
	}
	toolResult := `{` + discriminator("tool_call_update", "ToolCallUpdate") +
		`,"toolCallId":"tc-read","name":"read_file","status":"completed","output":"<svg />"}`

	return `#!/bin/sh
# Fake ACP runtime shared by the deliverable-boundary regression tests. All
# three text chunks belong in the streamed transcript; only the post-tool-call
# text is the backend's deliverable Result.Output.
#
# ACP_PRE_TOOL_TEXT replaces the pre-tool text, ACP_TOOL_TERMINATED=1 ends the
# turn on the tool call, and ACP_DEFERRED_TOOL=1 withholds the tool arguments so
# the ACP transport defers MessageToolUse to tool completion instead of emitting
# it at the tool call.
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"authMethods":[{"id":"cached_token","name":"Cached login"},{"id":"xai.api_key","name":"API key"}],"agentCapabilities":{"loadSession":true}}}\n' "$id"
      ;;
    *'"method":"authenticate"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"ses_deliverable"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      pre='` + acpDeliverableNarration + `'
      if [ -n "$ACP_PRE_TOOL_TEXT" ]; then
        pre=$ACP_PRE_TOOL_TEXT
      fi
` + notify(chunk("%s")) + ` "$pre"
      if [ "$ACP_DEFERRED_TOOL" = "1" ]; then
` + notify(toolCall(false)) + `
      else
` + notify(toolCall(true)) + `
      fi
` + notify(toolResult) + `
      if [ "$ACP_TOOL_TERMINATED" != "1" ]; then
` + notify(chunk(acpDeliverableAnswerHead)) + `
` + notify(chunk(acpDeliverableAnswerTail)) + `
      fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0
      ;;
  esac
done
`
}

func runACPDeliverableTurn(t *testing.T, c acpDeliverableCase, env map[string]string) (Result, []Message) {
	t.Helper()

	fakePath := filepath.Join(t.TempDir(), c.binary)
	writeTestExecutable(t, fakePath, []byte(fakeACPDeliverableScript(c)))

	backend, err := New(c.backend, Config{ExecutablePath: fakePath, Logger: slog.Default(), Env: env})
	if err != nil {
		t.Fatalf("new %s backend: %v", c.backend, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "inspect the diagram", ExecOptions{Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("execute %s: %v", c.backend, err)
	}

	var streamed []Message
	messagesDone := make(chan struct{})
	go func() {
		defer close(messagesDone)
		for msg := range session.Messages {
			streamed = append(streamed, msg)
		}
	}()

	result := <-session.Result
	<-messagesDone
	return result, streamed
}

// acpDeliverableEmissionPaths covers the two ways the ACP transport surfaces a
// tool call: immediately when the runtime pre-populates the arguments, and
// deferred to tool completion when it streams them. The deliverable boundary
// must hold on both.
var acpDeliverableEmissionPaths = []struct {
	name string
	env  map[string]string
}{
	{name: "tool use emitted at start"},
	{name: "tool use deferred until completion", env: map[string]string{"ACP_DEFERRED_TOOL": "1"}},
}

// TestACPBackendsDeliverFinalAnswerWithoutNarration pins the boundary across
// every ACP backend: interim narration stays in the streamed transcript but
// must not reach Result.Output, which becomes the channel reply and the
// auto-generated issue comment (#6006, MUL-5394).
func TestACPBackendsDeliverFinalAnswerWithoutNarration(t *testing.T) {
	t.Parallel()

	for _, c := range acpDeliverableCases {
		for _, path := range acpDeliverableEmissionPaths {
			t.Run(c.backend+"/"+path.name, func(t *testing.T) {
				t.Parallel()

				result, streamed := runACPDeliverableTurn(t, c, path.env)
				if result.Status != "completed" {
					t.Fatalf("status = %q, want completed (error=%q output=%q)", result.Status, result.Error, result.Output)
				}
				if result.Output != acpDeliverableAnswer {
					t.Fatalf("Result.Output = %q, want the final answer %q", result.Output, acpDeliverableAnswer)
				}

				var text []string
				for _, msg := range streamed {
					if msg.Type == MessageText {
						text = append(text, msg.Content)
					}
				}
				want := acpDeliverableNarration + "|" + acpDeliverableAnswerHead + "|" + acpDeliverableAnswerTail
				if got := strings.Join(text, "|"); got != want {
					t.Fatalf("transcript text = %q, want narration and final answer %q", got, want)
				}
			})
		}
	}
}

// TestACPBackendsToolTerminatedTurnFallsBackToLatestTextBlock covers the turn
// that ends on a tool call — the agent answers and then posts it with a tool.
// There is no post-tool text, so the deliverable must fall back to the latest
// text block instead of replying with an empty string.
func TestACPBackendsToolTerminatedTurnFallsBackToLatestTextBlock(t *testing.T) {
	t.Parallel()

	const want = "Done. Posted the summary to the issue."

	for _, c := range acpDeliverableCases {
		for _, path := range acpDeliverableEmissionPaths {
			t.Run(c.backend+"/"+path.name, func(t *testing.T) {
				t.Parallel()

				env := map[string]string{
					"ACP_PRE_TOOL_TEXT":   want,
					"ACP_TOOL_TERMINATED": "1",
					"ACP_DEFERRED_TOOL":   path.env["ACP_DEFERRED_TOOL"],
				}
				result, _ := runACPDeliverableTurn(t, c, env)
				if result.Status != "completed" {
					t.Fatalf("status = %q, want completed (error=%q output=%q)", result.Status, result.Error, result.Output)
				}
				if result.Output != want {
					t.Fatalf("Result.Output = %q, want the fallback text block %q", result.Output, want)
				}
			})
		}
	}
}
