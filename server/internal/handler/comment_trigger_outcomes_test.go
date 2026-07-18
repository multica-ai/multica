package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func findCommentOutcome(t *testing.T, outcomes []CommentTriggerOutcome, targetID string) CommentTriggerOutcome {
	t.Helper()
	for _, o := range outcomes {
		if o.TargetID == targetID {
			return o
		}
	}
	t.Fatalf("no trigger outcome for target %s in %+v", targetID, outcomes)
	return CommentTriggerOutcome{}
}

// TestCreateComment_MixedMentionSurfacesPartialTriggerOutcomes is the MUL-4525 §2
// acceptance test for Bohan's exact scenario: a comment @mentions an agent the
// author can invoke AND a squad whose private leader they cannot. The comment is
// still saved (one blocked mention must not reject it), and the response carries
// per-target outcomes — queued for the allowed agent, blocked +
// invocation_not_allowed for the squad — so the client can show partial success
// instead of a silent no-op. The preview surfaces the same split before sending.
func TestCreateComment_MixedMentionSurfacesPartialTriggerOutcomes(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	allowedAgentID := createHandlerTestAgent(t, "Outcome Allowed Agent", nil)
	// A private leader owned by someone other than testUserID: the workspace
	// owner can VIEW it but cannot INVOKE it (no admin bypass).
	privateLeaderID, _, _ := privateAgentTestFixture(t)
	squadID := createCommentTriggerPreviewSquad(t, "Outcome Private Squad", privateLeaderID)
	issueID := createCommentTriggerPreviewIssue(t, "mixed mention partial outcomes", "", "")

	content := fmt.Sprintf(
		"[@Allowed](mention://agent/%s) [@Squad](mention://squad/%s) please take a look",
		allowedAgentID, squadID,
	)

	// Preview surfaces both the allowed agent and the blocked squad.
	preview := previewCommentTriggersForTest(t, issueID, map[string]any{"content": content})
	requirePreviewAgents(t, preview, allowedAgentID)
	if len(preview.Blocked) != 1 {
		t.Fatalf("preview blocked = %+v, want 1 entry", preview.Blocked)
	}
	if b := preview.Blocked[0]; b.TargetType != "squad" || b.TargetID != squadID ||
		b.Status != DispatchBlocked || b.ReasonCode != ReasonInvocationNotAllowed {
		t.Fatalf("preview blocked[0] = %+v, want squad %s blocked/invocation_not_allowed", b, squadID)
	}

	// Create the comment: it must save (201) and report partial outcomes.
	w := httptest.NewRecorder()
	r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": content}), "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201 (comment must save despite blocked mention), got %d: %s", w.Code, w.Body.String())
	}
	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	if resp.ID == "" {
		t.Fatal("comment was not saved")
	}
	if len(resp.TriggerOutcomes) != 2 {
		t.Fatalf("trigger_outcomes = %+v, want 2 (one queued, one blocked)", resp.TriggerOutcomes)
	}

	allowed := findCommentOutcome(t, resp.TriggerOutcomes, allowedAgentID)
	if allowed.TargetType != "agent" || allowed.Status != DispatchQueued {
		t.Errorf("allowed outcome = %+v, want agent/queued", allowed)
	}
	blocked := findCommentOutcome(t, resp.TriggerOutcomes, squadID)
	if blocked.TargetType != "squad" || blocked.Status != DispatchBlocked || blocked.ReasonCode != ReasonInvocationNotAllowed {
		t.Errorf("blocked outcome = %+v, want squad/blocked/invocation_not_allowed", blocked)
	}

	// The allowed agent ran; the private leader was never enqueued.
	if got := countQueuedCommentTriggerTasks(t, issueID, allowedAgentID); got != 1 {
		t.Errorf("allowed agent queued tasks = %d, want 1", got)
	}
	var leaderTasks int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, issueID, privateLeaderID).Scan(&leaderTasks); err != nil {
		t.Fatalf("count leader tasks: %v", err)
	}
	if leaderTasks != 0 {
		t.Errorf("blocked private leader tasks = %d, want 0", leaderTasks)
	}
}

