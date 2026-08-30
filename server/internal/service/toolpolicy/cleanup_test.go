package toolpolicy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service/toolaction"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCleanupAgentAuditsCancellationBeforeDeletingCurrentPolicy(t *testing.T) {
	queries := &cleanupQueries{cancelled: []db.AgentToolApprovalRequest{cleanupApproval()}}
	recorder := toolaction.NewSQLService(nil)
	if err := CleanupAgent(context.Background(), queries, recorder, pgtype.UUID{Valid: true}, pgtype.UUID{Valid: true}, time.Unix(10, 0)); err != nil {
		t.Fatalf("CleanupAgent error = %v", err)
	}
	if queries.recorded != 1 || queries.deletedRules != 1 || queries.deletedPolicies != 1 {
		t.Fatalf("cleanup calls = recorded:%d rules:%d policies:%d", queries.recorded, queries.deletedRules, queries.deletedPolicies)
	}
}

func TestCleanupAgentFailsClosedWhenCancellationAuditFails(t *testing.T) {
	queries := &cleanupQueries{
		cancelled: []db.AgentToolApprovalRequest{cleanupApproval()},
		recordErr: errors.New("audit unavailable"),
	}
	err := CleanupAgent(context.Background(), queries, toolaction.NewSQLService(nil), pgtype.UUID{Valid: true}, pgtype.UUID{Valid: true}, time.Unix(10, 0))
	if err == nil {
		t.Fatal("CleanupAgent succeeded with a failed audit write")
	}
	if queries.deletedRules != 0 || queries.deletedPolicies != 0 {
		t.Fatalf("policy deletion continued after audit failure: rules=%d policies=%d", queries.deletedRules, queries.deletedPolicies)
	}
}

func TestCleanupWorkspaceDeletesEveryToolControlFamilyInDependencyOrder(t *testing.T) {
	queries := &workspaceCleanupQueries{}
	if err := CleanupWorkspace(context.Background(), queries, testCleanupUUID(9)); err != nil {
		t.Fatalf("CleanupWorkspace error = %v", err)
	}
	want := []string{"actions", "approvals", "rules", "revisions", "policies"}
	if len(queries.calls) != len(want) {
		t.Fatalf("cleanup calls = %v, want %v", queries.calls, want)
	}
	for i := range want {
		if queries.calls[i] != want[i] {
			t.Fatalf("cleanup calls = %v, want %v", queries.calls, want)
		}
	}
}

func cleanupApproval() db.AgentToolApprovalRequest {
	return db.AgentToolApprovalRequest{
		ID:            testCleanupUUID(1),
		WorkspaceID:   testCleanupUUID(2),
		AgentID:       testCleanupUUID(3),
		TaskID:        testCleanupUUID(4),
		InvocationID:  testCleanupUUID(5),
		TransportKind: "managed_mcp",
		ServerKey:     "linear",
		ToolName:      "list_issues",
		SchemaDigest:  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
}

func testCleanupUUID(seed byte) pgtype.UUID {
	var bytes [16]byte
	bytes[15] = seed
	return pgtype.UUID{Bytes: bytes, Valid: true}
}

type cleanupQueries struct {
	cancelled       []db.AgentToolApprovalRequest
	recordErr       error
	recorded        int
	deletedRules    int
	deletedPolicies int
}

type workspaceCleanupQueries struct {
	calls []string
}

func (q *workspaceCleanupQueries) DeleteAgentToolActionEventsForWorkspace(context.Context, pgtype.UUID) (int64, error) {
	q.calls = append(q.calls, "actions")
	return 1, nil
}

func (q *workspaceCleanupQueries) DeleteAgentToolApprovalRequestsForWorkspace(context.Context, pgtype.UUID) (int64, error) {
	q.calls = append(q.calls, "approvals")
	return 1, nil
}

func (q *workspaceCleanupQueries) DeleteAgentToolPolicyRulesForWorkspaceCleanup(context.Context, pgtype.UUID) (int64, error) {
	q.calls = append(q.calls, "rules")
	return 1, nil
}

func (q *workspaceCleanupQueries) DeleteAgentToolPolicyRevisionsForWorkspaceCleanup(context.Context, pgtype.UUID) (int64, error) {
	q.calls = append(q.calls, "revisions")
	return 1, nil
}

func (q *workspaceCleanupQueries) DeleteAgentToolPoliciesForWorkspaceCleanup(context.Context, pgtype.UUID) (int64, error) {
	q.calls = append(q.calls, "policies")
	return 1, nil
}

func (q *cleanupQueries) CancelAgentToolApprovalRequestsForAgentCleanup(context.Context, db.CancelAgentToolApprovalRequestsForAgentCleanupParams) ([]db.AgentToolApprovalRequest, error) {
	return q.cancelled, nil
}

func (q *cleanupQueries) CreateOrGetAgentToolActionEvent(context.Context, db.CreateOrGetAgentToolActionEventParams) (db.CreateOrGetAgentToolActionEventRow, error) {
	if q.recordErr != nil {
		return db.CreateOrGetAgentToolActionEventRow{}, q.recordErr
	}
	q.recorded++
	return db.CreateOrGetAgentToolActionEventRow{CreatedAt: pgtype.Timestamptz{Time: time.Unix(10, 0), Valid: true}}, nil
}

func (q *cleanupQueries) DeleteAgentToolPolicyRulesForAgent(context.Context, db.DeleteAgentToolPolicyRulesForAgentParams) (int64, error) {
	q.deletedRules++
	return 1, nil
}

func (q *cleanupQueries) DeleteAgentToolPolicyForAgent(context.Context, db.DeleteAgentToolPolicyForAgentParams) (int64, error) {
	q.deletedPolicies++
	return 1, nil
}
