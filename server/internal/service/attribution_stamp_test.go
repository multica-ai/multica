package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedAttributionFixture creates the minimal user/workspace/member/runtime/agent
// graph plus a member-created issue assigned to the agent, and returns the ids
// needed to enqueue a run. Everything is cleaned up via t.Cleanup.
func seedAttributionFixture(t *testing.T, pool *pgxpool.Pool) (workspaceID, userID, agentID, issueID string) {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Attr User', $1) RETURNING id`,
		fmt.Sprintf("attr-%d@multica.test", suffix)).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })

	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug) VALUES ('attr ws', $1) RETURNING id`,
		fmt.Sprintf("attr-%d", suffix)).Scan(&workspaceID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID) })

	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		workspaceID, userID); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	var runtimeID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
		VALUES ($1, 'attr-runtime', 'cloud', 'codex', 'online', '', '{}'::jsonb, $2)
		RETURNING id`, workspaceID, userID).Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility,
			max_concurrent_tasks, owner_id, instructions, custom_env, custom_args)
		VALUES ($1, 'attr-agent', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id`, workspaceID, runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, priority)
		VALUES ($1, 'attr issue', 'member', $2, 'agent', $3, 'medium')
		RETURNING id`, workspaceID, userID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	return workspaceID, userID, agentID, issueID
}

// TestEnqueueTaskForIssueStampsDirectHumanAttribution is the acceptance test for
// the Phase 1 foundation (MUL-4302 §11): a member-assigned run must land with a
// non-empty, correct attribution — source=direct_human, the accountable human
// equal to the issue creator, and evidence pointing back at the issue.
func TestEnqueueTaskForIssueStampsDirectHumanAttribution(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	task, err := svc.EnqueueTaskForIssue(ctx, db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(userID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	})
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue: %v", err)
	}

	// Read the stored row so we assert what actually persisted, not just the
	// returned struct.
	var source pgtype.Text
	var originator, accountable, evidenceRef pgtype.UUID
	var evidenceKind pgtype.Text
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id, trigger_evidence_kind, trigger_evidence_ref_id
		FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&source, &originator, &accountable, &evidenceKind, &evidenceRef); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}

	if source.String != string(attribution.SourceDirectHuman) {
		t.Errorf("originator_source = %q, want direct_human", source.String)
	}
	if !originator.Valid || originator.Bytes != util.MustParseUUID(userID).Bytes {
		t.Errorf("originator_user_id = %s, want %s", util.UUIDToString(originator), userID)
	}
	// MUL-4302 §11 invariant at the DB layer: a non-NULL originator implies the
	// accountable human equals it.
	if !accountable.Valid || accountable.Bytes != originator.Bytes {
		t.Errorf("accountable_user_id = %s, want == originator %s", util.UUIDToString(accountable), util.UUIDToString(originator))
	}
	if evidenceKind.String != string(attribution.EvidenceIssueAssignment) {
		t.Errorf("trigger_evidence_kind = %q, want issue_assignment", evidenceKind.String)
	}
	if !evidenceRef.Valid || evidenceRef.Bytes != util.MustParseUUID(issueID).Bytes {
		t.Errorf("trigger_evidence_ref_id = %s, want issue %s", util.UUIDToString(evidenceRef), issueID)
	}
}

// TestEnqueueTaskForIssueWithHandoffAttributesToActor is the acceptance test for
// the assign/promote actor fix (MUL-4302 §4): when a member assigns an issue that
// a DIFFERENT member created, the run's accountable human — and, honoring the
// invariant, its originator — is the assigning member (the actor), not the issue
// creator. The evidence still points at the issue.
func TestEnqueueTaskForIssueWithHandoffAttributesToActor(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, creatorID, agentID, issueID := seedAttributionFixture(t, pool)

	// A second member in the same workspace performs the assign.
	var actorID string
	suffix := time.Now().UnixNano()
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Actor', $1) RETURNING id`,
		fmt.Sprintf("actor-%d@multica.test", suffix)).Scan(&actorID); err != nil {
		t.Fatalf("seed actor user: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, actorID) })
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		workspaceID, actorID); err != nil {
		t.Fatalf("seed actor member: %v", err)
	}

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	task, err := svc.EnqueueTaskForIssueWithHandoff(ctx, db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(creatorID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	}, "", util.MustParseUUID(actorID))
	if err != nil {
		t.Fatalf("EnqueueTaskForIssueWithHandoff: %v", err)
	}

	var source pgtype.Text
	var originator, accountable pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id
		FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&source, &originator, &accountable); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}

	if source.String != string(attribution.SourceDirectHuman) {
		t.Errorf("originator_source = %q, want direct_human", source.String)
	}
	if !accountable.Valid || accountable.Bytes != util.MustParseUUID(actorID).Bytes {
		t.Errorf("accountable_user_id = %s, want actor %s (not creator %s)", util.UUIDToString(accountable), actorID, creatorID)
	}
	// Invariant: originator mirrors accountable, so it is the actor too — the
	// assigning member lends the authority, not the issue creator.
	if !originator.Valid || originator.Bytes != accountable.Bytes {
		t.Errorf("originator_user_id = %s, want == accountable (actor) %s", util.UUIDToString(originator), util.UUIDToString(accountable))
	}
}

