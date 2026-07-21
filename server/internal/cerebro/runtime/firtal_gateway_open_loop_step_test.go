package runtime

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestLoopStepCapabilityComesOnlyFromTrustedTaskContext(t *testing.T) {
	issueID := openStepTestUUID(t, "11111111-1111-1111-1111-111111111111")
	task := db.AgentTaskQueue{
		IssueID: issueID,
		Context: []byte(`{
			"type":"workflow_block",
			"loop_step":{
				"workflow_id":"22222222-2222-2222-2222-222222222222",
				"phase_id":"delivery",
				"block_id":"build",
				"step_number":2,
				"steps":{"allowed":true,"max":3},
				"phase_limits":{"max_steps":5,"max_rounds":2,"no_progress_stalls":2}
			}
		}`),
	}

	capability := loopStepCapabilityFromTask(task)
	if capability == nil {
		t.Fatal("steps-enabled task did not receive open-step capability")
	}
	if capability.Current.IssueID != issueID || capability.Current.PhaseID != "delivery" ||
		capability.Current.BlockID != "build" || capability.Current.Number != 2 {
		t.Fatalf("wrong pinned current step: %+v", capability.Current)
	}
	if capability.Steps.Max != 3 || capability.Limits.MaxSteps != 5 {
		t.Fatalf("trusted limits were not carried through: %+v", capability)
	}

	task.Context = []byte(`{"loop_step":{"steps":{"allowed":false,"max":3}}}`)
	if capability := loopStepCapabilityFromTask(task); capability != nil {
		t.Fatalf("non-steps task received capability: %+v", capability)
	}
}

func openStepTestUUID(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		t.Fatalf("parse UUID: %v", err)
	}
	return id
}
