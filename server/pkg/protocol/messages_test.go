package protocol

import (
	"encoding/json"
	"testing"
)

// TestDaemonHeartbeatAckQueuedTasksRoundTrip pins the NEX-38 AC-8 contract for
// the unified queued_tasks field: the server writes it only while a runtime is
// draining, the daemon reads the same JSON key back, and a non-draining ack
// omits it (nil) so the daemon keeps its previous cached value.
func TestDaemonHeartbeatAckQueuedTasksRoundTrip(t *testing.T) {
	// Draining ack: queued_tasks present.
	queued := 7
	draining := DaemonHeartbeatAckPayload{RuntimeID: "rt-1", QueuedTasks: &queued}
	data, err := json.Marshal(draining)
	if err != nil {
		t.Fatalf("marshal draining ack: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal draining ack: %v", err)
	}
	if _, ok := got["queued_tasks"]; !ok {
		t.Fatalf("draining ack JSON = %s, want queued_tasks present", data)
	}
	if v := got["queued_tasks"].(float64); v != 7 {
		t.Fatalf("queued_tasks = %v, want 7", v)
	}

	var round DaemonHeartbeatAckPayload
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal draining ack into struct: %v", err)
	}
	if round.QueuedTasks == nil || *round.QueuedTasks != 7 {
		t.Fatalf("round-trip QueuedTasks = %v, want 7", round.QueuedTasks)
	}

	// Non-draining ack: field omitted (nil), not "0".
	idle := DaemonHeartbeatAckPayload{RuntimeID: "rt-1"}
	data2, err := json.Marshal(idle)
	if err != nil {
		t.Fatalf("marshal idle ack: %v", err)
	}
	if got2 := string(data2); got2 != `{"runtime_id":"rt-1","status":""}` {
		t.Fatalf("idle ack JSON = %s, want queued_tasks omitted", got2)
	}
	var roundIdle DaemonHeartbeatAckPayload
	if err := json.Unmarshal(data2, &roundIdle); err != nil {
		t.Fatalf("unmarshal idle ack: %v", err)
	}
	if roundIdle.QueuedTasks != nil {
		t.Fatalf("idle ack QueuedTasks = %v, want nil", roundIdle.QueuedTasks)
	}
}