// TestMergeCommentIntoPendingTask_KeepsAccountableEqualsOriginator guards the
// MUL-4302 one-way invariant across the comment-coalescing merge (main #5192 ×
// attribution): when a coalescing run re-stamps originator_user_id to the newly
// arrived comment's human, accountable_user_id must mirror it. Otherwise folding
// member B's comment into member A's queued task leaves originator=B / accountable=A.
func TestMergeCommentIntoPendingTask_KeepsAccountableEqualsOriginator(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userA, agentID, issueID := seedAttributionFixture(t, pool)

	// A second member B whose comment will be coalesced in.
	var userB string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Attr User B', $1) RETURNING id`,
		fmt.Sprintf("attr-b-%d@multica.test", time.Now().UnixNano())).Scan(&userB); err != nil {
		t.Fatalf("seed user B: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userB) })
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`, workspaceID, userB); err != nil {
		t.Fatalf("add member B: %v", err)
	}

	// A queued task attributed to A with a STALE source label + no evidence, so the
	// merge has something to prove it re-stamped the whole snapshot, not just people.
	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, originator_user_id, accountable_user_id, originator_source)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'queued', 0, $3, $3, 'delegation')
		RETURNING id`, agentID, issueID, userA).Scan(&taskID); err != nil {
		t.Fatalf("seed queued task: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	// B's newly-arrived comment on the same issue.
	var commentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'B comment') RETURNING id`, issueID, workspaceID, userB).Scan(&commentID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	// Re-stamp the FULL snapshot for B's member comment (direct_human + comment
	// evidence), as the caller does.
	if _, err := q.MergeCommentIntoPendingTask(ctx, db.MergeCommentIntoPendingTaskParams{
		IssueID:                 util.MustParseUUID(issueID),
		AgentID:                 util.MustParseUUID(agentID),
		NewTriggerCommentID:     util.MustParseUUID(commentID),
		NewOriginatorUserID:     util.MustParseUUID(userB),
		NewAccountableUserID:    util.MustParseUUID(userB),
		NewOriginatorSource:     pgtype.Text{String: "direct_human", Valid: true},
		NewTriggerEvidenceKind:  pgtype.Text{String: "comment", Valid: true},
		NewTriggerEvidenceRefID: util.MustParseUUID(commentID),
	}); err != nil {
		t.Fatalf("MergeCommentIntoPendingTask: %v", err)
	}

	var originator, accountable pgtype.UUID
	var source, evidenceKind pgtype.Text
	if err := pool.QueryRow(ctx,
		`SELECT originator_user_id, accountable_user_id, originator_source, trigger_evidence_kind FROM agent_task_queue WHERE id = $1`, taskID,
	).Scan(&originator, &accountable, &source, &evidenceKind); err != nil {
		t.Fatalf("read task: %v", err)
	}
	if !originator.Valid || originator.Bytes != util.MustParseUUID(userB).Bytes {
		t.Errorf("originator = %s, want re-stamped to B %s", util.UUIDToString(originator), userB)
	}
	if !accountable.Valid || accountable.Bytes != originator.Bytes {
		t.Errorf("accountable = %s, want == originator (B); one-way invariant violated on merge", util.UUIDToString(accountable))
	}
	// Full-snapshot re-stamp: the stale 'delegation' source + NULL evidence must move
	// to the new comment's 'direct_human' + comment evidence, not linger.
	if source.String != "direct_human" {
		t.Errorf("originator_source = %q, want re-stamped to direct_human (stale snapshot left behind)", source.String)
	}
	if evidenceKind.String != "comment" {
		t.Errorf("trigger_evidence_kind = %q, want re-stamped to comment", evidenceKind.String)
	}
}

// TestAttributionForMergedComment_HonorsFailClosedPolicy is Elon's must-fix
// regression: folding a comment that resolves to NO precise human into a queued task
// re-opens the fail-closed decision. On a fail-closed workspace the merge must be
// REFUSED (ErrAttributionFailClosed) so the queued task keeps its original precise
// snapshot instead of being re-stamped to a degraded owner_fallback; on a fail-open
// workspace the same comment degrades to owner_fallback (accountable = agent owner)
// with no error, exactly as a fresh enqueue would (MUL-4302).
func TestAttributionForMergedComment_HonorsFailClosedPolicy(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, ownerID, agentID, issueID := seedAttributionFixture(t, pool)
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	agent, err := q.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}

	// An agent-authored comment with no source-task lineage → no precise human.
	var commentID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'agent', $3, 'autonomous ping') RETURNING id`,
		issueID, workspaceID, agentID).Scan(&commentID); err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	// Fail-CLOSED: the merge must be refused, not degraded.
	if _, err := pool.Exec(ctx, `UPDATE workspace SET attribution_fail_closed = true WHERE id = $1`, workspaceID); err != nil {
		t.Fatalf("set fail-closed: %v", err)
	}
	if _, err := svc.AttributionForMergedComment(ctx, util.MustParseUUID(workspaceID), util.MustParseUUID(commentID), false, agent); !errors.Is(err, ErrAttributionFailClosed) {
		t.Fatalf("fail-closed merge must return ErrAttributionFailClosed, got %v", err)
	}

	// Fail-OPEN (default): the same comment degrades to owner_fallback, no error.
	if _, err := pool.Exec(ctx, `UPDATE workspace SET attribution_fail_closed = false WHERE id = $1`, workspaceID); err != nil {
		t.Fatalf("clear fail-closed: %v", err)
	}
	attr, err := svc.AttributionForMergedComment(ctx, util.MustParseUUID(workspaceID), util.MustParseUUID(commentID), false, agent)
	if err != nil {
		t.Fatalf("fail-open merge must not error, got %v", err)
	}
	if attr.Source != attribution.SourceOwnerFallback {
		t.Errorf("fail-open unattributable merge source = %q, want owner_fallback", attr.Source)
	}
	if !attr.AccountableUserID.Valid || attr.AccountableUserID.Bytes != util.MustParseUUID(ownerID).Bytes {
		t.Errorf("owner_fallback accountable = %s, want agent owner %s", util.UUIDToString(attr.AccountableUserID), ownerID)
	}
	if attr.UserID.Valid {
		t.Errorf("owner_fallback must not set originator (authorization stays NULL), got %s", util.UUIDToString(attr.UserID))
	}
}

