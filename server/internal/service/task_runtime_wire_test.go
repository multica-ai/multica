package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestRuntimePoolTaskEventCarriesPlacementState(t *testing.T) {
	task := db.AgentTaskQueue{
		RuntimeBindingMode:   "pool",
		Status:               "waiting_runtime",
		WaitReason:           pgtype.Text{String: "no_eligible_runtime", Valid: true},
		SessionAffinityState: "none",
	}
	event := taskEvent(protocol.EventTaskWaitingRuntime, "workspace-1", task)
	payload, ok := event.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", event.Payload)
	}
	for key, want := range map[string]any{
		"runtime_id":             "",
		"runtime_binding_mode":   "pool",
		"status":                 "waiting_runtime",
		"wait_reason":            "no_eligible_runtime",
		"session_affinity_state": "none",
	} {
		if got := payload[key]; got != want {
			t.Fatalf("payload[%q] = %#v, want %#v", key, got, want)
		}
	}
}
