package claude

import "testing"

func TestCompactionLifecycle(t *testing.T) {
	t.Parallel()
	n := NewNormalizer("multica")
	start := `{"type":"system","subtype":"status","status":"compacting","uuid":"start-1"}`
	boundary := `{"type":"system","subtype":"compact_boundary","uuid":"boundary-1","compact_metadata":{"trigger":"auto","pre_tokens":2000}}`
	assertEvents(t, feed(t, n, start, start), "item.started compaction:start-1 kind=compaction")
	// Status reset alone does not confirm success; compact_boundary does.
	assertEvents(t, feed(t, n, `{"type":"system","subtype":"status","status":null}`))
	assertEvents(t, feed(t, n, boundary, boundary), "item.completed compaction:start-1 final kind=compaction status=completed")
	assertEvents(t, n.Close("cancelled"))
	assertEvents(t, feed(t, n,
		`{"type":"system","subtype":"status","status":"compacting","uuid":"start-2"}`,
		`{"type":"system","subtype":"compact_boundary","uuid":"boundary-2","compact_metadata":{"trigger":"manual","pre_tokens":1000}}`),
		"item.started compaction:start-2 kind=compaction",
		"item.completed compaction:start-2 final kind=compaction status=completed")
}

func TestCompactionBoundaryWithoutStart(t *testing.T) {
	t.Parallel()
	n := NewNormalizer("multica")
	assertEvents(t, feed(t, n, `{"type":"system","subtype":"compact_boundary","uuid":"boundary-1"}`),
		"item.completed compaction:boundary-1 final kind=compaction status=completed")
}

func TestCompactionFailed(t *testing.T) {
	t.Parallel()
	n := NewNormalizer("multica")
	feed(t, n, `{"type":"system","subtype":"status","status":"compacting","uuid":"start-1"}`)
	events := feed(t, n, `{"type":"system","subtype":"status","status":null,"compact_result":"failed","compact_error":"exhausted","uuid":"failed-1"}`)
	assertEvents(t, events, "item.completed compaction:start-1 final kind=compaction status=failed")
	if got := events[0].Payload["error"].(map[string]any)["message"]; got != "exhausted" {
		t.Fatalf("compaction error = %v", got)
	}
	assertEvents(t, n.Close("failed"))
}

func TestCompactionInterrupted(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"cancelled", "failed"} {
		t.Run(status, func(t *testing.T) {
			n := NewNormalizer("multica")
			feed(t, n, `{"type":"system","subtype":"status","status":"compacting","uuid":"start-1"}`)
			assertEvents(t, n.Close(status), "item.completed compaction:start-1 final kind=compaction status="+status)
			assertEvents(t, n.Close(status))
			// A later turn's compaction must get a new item.
			assertEvents(t, feed(t, n, `{"type":"system","subtype":"compact_boundary","uuid":"boundary-2"}`),
				"item.completed compaction:boundary-2 final kind=compaction status=completed")
		})
	}
}

func TestCompactionIgnoresSubagentAndUnrelatedStatus(t *testing.T) {
	t.Parallel()
	n := NewNormalizer("multica")
	assertEvents(t, feed(t, n,
		`{"type":"system","subtype":"status","status":"requesting","uuid":"requesting"}`,
		`{"type":"system","subtype":"status","status":"compacting","uuid":"sub-start","parent_tool_use_id":"tool-1"}`,
		`{"type":"system","subtype":"compact_boundary","uuid":"sub-end","parent_tool_use_id":"tool-1"}`))
}