// TestAttributionInvariantCheck_RejectsBypass verifies the DB-level cross-column
// CHECK (MUL-4302): a write in the ENFORCED regime (originator_source non-NULL — every
// real enqueue / coalesce path stamps it) that sets originator_user_id but leaves
// accountable_user_id NULL — or different — is rejected at the database, so a future
// code path that bypasses finalizeAttribution fails loudly instead of silently
// mis-attributing an audited run (the #5192 comment-merge bug class). The strict
// post-backfill handling of source-NULL rows is covered by
// TestAttributionInvariantCheck_RejectsUnbackfilledLegacyRows.
func TestAttributionInvariantCheck_RejectsBypass(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	_, userA, agentID, issueID := seedAttributionFixture(t, pool)

	// originator set, accountable NULL, source set (enforced) → must violate the check.
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, originator_user_id, originator_source)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'queued', 0, $3, 'comment_source')`,
		agentID, issueID, userA); err == nil {
		t.Fatal("expected the CHECK to reject originator set with NULL accountable, but insert succeeded")
	}

	// originator != accountable → also rejected.
	var userB string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Check B', $1) RETURNING id`,
		fmt.Sprintf("check-b-%d@multica.test", time.Now().UnixNano())).Scan(&userB); err != nil {
		t.Fatalf("seed user B: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userB) })
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, originator_user_id, accountable_user_id, originator_source)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'queued', 0, $3, $4, 'comment_source')`,
		agentID, issueID, userA, userB); err == nil {
		t.Fatal("expected the CHECK to reject originator != accountable, but insert succeeded")
	}

	// The legitimate shape (equal) is accepted.
	var okTaskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, originator_user_id, accountable_user_id, originator_source)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'queued', 0, $3, $3, 'direct_human') RETURNING id`,
		agentID, issueID, userA).Scan(&okTaskID); err != nil {
		t.Fatalf("equal originator/accountable must be accepted, got %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, okTaskID) })
}

// TestAttributionInvariantCheck_RejectsUnbackfilledLegacyRows verifies the second
// phase of the two-phase rollout (MUL-4302). Once the out-of-band backfill is complete,
// originator_source=NULL no longer exempts a row from the one-way invariant. A stale
// writer or missed backfill that tries to persist originator set with accountable NULL
// must fail loudly instead of recreating the legacy shape.
func TestAttributionInvariantCheck_RejectsUnbackfilledLegacyRows(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	_, userA, agentID, issueID := seedAttributionFixture(t, pool)

	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, originator_user_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'queued', 0, $3)`,
		agentID, issueID, userA); err == nil {
		t.Fatal("expected the strict CHECK to reject an unbackfilled legacy row, but insert succeeded")
	}
}

// TestTriggerOwnerAttribution_ScheduleTriggerCreator is the acceptance test for
// trigger_owner (MUL-4302; Bohan): an autopilot schedule/webhook run is accountable
// to the member who CREATED the firing trigger, with originator NULL (no human
// authorized the autonomous fire).
func TestTriggerOwnerAttribution_ScheduleTriggerCreator(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, creatorID, agentID, _ := seedAttributionFixture(t, pool)

	var autopilotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_id, execution_mode, created_by_type, created_by_id)
		VALUES ($1, 'trigger-owner-ap', $2, 'run_only', 'member', $3) RETURNING id`,
		workspaceID, agentID, creatorID).Scan(&autopilotID); err != nil {
		t.Fatalf("seed autopilot: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID) })

	// Schedule trigger whose responsible publisher is the creating member.
	var triggerID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_trigger (autopilot_id, kind, enabled, cron_expression, published_by_type, published_by_id)
		VALUES ($1, 'schedule', true, '0 * * * *', 'member', $2) RETURNING id`,
		autopilotID, creatorID).Scan(&triggerID); err != nil {
		t.Fatalf("seed trigger: %v", err)
	}

	got := triggerOwnerAttribution(ctx, q,
		util.MustParseUUID(triggerID), util.MustParseUUID(workspaceID), util.MustParseUUID(autopilotID),
		attribution.EvidenceAutopilotRun, util.MustParseUUID(autopilotID))
	if got.Source != attribution.SourceTriggerOwner {
		t.Fatalf("source = %q, want trigger_owner", got.Source)
	}
	if got.UserID.Valid {
		t.Errorf("trigger_owner is audit-only; originator must stay NULL, got %s", util.UUIDToString(got.UserID))
	}
	if !got.AccountableUserID.Valid || got.AccountableUserID.Bytes != util.MustParseUUID(creatorID).Bytes {
		t.Errorf("accountable = %s, want trigger creator %s", util.UUIDToString(got.AccountableUserID), creatorID)
	}
}

// TestTriggerOwnerAttribution_LegacyTriggerFallsBackToRuleOwner verifies the
// backward-compat path Bohan signed off on: a trigger with no recorded creator
// (created before this migration) degrades to the rule publisher (rule_owner),
// never fabricating a human.
func TestTriggerOwnerAttribution_LegacyTriggerFallsBackToRuleOwner(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, publisherID, agentID, _ := seedAttributionFixture(t, pool)

	var autopilotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_id, execution_mode, created_by_type, created_by_id)
		VALUES ($1, 'legacy-trigger-ap', $2, 'run_only', 'member', $3) RETURNING id`,
		workspaceID, agentID, publisherID).Scan(&autopilotID); err != nil {
		t.Fatalf("seed autopilot: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID) })

	// Trigger with NO creator recorded (pre-migration style).
	var triggerID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_trigger (autopilot_id, kind, enabled, cron_expression)
		VALUES ($1, 'schedule', true, '0 * * * *') RETURNING id`,
		autopilotID).Scan(&triggerID); err != nil {
		t.Fatalf("seed trigger: %v", err)
	}
	// Active rule version so the fallback resolves to rule_owner (the publisher).
	if _, err := pool.Exec(ctx, `
		INSERT INTO autopilot_rule_version (autopilot_id, workspace_id, published_by_type, published_by_id)
		VALUES ($1, $2, 'member', $3)`, autopilotID, workspaceID, publisherID); err != nil {
		t.Fatalf("seed rule version: %v", err)
	}

	got := triggerOwnerAttribution(ctx, q,
		util.MustParseUUID(triggerID), util.MustParseUUID(workspaceID), util.MustParseUUID(autopilotID),
		attribution.EvidenceAutopilotRun, util.MustParseUUID(autopilotID))
	if got.Source != attribution.SourceRuleOwner {
		t.Fatalf("source = %q, want rule_owner (legacy trigger falls back)", got.Source)
	}
	if !got.AccountableUserID.Valid || got.AccountableUserID.Bytes != util.MustParseUUID(publisherID).Bytes {
		t.Errorf("accountable = %s, want rule publisher %s", util.UUIDToString(got.AccountableUserID), publisherID)
	}
}

// seedExtraMember inserts a standalone user + workspace member and returns the
// user id, so a test can model an EDITOR distinct from the trigger creator.
func seedExtraMember(t *testing.T, pool *pgxpool.Pool, workspaceID, label string) string {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	var userID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		label, fmt.Sprintf("%s-%d@multica.test", label, suffix)).Scan(&userID); err != nil {
		t.Fatalf("seed %s user: %v", label, err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID) })
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		workspaceID, userID); err != nil {
		t.Fatalf("seed %s member: %v", label, err)
	}
	return userID
}

// TestTriggerOwnerAttribution_TransfersToSubstantiveEditor is Elon's must-fix
// acceptance test: it drives the REAL triggerOwnerAttribution resolver (not the
// ruleOwnerAttribution helper) across the SAME queries the handlers use, and proves
// both halves of the pinned model — (1) responsibility TRANSFERS from the creator to
// whoever substantively edits the trigger, and (2) a trigger-scoped edit re-stamps
// ONLY that trigger, never a sibling. It also proves an autopilot-level edit bumps
// every trigger together (MUL-4302).
func TestTriggerOwnerAttribution_TransfersToSubstantiveEditor(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, creatorA, agentID, _ := seedAttributionFixture(t, pool)
	editorB := seedExtraMember(t, pool, workspaceID, "editor-b")
	editorC := seedExtraMember(t, pool, workspaceID, "editor-c")

	var autopilotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_id, execution_mode, created_by_type, created_by_id)
		VALUES ($1, 'transfer-ap', $2, 'run_only', 'member', $3) RETURNING id`,
		workspaceID, agentID, creatorA).Scan(&autopilotID); err != nil {
		t.Fatalf("seed autopilot: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID) })

	// Two triggers, both initially published by creator A (the create-site default).
	seedTrigger := func(cron string) string {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO autopilot_trigger (autopilot_id, kind, enabled, cron_expression, published_by_type, published_by_id)
			VALUES ($1, 'schedule', true, $2, 'member', $3) RETURNING id`,
			autopilotID, cron, creatorA).Scan(&id); err != nil {
			t.Fatalf("seed trigger: %v", err)
		}
		return id
	}
	trigger1 := seedTrigger("0 * * * *")
	trigger2 := seedTrigger("0 0 * * *")

	accountableOf := func(triggerID string) string {
		got := triggerOwnerAttribution(ctx, q,
			util.MustParseUUID(triggerID), util.MustParseUUID(workspaceID), util.MustParseUUID(autopilotID),
			attribution.EvidenceAutopilotRun, util.MustParseUUID(autopilotID))
		if got.Source != attribution.SourceTriggerOwner {
			t.Fatalf("trigger %s: source = %q, want trigger_owner", triggerID, got.Source)
		}
		if got.UserID.Valid {
			t.Fatalf("trigger_owner is audit-only; originator must stay NULL, got %s", util.UUIDToString(got.UserID))
		}
		return util.UUIDToString(got.AccountableUserID)
	}

	// Baseline: both triggers attribute to creator A.
	if a := accountableOf(trigger1); a != creatorA {
		t.Fatalf("trigger1 baseline accountable = %s, want creator %s", a, creatorA)
	}
	if a := accountableOf(trigger2); a != creatorA {
		t.Fatalf("trigger2 baseline accountable = %s, want creator %s", a, creatorA)
	}

	// B substantively edits trigger1 — the SAME query UpdateAutopilotTrigger runs.
	if err := q.SetAutopilotTriggerPublisher(ctx, db.SetAutopilotTriggerPublisherParams{
		ID:              util.MustParseUUID(trigger1),
		PublishedByType: pgtype.Text{String: "member", Valid: true},
		PublishedByID:   util.MustParseUUID(editorB),
	}); err != nil {
		t.Fatalf("SetAutopilotTriggerPublisher: %v", err)
	}

	// Transfer: trigger1 now attributes to editor B, NOT the original creator.
	if a := accountableOf(trigger1); a != editorB {
		t.Fatalf("after edit, trigger1 accountable = %s, want editor %s (responsibility must transfer)", a, editorB)
	}
	// Isolation: editing trigger1 must NOT move trigger2 — it stays with creator A.
	if a := accountableOf(trigger2); a != creatorA {
		t.Fatalf("trigger2 accountable = %s, want creator %s (editing a sibling must not transfer)", a, creatorA)
	}

	// C makes an autopilot-level substantive edit — the SAME bump-all query
	// UpdateAutopilot runs — which governs every trigger of the autopilot.
	if err := q.SetAutopilotTriggerPublishersByAutopilot(ctx, db.SetAutopilotTriggerPublishersByAutopilotParams{
		AutopilotID:     util.MustParseUUID(autopilotID),
		PublishedByType: pgtype.Text{String: "member", Valid: true},
		PublishedByID:   util.MustParseUUID(editorC),
	}); err != nil {
		t.Fatalf("SetAutopilotTriggerPublishersByAutopilot: %v", err)
	}
	if a := accountableOf(trigger1); a != editorC {
		t.Fatalf("after autopilot-level edit, trigger1 accountable = %s, want %s", a, editorC)
	}
	if a := accountableOf(trigger2); a != editorC {
		t.Fatalf("after autopilot-level edit, trigger2 accountable = %s, want %s", a, editorC)
	}
}

// TestEnqueueTaskForIssueAutopilotOriginStampsRuleOwner is the acceptance test for
// rule_owner (MUL-4302 §3.4): an autopilot-origin issue's run has NO authorizing
// human (originator_user_id stays NULL) but IS accountable to the publisher of the
// autopilot's active rule version, with rule_version_id recording the snapshot.
// This is the accountable-diverges-from-originator case.
func TestEnqueueTaskForIssueAutopilotOriginStampsRuleOwner(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, publisherID, agentID, _ := seedAttributionFixture(t, pool)

	// A synthetic autopilot id (no FK) with an active rule version published by the
	// member. gen_random_uuid() gives the autopilot id back so the issue can point
	// its origin at it.
	var ruleVersionID, autopilotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_rule_version (autopilot_id, workspace_id, published_by_type, published_by_id)
		VALUES (gen_random_uuid(), $1, 'member', $2) RETURNING id, autopilot_id`,
		workspaceID, publisherID).Scan(&ruleVersionID, &autopilotID); err != nil {
		t.Fatalf("seed rule version: %v", err)
	}

	var issueID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, priority, number, origin_type, origin_id)
		VALUES ($1, 'autopilot issue', 'agent', $2, 'agent', $2, 'medium', 9001, 'autopilot', $3) RETURNING id`,
		workspaceID, agentID, autopilotID).Scan(&issueID); err != nil {
		t.Fatalf("seed autopilot-origin issue: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	task, err := svc.EnqueueTaskForIssue(ctx, db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "agent",
		CreatorID:    util.MustParseUUID(agentID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		OriginType:   pgtype.Text{String: "autopilot", Valid: true},
		OriginID:     util.MustParseUUID(autopilotID),
	})
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue: %v", err)
	}

	var source pgtype.Text
	var originator, accountable, ruleVersion pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id, rule_version_id
		FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&source, &originator, &accountable, &ruleVersion); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}

	if source.String != string(attribution.SourceRuleOwner) {
		t.Errorf("originator_source = %q, want rule_owner", source.String)
	}
	if originator.Valid {
		t.Errorf("autopilot run must NOT set originator (authorization stays NULL), got %s", util.UUIDToString(originator))
	}
	if !accountable.Valid || accountable.Bytes != util.MustParseUUID(publisherID).Bytes {
		t.Errorf("accountable_user_id = %s, want rule publisher %s", util.UUIDToString(accountable), publisherID)
	}
	if !ruleVersion.Valid || ruleVersion.Bytes != util.MustParseUUID(ruleVersionID).Bytes {
		t.Errorf("rule_version_id = %s, want %s", util.UUIDToString(ruleVersion), ruleVersionID)
	}
}

// TestEnqueueTaskForIssueAutopilotOriginWithoutVersionDegrades verifies that an
// autopilot-origin issue whose autopilot has NO published rule version degrades to
// unattributed (never fabricating a human) rather than crashing or bypassing.
// TestEnqueueTaskForIssueAutopilotOriginWithoutVersionOwnerFallback: an
// autopilot-origin issue whose autopilot has no published rule version resolves to
// unattributed, then the default (non-fail-closed) workspace policy degrades it to
// owner_fallback — accountable = agent owner, originator still NULL — so no run is
// left without an accountable human (MUL-4302 §3.5).
func TestEnqueueTaskForIssueAutopilotOriginWithoutVersionOwnerFallback(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, ownerID, agentID, _ := seedAttributionFixture(t, pool)

	var issueID, autopilotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, priority, number, origin_type, origin_id)
		VALUES ($1, 'autopilot issue', 'agent', $2, 'agent', $2, 'medium', 9002, 'autopilot', gen_random_uuid()) RETURNING id, origin_id`,
		workspaceID, agentID).Scan(&issueID, &autopilotID); err != nil {
		t.Fatalf("seed autopilot-origin issue: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	task, err := svc.EnqueueTaskForIssue(ctx, db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "agent",
		CreatorID:    util.MustParseUUID(agentID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		OriginType:   pgtype.Text{String: "autopilot", Valid: true},
		OriginID:     util.MustParseUUID(autopilotID),
	})
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue: %v", err)
	}

	var source pgtype.Text
	var originator, accountable pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id FROM agent_task_queue WHERE id = $1`,
		task.ID).Scan(&source, &originator, &accountable); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}
	if source.String != string(attribution.SourceOwnerFallback) {
		t.Errorf("originator_source = %q, want owner_fallback", source.String)
	}
	if originator.Valid {
		t.Errorf("owner_fallback is audit-only; originator must stay NULL, got %s", util.UUIDToString(originator))
	}
	if !accountable.Valid || accountable.Bytes != util.MustParseUUID(ownerID).Bytes {
		t.Errorf("accountable_user_id = %s, want agent owner %s", util.UUIDToString(accountable), ownerID)
	}
}

// TestEnqueueTaskFailClosedRefusesUnattributed: with the workspace opted into
// fail-closed, an unattributable run (autopilot-origin issue, no rule version) is
// REFUSED at enqueue (ErrAttributionFailClosed) rather than degraded to
// owner_fallback (MUL-4302 §3.5).
func TestEnqueueTaskFailClosedRefusesUnattributed(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, _, agentID, _ := seedAttributionFixture(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE workspace SET attribution_fail_closed = TRUE WHERE id = $1`, workspaceID); err != nil {
		t.Fatalf("set fail-closed: %v", err)
	}

	var issueID, autopilotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, priority, number, origin_type, origin_id)
		VALUES ($1, 'autopilot issue', 'agent', $2, 'agent', $2, 'medium', 9003, 'autopilot', gen_random_uuid()) RETURNING id, origin_id`,
		workspaceID, agentID).Scan(&issueID, &autopilotID); err != nil {
		t.Fatalf("seed autopilot-origin issue: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	_, err := svc.EnqueueTaskForIssue(ctx, db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "agent",
		CreatorID:    util.MustParseUUID(agentID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		OriginType:   pgtype.Text{String: "autopilot", Valid: true},
		OriginID:     util.MustParseUUID(autopilotID),
	})
	if !errors.Is(err, ErrAttributionFailClosed) {
		t.Fatalf("EnqueueTaskForIssue error = %v, want ErrAttributionFailClosed", err)
	}
	// No task row should have been created for the issue.
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 0 {
		t.Errorf("fail-closed must not enqueue any task, found %d", count)
	}
}

// seedRunOnlyAutopilot creates an active run_only autopilot (agent-assigned) plus a
// running autopilot_run for it, and returns their ids. Used to exercise
// dispatchRunOnly's direct CreateAutopilotTask path.
func seedRunOnlyAutopilot(t *testing.T, pool *pgxpool.Pool, workspaceID, agentID, creatorID string) (autopilotID, runID string) {
	t.Helper()
	ctx := context.Background()
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_type, assignee_id, status, execution_mode, created_by_type, created_by_id)
		VALUES ($1, 'run-only ap', 'agent', $2, 'active', 'run_only', 'member', $3) RETURNING id`,
		workspaceID, agentID, creatorID).Scan(&autopilotID); err != nil {
		t.Fatalf("seed autopilot: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID) })
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_run (autopilot_id, source, status) VALUES ($1, 'manual', 'running') RETURNING id`,
		autopilotID).Scan(&runID); err != nil {
		t.Fatalf("seed autopilot run: %v", err)
	}
	return autopilotID, runID
}

// TestDispatchRunOnlyScheduleStampsRuleOwnerRow is the run_only row assertion Elon
// asked for: the direct CreateAutopilotTask path (no member actor → schedule-like)
// must persist rule_owner on the queue row — originator NULL, accountable = the
// active rule version publisher, rule_version_id set.
func TestDispatchRunOnlyScheduleStampsRuleOwnerRow(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, publisherID, agentID, _ := seedAttributionFixture(t, pool)
	autopilotID, runID := seedRunOnlyAutopilot(t, pool, workspaceID, agentID, publisherID)

	var ruleVersionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_rule_version (autopilot_id, workspace_id, published_by_type, published_by_id)
		VALUES ($1, $2, 'member', $3) RETURNING id`, autopilotID, workspaceID, publisherID).Scan(&ruleVersionID); err != nil {
		t.Fatalf("seed rule version: %v", err)
	}

	svc := &AutopilotService{Queries: q, TxStarter: pool, Bus: events.New(), TaskSvc: &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}}
	ap, err := q.GetAutopilot(ctx, util.MustParseUUID(autopilotID))
	if err != nil {
		t.Fatalf("get autopilot: %v", err)
	}
	run, err := q.GetAutopilotRun(ctx, util.MustParseUUID(runID))
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	// No member actor → schedule/webhook-style rule_owner attribution.
	if err := svc.dispatchRunOnly(ctx, ap, &run, pgtype.UUID{}); err != nil {
		t.Fatalf("dispatchRunOnly: %v", err)
	}

	var source pgtype.Text
	var originator, accountable, ruleVersion pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id, rule_version_id
		FROM agent_task_queue WHERE autopilot_run_id = $1`, run.ID).Scan(&source, &originator, &accountable, &ruleVersion); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}
	if source.String != string(attribution.SourceRuleOwner) {
		t.Errorf("originator_source = %q, want rule_owner", source.String)
	}
	if originator.Valid {
		t.Errorf("run_only autopilot must NOT set originator, got %s", util.UUIDToString(originator))
	}
	if !accountable.Valid || accountable.Bytes != util.MustParseUUID(publisherID).Bytes {
		t.Errorf("accountable_user_id = %s, want publisher %s", util.UUIDToString(accountable), publisherID)
	}
	if !ruleVersion.Valid || ruleVersion.Bytes != util.MustParseUUID(ruleVersionID).Bytes {
		t.Errorf("rule_version_id = %s, want %s", util.UUIDToString(ruleVersion), ruleVersionID)
	}
}

// TestDispatchRunOnlyManualStampsDirectHuman verifies the blocking-finding fix on the
// run_only path: a MANUAL trigger attributes direct_human to the triggering member —
// originator == accountable == actor, no rule_version — even when the autopilot has a
// published rule owned by someone else (MUL-4302 §4).
func TestDispatchRunOnlyManualStampsDirectHuman(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, publisherID, agentID, _ := seedAttributionFixture(t, pool)
	autopilotID, runID := seedRunOnlyAutopilot(t, pool, workspaceID, agentID, publisherID)

	// A rule version published by the creator exists; the manual actor is a
	// DIFFERENT member, who must win.
	if _, err := pool.Exec(ctx, `
		INSERT INTO autopilot_rule_version (autopilot_id, workspace_id, published_by_type, published_by_id)
		VALUES ($1, $2, 'member', $3)`, autopilotID, workspaceID, publisherID); err != nil {
		t.Fatalf("seed rule version: %v", err)
	}
	var actorID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Trigger', $1) RETURNING id`,
		fmt.Sprintf("trigger-%d@multica.test", time.Now().UnixNano())).Scan(&actorID); err != nil {
		t.Fatalf("seed actor: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, actorID) })
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		workspaceID, actorID); err != nil {
		t.Fatalf("seed actor member: %v", err)
	}

	svc := &AutopilotService{Queries: q, TxStarter: pool, Bus: events.New(), TaskSvc: &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}}
	ap, err := q.GetAutopilot(ctx, util.MustParseUUID(autopilotID))
	if err != nil {
		t.Fatalf("get autopilot: %v", err)
	}
	run, err := q.GetAutopilotRun(ctx, util.MustParseUUID(runID))
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if err := svc.dispatchRunOnly(ctx, ap, &run, util.MustParseUUID(actorID)); err != nil {
		t.Fatalf("dispatchRunOnly: %v", err)
	}

	var source pgtype.Text
	var originator, accountable, ruleVersion pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id, rule_version_id
		FROM agent_task_queue WHERE autopilot_run_id = $1`, run.ID).Scan(&source, &originator, &accountable, &ruleVersion); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}
	if source.String != string(attribution.SourceDirectHuman) {
		t.Errorf("originator_source = %q, want direct_human", source.String)
	}
	if !originator.Valid || originator.Bytes != util.MustParseUUID(actorID).Bytes {
		t.Errorf("originator_user_id = %s, want triggering member %s", util.UUIDToString(originator), actorID)
	}
	if !accountable.Valid || accountable.Bytes != originator.Bytes {
		t.Errorf("accountable_user_id = %s, want == originator (actor)", util.UUIDToString(accountable))
	}
	if ruleVersion.Valid {
		t.Errorf("manual direct_human must not set rule_version_id, got %s", util.UUIDToString(ruleVersion))
	}
}

// TestDispatchRunOnlyScheduleTransfersToEditor is Elon's must-fix REAL dispatch test:
// it drives dispatchRunOnly end to end (schedule-style, no member actor) and asserts
// the PERSISTED agent_task_queue row's accountable_user_id follows the trigger's
// current responsible publisher after a substantive edit — the creator seeds it, then
// a later editor's re-stamp (the same SetAutopilotTriggerPublisher the UpdateTrigger
// handler runs) makes future runs attribute to the editor, with originator still NULL
// (MUL-4302). The resolver-level before/after and per-trigger isolation are covered by
// TestTriggerOwnerAttribution_TransfersToSubstantiveEditor.
func TestDispatchRunOnlyScheduleTransfersToEditor(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, creatorA, agentID, _ := seedAttributionFixture(t, pool)
	editorB := seedExtraMember(t, pool, workspaceID, "dispatch-editor-b")

	var autopilotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_type, assignee_id, status, execution_mode, created_by_type, created_by_id)
		VALUES ($1, 'dispatch-transfer-ap', 'agent', $2, 'active', 'run_only', 'member', $3) RETURNING id`,
		workspaceID, agentID, creatorA).Scan(&autopilotID); err != nil {
		t.Fatalf("seed autopilot: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID) })

	// Schedule trigger initially published by creator A, then edited by B.
	var triggerID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_trigger (autopilot_id, kind, enabled, cron_expression, published_by_type, published_by_id)
		VALUES ($1, 'schedule', true, '0 * * * *', 'member', $2) RETURNING id`,
		autopilotID, creatorA).Scan(&triggerID); err != nil {
		t.Fatalf("seed trigger: %v", err)
	}
	if err := q.SetAutopilotTriggerPublisher(ctx, db.SetAutopilotTriggerPublisherParams{
		ID:              util.MustParseUUID(triggerID),
		PublishedByType: pgtype.Text{String: "member", Valid: true},
		PublishedByID:   util.MustParseUUID(editorB),
	}); err != nil {
		t.Fatalf("SetAutopilotTriggerPublisher: %v", err)
	}

	// A running schedule run bound to that trigger — the shape the scheduler dispatches.
	var runID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_run (autopilot_id, trigger_id, source, status)
		VALUES ($1, $2, 'schedule', 'running') RETURNING id`,
		autopilotID, triggerID).Scan(&runID); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	svc := &AutopilotService{Queries: q, TxStarter: pool, Bus: events.New(), TaskSvc: &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}}
	ap, err := q.GetAutopilot(ctx, util.MustParseUUID(autopilotID))
	if err != nil {
		t.Fatalf("get autopilot: %v", err)
	}
	run, err := q.GetAutopilotRun(ctx, util.MustParseUUID(runID))
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	// No member actor → schedule/webhook-style trigger_owner attribution.
	if err := svc.dispatchRunOnly(ctx, ap, &run, pgtype.UUID{}); err != nil {
		t.Fatalf("dispatchRunOnly: %v", err)
	}

	var source pgtype.Text
	var originator, accountable pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id
		FROM agent_task_queue WHERE autopilot_run_id = $1`, run.ID).Scan(&source, &originator, &accountable); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}
	if source.String != string(attribution.SourceTriggerOwner) {
		t.Errorf("originator_source = %q, want trigger_owner", source.String)
	}
	if originator.Valid {
		t.Errorf("schedule dispatch must NOT set originator, got %s", util.UUIDToString(originator))
	}
	if !accountable.Valid || accountable.Bytes != util.MustParseUUID(editorB).Bytes {
		t.Errorf("accountable_user_id = %s, want editor %s (dispatch must follow the transferred publisher, not creator %s)",
			util.UUIDToString(accountable), editorB, creatorA)
	}
}

// TestEnqueueTaskForIssueAutopilotManualStampsDirectHuman verifies the manual fix on
// the create_issue path: enqueuing an autopilot-origin issue WITH a triggering actor
// (as dispatchCreateIssue does for a manual trigger) attributes direct_human to that
// actor, not rule_owner — the actor override wins over the autopilot-origin branch.
func TestEnqueueTaskForIssueAutopilotManualStampsDirectHuman(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, publisherID, agentID, _ := seedAttributionFixture(t, pool)

	var autopilotID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO autopilot_rule_version (autopilot_id, workspace_id, published_by_type, published_by_id)
		VALUES (gen_random_uuid(), $1, 'member', $2) RETURNING autopilot_id`,
		workspaceID, publisherID).Scan(&autopilotID); err != nil {
		t.Fatalf("seed rule version: %v", err)
	}
	var issueID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, priority, number, origin_type, origin_id)
		VALUES ($1, 'autopilot issue', 'agent', $2, 'agent', $2, 'medium', 9101, 'autopilot', $3) RETURNING id`,
		workspaceID, agentID, autopilotID).Scan(&issueID); err != nil {
		t.Fatalf("seed autopilot-origin issue: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	// A distinct triggering member (not the rule publisher) manually triggers.
	var actorID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Trigger', $1) RETURNING id`,
		fmt.Sprintf("trig2-%d@multica.test", time.Now().UnixNano())).Scan(&actorID); err != nil {
		t.Fatalf("seed actor: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, actorID) })

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	// dispatchCreateIssue routes a manual trigger through the actor-carrying enqueue.
	task, err := svc.EnqueueTaskForIssueWithHandoff(ctx, db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "agent",
		CreatorID:    util.MustParseUUID(agentID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		OriginType:   pgtype.Text{String: "autopilot", Valid: true},
		OriginID:     util.MustParseUUID(autopilotID),
	}, "", util.MustParseUUID(actorID))
	if err != nil {
		t.Fatalf("EnqueueTaskForIssueWithHandoff: %v", err)
	}

	var source pgtype.Text
	var originator, accountable, ruleVersion pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id, rule_version_id
		FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&source, &originator, &accountable, &ruleVersion); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}
	if source.String != string(attribution.SourceDirectHuman) {
		t.Errorf("originator_source = %q, want direct_human", source.String)
	}
	if !originator.Valid || originator.Bytes != util.MustParseUUID(actorID).Bytes {
		t.Errorf("originator_user_id = %s, want actor %s", util.UUIDToString(originator), actorID)
	}
	if !accountable.Valid || accountable.Bytes != originator.Bytes {
		t.Errorf("accountable_user_id = %s, want == originator (actor)", util.UUIDToString(accountable))
	}
	if ruleVersion.Valid {
		t.Errorf("manual direct_human must not set rule_version_id, got %s", util.UUIDToString(ruleVersion))
	}
}

