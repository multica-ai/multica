package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// A manual quick-create retry is claimable only on its agent's current
// runtime: claims fence on agent.runtime_id = task.runtime_id. Automatic
// retries are covered by TestRetryPreservesChildAcrossRuntimeBindingChanges.
func TestManualQuickCreateRetryFollowsAgentToItsNewRuntime(t *testing.T) {
	f, _, _, agent := wakeFixture(t)
	var oldRuntime string
	if err := f.Pool.QueryRow(context.Background(), "SELECT runtime_id::text FROM agent WHERE id=$1", agent).Scan(&oldRuntime); err != nil {
		t.Fatal(err)
	}
	newRuntime := f.Runtime(t, fmt.Sprintf("rt-rebind-%d", principalSeq.Add(1)))
	parent := f.Task(t, agent, testutil.Cols{
		"status": "failed", "runtime_id": oldRuntime,
		"context": testutil.Raw(`'{"type":"quick_create","prompt":"file a bug"}'::jsonb`),
	})
	f.Cleanup(t, "DELETE FROM agent_task_queue WHERE rerun_of_task_id=$1", parent)
	f.Exec(t, "UPDATE agent SET runtime_id=$2 WHERE id=$1", agent, newRuntime)

	child, err := f.q.CreateManualQuickCreateRetryTask(context.Background(), db.CreateManualQuickCreateRetryTaskParams{
		SourceTaskID: parseTestUUID(t, parent),
		ActorUserID:  parseTestUUID(t, f.UserID),
		NewTaskID:    dbid.NewV7(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if child.RuntimeID != parseTestUUID(t, newRuntime) {
		t.Fatalf("quick-create retry runtime = %v, want the agent's new runtime %s", child.RuntimeID, newRuntime)
	}
}
