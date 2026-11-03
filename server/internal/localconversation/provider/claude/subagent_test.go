package claude

import (
	"strings"
	"testing"
)

func TestSubagentEventsAreIsolatedFromTheRootAndEachOther(t *testing.T) {
	n := NewNormalizer("multica")
	feed(t, n, `{"type":"system","subtype":"init","session_id":"root"}`)
	a := feed(t, n,
		`{"type":"assistant","parent_tool_use_id":"agent-a","session_id":"child-a","message":{"id":"same-message","content":[{"type":"thinking","thinking":"private"},{"type":"text","text":"Reviewing"},{"type":"tool_use","id":"same-call","name":"Bash","input":{"command":"git diff"}}]}}`,
	)
	b := feed(t, n, `{"type":"assistant","parent_tool_use_id":"agent-b","message":{"id":"same-message","content":[{"type":"tool_use","id":"same-call","name":"Read","input":{"file_path":"README.md"}}]}}`)
	if len(a) != 2 || len(b) != 1 {
		t.Fatalf("missing public child events: a=%v b=%v", summary(a), summary(b))
	}
	if a[1].ItemID == b[0].ItemID || a[1].Payload["parent_tool_use_id"] != "agent-a" || b[0].Payload["parent_tool_use_id"] != "agent-b" {
		t.Fatalf("child identity collision: a=%v b=%v", a, b)
	}
	result := feed(t, n, `{"type":"user","parent_tool_use_id":"agent-b","message":{"content":[{"type":"tool_result","tool_use_id":"same-call","content":"missing file","is_error":true}]}}`)
	if len(result) != 1 || result[0].ItemID != b[0].ItemID || result[0].Payload["status"] != "failed" {
		t.Fatalf("child result = %v", result)
	}
	feed(t, n, `{"type":"result","parent_tool_use_id":"agent-b","is_error":true,"result":"child failed"}`)
	if n.TakeResult() != nil || n.SessionID() != "root" {
		t.Fatal("child changed root result or session")
	}
	for _, e := range append(a, result...) {
		if strings.Contains(strings.Join(summary([]Event{e}), ""), "private") {
			t.Fatal("private reasoning leaked")
		}
	}
	if got := n.Close("cancelled"); len(got) != 0 {
		t.Fatalf("foreground completion closed a live child: %v", summary(got))
	}
}

func TestTaskCompletionClosesOnlyItsChildAndKeepsParentAssociation(t *testing.T) {
	n := NewNormalizer("multica")
	started := feed(t, n, `{"type":"system","subtype":"task_started","task_id":"task-a","tool_use_id":"agent-a","task_type":"local_agent","description":"Review"}`)
	if len(started) != 1 || started[0].Payload["tool_use_id"] != "agent-a" {
		t.Fatalf("task lost spawning call: %v", started)
	}
	feed(t, n, `{"type":"assistant","parent_tool_use_id":"agent-a","message":{"id":"m","content":[{"type":"tool_use","id":"call","name":"Read","input":{}}]}}`)
	got := feed(t, n, `{"type":"system","subtype":"task_notification","task_id":"task-a","status":"stopped","summary":"Stopped"}`)
	if len(got) != 2 || !got[0].Final || got[0].Payload["parent_tool_use_id"] != "agent-a" || got[0].Payload["status"] != "cancelled" || got[1].Payload["status"] != "cancelled" {
		t.Fatalf("task completion = %v", got)
	}
}

func TestSubagentStreamsKeepIndependentIndicesAndStableItemOrder(t *testing.T) {
	n := NewNormalizer("multica")
	got := feed(t, n,
		`{"type":"stream_event","parent_tool_use_id":"a","event":{"type":"message_start","message":{"id":"same"}}}`,
		`{"type":"stream_event","parent_tool_use_id":"a","event":{"type":"content_block_start","index":0,"content_block":{"type":"text"}}}`,
		`{"type":"stream_event","parent_tool_use_id":"b","event":{"type":"message_start","message":{"id":"same"}}}`,
		`{"type":"stream_event","parent_tool_use_id":"b","event":{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}}`,
		`{"type":"stream_event","parent_tool_use_id":"b","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"private"}}}`,
		`{"type":"stream_event","parent_tool_use_id":"a","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Public progress"}}}`,
		`{"type":"assistant","parent_tool_use_id":"a","message":{"id":"tools","content":[{"type":"tool_use","id":"read","name":"Read","input":{}}]}}`,
		`{"type":"assistant","parent_tool_use_id":"a","message":{"id":"same","content":[{"type":"text","text":"Public progress"}]}}`,
	)
	if len(got) != 4 || got[0].ItemID != got[1].ItemID || got[0].ItemID != got[3].ItemID {
		t.Fatalf("interleaved streams = %v", got)
	}
	for i, e := range got {
		if e.Payload["kind"] != "subagent" || e.Payload["parent_tool_use_id"] != "a" {
			t.Fatalf("private or root event at %d: %v", i, e)
		}
	}
	if got[2].Payload["item_order"] != 1 || got[3].Payload["item_order"] != 0 {
		t.Fatalf("completion changed item order: %v", got)
	}
}