// TestRecordAutopilotRuleVersionRepublishReattributes verifies the final Phase 1
// item (MUL-4302 §3.4): republishing a rule (as a trigger edit / archive / system
// pause does) appends a new version, the LATEST version is the active one, and
// dispatch attribution follows it. So editing member A's autopilot as member B
// re-attributes subsequent runs to B; a system pause records a 'system' publisher.
func TestRecordAutopilotRuleVersionRepublishReattributes(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, memberA, agentID, _ := seedAttributionFixture(t, pool)
	autopilotID, _ := seedRunOnlyAutopilot(t, pool, workspaceID, agentID, memberA)

	var memberB string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Editor', $1) RETURNING id`,
		fmt.Sprintf("editor-%d@multica.test", time.Now().UnixNano())).Scan(&memberB); err != nil {
		t.Fatalf("seed member B: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberB) })
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		workspaceID, memberB); err != nil {
		t.Fatalf("seed member B membership: %v", err)
	}

	ap, err := q.GetAutopilot(ctx, util.MustParseUUID(autopilotID))
	if err != nil {
		t.Fatalf("get autopilot: %v", err)
	}
	verParams := db.GetActiveAutopilotRuleVersionParams{WorkspaceID: ap.WorkspaceID, AutopilotID: ap.ID}

	// v1: creator (member A) publishes; active + dispatch attribute to A.
	if err := RecordAutopilotRuleVersion(ctx, q, ap, "member", util.MustParseUUID(memberA)); err != nil {
		t.Fatalf("record v1: %v", err)
	}
	active, err := q.GetActiveAutopilotRuleVersion(ctx, verParams)
	if err != nil || active.PublishedByType != "member" || active.PublishedByID.Bytes != util.MustParseUUID(memberA).Bytes {
		t.Fatalf("v1 active = %+v (err %v), want member A", active, err)
	}

	// v2: member B republishes (e.g. edited a trigger) → latest wins.
	if err := RecordAutopilotRuleVersion(ctx, q, ap, "member", util.MustParseUUID(memberB)); err != nil {
		t.Fatalf("record v2: %v", err)
	}
	attr := ruleOwnerAttribution(ctx, q, ap.WorkspaceID, ap.ID, attribution.EvidenceAutopilotRun, ap.ID)
	if attr.Source != attribution.SourceRuleOwner || attr.AccountableUserID.Bytes != util.MustParseUUID(memberB).Bytes {
		t.Errorf("after republish, dispatch attribution = %+v, want rule_owner accountable = member B", attr)
	}

	// v3: system auto-pause records a 'system' publisher (no member id).
	if err := RecordAutopilotRuleVersion(ctx, q, ap, "system", pgtype.UUID{}); err != nil {
		t.Fatalf("record v3 (system): %v", err)
	}
	active, err = q.GetActiveAutopilotRuleVersion(ctx, verParams)
	if err != nil || active.PublishedByType != "system" || active.PublishedByID.Valid {
		t.Errorf("v3 active = %+v (err %v), want system publisher with NULL id", active, err)
	}
	// A system-published version has no member → dispatch degrades to unattributed
	// (never fabricates a human).
	sysAttr := ruleOwnerAttribution(ctx, q, ap.WorkspaceID, ap.ID, attribution.EvidenceAutopilotRun, ap.ID)
	if sysAttr.Source != attribution.SourceUnattributed || sysAttr.AccountableUserID.Valid {
		t.Errorf("system-published version must yield unattributed, got %+v", sysAttr)
	}
}

