package service

import (
	"context"
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
func TestEnqueueTaskForIssueAutopilotOriginWithoutVersionDegrades(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, _, agentID, _ := seedAttributionFixture(t, pool)

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
	var accountable pgtype.UUID
	if err := pool.QueryRow(ctx, `
		SELECT originator_source, accountable_user_id FROM agent_task_queue WHERE id = $1`,
		task.ID).Scan(&source, &accountable); err != nil {
		t.Fatalf("read stored attribution: %v", err)
	}
	if source.String != string(attribution.SourceUnattributed) {
		t.Errorf("originator_source = %q, want unattributed (no rule version)", source.String)
	}
	if accountable.Valid {
		t.Errorf("no rule version must not fabricate an accountable human, got %s", util.UUIDToString(accountable))
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
