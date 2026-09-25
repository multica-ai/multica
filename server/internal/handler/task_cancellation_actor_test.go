package handler

import (
	"context"
	"testing"
)

func TestTaskCancellationActor_SnapshotsAgentName(t *testing.T) {
	const name = "Cancellation actor agent"
	agentID := createHandlerTestAgent(t, name, nil)

	actor := testHandler.taskCancellationActor(context.Background(), "agent", agentID)
	if actor.Type != "agent" || !actor.ID.Valid || uuidToString(actor.ID) != agentID || actor.Name != name {
		t.Fatalf("actor = %#v, want agent %s (%s)", actor, name, agentID)
	}
}
