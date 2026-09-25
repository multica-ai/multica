package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ── Fixtures from real Copilot CLI v1.0.28 --output-format json output ──

const fixtureAssistantMessageDelta = `{"type":"assistant.message_delta","data":{"messageId":"b5148f3f-d24b-4a5e-a95c-2be7d6493a52","deltaContent":"pong"},"id":"eb6c3ef1-0388-4010-bf8e-4002b62db58c","timestamp":"2026-04-16T08:43:38.401Z","parentId":"417b175a-b303-4378-9c43-d4fcb177c05a","ephemeral":true}`

const fixtureAssistantMessage = `{"type":"assistant.message","data":{"messageId":"b5148f3f-d24b-4a5e-a95c-2be7d6493a52","content":"pong","toolRequests":[],"interactionId":"267266f6-47bc-4f31-8338-4e95961cf900","outputTokens":5,"requestId":"D012:2F8B66:8CB3C8:98A605:69E0A137"},"id":"ddff21bc-5829-4892-822a-06f3f543ea1d","timestamp":"2026-04-16T08:43:38.493Z","parentId":"417b175a-b303-4378-9c43-d4fcb177c05a"}`

const fixtureAssistantMessageWithTools = `{"type":"assistant.message","data":{"messageId":"0c48f3f5-74a2-485b-8969-3ea8ddc4c303","content":"","toolRequests":[{"toolCallId":"toolu_vrtx_01UqgJdCxuteCRZvKpdjUFyL","name":"bash","arguments":{"command":"ls","description":"List files"},"type":"function","intentionSummary":"List files in current directory"}],"interactionId":"b7bede2d-6996-4728-bdfa-33ba546ed511","outputTokens":112,"requestId":"EB94:21B867:8C33D3:983053:69E0A149"},"id":"6c005d04-bf23-4114-8dcb-f2f9bcdd3880","timestamp":"2026-04-16T08:43:59.066Z","parentId":"387c2814-f893-443c-82b8-00db66fef14c"}`

const fixtureToolExecComplete = `{"type":"tool.execution_complete","data":{"toolCallId":"toolu_vrtx_01UqgJdCxuteCRZvKpdjUFyL","model":"claude-opus-4.6","interactionId":"b7bede2d-6996-4728-bdfa-33ba546ed511","success":true,"result":{"content":"file1.go\nfile2.go\n","detailedContent":"file1.go\nfile2.go\n"},"toolTelemetry":{}},"id":"1662b7b1-5160-4c03-bc83-59a9a367f070","timestamp":"2026-04-16T08:43:59.530Z","parentId":"92531882-91ba-442a-9974-3dd8745fffd0"}`

const fixtureToolExecCompleteError = `{"type":"tool.execution_complete","data":{"toolCallId":"toolu_err_01","model":"claude-opus-4.6","interactionId":"int-1","success":false,"error":{"message":"command not found: foobar"}},"id":"err-1","timestamp":"2026-04-16T08:44:00.000Z","parentId":"p-1"}`

const fixtureTurnStart = `{"type":"assistant.turn_start","data":{"turnId":"0","interactionId":"267266f6-47bc-4f31-8338-4e95961cf900"},"id":"417b175a-b303-4378-9c43-d4fcb177c05a","timestamp":"2026-04-16T08:43:36.401Z","parentId":"ed1a637b-c636-4b74-bc82-4ba3f3386aad"}`

const fixtureResult = `{"type":"result","timestamp":"2026-04-16T08:43:38.524Z","sessionId":"35059dc3-d928-4ffb-8616-b78938621d85","exitCode":0,"usage":{"premiumRequests":3,"totalApiDurationMs":1763,"sessionDurationMs":6275,"codeChanges":{"linesAdded":0,"linesRemoved":0,"filesModified":[]}}}`

const fixtureResultNonZero = `{"type":"result","timestamp":"2026-04-16T08:50:00.000Z","sessionId":"dead-beef","exitCode":1,"usage":{"premiumRequests":1,"totalApiDurationMs":500,"sessionDurationMs":1000}}`

const fixtureSessionError = `{"type":"session.error","data":{"errorType":"rate_limit","message":"Rate limit exceeded"},"id":"se-1","timestamp":"2026-04-16T09:00:00.000Z","parentId":"p-1"}`

const fixtureEphemeral = `{"type":"session.mcp_servers_loaded","data":{"servers":[{"name":"github-mcp-server","status":"connected","source":"builtin"}]},"id":"330ac6bb-b2db-435e-8082-686face58a72","timestamp":"2026-04-16T08:43:34.803Z","parentId":"fe20d689-31ec-492c-9eb5-57a0d0834d70","ephemeral":true}`

const fixtureSessionStart = `{"type":"session.start","data":{"sessionId":"35059dc3-d928-4ffb-8616-b78938621d85","selectedModel":"claude-sonnet-4","context":{"cwd":"/tmp"}},"id":"ss-1","timestamp":"2026-04-16T08:43:34.000Z"}`

const fixtureReasoning = `{"type":"assistant.reasoning","data":{"content":"Let me think about this..."},"id":"r-1","timestamp":"2026-04-16T08:43:37.000Z","parentId":"p-1"}`

const fixtureReasoningDelta = `{"type":"assistant.reasoning_delta","data":{"deltaContent":"thinking step"},"id":"rd-1","timestamp":"2026-04-16T08:43:37.100Z","parentId":"p-1","ephemeral":true}`

const fixtureSessionWarning = `{"type":"session.warning","data":{"warningType":"rate_limit_approaching","message":"You are approaching your rate limit"},"id":"sw-1","timestamp":"2026-04-16T09:00:00.000Z","parentId":"p-1"}`

// parseCopilotEvent is a test helper that unmarshals a JSONL line into a copilotEvent.
func parseCopilotEvent(t *testing.T, line string) copilotEvent {
	t.Helper()
	var evt copilotEvent
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		t.Fatalf("failed to parse fixture: %v", err)
	}
	return evt
}

