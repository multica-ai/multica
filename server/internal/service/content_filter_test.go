package service

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestFailTaskContentFilterDoesNotRetry(t *testing.T) {
	pool := sharedTestPool(t)
	ctx := context.Background()
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	fixture := testutil.New(pool, workspaceID, userID)
	queries := db.New(pool)
	agent, err := queries.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatal(err)
	}
	svc := &TaskService{Queries: queries, TxStarter: pool, Bus: events.New()}
	const message = "stream disconnected before completion: Incomplete response returned, reason: content_filter"
	for _, reported := range []string{"", "agent_error.provider_network", taskfailure.ReasonAgentProviderContentFilter.String()} {
		t.Run("reported="+reported, func(t *testing.T) {
			id := fixture.Task(t, agentID, testutil.Cols{
				"runtime_id":   agent.RuntimeID,
				"issue_id":     issueID,
				"status":       "running",
				"attempt":      1,
				"max_attempts": 3,
			})
			failed, err := svc.FailTask(ctx, util.MustParseUUID(id), message, "source-session", "/tmp/source-workdir", "", reported, false, "", "")
			if err != nil {
				t.Fatalf("FailTask: %v", err)
			}
			if failed.Status != "failed" || failed.FailureReason.String != taskfailure.ReasonAgentProviderContentFilter.String() {
				t.Fatalf("failure = %s/%s, want failed/%s", failed.Status, failed.FailureReason.String, taskfailure.ReasonAgentProviderContentFilter)
			}
			stored, err := queries.GetAgentTask(ctx, util.MustParseUUID(id))
			if err != nil {
				t.Fatal(err)
			}
			if stored.FailureReason.String != taskfailure.ReasonAgentProviderContentFilter.String() {
				t.Fatalf("stored failure reason = %s", stored.FailureReason.String)
			}
			if n := fixture.Count(t, "SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1", id); n != 0 {
				t.Fatalf("content filtering created %d retry tasks", n)
			}
		})
	}
}
