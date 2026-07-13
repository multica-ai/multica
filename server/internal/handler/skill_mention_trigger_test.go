package handler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// skillMentionFixture wires the seeded workspace-visible agent ("Handler Test
// Agent") to a workspace skill so we can exercise the @skill mention path on
// the computeCommentAgentTriggers → resolveMentionedAgentCommentTriggers
// → resolveSkillMentionTrigger chain. The tests below lock in the U7 contract:
//
//   - @skill mention with a frontend-resolved agent enqueues that agent
//   - @skill mention without metadata falls back to the agent_skill lookup
//   - issue assignee with the skill wins the tie (R13)
//   - dedup prevents double-triggering against a pending task
//   - multiple @skill mentions resolve independently
//   - unknown / invalid skill IDs do not crash the comment handler
type skillMentionFixture struct {
	JID        string
	RuntimeID  string
	IssueID    string
	Issue      db.Issue
	CommentID  string
	Comment    db.Comment
	SkillID    string
	SecondSkillID string
	// OtherAgentID is a second handler-test agent used for the
	// "multiple skill mentions in one comment" scenario.
	OtherAgentID string
	OtherRuntime string
}

// newSkillMentionFixture creates one issue + a few skill bindings plus the
// extra agent the multi-skill test needs. Cleanup is wired through t.Cleanup
// so each sub-test gets a fresh fixture.
func newSkillMentionFixture(t *testing.T) skillMentionFixture {
	t.Helper()
	ctx := context.Background()

	// Load the seeded "Handler Test Agent" — same one the other mention
	// tests reuse, so we know it has a runtime and a workspace invocation
	// target (MUL-3963).
	var jID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&jID); err != nil {
		t.Fatalf("load seeded agent: %v", err)
	}
	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, jID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}

	// Pick a per-workspace issue number so we don't collide with other
	// tests' fixtures.
	var number int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
		WHERE id = $1 RETURNING issue_counter
	`, testWorkspaceID).Scan(&number); err != nil {
		t.Fatalf("next issue number: %v", err)
	}

	// The issue is unassigned so the resolveSkillMentionTrigger falls
	// through to the agent_skill lookup rather than taking the
	// routeAssigneeFallback shortcut. Tests that want to assert the
	// assignee-wins-the-tie path update the assignee explicitly after
	// creating the fixture.
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title, number)
		VALUES ($1, 'member', $2, $3, $4)
		RETURNING id
	`, testWorkspaceID, testUserID, "skill mention test", number).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	skillA := insertHandlerTestSkill(t, "skill-mention-a", "skill A")
	skillB := insertHandlerTestSkill(t, "skill-mention-b", "skill B")

	// Bind skillA to the seeded agent J. This is what the @skill mention
	// will resolve to when no frontend routing is supplied.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)
	`, jID, skillA); err != nil {
		t.Fatalf("bind skillA to J: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_skill WHERE agent_id = $1 AND skill_id = $2`, jID, skillA)
	})

	// Create a second agent for the multi-skill scenario. Skill B is bound
	// only to this other agent, so a comment that mentions both skills
	// must enqueue two distinct triggers.
	otherAgentID := createHandlerTestAgent(t, "Handler Skill Other", nil)
	otherRuntime := handlerTestRuntimeID(t)
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)
	`, otherAgentID, skillB); err != nil {
		t.Fatalf("bind skillB to other agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_skill WHERE agent_id = $1 AND skill_id = $2`, otherAgentID, skillB)
	})

	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	return skillMentionFixture{
		JID:           jID,
		RuntimeID:     runtimeID,
		IssueID:       issueID,
		Issue:         issue,
		SkillID:       skillA,
		SecondSkillID: skillB,
		OtherAgentID:  otherAgentID,
		OtherRuntime:  otherRuntime,
	}
}