// ── Parser tests using real JSONL fixtures ──

func TestCopilotParseAssistantMessageDelta(t *testing.T) {
	t.Parallel()
	evt := parseCopilotEvent(t, fixtureAssistantMessageDelta)

	if evt.Type != "assistant.message_delta" {
		t.Fatalf("expected type assistant.message_delta, got %q", evt.Type)
	}
	var delta copilotMessageDelta
	if err := json.Unmarshal(evt.Data, &delta); err != nil {
		t.Fatalf("unmarshal delta data: %v", err)
	}
	if delta.DeltaContent != "pong" {
		t.Fatalf("expected deltaContent 'pong', got %q", delta.DeltaContent)
	}
	if delta.MessageID != "b5148f3f-d24b-4a5e-a95c-2be7d6493a52" {
		t.Fatalf("unexpected messageId: %q", delta.MessageID)
	}
}

func TestCopilotParseAssistantMessage(t *testing.T) {
	t.Parallel()
	evt := parseCopilotEvent(t, fixtureAssistantMessage)

	if evt.Type != "assistant.message" {
		t.Fatalf("expected type assistant.message, got %q", evt.Type)
	}
	var msg copilotAssistantMessage
	if err := json.Unmarshal(evt.Data, &msg); err != nil {
		t.Fatalf("unmarshal message data: %v", err)
	}
	if msg.Content != "pong" {
		t.Fatalf("expected content 'pong', got %q", msg.Content)
	}
	if msg.OutputTokens != 5 {
		t.Fatalf("expected outputTokens 5, got %d", msg.OutputTokens)
	}
	if len(msg.ToolRequests) != 0 {
		t.Fatalf("expected no tool requests, got %d", len(msg.ToolRequests))
	}
}

