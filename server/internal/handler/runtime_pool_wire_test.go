package handler

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestRuntimePoolWireKeepsEmptyRuntimeForWaitingPoolTask(t *testing.T) {
	task := db.AgentTaskQueue{
		RuntimeBindingMode:   "pool",
		Status:               "waiting_runtime",
		WaitReason:           pgtype.Text{String: "no_eligible_runtime", Valid: true},
		SessionAffinityState: "none",
	}
	runtimeID, mode, reason := taskRuntimeWire(task)
	if runtimeID != "" || mode != "pool" || reason != "no_eligible_runtime" {
		t.Fatalf("wire=(%q,%q,%q)", runtimeID, mode, reason)
	}

	response := taskToResponse(task, "workspace-1")
	if response.RuntimeID != "" || response.RuntimeBindingMode != "pool" ||
		response.Status != "waiting_runtime" || response.WaitReason != "no_eligible_runtime" ||
		response.SessionAffinityState != "none" {
		t.Fatalf("task response runtime wire = %+v", response)
	}
}

func TestAgentResponseRuntimePoolIsRoutableWithoutRuntime(t *testing.T) {
	requirements := json.RawMessage(`{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["multica.extension.execute/v1"]}`)
	response := (&Handler{}).agentToResponse(db.Agent{
		RuntimeBindingMode:  "pool",
		RuntimeMode:         "pool",
		RuntimeRequirements: requirements,
	})
	if response.RuntimeID != "" || response.RuntimeBound || !response.RuntimeRoutable || response.RuntimeBindingMode != "pool" {
		t.Fatalf("Pool Agent runtime wire = %+v", response)
	}
	if string(response.RuntimeRequirements) != string(requirements) {
		t.Fatalf("runtime_requirements = %s, want %s", response.RuntimeRequirements, requirements)
	}
}