// TestApplyAttributionFallbackRefusesOnMissingOwner: an unattributed run in an
// OPEN (non-fail-closed) workspace whose agent has no valid owner cannot resolve an
// accountable human via owner_fallback, so the enqueue is refused rather than
// creating a NULL-accountable task (MUL-4302 §3.5, Elon must-fix 1).
func TestApplyAttributionFallbackRefusesOnMissingOwner(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, _, _, _ := seedAttributionFixture(t, pool) // default policy = open

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	unattr := attribution.Unattributed(attribution.EvidenceIssueAssignment, util.MustParseUUID(workspaceID))
	_, err := svc.applyAttributionFallback(ctx, unattr, db.Agent{WorkspaceID: util.MustParseUUID(workspaceID)}) // OwnerID zero
	if !errors.Is(err, ErrAttributionFailClosed) {
		t.Fatalf("missing owner: err = %v, want ErrAttributionFailClosed", err)
	}
}

// TestApplyAttributionFallbackRefusesOnPolicyReadFailure: if the workspace policy
// cannot be read for an unattributed run, fail CLOSED (refuse) rather than silently
// running an unattributable task — even when a valid owner is present (Elon must-fix 1).
func TestApplyAttributionFallbackRefusesOnPolicyReadFailure(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	_, ownerID, _, _ := seedAttributionFixture(t, pool)

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	unattr := attribution.Unattributed(attribution.EvidenceIssueAssignment, pgtype.UUID{})
	missingWs := pgtype.UUID{Bytes: [16]byte{0xDE, 0xAD, 0xBE, 0xEF}, Valid: true} // no such workspace
	_, err := svc.applyAttributionFallback(ctx, unattr, db.Agent{WorkspaceID: missingWs, OwnerID: util.MustParseUUID(ownerID)})
	if !errors.Is(err, ErrAttributionFailClosed) {
		t.Fatalf("policy read failure: err = %v, want ErrAttributionFailClosed", err)
	}
}

