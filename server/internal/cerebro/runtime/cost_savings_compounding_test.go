package runtime

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/mcp"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// echoToolCallResponse is one scripted chat-completions gateway turn where the
// model asks to call the echo tool with the given text. The echo tool returns
// "echoed:"+text, so the text length controls the size of the tool result that
// lands in the transcript (and is later pruned).
func echoToolCallResponse(text string) string {
	return fmt.Sprintf(`{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c","type":"function","function":{"name":"echo","arguments":"{\"text\":\"%s\"}"}}]}}],"firtal":{"input_tokens":1,"output_tokens":1}}`, text)
}

// anthropicEchoToolUse is the Anthropic-native (/v1/messages) equivalent: a
// tool_use block calling echo with the given text.
func anthropicEchoToolUse(text string) string {
	return fmt.Sprintf(`{"content":[{"type":"tool_use","id":"c1","name":"echo","input":{"text":"%s"}}],"firtal":{"input_tokens":1,"output_tokens":1}}`, text)
}

// echoMCPServer registers the echo tool used by the compounding loops. The tool
// returns "echoed:"+text so a caller can size the tool result by the arg length.
func echoMCPServer() *mcp.Server {
	srv := mcp.NewServer("test-tools", "0.0.0")
	srv.RegisterTool(mcp.Tool{Name: "echo"}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		return mcp.TextResult("echoed:" + args["text"].(string)), nil
	})
	return srv
}

// runPruneLoopWithTexts drives the OpenAI-compat MCP tool loop
// (runToolLoopWithServer) for len(texts) tool rounds (one echo call per text)
// plus the forced final text call, with pruning toggled by pruneOn.
func runPruneLoopWithTexts(t *testing.T, texts []string, pruneOn bool) GatewayCompletion {
	t.Helper()
	script := make([]string, 0, len(texts)+1)
	for _, txt := range texts {
		script = append(script, echoToolCallResponse(txt))
	}
	script = append(script, `{"choices":[{"message":{"content":"final answer"}}],"firtal":{"input_tokens":1,"output_tokens":1}}`)
	gateway, _ := fakeGatewayScript(t, script)

	e := &FirtalGatewayExecutor{gateway: gateway, logger: testLogger()}
	completion, err := e.runToolLoopWithServer(context.Background(),
		// MaxToolRounds == len(texts) forces every text into its own round, so
		// pruning fires after each round and the loop ends on the forced final call.
		FirtalGatewayRuntimeConfig{BaseURL: "https://x", APIKey: "rk", Model: "claude-sonnet-4-6", MaxTokens: 4096, MaxToolRounds: len(texts)},
		db.Agent{},
		[]GatewayMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "go"}},
		GatewayRequestMeta{TaskID: "t1"},
		pgtype.UUID{},
		pgtype.UUID{},
		echoMCPServer(),
		[]GatewayToolDef{{Type: "function", Function: GatewayToolFunction{Name: "echo"}}},
		pruneOn,
	)
	if err != nil {
		t.Fatalf("runToolLoopWithServer error = %v", err)
	}
	return completion
}

// runAnthropicPruneLoop drives the PRODUCTION Anthropic-native tool loop
// (runAnthropicToolLoop) the same way, so the compounding measure is proven on
// the path real gateway runs take, not only the OpenAI-compat fallback.
func runAnthropicPruneLoop(t *testing.T, texts []string, pruneOn bool) GatewayCompletion {
	t.Helper()
	script := make([]string, 0, len(texts)+1)
	for _, txt := range texts {
		script = append(script, anthropicEchoToolUse(txt))
	}
	script = append(script, `{"content":[{"type":"text","text":"final answer"}],"firtal":{"input_tokens":1,"output_tokens":1}}`)
	gateway, _ := fakeGatewayScript(t, script)

	e := &FirtalGatewayExecutor{gateway: gateway, logger: testLogger()}
	completion, err := e.runAnthropicToolLoop(context.Background(),
		FirtalGatewayRuntimeConfig{BaseURL: "https://x", APIKey: "rk", Model: "claude-sonnet-4-6", MaxTokens: 4096, MaxToolRounds: len(texts)},
		db.Agent{},
		[]GatewayMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "go"}},
		GatewayRequestMeta{TaskID: "t1"},
		pgtype.UUID{},
		pgtype.UUID{},
		[]AnthropicTool{{Name: "echo", InputSchema: map[string]any{"type": "object"}}},
		ToolContext{},
		echoMCPServer(),
		nil,   // registry
		false, // useRegistry -> MCP server path
		pruneOn,
	)
	if err != nil {
		t.Fatalf("runAnthropicToolLoop error = %v", err)
	}
	return completion
}

