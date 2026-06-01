package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/mcp"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// FIR-2572: anthropicHistoryChars must count the system prompt plus every part
// of the transcript that is actually sent — plain-string turns, tool_use input
// arguments, and the nested text of tool_result blocks. This is the denominator
// of the chars→tokens calibration, so an undercount inflates the saved-token
// figure.
func TestAnthropicHistoryChars_CountsSystemAndTranscript(t *testing.T) {
	history := []AnthropicMessage{
		{Role: "user", Content: "hello"}, // 5
		{Role: "assistant", Content: []AnthropicContentBlock{
			// name "echo" (4) + marshalled input {"text":"hi"} (13) = 17
			{Type: "tool_use", ID: "c1", Name: "echo", Input: map[string]any{"text": "hi"}},
		}},
		{Role: "user", Content: []AnthropicContentBlock{
			// nested text "echoed:hi" (9); outer tool_result block has no own text
			{Type: "tool_result", ToolUseID: "c1", Content: []AnthropicContentBlock{
				{Type: "text", Text: "echoed:hi"},
			}},
		}},
	}
	// systemText "SYS" (3) + 5 + 17 + 9 = 34
	got := anthropicHistoryChars("SYS", history)
	if got != 34 {
		t.Fatalf("anthropicHistoryChars = %d, want 34", got)
	}
}

// FIR-2572 regression: the Anthropic-native tool loop sums provider prompt
// TOKENS across every round (each round re-sends the growing transcript). The
// matching prompt-CHAR count must therefore also be summed per request, on the
// same basis, otherwise the chars→tokens ratio is skewed and savedContextTokens
// over-measures. The earlier code overwrote PromptInputChars at exit with a
// single-transcript formula, so the denominator was one transcript while the
// numerator was N transcripts.
//
// We send a large user prompt and force three model requests (two tool rounds
// then a text answer). Summed across three requests the prompt chars must exceed
// twice the user prompt; the buggy single-transcript value counted that prompt
// only once and would fall below the bound.
func TestRunAnthropicToolLoop_PromptInputCharsSummedAcrossRounds(t *testing.T) {
	const userChars = 10_000
	bigUser := strings.Repeat("u", userChars)

	toolUse := `{"content":[{"type":"tool_use","id":"c1","name":"echo","input":{"text":"hi"}}],"firtal":{"input_tokens":100,"output_tokens":5}}`
	finalText := `{"content":[{"type":"text","text":"done"}],"firtal":{"input_tokens":50,"output_tokens":5}}`
	gateway, calls := fakeGatewayScript(t, []string{toolUse, toolUse, finalText})

	toolSrv := mcp.NewServer("test-tools", "0.0.0")
	toolSrv.RegisterTool(mcp.Tool{Name: "echo"}, func(ctx context.Context, args map[string]any) (mcp.CallToolResult, error) {
		return mcp.TextResult("ok"), nil
	})

	e := &FirtalGatewayExecutor{gateway: gateway, logger: testLogger()}
	cfg := FirtalGatewayRuntimeConfig{BaseURL: "https://x", APIKey: "rk", Model: "claude-sonnet-4-6", MaxTokens: 4096}

	completion, err := e.runAnthropicToolLoop(context.Background(), cfg, db.Agent{},
		[]GatewayMessage{
			{Role: "system", Content: "be brief"},
			{Role: "user", Content: bigUser},
		},
		GatewayRequestMeta{TaskID: "t1"},
		pgtype.UUID{}, pgtype.UUID{},
		[]AnthropicTool{{Name: "echo", InputSchema: map[string]any{"type": "object"}}},
		ToolContext{},
		toolSrv,
		nil,   // registry
		false, // useRegistry -> MCP server path
		false, // pruneOn
	)
	if err != nil {
		t.Fatalf("runAnthropicToolLoop error = %v", err)
	}
	if got := *calls; got != 3 {
		t.Fatalf("gateway called %d times, want 3", got)
	}
	if completion.Output != "done" {
		t.Fatalf("Output = %q, want %q", completion.Output, "done")
	}
	// Usage tokens are summed across the three requests: 100+100+50 input.
	if completion.Usage.InputTokens != 250 {
		t.Fatalf("InputTokens = %d, want 250 (summed across rounds)", completion.Usage.InputTokens)
	}
	// The big user prompt is resent on all three requests, so the summed prompt
	// chars must exceed 2x it. The old single-transcript overwrite counted it
	// once (~userChars) and would fail this bound.
	if completion.PromptInputChars <= 2*userChars {
		t.Fatalf("PromptInputChars = %d, want > %d (summed across 3 rounds, not a single transcript)",
			completion.PromptInputChars, 2*userChars)
	}
}
