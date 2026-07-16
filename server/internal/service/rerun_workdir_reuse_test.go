package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestRerunIssueForceFreshSessionByFailureClass locks in the MUL-4869 contract on
// the enqueue side: a manual retry no longer hardcodes force_fresh_session=true.
// It derives the flag from the source task's failure classification so a
// transient failure (network / timeout / provider blip) resumes the SESSION,
// while a conversation-poisoning failure starts a fresh session. Either way the
// workdir is reused — that half is asserted at the claim layer in
// TestClaimTask_ManualRetryReusesWorkdir.
func TestRerunIssueForceFreshSessionByFailureClass(t *testing.T) {
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

	cases := []struct {
		name           string
		status         string
		failureReason  any // string, or nil for a NULL failure_reason
		wantForceFresh bool
	}{
		{name: "transient_timeout_resumes", status: "failed", failureReason: "timeout", wantForceFresh: false},
		{name: "transient_provider_network_resumes", status: "failed", failureReason: "agent_error.provider_network", wantForceFresh: false},
		{name: "poisoned_context_overflow_fresh", status: "failed", failureReason: "agent_error.context_overflow", wantForceFresh: true},
		{name: "poisoned_api_invalid_request_fresh", status: "failed", failureReason: "api_invalid_request", wantForceFresh: true},
		{name: "cancelled_no_reason_resumes", status: "cancelled", failureReason: nil, wantForceFresh: false},
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
			if forceFresh != tc.wantForceFresh {
				t.Errorf("force_fresh_session = %v, want %v (source failure_reason=%v)", forceFresh, tc.wantForceFresh, tc.failureReason)
			}
			if !rerunOf.Valid || rerunOf.Bytes != sourceID.Bytes {
				t.Errorf("rerun_of_task_id = %s, want source %s", util.UUIDToString(rerunOf), util.UUIDToString(sourceID))
			}
		})
	}
}