// TestApplyAttributionFallbackPreciseUntouched: a precise attribution never reads
// the policy and passes through unchanged (proven with a nil-Queries service).
func TestApplyAttributionFallbackPreciseUntouched(t *testing.T) {
	svc := &TaskService{} // no Queries: a policy read would panic/error if attempted
	precise := attribution.DirectHumanRun(pgtype.UUID{Bytes: [16]byte{0x11}, Valid: true}, attribution.EvidenceComment, pgtype.UUID{})
	got, err := svc.applyAttributionFallback(context.Background(), precise, db.Agent{})
	if err != nil {
		t.Fatalf("precise attribution must not error: %v", err)
	}
	if got.Source != attribution.SourceDirectHuman || got != precise {
		t.Errorf("precise attribution must pass through unchanged, got %+v", got)
	}
}

// TestRerunIssueAttributesToRerunningMember is the §5 acceptance test: a manual
// rerun is a NEW direct_human trigger attributed to the member who re-ran — not the
// original run's human — and records rerun_of_task_id lineage back to the source.
func TestRerunIssueAttributesToRerunningMember(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, creatorID, agentID, issueID := seedAttributionFixture(t, pool)

	// A distinct member performs the rerun.
	var rerunnerID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Rerunner', $1) RETURNING id`,
		fmt.Sprintf("rerunner-%d@multica.test", time.Now().UnixNano())).Scan(&rerunnerID); err != nil {
		t.Fatalf("seed rerunner: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, rerunnerID) })
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		workspaceID, rerunnerID); err != nil {
		t.Fatalf("seed rerunner member: %v", err)
	}

	issueStruct := db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(creatorID),
		WorkspaceID:  util.MustParseUUID(workspaceID),
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	}
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	// The original run, attributed to the issue creator.
	orig, err := svc.EnqueueTaskForIssue(ctx, issueStruct)
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue (original): %v", err)
	}

	// The rerun, performed by a different member.
	task, err := svc.RerunIssue(ctx, util.MustParseUUID(issueID), orig.ID, pgtype.UUID{}, util.MustParseUUID(rerunnerID), nil)
	if err != nil {
		t.Fatalf("RerunIssue: %v", err)
	}

	var source pgtype.Text
	var originator, accountable, rerunOf pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id, rerun_of_task_id
		FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&source, &originator, &accountable, &rerunOf); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}
	if source.String != string(attribution.SourceDirectHuman) {
		t.Errorf("originator_source = %q, want direct_human", source.String)
	}
	if !originator.Valid || originator.Bytes != util.MustParseUUID(rerunnerID).Bytes {
		t.Errorf("originator_user_id = %s, want rerunner %s (not creator %s)", util.UUIDToString(originator), rerunnerID, creatorID)
	}
	if !accountable.Valid || accountable.Bytes != originator.Bytes {
		t.Errorf("accountable_user_id = %s, want == originator (rerunner)", util.UUIDToString(accountable))
	}
	if !rerunOf.Valid || rerunOf.Bytes != orig.ID.Bytes {
		t.Errorf("rerun_of_task_id = %s, want original task %s", util.UUIDToString(rerunOf), util.UUIDToString(orig.ID))
	}
}

