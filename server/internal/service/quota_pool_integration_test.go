package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

func TestProviderQuotaPoolFailureProbeAndRelease(t *testing.T) {
	pool := sharedTestPool(t)
	ctx := context.Background()
	q := db.New(pool)
	fx := testutil.New(pool, "", "")
	suffix := time.Now().UnixNano()
	userID := fx.User(t, "Quota fixture", fmt.Sprintf("quota-%d@multica.test", suffix))
	workspaceID := fx.Workspace(t, "Quota fixture", fmt.Sprintf("quota-%d", suffix))
	fx.UserID, fx.WorkspaceID = userID, workspaceID
	fx.Member(t, workspaceID, userID, "owner")
	runtimeID := fx.Runtime(t, "quota runtime")
	agentID := fx.Agent(t, "quota agent", runtimeID)
	issueID := fx.Issue(t, "Quota probe task", testutil.Cols{"assignee_type": "agent", "assignee_id": agentID})
	poolID := fx.Insert(t, "provider_quota_pool", testutil.Cols{
		"id": dbid.NewV7(), "owner_id": userID, "name": "vps-openai", "timezone": "America/Los_Angeles",
	})
	fx.Cleanup(t, `DELETE FROM provider_quota_pool_event WHERE pool_id = $1`, poolID)
	fx.InsertNoID(t, "provider_quota_pool_agent", testutil.Cols{
		"agent_id": agentID, "pool_id": poolID,
	}, "agent_id = $1", agentID)
	failureTaskID := fx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "status": "running",
		"attempt": 1, "max_attempts": 1, "started_at": testutil.Raw("now()"),
	})
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	_, err := svc.FailTask(ctx, util.MustParseUUID(failureTaskID),
		"You've hit your weekly limit; resets Sep 29 at 3am (America/Los_Angeles)",
		"", "", "", "", false, "", "")
	if err != nil {
		t.Fatalf("FailTask: %v", err)
	}
	state := func() db.ProviderQuotaPool {
		t.Helper()
		row, err := q.GetProviderQuotaPool(ctx, util.MustParseUUID(poolID))
		if err != nil {
			t.Fatalf("GetProviderQuotaPool: %v", err)
		}
		return row
	}
	if held := state(); held.State != "held_exact" || held.SourceTaskID != util.MustParseUUID(failureTaskID) {
		t.Fatalf("quota failure did not hold its pool: %+v", held)
	}
	probeTaskID := fx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "attempt": 1, "max_attempts": 1,
	})
	if _, err := q.ClaimAgentTask(ctx, db.ClaimAgentTaskParams{
		AgentID: util.MustParseUUID(agentID), RuntimeID: util.MustParseUUID(runtimeID),
		PrepareLeaseSecs: 45, RuntimeStaleSecs: RuntimeClaimFreshnessSeconds,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("held pool admitted a claim: %v", err)
	}
	fx.Exec(t, `UPDATE provider_quota_pool SET reset_at = now() - interval '1 second' WHERE id = $1`, poolID)
	if err := svc.ReconcileDueProviderQuotaPools(ctx, 10); err != nil {
		t.Fatalf("reconcile reset: %v", err)
	}
	if got := state().State; got != "probe_due" {
		t.Fatalf("due pool state = %q, want probe_due", got)
	}
	claim, err := svc.ClaimTask(ctx, util.MustParseUUID(agentID))
	if err != nil || claim == nil || claim.ID != util.MustParseUUID(probeTaskID) {
		t.Fatalf("reserved probe claim = %+v, %v", claim, err)
	}
	if probing := state(); probing.State != "probing" || probing.ProbeTaskID != claim.ID || probing.ProbeAttempts != 1 {
		t.Fatalf("probe reservation = %+v", probing)
	}
	if _, err := svc.StartTaskForClaim(ctx, db.LockAgentTaskStartClaimParams{
		ID: claim.ID, RuntimeID: claim.RuntimeID, DispatchedAt: claim.DispatchedAt,
	}); err != nil {
		t.Fatalf("reserved probe did not start: %v", err)
	}
	if _, err := svc.CompleteTask(ctx, claim.ID, []byte(`{"output":"Done"}`), "", "", "", false, "", ""); err != nil {
		t.Fatalf("complete probe: %v", err)
	}
	if got := state().State; got != "open" {
		t.Fatalf("successful probe state = %q, want open", got)
	}
	if got := fx.Count(t, `SELECT count(*) FROM provider_quota_pool_event WHERE pool_id = $1`, poolID); got != 4 {
		t.Fatalf("pool audit events = %d, want held, due, probe started, probe succeeded", got)
	}
	// Two agents on one provider account race for its next probe. The pool
	// lock admits exactly one across agents, then a cancelled probe backs off
	// instead of leaving the account stuck in probing.
	peerAgentID := fx.Agent(t, "quota peer", runtimeID)
	fx.InsertNoID(t, "provider_quota_pool_agent", testutil.Cols{
		"agent_id": peerAgentID, "pool_id": poolID,
	}, "agent_id = $1", peerAgentID)
	firstIssueID := fx.Issue(t, "First competing probe", testutil.Cols{"assignee_type": "agent", "assignee_id": agentID})
	secondIssueID := fx.Issue(t, "Second competing probe", testutil.Cols{"assignee_type": "agent", "assignee_id": peerAgentID})
	for _, row := range []struct{ agentID, issueID string }{{agentID, firstIssueID}, {peerAgentID, secondIssueID}} {
		fx.Task(t, row.agentID, testutil.Cols{
			"runtime_id": runtimeID, "issue_id": row.issueID, "attempt": 1, "max_attempts": 1,
		})
	}
	fx.Exec(t, `UPDATE provider_quota_pool SET state = 'probe_due', revision = revision + 1 WHERE id = $1`, poolID)
	type claimResult struct {
		task *db.AgentTaskQueue
		err  error
	}
	results := make(chan claimResult, 2)
	var wg sync.WaitGroup
	for _, id := range []string{agentID, peerAgentID} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			task, err := svc.ClaimTask(ctx, util.MustParseUUID(id))
			results <- claimResult{task: task, err: err}
		}(id)
	}
	wg.Wait()
	close(results)
	var winner *db.AgentTaskQueue
	for result := range results {
		if result.err != nil {
			t.Fatalf("competing probe claim: %v", result.err)
		}
		if result.task != nil {
			if winner != nil {
				t.Fatalf("two probe claims: %s and %s", util.UUIDToString(winner.ID), util.UUIDToString(result.task.ID))
			}
			winner = result.task
		}
	}
	if winner == nil || state().ProbeTaskID != winner.ID {
		t.Fatalf("one probe was not reserved: winner=%+v pool=%+v", winner, state())
	}
	fx.Exec(t, `UPDATE agent_task_queue SET status = 'cancelled' WHERE id = $1`, winner.ID)
	if err := svc.ReconcileDueProviderQuotaPools(ctx, 10); err != nil {
		t.Fatalf("reconcile cancelled probe: %v", err)
	}
	if got := state(); got.State != "probe_backoff" || got.ProbeTaskID.Valid {
		t.Fatalf("cancelled probe state = %+v, want backoff without reserved task", got)
	}
}