func TestCopilotParseAssistantMessageWithToolRequests(t *testing.T) {
	t.Parallel()
	evt := parseCopilotEvent(t, fixtureAssistantMessageWithTools)

	var msg copilotAssistantMessage
	if err := json.Unmarshal(evt.Data, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(msg.ToolRequests) != 1 {
		t.Fatalf("expected 1 tool request, got %d", len(msg.ToolRequests))
	}
	tr := msg.ToolRequests[0]
	if tr.Name != "bash" {
		t.Fatalf("expected tool name 'bash', got %q", tr.Name)
	}
	if tr.ToolCallID != "toolu_vrtx_01UqgJdCxuteCRZvKpdjUFyL" {
		t.Fatalf("unexpected toolCallId: %q", tr.ToolCallID)
	}
	var args map[string]any
	if err := json.Unmarshal(tr.Arguments, &args); err != nil {
		t.Fatalf("unmarshal arguments: %v", err)
	}
	if args["command"] != "ls" {
		t.Fatalf("expected command 'ls', got %v", args["command"])
	}
}

func TestCopilotParseToolExecComplete(t *testing.T) {
	t.Parallel()
	evt := parseCopilotEvent(t, fixtureToolExecComplete)

	if evt.Type != "tool.execution_complete" {
		t.Fatalf("expected type tool.execution_complete, got %q", evt.Type)
	}
	var tc copilotToolExecComplete
	if err := json.Unmarshal(evt.Data, &tc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !tc.Success {
		t.Fatal("expected success=true")
	}
	if tc.Model != "claude-opus-4.6" {
		t.Fatalf("expected model claude-opus-4.6, got %q", tc.Model)
	}
	if tc.Result == nil || tc.Result.Content != "file1.go\nfile2.go\n" {
		t.Fatalf("unexpected result content: %v", tc.Result)
	}
}

func TestCopilotParseToolExecCompleteError(t *testing.T) {
	t.Parallel()
	evt := parseCopilotEvent(t, fixtureToolExecCompleteError)

	var tc copilotToolExecComplete
	if err := json.Unmarshal(evt.Data, &tc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tc.Success {
		t.Fatal("expected success=false")
	}
	if tc.Error == nil || tc.Error.Message != "command not found: foobar" {
		t.Fatalf("unexpected error: %v", tc.Error)
	}
}

func TestCopilotParseResultFractionalPremiumRequests(t *testing.T) {
	t.Parallel()
	// Regression: real Copilot CLI v1.0.32 emits premiumRequests as a float
	// (e.g. 7.5). Decoding into an int field used to fail the entire result
	// line, dropping sessionId and breaking chat-session resume.
	const line = `{"type":"result","timestamp":"2026-04-20T05:34:30.469Z","sessionId":"349793b7-7067-49d4-a807-8788561643bd","exitCode":0,"usage":{"premiumRequests":7.5,"totalApiDurationMs":1500,"sessionDurationMs":5842,"codeChanges":{"linesAdded":0,"linesRemoved":0,"filesModified":[]}}}`
	var evt copilotEvent
	if err := json.Unmarshal([]byte(line), &evt); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if evt.SessionID != "349793b7-7067-49d4-a807-8788561643bd" {
		t.Fatalf("unexpected sessionId: %q", evt.SessionID)
	}
	if evt.Usage == nil || evt.Usage.PremiumRequests != 7.5 {
		t.Fatalf("expected premiumRequests=7.5, got %#v", evt.Usage)
	}
}

func TestCopilotParseResult(t *testing.T) {
	t.Parallel()
	evt := parseCopilotEvent(t, fixtureResult)

	if evt.Type != "result" {
		t.Fatalf("expected type result, got %q", evt.Type)
	}
	if evt.SessionID != "35059dc3-d928-4ffb-8616-b78938621d85" {
		t.Fatalf("unexpected sessionId: %q", evt.SessionID)
	}
	if evt.ExitCode != 0 {
		t.Fatalf("expected exitCode 0, got %d", evt.ExitCode)
	}
	if evt.Usage == nil {
		t.Fatal("expected usage to be present")
	}
	if evt.Usage.PremiumRequests != 3 {
		t.Fatalf("expected 3 premiumRequests, got %v", evt.Usage.PremiumRequests)
	}
	if evt.Usage.TotalAPIDurationMs != 1763 {
		t.Fatalf("expected totalApiDurationMs 1763, got %d", evt.Usage.TotalAPIDurationMs)
	}
}

func TestCopilotParseResultNonZeroExit(t *testing.T) {
	t.Parallel()
	evt := parseCopilotEvent(t, fixtureResultNonZero)

	if evt.ExitCode != 1 {
		t.Fatalf("expected exitCode 1, got %d", evt.ExitCode)
	}
	if evt.SessionID != "dead-beef" {
		t.Fatalf("unexpected sessionId: %q", evt.SessionID)
	}
}

func TestCopilotParseSessionError(t *testing.T) {
	t.Parallel()
	evt := parseCopilotEvent(t, fixtureSessionError)

	if evt.Type != "session.error" {
		t.Fatalf("expected type session.error, got %q", evt.Type)
	}
	var se copilotSessionError
	if err := json.Unmarshal(evt.Data, &se); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if se.ErrorType != "rate_limit" {
		t.Fatalf("expected errorType rate_limit, got %q", se.ErrorType)
	}
	if se.Message != "Rate limit exceeded" {
		t.Fatalf("unexpected message: %q", se.Message)
	}
}

// ── Integration-style tests: feed fixture JSONL through the event loop ──

// simulateCopilotEventLoop feeds JSONL lines through handleCopilotEvent —
// the exact same function used in production — and collects the results.
func simulateCopilotEventLoop(t *testing.T, lines []string) ([]Message, string, string, map[string]TokenUsage) {
	return simulateCopilotEventLoopWithModel(t, lines, "copilot")
}

func simulateCopilotEventLoopWithModel(t *testing.T, lines []string, seedModel string) ([]Message, string, string, map[string]TokenUsage) {
	t.Helper()
	msgs, st := runCopilotEventLoop(t, lines, seedModel, false)
	return msgs, st.sessionID, st.finalStatus, st.resolveUsage()
}

func runCopilotEventLoop(t *testing.T, lines []string, seedModel string, resumed bool) ([]Message, *copilotEventState) {
	t.Helper()
	var msgs []Message
	st := newCopilotEventState(seedModel, resumed)

	for _, line := range lines {
		var evt copilotEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			continue
		}
		msgs = append(msgs, handleCopilotEvent(evt, st)...)
	}
	return msgs, st
}

func TestCopilotEventLoopSimpleMessage(t *testing.T) {
	t.Parallel()
	lines := []string{
		fixtureTurnStart,
		fixtureAssistantMessageDelta,
		fixtureAssistantMessage,
		`{"type":"assistant.turn_end","data":{"turnId":"0"},"id":"fc387368","timestamp":"2026-04-16T08:43:38.494Z","parentId":"ddff21bc"}`,
		fixtureResult,
	}

	msgs, sessionID, status, usage := simulateCopilotEventLoop(t, lines)

	if sessionID != "35059dc3-d928-4ffb-8616-b78938621d85" {
		t.Fatalf("unexpected sessionId: %q", sessionID)
	}
	if status != "completed" {
		t.Fatalf("expected completed, got %q", status)
	}

	// Should have: turn_start(status), delta(text:pong), message doesn't re-emit text
	var gotStatus, gotText bool
	for _, m := range msgs {
		if m.Type == MessageStatus && m.Status == "running" {
			gotStatus = true
		}
		if m.Type == MessageText && m.Content == "pong" {
			gotText = true
		}
	}
	if !gotStatus {
		t.Fatal("expected status=running message")
	}
	if !gotText {
		t.Fatal("expected text=pong message")
	}

	u, ok := usage["copilot"]
	if !ok {
		t.Fatal("expected usage entry for 'copilot'")
	}
	if u.OutputTokens != 5 {
		t.Fatalf("expected 5 outputTokens, got %d", u.OutputTokens)
	}
}

func TestCopilotEventLoopToolUseFlow(t *testing.T) {
	t.Parallel()
	lines := []string{
		fixtureTurnStart,
		fixtureAssistantMessageWithTools,
		fixtureToolExecComplete,
		fixtureResult,
	}

	msgs, sessionID, status, usage := simulateCopilotEventLoop(t, lines)

	if sessionID != "35059dc3-d928-4ffb-8616-b78938621d85" {
		t.Fatalf("unexpected sessionId: %q", sessionID)
	}
	if status != "completed" {
		t.Fatalf("expected completed, got %q", status)
	}

	// Find tool use and tool result messages.
	var toolUse, toolResult *Message
	for i := range msgs {
		if msgs[i].Type == MessageToolUse {
			toolUse = &msgs[i]
		}
		if msgs[i].Type == MessageToolResult {
			toolResult = &msgs[i]
		}
	}
	if toolUse == nil {
		t.Fatal("expected MessageToolUse")
	}
	if toolUse.Tool != "bash" {
		t.Fatalf("expected tool 'bash', got %q", toolUse.Tool)
	}
	if toolUse.CallID != "toolu_vrtx_01UqgJdCxuteCRZvKpdjUFyL" {
		t.Fatalf("unexpected callID: %q", toolUse.CallID)
	}
	if toolResult == nil {
		t.Fatal("expected MessageToolResult")
	}
	if toolResult.CallID != toolUse.CallID {
		t.Fatalf("tool result callID %q doesn't match tool use callID %q", toolResult.CallID, toolUse.CallID)
	}
	if !strings.Contains(toolResult.Output, "file1.go") {
		t.Fatalf("expected tool result to contain 'file1.go', got %q", toolResult.Output)
	}

	// After tool.execution_complete with model, activeModel should be updated.
	if _, ok := usage["claude-opus-4.6"]; ok {
		// outputTokens from assistant.message came BEFORE tool.execution_complete,
		// so they should be under "copilot", not "claude-opus-4.6".
		t.Log("model attribution is correct: assistant.message tokens go under initial model")
	}
	u := usage["copilot"]
	if u.OutputTokens != 112 {
		t.Fatalf("expected 112 outputTokens under 'copilot', got %d", u.OutputTokens)
	}
}

func TestCopilotEventLoopToolExecError(t *testing.T) {
	t.Parallel()
	lines := []string{fixtureToolExecCompleteError}

	msgs, _, _, _ := simulateCopilotEventLoop(t, lines)

	var found bool
	for _, m := range msgs {
		if m.Type == MessageToolResult && strings.Contains(m.Output, "command not found: foobar") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected tool result with error message")
	}
}

func TestCopilotEventLoopSessionStartCapturesSessionID(t *testing.T) {
	t.Parallel()
	// session.start arrives but the run is killed (timeout/cancel/crash) before
	// the synthetic "result" line is emitted. We must still report the session
	// id from session.start so the chat-session resume pointer can advance.
	lines := []string{fixtureSessionStart}

	_, sessionID, _, _ := simulateCopilotEventLoop(t, lines)

	if sessionID != "35059dc3-d928-4ffb-8616-b78938621d85" {
		t.Fatalf("expected session id captured from session.start, got %q", sessionID)
	}
}

func TestCopilotEventLoopResultOverridesSessionStart(t *testing.T) {
	t.Parallel()
	// When both session.start and result carry a session id, the result event
	// wins (it is the authoritative end-of-turn record).
	lines := []string{
		fixtureSessionStart,
		// Different sessionId on the result event (defensive: in practice
		// they should match, but the contract is "result wins").
		`{"type":"result","sessionId":"final-id","exitCode":0}`,
	}

	_, sessionID, _, _ := simulateCopilotEventLoop(t, lines)

	if sessionID != "final-id" {
		t.Fatalf("expected result session id to win, got %q", sessionID)
	}
}

func TestCopilotEventLoopResultWithoutSessionIDPreservesSessionStart(t *testing.T) {
	t.Parallel()
	// Defensive: if a result line arrives without a sessionId (older CLI,
	// truncated output), the session.start id must not be wiped.
	lines := []string{
		fixtureSessionStart,
		`{"type":"result","exitCode":0}`,
	}

	_, sessionID, _, _ := simulateCopilotEventLoop(t, lines)

	if sessionID != "35059dc3-d928-4ffb-8616-b78938621d85" {
		t.Fatalf("expected session.start id to be preserved when result has none, got %q", sessionID)
	}
}

func TestCopilotEventLoopNonZeroExit(t *testing.T) {
	t.Parallel()
	lines := []string{fixtureResultNonZero}

	_, sessionID, status, _ := simulateCopilotEventLoop(t, lines)

	if status != "failed" {
		t.Fatalf("expected failed, got %q", status)
	}
	if sessionID != "dead-beef" {
		t.Fatalf("unexpected sessionId: %q", sessionID)
	}
}

func TestCopilotEventLoopSessionErrorSurvivesNonZeroResult(t *testing.T) {
	t.Parallel()
	st := newCopilotEventState("copilot", false)

	for _, line := range []string{fixtureSessionError, fixtureResultNonZero} {
		var evt copilotEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Fatal(err)
		}
		handleCopilotEvent(evt, st)
	}

	if st.finalStatus != "failed" {
		t.Fatalf("expected failed, got %q", st.finalStatus)
	}
	if !strings.Contains(st.finalError, "Rate limit exceeded") {
		t.Fatalf("expected session.error detail to survive result event, got %q", st.finalError)
	}
	if !strings.Contains(st.finalError, "copilot exited with code 1") {
		t.Fatalf("expected exit code context, got %q", st.finalError)
	}
}

func TestCopilotEventLoopSessionError(t *testing.T) {
	t.Parallel()
	lines := []string{fixtureSessionError}

	msgs, _, status, _ := simulateCopilotEventLoop(t, lines)

	if status != "failed" {
		t.Fatalf("expected failed, got %q", status)
	}
	var found bool
	for _, m := range msgs {
		if m.Type == MessageLog && m.Level == "error" && m.Content == "Rate limit exceeded" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected error log message")
	}
}

func TestCopilotEventLoopSkipsUnknownTypes(t *testing.T) {
	t.Parallel()
	lines := []string{
		fixtureEphemeral, // session.mcp_servers_loaded — should not produce messages
		`{"type":"session.skills_loaded","data":{},"id":"x","timestamp":"2026-04-16T08:43:34.811Z","parentId":"y","ephemeral":true}`,
		`{"type":"session.tools_updated","data":{"model":"claude-opus-4.6"},"id":"z","timestamp":"2026-04-16T08:43:36.397Z","parentId":"w","ephemeral":true}`,
	}

	msgs, _, _, _ := simulateCopilotEventLoop(t, lines)

	if len(msgs) != 0 {
		t.Fatalf("expected no messages for unknown/ephemeral event types, got %d: %+v", len(msgs), msgs)
	}
}

func TestCopilotEventLoopMultiTurnUsage(t *testing.T) {
	t.Parallel()
	// Simulate: turn 0 has tool use (112 tokens), tool completes with model info,
	// turn 1 has text response (106 tokens) — now under claude-opus-4.6 model.
	lines := []string{
		fixtureTurnStart,
		fixtureAssistantMessageWithTools, // 112 outputTokens, activeModel="copilot"
		fixtureToolExecComplete,          // sets activeModel="claude-opus-4.6"
		`{"type":"assistant.turn_start","data":{"turnId":"1"},"id":"t1","timestamp":"2026-04-16T08:44:01.000Z","parentId":"p1"}`,
		// Turn 1 assistant.message with 106 tokens — should go under "claude-opus-4.6"
		`{"type":"assistant.message","data":{"messageId":"msg-2","content":"Here are the files.","toolRequests":[],"interactionId":"int-1","outputTokens":106},"id":"m2","timestamp":"2026-04-16T08:44:02.000Z","parentId":"t1"}`,
		fixtureResult,
	}

	_, _, _, usage := simulateCopilotEventLoop(t, lines)

	if u := usage["copilot"]; u.OutputTokens != 112 {
		t.Fatalf("expected 112 tokens under 'copilot', got %d", u.OutputTokens)
	}
	if u := usage["claude-opus-4.6"]; u.OutputTokens != 106 {
		t.Fatalf("expected 106 tokens under 'claude-opus-4.6', got %d", u.OutputTokens)
	}
}

// ── Token accounting (MUL-5712) ──

const fixtureAssistantUsage = `{"type":"assistant.usage","data":{"model":"claude-sonnet-4.5","inputTokens":12000,"outputTokens":250,"cacheReadTokens":9000,"cacheWriteTokens":1500,"reasoningTokens":80,"duration":1400},"id":"u-1","timestamp":"2026-08-04T08:00:01.000Z","parentId":"p-1","ephemeral":true}`

const fixtureShutdown = `{"type":"session.shutdown","data":{"shutdownType":"routine","totalPremiumRequests":3,"modelMetrics":{"claude-sonnet-4.5":{"requests":{"count":3,"cost":3},"usage":{"inputTokens":30000,"outputTokens":900,"cacheReadTokens":24000,"cacheWriteTokens":2000,"reasoningTokens":100}}}},"id":"sd-1","timestamp":"2026-08-04T08:00:09.000Z","parentId":"p-1"}`

func TestCopilotAssistantUsageIsAuthoritative(t *testing.T) {
	t.Parallel()
	// assistant.usage carries the full breakdown; assistant.message.outputTokens
	// describes the SAME tokens, so the two must never be summed.
	lines := []string{
		fixtureSessionStart,
		fixtureTurnStart,
		fixtureAssistantUsage,
		fixtureAssistantMessage, // 5 outputTokens — superseded
		fixtureResult,
	}

	_, _, _, usage := simulateCopilotEventLoop(t, lines)

	if len(usage) != 1 {
		t.Fatalf("expected exactly one model entry, got %#v", usage)
	}
	u, ok := usage["claude-sonnet-4.5"]
	if !ok {
		t.Fatalf("expected usage under 'claude-sonnet-4.5', got %#v", usage)
	}
	// inputTokens includes the cached tiers, so uncached input is 12000-9000-1500.
	if u.InputTokens != 1500 {
		t.Fatalf("expected 1500 uncached input tokens, got %d", u.InputTokens)
	}
	if u.OutputTokens != 250 {
		t.Fatalf("expected 250 output tokens (not 255 — message tokens must not be added), got %d", u.OutputTokens)
	}
	if u.CacheReadTokens != 9000 || u.CacheWriteTokens != 1500 {
		t.Fatalf("expected cache 9000/1500, got %d/%d", u.CacheReadTokens, u.CacheWriteTokens)
	}
}

func TestCopilotAssistantUsageNeverGoesNegative(t *testing.T) {
	t.Parallel()
	// Defensive: a CLI that reports inputTokens EXCLUDING cache would otherwise
	// drive uncached input negative.
	lines := []string{
		`{"type":"assistant.usage","data":{"model":"m","inputTokens":100,"outputTokens":10,"cacheReadTokens":9000,"cacheWriteTokens":0},"id":"u","timestamp":"2026-08-04T08:00:01.000Z"}`,
	}

	_, _, _, usage := simulateCopilotEventLoop(t, lines)

	if u := usage["m"]; u.InputTokens != 0 {
		t.Fatalf("expected input tokens clamped to 0, got %d", u.InputTokens)
	}
}

func TestCopilotShutdownUsageFallback(t *testing.T) {
	t.Parallel()
	// No assistant.usage and no outputTokens: the session totals are all we have.
	lines := []string{fixtureSessionStart, fixtureTurnStart, fixtureShutdown, fixtureResult}

	_, _, _, usage := simulateCopilotEventLoop(t, lines)

	u, ok := usage["claude-sonnet-4.5"]
	if !ok {
		t.Fatalf("expected session.shutdown totals to be used, got %#v", usage)
	}
	if u.InputTokens != 4000 || u.OutputTokens != 900 {
		t.Fatalf("expected 4000/900 input/output, got %d/%d", u.InputTokens, u.OutputTokens)
	}
}

func TestCopilotShutdownBeatsMessageOnFreshRun(t *testing.T) {
	t.Parallel()
	// A CLI that still populates assistant.message.outputTokens AND emits the
	// session totals: the totals win, because outputTokens describes only part
	// of the same tokens and taking it would drop input and cache entirely.
	lines := []string{
		fixtureSessionStart,
		fixtureTurnStart,
		fixtureAssistantMessage, // 5 outputTokens, no input/cache
		fixtureShutdown,
		fixtureResult,
	}

	_, _, _, usage := simulateCopilotEventLoop(t, lines)

	u, ok := usage["claude-sonnet-4.5"]
	if !ok {
		t.Fatalf("expected the session totals to win over message outputTokens, got %#v", usage)
	}
	if u.InputTokens != 4000 || u.OutputTokens != 900 {
		t.Fatalf("expected 4000/900 input/output from session.shutdown, got %d/%d", u.InputTokens, u.OutputTokens)
	}
	if u.CacheReadTokens != 24000 || u.CacheWriteTokens != 2000 {
		t.Fatalf("expected cache 24000/2000, got %d/%d", u.CacheReadTokens, u.CacheWriteTokens)
	}
}

func TestCopilotTokenlessUsageEventDoesNotShadow(t *testing.T) {
	t.Parallel()
	// Every token field on assistant.usage is optional upstream. A usage event
	// that names only a model must not mark that source populated and shadow
	// the sources that do carry numbers.
	tokenless := `{"type":"assistant.usage","data":{"model":"claude-sonnet-4.5","duration":900},"id":"u-0","timestamp":"2026-08-04T08:00:01.000Z","ephemeral":true}`

	t.Run("falls through to session totals", func(t *testing.T) {
		t.Parallel()
		lines := []string{fixtureSessionStart, fixtureTurnStart, tokenless, fixtureShutdown, fixtureResult}

		_, _, _, usage := simulateCopilotEventLoop(t, lines)

		if u := usage["claude-sonnet-4.5"]; u.OutputTokens != 900 {
			t.Fatalf("expected session totals to survive a tokenless usage event, got %#v", usage)
		}
	})

	t.Run("falls through to message tokens on resume", func(t *testing.T) {
		t.Parallel()
		// Resumed, so the session totals are off the table and the legacy
		// message tokens are the only thing left.
		lines := []string{fixtureSessionStart, fixtureTurnStart, tokenless, fixtureAssistantMessage, fixtureShutdown, fixtureResult}

		_, st := runCopilotEventLoop(t, lines, "copilot", true)

		usage := st.resolveUsage()
		if u := usage["claude-sonnet-4.5"]; u.OutputTokens != 5 {
			t.Fatalf("expected the 5 message outputTokens to survive, got %#v", usage)
		}
	})
}

func TestCopilotShutdownUsageSkippedOnResume(t *testing.T) {
	t.Parallel()
	// session.shutdown totals span the WHOLE session, and the CLI restores its
	// accumulators on resume — reporting them on a follow-up turn would bill
	// every earlier turn again. Reporting nothing is the lesser error.
	lines := []string{fixtureSessionStart, fixtureTurnStart, fixtureShutdown, fixtureResult}

	_, st := runCopilotEventLoop(t, lines, "copilot", true)

	if usage := st.resolveUsage(); len(usage) != 0 {
		t.Fatalf("expected no usage on a resumed run, got %#v", usage)
	}
}

func TestCopilotResumeStillReportsPerCallUsage(t *testing.T) {
	t.Parallel()
	// assistant.usage is per model call, so a resumed run reports it normally.
	lines := []string{fixtureSessionStart, fixtureTurnStart, fixtureAssistantUsage, fixtureShutdown, fixtureResult}

	_, st := runCopilotEventLoop(t, lines, "copilot", true)

	usage := st.resolveUsage()
	if len(usage) != 1 {
		t.Fatalf("expected per-call usage on a resumed run, got %#v", usage)
	}
	if u := usage["claude-sonnet-4.5"]; u.OutputTokens != 250 {
		t.Fatalf("expected the per-call figure (250), not the session total (900), got %d", u.OutputTokens)
	}
}

func TestCopilotAssistantMessageModelAttribution(t *testing.T) {
	t.Parallel()
	// assistant.message names its own model. Before this the first turn's tokens
	// landed under the seed model — the unpriceable literal "copilot".
	lines := []string{
		fixtureTurnStart,
		`{"type":"assistant.message","data":{"messageId":"m1","model":"claude-opus-4.6","content":"hi","toolRequests":[],"outputTokens":42},"id":"e1","timestamp":"2026-08-04T08:00:02.000Z"}`,
		fixtureResult,
	}

	_, _, _, usage := simulateCopilotEventLoop(t, lines)

	if _, ok := usage["copilot"]; ok {
		t.Fatalf("tokens must not be filed under the seed model when the message names one: %#v", usage)
	}
	if u := usage["claude-opus-4.6"]; u.OutputTokens != 42 {
		t.Fatalf("expected 42 output tokens under 'claude-opus-4.6', got %#v", usage)
	}
}

func TestCopilotModernStreamReportsNoUsage(t *testing.T) {
	t.Parallel()
	// Copilot CLI 1.0.77 shape: assistant.message without outputTokens or model,
	// tool.execution_complete without model, and no assistant.usage /
	// session.shutdown (the CLI filters both off stdout). Nothing to report —
	// this documents the upstream gap so a future CLI change shows up as a
	// failing test rather than a silent one.
	lines := []string{
		`{"type":"session.start","data":{"sessionId":"s1","version":1,"producer":"copilot-agent"},"id":"e1","timestamp":"2026-08-04T08:00:00.000Z"}`,
		`{"type":"assistant.turn_start","data":{"turnId":"t1"},"id":"e2","timestamp":"2026-08-04T08:00:01.000Z"}`,
		`{"type":"assistant.message","data":{"messageId":"m1","content":"","toolRequests":[{"toolCallId":"c1","name":"bash","arguments":{"command":"ls"}}]},"id":"e3","timestamp":"2026-08-04T08:00:02.000Z"}`,
		`{"type":"tool.execution_complete","data":{"toolCallId":"c1","success":true,"turnId":"t1","result":{"content":"a.go\n"}},"id":"e4","timestamp":"2026-08-04T08:00:03.000Z"}`,
		`{"type":"assistant.message","data":{"messageId":"m2","content":"Done."},"id":"e5","timestamp":"2026-08-04T08:00:04.000Z"}`,
		fixtureResult,
	}

	_, _, _, usage := simulateCopilotEventLoop(t, lines)

	if len(usage) != 0 {
		t.Fatalf("expected no usage from a 1.0.77-shaped stream, got %#v", usage)
	}
}

func TestCopilotEventLoopSessionStartSetsModel(t *testing.T) {
	t.Parallel()
	lines := []string{
		fixtureSessionStart,
		fixtureTurnStart,
		fixtureAssistantMessage, // 5 outputTokens
		fixtureResult,
	}

	_, _, _, usage := simulateCopilotEventLoop(t, lines)

	// session.start sets selectedModel to "claude-sonnet-4",
	// so tokens should be attributed there, not "copilot".
	if _, ok := usage["copilot"]; ok {
		t.Fatal("expected no tokens under 'copilot' when session.start provides selectedModel")
	}
	u, ok := usage["claude-sonnet-4"]
	if !ok {
		t.Fatal("expected tokens under 'claude-sonnet-4'")
	}
	if u.OutputTokens != 5 {
		t.Fatalf("expected 5 outputTokens, got %d", u.OutputTokens)
	}
}

func TestCopilotEventLoopSeedModelFromOpts(t *testing.T) {
	t.Parallel()
	// No session.start — seed model comes from opts.Model (simulated via seedModel param).
	lines := []string{
		fixtureTurnStart,
		fixtureAssistantMessage, // 5 outputTokens
		fixtureResult,
	}

	_, _, _, usage := simulateCopilotEventLoopWithModel(t, lines, "gpt-4o")

	u, ok := usage["gpt-4o"]
	if !ok {
		t.Fatal("expected tokens under 'gpt-4o'")
	}
	if u.OutputTokens != 5 {
		t.Fatalf("expected 5 outputTokens, got %d", u.OutputTokens)
	}
}

func TestCopilotEventLoopReasoning(t *testing.T) {
	t.Parallel()
	lines := []string{
		fixtureReasoning,
		fixtureReasoningDelta,
	}

	msgs, _, _, _ := simulateCopilotEventLoop(t, lines)

	var thinking []string
	for _, m := range msgs {
		if m.Type == MessageThinking {
			thinking = append(thinking, m.Content)
		}
	}
	if len(thinking) != 2 {
		t.Fatalf("expected 2 thinking messages, got %d: %v", len(thinking), thinking)
	}
	if thinking[0] != "Let me think about this..." {
		t.Fatalf("unexpected reasoning content: %q", thinking[0])
	}
	if thinking[1] != "thinking step" {
		t.Fatalf("unexpected reasoning_delta content: %q", thinking[1])
	}
}

func TestCopilotEventLoopReasoningTextInMessage(t *testing.T) {
	t.Parallel()
	// assistant.message with reasoningText field set.
	lines := []string{
		`{"type":"assistant.message","data":{"messageId":"msg-r","content":"answer","toolRequests":[],"interactionId":"int-r","outputTokens":10,"reasoningText":"I thought carefully"},"id":"mr","timestamp":"2026-04-16T08:44:00.000Z","parentId":"p-1"}`,
	}

	msgs, _, _, _ := simulateCopilotEventLoop(t, lines)

	var gotThinking bool
	for _, m := range msgs {
		if m.Type == MessageThinking && m.Content == "I thought carefully" {
			gotThinking = true
		}
	}
	if !gotThinking {
		t.Fatal("expected MessageThinking from reasoningText in assistant.message")
	}
}

func TestCopilotEventLoopSessionWarning(t *testing.T) {
	t.Parallel()
	lines := []string{fixtureSessionWarning}

	msgs, _, status, _ := simulateCopilotEventLoop(t, lines)

	// Warnings should NOT change finalStatus.
	if status != "completed" {
		t.Fatalf("expected completed, got %q", status)
	}
	var found bool
	for _, m := range msgs {
		if m.Type == MessageLog && m.Level == "warn" && m.Content == "You are approaching your rate limit" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected warn log message for session.warning")
	}
}

func TestCopilotEventLoopDeltaFallbackOutput(t *testing.T) {
	t.Parallel()
	// Only deltas, no assistant.message — simulates process killed mid-stream.
	lines := []string{
		`{"type":"assistant.message_delta","data":{"messageId":"m1","deltaContent":"hello "},"id":"d1","timestamp":"2026-04-16T08:43:38.000Z","parentId":"p1","ephemeral":true}`,
		`{"type":"assistant.message_delta","data":{"messageId":"m1","deltaContent":"world"},"id":"d2","timestamp":"2026-04-16T08:43:38.100Z","parentId":"p1","ephemeral":true}`,
	}

	st := newCopilotEventState("copilot", false)
	for _, line := range lines {
		var evt copilotEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Fatal(err)
		}
		handleCopilotEvent(evt, st)
	}

	if st.finalOutput() != "hello world" {
		t.Fatalf("expected output 'hello world', got %q", st.finalOutput())
	}
}

