package claude

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func feed(t *testing.T, n *Normalizer, messages ...string) []Event {
	t.Helper()
	var out []Event
	for _, m := range messages {
		if !json.Valid([]byte(m)) {
			t.Fatalf("fixture is not JSON: %s", m)
		}
		out = append(out, n.Accept(json.RawMessage(m))...)
	}
	return out
}

// summary renders events compactly: type/item/final plus key payload fields.
func summary(events []Event) []string {
	out := make([]string, len(events))
	for i, e := range events {
		s := fmt.Sprintf("%s %s", e.Type, e.ItemID)
		if e.Final {
			s += " final"
		}
		for _, k := range []string{"kind", "status", "text", "output", "command", "tool"} {
			if v, ok := e.Payload[k]; ok {
				s += fmt.Sprintf(" %s=%v", k, v)
			}
		}
		out[i] = s
	}
	return out
}

func assertEvents(t *testing.T, got []Event, want ...string) {
	t.Helper()
	g := summary(got)
	if strings.Join(g, "\n") != strings.Join(want, "\n") {
		t.Fatalf("events:\n%s\nwant:\n%s", strings.Join(g, "\n"), strings.Join(want, "\n"))
	}
}

func TestNormalizerStreamsTextAndThinkingThenFinalizesFromCompleteBlocks(t *testing.T) {
	t.Parallel()

	n := NewNormalizer("multica")
	got := feed(t, n,
		`{"type":"system","subtype":"init","session_id":"s-1"}`,
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"m1"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"plan"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"text"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"测试"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"通过"}}}`,
		// Claude Code emits one complete assistant message per block.
		`{"type":"assistant","message":{"id":"m1","content":[{"type":"thinking","thinking":"plan it"}]}}`,
		`{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"测试通过。"}]}}`,
	)
	assertEvents(t, got,
		"item.started m1:0 kind=reasoning",
		"item.delta m1:0 kind=reasoning text=plan",
		"item.started m1:1 kind=assistant_text",
		"item.delta m1:1 kind=assistant_text text=测试",
		"item.delta m1:1 kind=assistant_text text=通过",
		// The complete block is authoritative, even where deltas differ.
		"item.completed m1:0 final kind=reasoning status=completed text=plan it",
		"item.completed m1:1 final kind=assistant_text status=completed text=测试通过。",
	)
	if n.SessionID() != "s-1" {
		t.Fatalf("session = %q", n.SessionID())
	}
	if closed := n.Close("cancelled"); len(closed) != 0 {
		t.Fatalf("finished items reopened by Close: %v", summary(closed))
	}
}

func TestNormalizerWithoutPartialMessagesEmitsFinalsOnly(t *testing.T) {
	t.Parallel()

	got := feed(t, NewNormalizer("multica"),
		`{"type":"assistant","message":{"id":"m2","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}}`)
	assertEvents(t, got,
		"item.completed m2:0 final kind=assistant_text status=completed text=a",
		"item.completed m2:1 final kind=assistant_text status=completed text=b",
	)
}

func TestNormalizerToolCallsFinishWithTheirResult(t *testing.T) {
	t.Parallel()

	n := NewNormalizer("multica")
	got := feed(t, n,
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"m3"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tu-1"}}}`,
		`{"type":"assistant","message":{"id":"m3","content":[{"type":"tool_use","id":"tu-1","name":"Bash","input":{"command":"pnpm test"}}]}}`,
		`{"type":"assistant","message":{"id":"m3","content":[{"type":"tool_use","id":"tu-2","name":"mcp__multica__search_issues","input":{"query":"crash"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu-1","content":"PASS 42"}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu-2","content":"timeout","is_error":true}]}}`,
		// A repeated result never produces a second final event.
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu-1","content":"again"}]}}`,
	)
	assertEvents(t, got,
		"item.started tu-1 kind=command command=pnpm test",
		"item.started tu-2 kind=tool_call tool=mcp__multica__search_issues",
		"item.completed tu-1 final kind=command status=completed output=PASS 42 command=pnpm test",
		"item.completed tu-2 final kind=tool_call status=failed output=timeout tool=mcp__multica__search_issues",
	)
	if errPayload := got[3].Payload["error"].(map[string]any); errPayload["kind"] != "tool_error" {
		t.Fatalf("error payload = %v", errPayload)
	}
}

func TestNormalizerSeparatesInlineImagesFromToolOutput(t *testing.T) {
	t.Parallel()

	got := feed(t, NewNormalizer("multica"),
		`{"type":"assistant","message":{"id":"m4","content":[{"type":"tool_use","id":"tu-3","name":"Screenshot","input":{}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu-3","content":[{"type":"text","text":"shot"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo="}},{"type":"image","source":{"type":"url","media_type":"image/png","data":"https://example.com/x.png"}}]}]}}`,
	)
	assertEvents(t, got,
		"item.started tu-3 kind=tool_call tool=Screenshot",
		`item.completed tu-3 final kind=tool_call status=completed output=[{"type":"text","text":"shot"},{"type":"image","source":{"type":"url","media_type":"image/png","data":"https://example.com/x.png"}}] tool=Screenshot`,
		"item.completed output-image:tu-3:0 final kind=image status=completed",
	)
	if img := got[2].Image; img == nil || img.MimeType != "image/png" || img.Data != "iVBORw0KGgo=" {
		t.Fatalf("image = %+v", img)
	}
}

func TestNormalizerHidesAskUserQuestion(t *testing.T) {
	t.Parallel()

	got := feed(t, NewNormalizer("multica"),
		`{"type":"assistant","message":{"id":"m5","content":[{"type":"tool_use","id":"q-1","name":"mcp__multica__ask_user_question","input":{}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"q-1","content":"yes"}]}}`,
	)
	if len(got) != 0 {
		t.Fatalf("events = %v", summary(got))
	}
}

func TestNormalizerCloseFinalizesOpenItemsWithPartialContent(t *testing.T) {
	t.Parallel()

	n := NewNormalizer("multica")
	feed(t, n,
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"m6"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"half"}}}`,
		`{"type":"assistant","message":{"id":"m7","content":[{"type":"tool_use","id":"tu-4","name":"Bash","input":{"command":"sleep 9"}}]}}`,
	)
	assertEvents(t, n.Close("cancelled"),
		"item.completed m6:0 final kind=assistant_text status=cancelled text=half",
		"item.completed tu-4 final kind=command status=cancelled output= command=sleep 9",
	)
	if again := n.Close("cancelled"); len(again) != 0 {
		t.Fatalf("second Close = %v", summary(again))
	}
}

func TestNormalizerReportsTurnResult(t *testing.T) {
	t.Parallel()

	n := NewNormalizer("multica")
	feed(t, n, `{"type":"result","subtype":"success","is_error":false,"result":"done"}`)
	if r := n.TakeResult(); r == nil || r.IsError || r.Output != "done" {
		t.Fatalf("result = %+v", r)
	}
	if n.TakeResult() != nil {
		t.Fatal("result not cleared")
	}
	feed(t, n, `{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["rate limited","retry later"]}`)
	if r := n.TakeResult(); r == nil || !r.IsError || r.Error != "rate limited\nretry later" {
		t.Fatalf("error result = %+v", r)
	}
}

func TestNormalizerBackgroundTaskLifecycle(t *testing.T) {
	t.Parallel()

	n := NewNormalizer("multica")
	got := feed(t, n,
		`{"type":"system","subtype":"task_started","task_id":"t1","description":"watch build"}`,
		`{"type":"system","subtype":"task_progress","task_id":"t1","summary":"compiling","usage":{"total_tokens":10,"tool_uses":1,"duration_ms":500}}`,
		`{"type":"system","subtype":"task_updated","task_id":"t1","patch":{"status":"killed","error":"stopped by user"}}`,
		// Late notifications after the final event are ignored.
		`{"type":"system","subtype":"task_notification","task_id":"t1","status":"completed"}`,
	)
	assertEvents(t, got,
		"item.started task:t1 kind=background_task",
		"item.updated task:t1 kind=background_task status=in_progress",
		"item.completed task:t1 final kind=background_task status=cancelled",
	)
	if got[2].Payload["summary"] != "stopped by user" || got[1].Payload["progress_summary"] != "compiling" {
		t.Fatalf("payloads = %v / %v", got[1].Payload, got[2].Payload)
	}
	if usage := got[1].Payload["usage"].(map[string]any); usage["total_tokens"] != int64(10) {
		t.Fatalf("usage = %v", usage)
	}
}

func TestNormalizerCompletesBackgroundTasksMissingFromSnapshot(t *testing.T) {
	t.Parallel()

	n := NewNormalizer("multica")
	feed(t, n,
		`{"type":"system","subtype":"task_started","task_id":"t1","description":"a"}`,
		`{"type":"system","subtype":"task_started","task_id":"t2","description":"b"}`,
		`{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"t1"},{"task_id":"t2"}]}`,
	)
	got := feed(t, n, `{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"t2"}]}`)
	assertEvents(t, got, "item.completed task:t1 final kind=background_task status=completed")
	// Background tasks outlive the turn; Close leaves them open.
	if closed := n.Close("cancelled"); len(closed) != 0 {
		t.Fatalf("Close touched background tasks: %v", summary(closed))
	}
}

func TestNormalizerIgnoresMalformedMessages(t *testing.T) {
	t.Parallel()

	n := NewNormalizer("multica")
	if got := n.Accept(json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_delta","index":5,"delta":{"type":"text_delta","text":"x"}}}`)); len(got) != 0 {
		t.Fatalf("delta for unknown block = %v", summary(got))
	}
	if got := n.Accept(json.RawMessage(`[1]`)); got != nil {
		t.Fatalf("non-object message = %v", got)
	}
}
