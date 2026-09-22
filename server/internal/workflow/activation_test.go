package workflow

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestReplaceNodeActivationKeepsScopeGenerationForRecovery(t *testing.T) {
	node := NodeRun{
		NodeID:            "draft",
		Generation:        2,
		ActivationNo:      3,
		ActivationID:      "old-activation",
		ScopeInstanceID:   "scope-instance",
		ScopeDefinitionID: "revision",
		TaskID:            "old-task",
		Attempt:           4,
	}

	replaceNodeActivation(&node, false)

	if node.Generation != 2 || node.ActivationNo != 4 {
		t.Fatalf("recovery replacement changed scope generation/activation number: %#v", node)
	}
	if node.ActivationID == "old-activation" || node.ReplacesActivationID != "old-activation" {
		t.Fatalf("replacement lineage was not recorded: %#v", node)
	}
	if node.ScopeInstanceID != "scope-instance" || node.ScopeDefinitionID != "revision" {
		t.Fatalf("recovery replacement moved scope: %#v", node)
	}
	if node.TaskID != "" || node.Attempt != 0 {
		t.Fatalf("recovery replacement retained execution state: %#v", node)
	}
}

func TestReplaceNodeActivationStartsNewScopeGeneration(t *testing.T) {
	node := NodeRun{
		NodeID:            "draft",
		Generation:        1,
		ActivationNo:      5,
		ActivationID:      "old-activation",
		ScopeInstanceID:   "old-scope",
		ScopeDefinitionID: "revision",
	}

	replaceNodeActivation(&node, true)

	if node.Generation != 2 || node.ActivationNo != 1 {
		t.Fatalf("new scope generation was not reset: %#v", node)
	}
	if node.ScopeInstanceID != "" || node.ScopeDefinitionID != "" {
		t.Fatalf("new scope generation retained the old scope: %#v", node)
	}
}

func TestAccountTaskExecutionOnlyCountsOnce(t *testing.T) {
	started := time.Date(2026, 9, 11, 1, 0, 0, 0, time.UTC)
	completed := started.Add(1500 * time.Millisecond)
	run := Run{}
	node := NodeRun{}
	task := db.AgentTaskQueue{
		StartedAt:   pgtype.Timestamptz{Time: started, Valid: true},
		CompletedAt: pgtype.Timestamptz{Time: completed, Valid: true},
	}

	accountTaskExecution(&run, &node, task, completed.Add(time.Second))
	accountTaskExecution(&run, &node, task, completed.Add(2*time.Second))

	if run.ActiveExecutionMS != 1500 {
		t.Fatalf("active execution = %dms, want 1500ms", run.ActiveExecutionMS)
	}
	if !node.ExecutionAccounted {
		t.Fatal("task execution should be marked accounted")
	}
}