// insertSkillMentionComment writes a comment whose content is the supplied
// mention text. The author is the seeded member (testUserID) — skill mentions
// from members are the user flow the plan calls out as the AE2 case.
func insertSkillMentionComment(t *testing.T, issueID, content string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, $4)
		RETURNING id
	`, testWorkspaceID, issueID, testUserID, content).Scan(&id); err != nil {
		t.Fatalf("insert skill mention comment: %v", err)
	}
	return id
}

// triggerSkillMentions drives the full enqueue path that production uses, so
// these integration tests assert end-to-end enqueue side effects (not just
// what resolveSkillMentionTrigger returns). Mirrors
// enqueueMentionedAgentTasksForTest.
func triggerSkillMentions(t *testing.T, ctx context.Context, fx skillMentionFixture, commentID string, skillAgents map[string]pgtype.UUID) {
	t.Helper()
	comment, err := testHandler.Queries.GetComment(ctx, parseUUID(commentID))
	if err != nil {
		t.Fatalf("load comment: %v", err)
	}
	triggers := testHandler.computeCommentAgentTriggers(ctx, fx.Issue, comment.Content, nil, "member", testUserID, commentTriggerComputeOptions{
		SkillMentionAgents: skillAgents,
	})
	testHandler.enqueueCommentAgentTriggers(ctx, fx.Issue, comment.ID, triggers)
}

// TestEnqueueSkillMention_UsesFrontendResolvedAgent proves that when the
// frontend's smart routing supplies a target agent via skill_mention_agents,
// the backend enqueues exactly that agent — even if it differs from the
// agent_skill table's first binding. This is the R12 happy path.
func TestEnqueueSkillMention_UsesFrontendResolvedAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSkillMentionFixture(t)

	commentID := insertSkillMentionComment(t, fx.IssueID,
		"[@SkillA](mention://skill/"+fx.SkillID+") please review")

	// Frontend pre-resolves to "other" (not J, even though J has the
	// binding). Backend must honor this mapping and enqueue "other".
	resolved := parseUUIDForTest(t, fx.OtherAgentID)
	triggerSkillMentions(t, ctx, fx, commentID, map[string]pgtype.UUID{
		fx.SkillID: resolved,
	})

	if got := countQueuedOrDispatched(t, fx.OtherAgentID, fx.IssueID); got != 1 {
		t.Fatalf("expected 1 queued task on frontend-resolved agent, got %d", got)
	}
	if got := countQueuedOrDispatched(t, fx.JID, fx.IssueID); got != 0 {
		t.Fatalf("expected 0 queued tasks on agent_skill binding (frontend overrode it), got %d", got)
	}
}

// TestEnqueueSkillMention_FallsBackToAgentSkillBinding is the no-metadata
// path: when the frontend does not supply skill_mention_agents, the backend
// must resolve via the agent_skill junction table. The seeded binding is J,
// so we expect exactly one queued task on J.
func TestEnqueueSkillMention_FallsBackToAgentSkillBinding(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSkillMentionFixture(t)

	commentID := insertSkillMentionComment(t, fx.IssueID,
		"[@SkillA](mention://skill/"+fx.SkillID+") please review")

	triggerSkillMentions(t, ctx, fx, commentID, nil)

	if got := countQueuedOrDispatched(t, fx.JID, fx.IssueID); got != 1 {
		t.Fatalf("expected 1 queued task on agent_skill binding, got %d", got)
	}
}

// TestEnqueueSkillMention_AssigneeWithSkillWins covers R13 in its
// competition form: two agents have the skill, the issue's assignee is one
// of them, and the assignee must be selected over the other candidate.
func TestEnqueueSkillMention_AssigneeWithSkillWins(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSkillMentionFixture(t)

	// Bind skill A to BOTH J and the other agent.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)
	`, fx.OtherAgentID, fx.SkillID); err != nil {
		t.Fatalf("bind skillA to other agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent_skill WHERE agent_id = $1 AND skill_id = $2`,
			fx.OtherAgentID, fx.SkillID)
	})

	// Make J the issue assignee — J must win the tie.
	if _, err := testPool.Exec(ctx, `
		UPDATE issue SET assignee_type = 'agent', assignee_id = $1 WHERE id = $2
	`, fx.JID, fx.IssueID); err != nil {
		t.Fatalf("assign issue to J: %v", err)
	}
	fx.Issue, _ = testHandler.Queries.GetIssue(ctx, parseUUID(fx.IssueID))

	commentID := insertSkillMentionComment(t, fx.IssueID,
		"[@SkillA](mention://skill/"+fx.SkillID+") please review")

	triggerSkillMentions(t, ctx, fx, commentID, nil)

	if got := countQueuedOrDispatched(t, fx.JID, fx.IssueID); got != 1 {
		t.Fatalf("expected 1 queued task on assignee J, got %d", got)
	}
	if got := countQueuedOrDispatched(t, fx.OtherAgentID, fx.IssueID); got != 0 {
		t.Fatalf("expected 0 queued tasks on non-assignee binding (other), got %d", got)
	}
}