// TestCreateComment_BlockedMentionReasonDoesNotEnumeratePrivateAgent pins the
// enumeration-safety rule (MUL-4525 §2): a mention the author cannot invoke and a
// mention of a truly nonexistent agent both return the same generic
// invocation_not_allowed, so a blocked reason can never confirm a private
// agent's existence.
func TestCreateComment_BlockedMentionReasonDoesNotEnumeratePrivateAgent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	privateAgentID, _, _ := privateAgentTestFixture(t)
	issueID := createCommentTriggerPreviewIssue(t, "blocked mention enumeration safety", "", "")
	nonexistentID := "00000000-0000-0000-0000-0000000000ff"

	content := fmt.Sprintf(
		"[@Private](mention://agent/%s) [@Ghost](mention://agent/%s) ping",
		privateAgentID, nonexistentID,
	)
	preview := previewCommentTriggersForTest(t, issueID, map[string]any{"content": content})
	if len(preview.Agents) != 0 {
		t.Fatalf("preview agents = %+v, want none", preview.Agents)
	}
	if len(preview.Blocked) != 2 {
		t.Fatalf("preview blocked = %+v, want 2", preview.Blocked)
	}
	for _, b := range preview.Blocked {
		if b.ReasonCode != ReasonInvocationNotAllowed {
			t.Errorf("blocked %s reason = %q, want invocation_not_allowed (must not distinguish private-exists from not-found)", b.TargetID, b.ReasonCode)
		}
	}
}

// TestCreateComment_AgentAndSameLeaderSquad is Elon's round-3 must-fix 1
// acceptance test: when a comment names BOTH @Agent A and @Squad S whose leader
// is A, the run coalesces to ONE task that carries the LEADER role
// (is_leader_task + squad_id=S, so the daemon injects S's briefing) regardless
// of mention order, and each explicitly-named target still gets its own outcome
// (MUL-4525). The old first-mention-wins dedup could drop the leader role when
// @Agent A came first — this asserts the role independent of order.
func TestCreateComment_AgentAndSameLeaderSquad(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	cases := []struct {
		name    string
		content func(agentID, squadID string) string
	}{
		{"agent mention first", func(a, s string) string {
			return fmt.Sprintf("[@A](mention://agent/%s) [@S](mention://squad/%s) please", a, s)
		}},
		{"squad mention first", func(a, s string) string {
			return fmt.Sprintf("[@S](mention://squad/%s) [@A](mention://agent/%s) please", s, a)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agentID := createHandlerTestAgent(t, "Shared Leader Agent "+tc.name, nil)
			squadID := createCommentTriggerPreviewSquad(t, "Shared Leader Squad "+tc.name, agentID)
			issueID := createCommentTriggerPreviewIssue(t, "same-leader "+tc.name, "", "")

			w := httptest.NewRecorder()
			r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": tc.content(agentID, squadID)}), "id", issueID)
			testHandler.CreateComment(w, r)
			if w.Code != http.StatusCreated {
				t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
			}
			var resp CommentResponse
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode comment: %v", err)
			}

			// One coalesced execution carrying the leader role for squad S.
			var taskCount int
			var isLeader bool
			var taskSquadID string
			if err := testPool.QueryRow(ctx, `
				SELECT count(*), COALESCE(bool_or(is_leader_task), false), COALESCE(max(squad_id::text), '')
				FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
			`, issueID, agentID).Scan(&taskCount, &isLeader, &taskSquadID); err != nil {
				t.Fatalf("read task role: %v", err)
			}
			if taskCount != 1 {
				t.Fatalf("queued tasks = %d, want 1 (coalesced execution)", taskCount)
			}
			if !isLeader {
				t.Errorf("execution is_leader_task = false, want true (leader role must win regardless of order)")
			}
			if taskSquadID != squadID {
				t.Errorf("execution squad_id = %q, want %q", taskSquadID, squadID)
			}

			// Two outcomes — one per explicitly-named target — both queued.
			if len(resp.TriggerOutcomes) != 2 {
				t.Fatalf("trigger_outcomes = %+v, want 2 (agent + squad)", resp.TriggerOutcomes)
			}
			if o := findCommentOutcome(t, resp.TriggerOutcomes, agentID); o.TargetType != "agent" || o.Status != DispatchQueued {
				t.Errorf("agent outcome = %+v, want agent/queued", o)
			}
			if o := findCommentOutcome(t, resp.TriggerOutcomes, squadID); o.TargetType != "squad" || o.Status != DispatchQueued {
				t.Errorf("squad outcome = %+v, want squad/queued", o)
			}
		})
	}
}

