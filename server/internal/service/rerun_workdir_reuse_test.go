package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestRerunIssuePinsForceFreshSessionForRollbackSafety locks in the rollback-safe
// half of the MUL-4869 contract: RerunIssue ALWAYS persists
// force_fresh_session=true on the rerun row, no matter how the source task
// failed. The session-reuse decision is made later by the (new) claim handler
// from the source task, so an OLD claim handler picked up mid rolling-deploy —
// which gates the whole resume lookup on !force_fresh_session — degrades to a
// clean start instead of resuming a different execution via the
// (agent, issue) most-recent query. The claim-layer behaviour (workdir always
// reused, session gated by source failure class) is asserted in
// TestClaimTask_ManualRetryReusesWorkdir.
func TestRerunIssuePinsForceFreshSessionForRollbackSafety(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, creatorID, agentID, issueID := seedAttributionFixture(t, pool)
	_ = workspaceID

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}

	// agent_task_queue.runtime_id is NOT NULL; reuse the fixture agent's runtime.
	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read agent runtime: %v", err)
	}

	// The flag must be true across resume-safe, resume-poisoned, and cancelled
	// source failures alike — the enqueue row never encodes the classification.
	cases := []struct {
		name          string
		status        string
		failureReason any // string, or nil for a NULL failure_reason
	}{
		{name: "transient_timeout", status: "failed", failureReason: "timeout"},
		{name: "transient_provider_network", status: "failed", failureReason: "agent_error.provider_network"},
		{name: "poisoned_context_overflow", status: "failed", failureReason: "agent_error.context_overflow"},
		{name: "cancelled_no_reason", status: "cancelled", failureReason: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sourceID pgtype.UUID
			if err := pool.QueryRow(ctx, `
				INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, failure_reason, session_id, work_dir)
				VALUES ($1, $2, $3, $4, 0, $5, 'src-session', '/tmp/src-workdir')
				RETURNING id
			`, agentID, runtimeID, issueID, tc.status, tc.failureReason).Scan(&sourceID); err != nil {
				t.Fatalf("insert source task: %v", err)
			}
			t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, sourceID) })

			task, err := svc.RerunIssue(ctx, util.MustParseUUID(issueID), sourceID, pgtype.UUID{}, util.MustParseUUID(creatorID), nil)
			if err != nil {
				t.Fatalf("RerunIssue: %v", err)
			}
			t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID) })

			var forceFresh bool
			var rerunOf pgtype.UUID
			if err := pool.QueryRow(ctx, `
				SELECT force_fresh_session, rerun_of_task_id FROM agent_task_queue WHERE id = $1
			`, task.ID).Scan(&forceFresh, &rerunOf); err != nil {
				t.Fatalf("read rerun task: %v", err)
			}
			if !forceFresh {
				t.Errorf("force_fresh_session = false, want true for rollback safety (source failure_reason=%v)", tc.failureReason)
			}
			if !rerunOf.Valid || rerunOf.Bytes != sourceID.Bytes {
				t.Errorf("rerun_of_task_id = %s, want source %s", util.UUIDToString(rerunOf), util.UUIDToString(sourceID))
			}
		})
	}
}