// TestCopilotEventLoopDeliversFinalTurnOnly pins Result.Output to the LAST
// assistant turn. Copilot used to join every turn with "\n\n", so a run that
// narrated, called a tool, then answered shipped narration + answer to Slack and
// Lark as one reply (GH #6006). The narration still reaches the transcript as
// MessageText; only the deliverable narrows.
func TestCopilotEventLoopDeliversFinalTurnOnly(t *testing.T) {
	t.Parallel()
	lines := []string{
		`{"type":"assistant.message_delta","data":{"messageId":"m1","deltaContent":"Let me check the logs."},"id":"d1","timestamp":"2026-04-16T08:43:38.000Z","parentId":"p1","ephemeral":true}`,
		`{"type":"assistant.message","data":{"messageId":"m1","content":"Let me check the logs.","toolRequests":[{"toolCallId":"t1","name":"shell","arguments":{}}]},"id":"a1","timestamp":"2026-04-16T08:43:38.100Z","parentId":"p1"}`,
		`{"type":"tool.execution_complete","data":{"toolCallId":"t1","success":true,"result":{"content":"nothing there"}},"id":"tc1","timestamp":"2026-04-16T08:43:39.000Z","parentId":"p1"}`,
		`{"type":"assistant.message_delta","data":{"messageId":"m2","deltaContent":"The retry loop is the cause."},"id":"d2","timestamp":"2026-04-16T08:43:40.000Z","parentId":"p1","ephemeral":true}`,
		`{"type":"assistant.message","data":{"messageId":"m2","content":"The retry loop is the cause."},"id":"a2","timestamp":"2026-04-16T08:43:40.100Z","parentId":"p1"}`,
	}

	st := newCopilotEventState("copilot", false)
	var streamed []string
	for _, line := range lines {
		var evt copilotEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Fatal(err)
		}
		for _, m := range handleCopilotEvent(evt, st) {
			if m.Type == MessageText {
				streamed = append(streamed, m.Content)
			}
		}
	}

	if got := st.finalOutput(); got != "The retry loop is the cause." {
		t.Fatalf("Result.Output should be the final turn only, got %q", got)
	}
	// The transcript must still see the narration — this fix narrows delivery,
	// not the timeline the Multica UI renders.
	if len(streamed) != 2 || streamed[0] != "Let me check the logs." {
		t.Fatalf("expected both turns streamed as MessageText, got %q", streamed)
	}
}