// TestCreateComment_TwoSquadsSharingLeaderCoalescesNonWinner is Elon's round-3
// must-fix 1 (multi-squad case): two DIFFERENT squads share the same leader and
// both are @mentioned. The single leader agent runs ONCE carrying one squad's
// context; the other squad's mention folds into that run and is reported
// coalesced — never a second task, and never both reported queued (MUL-4525).
func TestCreateComment_TwoSquadsSharingLeaderCoalescesNonWinner(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	leaderID := createHandlerTestAgent(t, "Two-Squad Shared Leader", nil)
	squad1 := createCommentTriggerPreviewSquad(t, "Two-Squad S1", leaderID)
	squad2 := createCommentTriggerPreviewSquad(t, "Two-Squad S2", leaderID)
	issueID := createCommentTriggerPreviewIssue(t, "two squads sharing leader", "", "")
	content := fmt.Sprintf("[@S1](mention://squad/%s) [@S2](mention://squad/%s) please", squad1, squad2)

	w := httptest.NewRecorder()
	r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": content}), "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode comment: %v", err)
	}

	// Exactly one queued task (a single leader agent can only run once).
	if got := countQueuedCommentTriggerTasks(t, issueID, leaderID); got != 1 {
		t.Fatalf("queued tasks = %d, want 1", got)
	}
	// Two squad outcomes; exactly one queued (the executed squad) and one
	// coalesced (the folded squad) — never both queued.
	if len(resp.TriggerOutcomes) != 2 {
		t.Fatalf("trigger_outcomes = %+v, want 2", resp.TriggerOutcomes)
	}
	statuses := map[DispatchStatus]int{}
	for _, o := range resp.TriggerOutcomes {
		if o.TargetType != "squad" {
			t.Errorf("outcome %+v: want squad target", o)
		}
		statuses[o.Status]++
	}
	if statuses[DispatchQueued] != 1 || statuses[DispatchCoalesced] != 1 {
		t.Errorf("outcome statuses = %v, want exactly 1 queued + 1 coalesced (not both queued)", statuses)
	}
}

// TestCreateComment_SquadLeaderSelfMentionCompletedTaskDoesNotFakeSuccess is
// Elon's round-3 must-fix 2: a squad leader's own @mention of its squad is
// suppressed by the self-trigger guard, but when its latest task is already
// TERMINAL (no active run), the outcome must NOT be a success-shaped `deferred`
// — it is a non-success `blocked/self_trigger_suppressed`, and no new task runs.
func TestCreateComment_SquadLeaderSelfMentionCompletedTaskDoesNotFakeSuccess(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	leaderID := createHandlerTestAgent(t, "Self-Mention Leader", nil)
	squadID := createCommentTriggerPreviewSquad(t, "Self-Mention Squad", leaderID)
	issueID := createCommentTriggerPreviewIssue(t, "self-mention completed task", "", "")

	// The leader's latest task on this issue is a COMPLETED leader task: the
	// self-trigger guard suppresses (latest role is leader) AND there is no
	// active run to cover the comment.
	var completedTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, is_leader_task, squad_id, started_at, completed_at)
		VALUES ($1, $2, 'completed', 0, $3, true, $4, now(), now())
		RETURNING id
	`, leaderID, handlerTestRuntimeID(t), issueID, squadID).Scan(&completedTaskID); err != nil {
		t.Fatalf("seed completed leader task: %v", err)
	}

	content := fmt.Sprintf("[@S](mention://squad/%s) revisit please", squadID)
	w := httptest.NewRecorder()
	r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": content}), "id", issueID)
	// Author the comment AS the leader agent (A2A self-mention).
	r.Header.Set("X-Agent-ID", leaderID)
	r.Header.Set("X-Task-ID", completedTaskID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode comment: %v", err)
	}

	// The self-mention neither re-fired the leader nor is covered by an active
	// run: the outcome is non-success, never a fake `deferred`.
	if len(resp.TriggerOutcomes) != 1 {
		t.Fatalf("trigger_outcomes = %+v, want 1", resp.TriggerOutcomes)
	}
	o := resp.TriggerOutcomes[0]
	if o.TargetType != "squad" || o.TargetID != squadID {
		t.Fatalf("outcome target = %+v, want squad %s", o, squadID)
	}
	if o.Status != DispatchBlocked || o.ReasonCode != ReasonSelfTriggerSuppressed {
		t.Errorf("outcome = %+v, want blocked/self_trigger_suppressed (must not fake success)", o)
	}
	// No new task was enqueued (only the pre-seeded completed one exists).
	var total int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, issueID, leaderID).Scan(&total); err != nil {
		t.Fatalf("count leader tasks: %v", err)
	}
	if total != 1 {
		t.Errorf("leader tasks = %d, want 1 (self-mention suppressed, no new task)", total)
	}
}