// assertCompoundsByPosition is the shared proof: two runs prune an identical set
// of bytes — only the POSITION of the big result differs — so the once-counted
// PrunedToolResultChars is equal, but the compounding measure must be strictly
// larger for the run that pruned early (its bytes were omitted from more
// subsequent model calls). FIR-2639.
func assertCompoundsByPosition(t *testing.T, loop string, early, late GatewayCompletion) {
	t.Helper()
	if early.PrunedToolResultChars != late.PrunedToolResultChars {
		t.Fatalf("[%s] once-counted pruned chars differ (early=%d late=%d) — runs not comparable",
			loop, early.PrunedToolResultChars, late.PrunedToolResultChars)
	}
	if early.PrunedToolResultChars == 0 {
		t.Fatalf("[%s] expected some chars to be pruned, got 0", loop)
	}
	if early.PrunedContextCharsCompounded <= late.PrunedContextCharsCompounded {
		t.Fatalf("[%s] compounding did not reward early pruning: early=%d late=%d (want early > late)",
			loop, early.PrunedContextCharsCompounded, late.PrunedContextCharsCompounded)
	}
	// Compounding always meets or exceeds the once-counted figure: every pruned
	// round is followed by at least the final model call.
	if early.PrunedContextCharsCompounded < early.PrunedToolResultChars ||
		late.PrunedContextCharsCompounded < late.PrunedToolResultChars {
		t.Errorf("[%s] compounding below once-counted: early %d<%d or late %d<%d", loop,
			early.PrunedContextCharsCompounded, early.PrunedToolResultChars,
			late.PrunedContextCharsCompounded, late.PrunedToolResultChars)
	}
	// Pruning the big chunk early genuinely compounds beyond a single count.
	if early.PrunedContextCharsCompounded <= early.PrunedToolResultChars {
		t.Errorf("[%s] early compounding %d not greater than once-counted %d — no compounding effect",
			loop, early.PrunedContextCharsCompounded, early.PrunedToolResultChars)
	}
}

// 100 chars vs 2 chars: same four rounds, same pruned byte total — the big
// result just sits in an early round vs. a late round.
const (
	bigToolText   = "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	smallToolText = "yy"
)

// FIR-2639: the prune saving compounds. A tool result pruned EARLY in a run
// would otherwise have been resent in every subsequent model call, so it must
// count more than the SAME result pruned in the last round. Proven on the
// OpenAI-compat MCP loop.
func TestRunToolLoop_PruneSavingCompoundsByPosition(t *testing.T) {
	early := runPruneLoopWithTexts(t, []string{bigToolText, smallToolText, smallToolText, smallToolText}, true)
	late := runPruneLoopWithTexts(t, []string{smallToolText, smallToolText, bigToolText, smallToolText}, true)
	assertCompoundsByPosition(t, "openai-compat", early, late)
}

// Same proof on the PRODUCTION Anthropic-native loop (the path real gateway runs
// take), closing the QA caveat that it was only verified by code inspection.
func TestRunAnthropicToolLoop_PruneSavingCompoundsByPosition(t *testing.T) {
	early := runAnthropicPruneLoop(t, []string{bigToolText, smallToolText, smallToolText, smallToolText}, true)
	late := runAnthropicPruneLoop(t, []string{smallToolText, smallToolText, bigToolText, smallToolText}, true)
	assertCompoundsByPosition(t, "anthropic", early, late)
}

// With pruning off, no compounding saving is recorded — the field stays zero so
// shadow / control runs never report a phantom saving. Checked on both loops.
func TestRunToolLoop_NoCompoundingWhenPruneOff(t *testing.T) {
	compat := runPruneLoopWithTexts(t, []string{"aaaa", "bbbb", "cccc"}, false)
	if compat.PrunedToolResultChars != 0 || compat.PrunedContextCharsCompounded != 0 {
		t.Fatalf("openai-compat prune off must record nothing, got once=%d compounded=%d",
			compat.PrunedToolResultChars, compat.PrunedContextCharsCompounded)
	}
	anthropic := runAnthropicPruneLoop(t, []string{"aaaa", "bbbb", "cccc"}, false)
	if anthropic.PrunedToolResultChars != 0 || anthropic.PrunedContextCharsCompounded != 0 {
		t.Fatalf("anthropic prune off must record nothing, got once=%d compounded=%d",
			anthropic.PrunedToolResultChars, anthropic.PrunedContextCharsCompounded)
	}
}