// TestCopilotEventLoopToolOnlyTurnClearsPendingDelta covers the turn shape the
// delta fallback can get wrong: a turn whose authoritative assistant.message
// reports content:"" because the tool requests ARE the turn. Its deltas must not
// stay buffered, or a later turn that dies mid-stream would deliver the two
// stitched together.
func TestCopilotEventLoopToolOnlyTurnClearsPendingDelta(t *testing.T) {
	t.Parallel()
	lines := []string{
		`{"type":"assistant.message_delta","data":{"messageId":"m1","deltaContent":"Checking the logs now."},"id":"d1","timestamp":"2026-04-16T08:43:38.000Z","parentId":"p1","ephemeral":true}`,
		`{"type":"assistant.message","data":{"messageId":"m1","content":"","toolRequests":[{"toolCallId":"t1","name":"shell","arguments":{}}]},"id":"a1","timestamp":"2026-04-16T08:43:38.100Z","parentId":"p1"}`,
		// Next turn starts streaming, then the process dies before its
		// assistant.message lands.
		`{"type":"assistant.message_delta","data":{"messageId":"m2","deltaContent":"The retry loop"},"id":"d2","timestamp":"2026-04-16T08:43:40.000Z","parentId":"p1","ephemeral":true}`,
	}

	st := newCopilotEventState("copilot", false)
	for _, line := range lines {
		var evt copilotEvent
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Fatal(err)
		}
		handleCopilotEvent(evt, st)
	}

	if got := st.finalOutput(); got != "The retry loop" {
		t.Fatalf("killed mid-turn should deliver that turn's partial only, got %q", got)
	}
}

