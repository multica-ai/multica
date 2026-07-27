package handler

import (
	"encoding/json"
	"testing"
)

// td-836aa9: TaskFailRequest carries the daemon's failover side-effect evidence.
// A new daemon sends it; the handler decodes it and threads it to the service.
// An old daemon omits the field, which MUST decode to a nil pointer so active
// failover stays fail-closed (completeness never proven) — this is the API
// backward-compatibility contract.
func TestTaskFailRequest_DecodesFailoverEvidence(t *testing.T) {
	t.Parallel()

	t.Run("present evidence decodes with all fields", func(t *testing.T) {
		var req TaskFailRequest
		body := `{"error":"rate limit","failure_reason":"agent_error.provider_capacity_or_rate_limit",` +
			`"failover_evidence":{"observed_tool_calls":3,"partial_user_output":true,"complete":true}}`
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if req.FailoverEvidence == nil {
			t.Fatal("failover_evidence must decode to a non-nil pointer when present")
		}
		if req.FailoverEvidence.ObservedToolCalls != 3 {
			t.Errorf("observed_tool_calls = %d, want 3", req.FailoverEvidence.ObservedToolCalls)
		}
		if !req.FailoverEvidence.PartialUserOutput {
			t.Error("partial_user_output must be true")
		}
		if !req.FailoverEvidence.Complete {
			t.Error("complete must be true")
		}
	})

	t.Run("absent evidence (legacy daemon) decodes to nil", func(t *testing.T) {
		var req TaskFailRequest
		if err := json.Unmarshal([]byte(`{"error":"boom"}`), &req); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if req.FailoverEvidence != nil {
			t.Fatalf("omitted failover_evidence must decode to nil (fail-closed), got %+v", req.FailoverEvidence)
		}
	})
}