// TestEnqueueChatTaskStampsChatEvidence verifies the chat enqueue path is no
// longer a NULL-source bypass and uses the UNIFORM evidence pair: the sender is a
// direct_human originator+accountable, and evidence is (kind=chat,
// ref=chat_session_id) so the attribution UI links to the conversation the same
// way it does for autopilot_run / issue_assignment (MUL-4302 §2, Elon 2nd-round).
func TestEnqueueChatTaskStampsChatEvidence(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, chatSessionID) })

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	task, err := svc.EnqueueChatTask(ctx, db.ChatSession{
		ID:      util.MustParseUUID(chatSessionID),
		AgentID: util.MustParseUUID(agentID),
	}, util.MustParseUUID(userID), false)
	if err != nil {
		t.Fatalf("EnqueueChatTask: %v", err)
	}

	var source, evidenceKind pgtype.Text
	var originator, accountable, evidenceRef pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, originator_user_id, accountable_user_id, trigger_evidence_kind, trigger_evidence_ref_id
		FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&source, &originator, &accountable, &evidenceKind, &evidenceRef); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}

	if source.String != string(attribution.SourceDirectHuman) {
		t.Errorf("originator_source = %q, want direct_human", source.String)
	}
	if !originator.Valid || originator.Bytes != util.MustParseUUID(userID).Bytes {
		t.Errorf("originator_user_id = %s, want sender %s", util.UUIDToString(originator), userID)
	}
	if !accountable.Valid || accountable.Bytes != originator.Bytes {
		t.Errorf("accountable_user_id = %s, want == originator", util.UUIDToString(accountable))
	}
	if evidenceKind.String != string(attribution.EvidenceChat) {
		t.Errorf("trigger_evidence_kind = %q, want chat", evidenceKind.String)
	}
	if !evidenceRef.Valid || evidenceRef.Bytes != util.MustParseUUID(chatSessionID).Bytes {
		t.Errorf("trigger_evidence_ref_id = %s, want chat session %s", util.UUIDToString(evidenceRef), chatSessionID)
	}
}