func TestCopilotExecuteSurfacesStderrOnNonZeroResult(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "copilot")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '{\"type\":\"result\",\"sessionId\":\"sess-fail\",\"exitCode\":1}'\n" +
		"echo \"error: authentication failed: refresh token expired\" >&2\n" +
		"exit 1\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("copilot", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new copilot backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "failed" {
			t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Error, "copilot exited with code 1") {
			t.Fatalf("expected error to mention exit code, got %q", result.Error)
		}
		if !strings.Contains(result.Error, "refresh token expired") {
			t.Fatalf("expected error to include stderr hint, got %q", result.Error)
		}
		if !strings.Contains(result.Error, "copilot stderr:") {
			t.Fatalf("expected stderr label in error, got %q", result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// ── Arg builder tests ──

func TestBuildCopilotArgsBaseline(t *testing.T) {
	t.Parallel()

	args := buildCopilotArgs("write a haiku", ExecOptions{}, slog.Default())
	expected := []string{
		"-p", "write a haiku",
		"--output-format", "json",
		"--allow-all",
		"--no-ask-user",
	}

	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Fatalf("expected args[%d] = %q, got %q", i, want, args[i])
		}
	}
}

func TestBuildCopilotArgsWithModel(t *testing.T) {
	t.Parallel()

	args := buildCopilotArgs("hi", ExecOptions{Model: "gpt-4o"}, slog.Default())

	var foundModel bool
	for i, a := range args {
		if a == "--model" {
			if i+1 >= len(args) || args[i+1] != "gpt-4o" {
				t.Fatalf("expected --model followed by gpt-4o, got %v", args)
			}
			foundModel = true
			break
		}
	}
	if !foundModel {
		t.Fatalf("expected --model flag when Model is set, got args=%v", args)
	}
}