// TestEnqueueSkillMention_DedupesAgainstPendingTask locks in that the
// hasPendingTaskForIssueAndAgent dedupe applies to the new skill branch
// just like it does for @agent / @squad mentions. A queued task must block
// a second queued task; the second enqueue must report AlreadyPending=true.
func TestEnqueueSkillMention_DedupesAgainstPendingTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSkillMentionFixture(t)

	// Seed a queued task on J — simulates a previous @agent or @skill
	// mention that already enqueued against this issue.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status)
		VALUES ($1, $2, $3, 'queued')
	`, fx.JID, fx.RuntimeID, fx.IssueID); err != nil {
		t.Fatalf("seed queued task: %v", err)
	}

	commentID := insertSkillMentionComment(t, fx.IssueID,
		"[@SkillA](mention://skill/"+fx.SkillID+") please review")

	triggerSkillMentions(t, ctx, fx, commentID, nil)

	if got := countQueuedOrDispatched(t, fx.JID, fx.IssueID); got != 1 {
		t.Fatalf("expected dedupe (still 1 queued task), got %d", got)
	}
}

// TestEnqueueSkillMention_MultipleSkillMentionsIndependent verifies that two
// @skill mentions in one comment each enqueue independently — one queued
// task per distinct resolved agent, no merging across skills.
func TestEnqueueSkillMention_MultipleSkillMentionsIndependent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSkillMentionFixture(t)

	content := "[@SkillA](mention://skill/" + fx.SkillID + ") and " +
		"[@SkillB](mention://skill/" + fx.SecondSkillID + ") please review"
	commentID := insertSkillMentionComment(t, fx.IssueID, content)

	triggerSkillMentions(t, ctx, fx, commentID, nil)

	// J has skill A → 1 task on J.
	if got := countQueuedOrDispatched(t, fx.JID, fx.IssueID); got != 1 {
		t.Fatalf("expected 1 queued task on J (skill A), got %d", got)
	}
	// Other agent has skill B → 1 task on other.
	if got := countQueuedOrDispatched(t, fx.OtherAgentID, fx.IssueID); got != 1 {
		t.Fatalf("expected 1 queued task on other (skill B), got %d", got)
	}
}

// TestEnqueueSkillMention_UnknownSkillIDIsNoCrash confirms the failure
// contract: a mention whose skill ID does not exist in this workspace must
// not abort the trigger computation, must not enqueue a task, and must not
// surface as a 5xx to the comment writer.
func TestEnqueueSkillMention_UnknownSkillIDIsNoCrash(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSkillMentionFixture(t)

	bogusID := "00000000-0000-0000-0000-000000000000"
	commentID := insertSkillMentionComment(t, fx.IssueID,
		"[@Unknown](mention://skill/"+bogusID+")")

	// Must not panic.
	triggerSkillMentions(t, ctx, fx, commentID, nil)

	if got := countQueuedOrDispatched(t, fx.JID, fx.IssueID); got != 0 {
		t.Fatalf("expected 0 queued tasks on bogus skill, got %d", got)
	}
	if got := countQueuedOrDispatched(t, fx.OtherAgentID, fx.IssueID); got != 0 {
		t.Fatalf("expected 0 queued tasks on bogus skill, got %d", got)
	}
}

// TestEnqueueSkillMention_UnboundSkillSilentlyDropped covers the R13 edge
// case: a real skill exists, but no agent has it bound in the workspace.
// The mention must be silently dropped — same contract as @member mentions —
// and no error must leak through the trigger computation.
func TestEnqueueSkillMention_UnboundSkillSilentlyDropped(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSkillMentionFixture(t)

	// Skill C is created but never bound to any agent.
	skillC := insertHandlerTestSkill(t, "skill-mention-c", "no bindings")

	commentID := insertSkillMentionComment(t, fx.IssueID,
		"[@SkillC](mention://skill/"+skillC+")")

	triggerSkillMentions(t, ctx, fx, commentID, nil)

	if got := countQueuedOrDispatched(t, fx.JID, fx.IssueID); got != 0 {
		t.Fatalf("expected 0 queued tasks on unbound skill, got %d on J", got)
	}
	if got := countQueuedOrDispatched(t, fx.OtherAgentID, fx.IssueID); got != 0 {
		t.Fatalf("expected 0 queued tasks on unbound skill, got %d on other", got)
	}
}

// parseUUIDForTest wraps parseUUID so the test file does not have to import
// the util package directly for a single call site.
func parseUUIDForTest(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u := parseUUID(s)
	if !u.Valid {
		t.Fatalf("parseUUIDForTest(%q): invalid uuid", s)
	}
	return u
}