func TestBuildCopilotArgsWithResume(t *testing.T) {
	t.Parallel()

	args := buildCopilotArgs("hi", ExecOptions{ResumeSessionID: "sess-42"}, slog.Default())

	var foundResume bool
	for i, a := range args {
		if a == "--resume" {
			if i+1 >= len(args) || args[i+1] != "sess-42" {
				t.Fatalf("expected --resume followed by session id, got %v", args)
			}
			foundResume = true
			break
		}
	}
	if !foundResume {
		t.Fatalf("expected --resume flag when ResumeSessionID is set, got args=%v", args)
	}
}

func TestBuildCopilotArgsOmitsOptionalWhenEmpty(t *testing.T) {
	t.Parallel()

	args := buildCopilotArgs("hi", ExecOptions{}, slog.Default())
	for _, a := range args {
		if a == "--model" {
			t.Fatalf("expected no --model flag when Model is empty, got args=%v", args)
		}
		if a == "--resume" {
			t.Fatalf("expected no --resume flag when ResumeSessionID is empty, got args=%v", args)
		}
	}
}

func TestBuildCopilotArgsPassesThroughCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildCopilotArgs("hi", ExecOptions{
		CustomArgs: []string{"--max-turns", "50"},
	}, slog.Default())

	if args[len(args)-2] != "--max-turns" || args[len(args)-1] != "50" {
		t.Fatalf("expected --max-turns 50 at end of args, got %v", args)
	}
}

func TestBuildCopilotArgsFiltersBlockedCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildCopilotArgs("hi", ExecOptions{
		CustomArgs: []string{"--output-format", "text", "--max-turns", "50"},
	}, slog.Default())

	for i, a := range args {
		if a == "--output-format" && i+1 < len(args) && args[i+1] == "text" {
			t.Fatalf("blocked --output-format text should have been filtered: %v", args)
		}
	}
	found := false
	for i, a := range args {
		if a == "--max-turns" && i+1 < len(args) && args[i+1] == "50" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected --max-turns 50 to pass through, got %v", args)
	}
}

func TestBuildCopilotArgsBlocksResumeAndACP(t *testing.T) {
	t.Parallel()

	args := buildCopilotArgs("hi", ExecOptions{
		CustomArgs: []string{"--resume", "bad-session", "--acp", "--yolo"},
	}, slog.Default())

	for _, a := range args {
		if a == "bad-session" {
			t.Fatalf("blocked --resume value should have been filtered: %v", args)
		}
		if a == "--acp" {
			t.Fatalf("blocked --acp should have been filtered: %v", args)
		}
		if a == "--yolo" {
			t.Fatalf("blocked --yolo should have been filtered: %v", args)
		}
	}
}
