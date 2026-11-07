package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// conditionFixture is wakeFixture plus cleanup for the timeline entries rules
// write.
func conditionFixture(t *testing.T) (principalFixture, *IssueWakeupService, pgtype.UUID, string) {
	t.Helper()
	f, s, issue, agent := wakeFixture(t)
	f.Cleanup(t, "DELETE FROM activity_log WHERE issue_id=$1", issue)
	return f, s, issue, agent
}

// wakeTick makes the rule due and dispatches it, as the scheduler would.
func wakeTick(t *testing.T, f principalFixture, s *IssueWakeupService, id pgtype.UUID) db.IssueWakeup {
	t.Helper()
	f.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()-interval '1 second' WHERE id=$1 AND next_fire_at IS NOT NULL", id)
	w, err := f.q.LocklessWakeup(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	wakeDispatch(t, s, w)
	w, err = f.q.LocklessWakeup(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func wakeRuns(t *testing.T, f principalFixture, id pgtype.UUID) int {
	t.Helper()
	return f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE context->>'wakeup_id'=$1", util.UUIDToString(id))
}

func condition(t *testing.T, v map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestWakeupConditionFieldValueWakesOnceWhenItHolds(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Review when ready",
		Condition: condition(t, map[string]any{"type": "issue_field", "field": "status", "value": "in_review"})})
	if len(w.EventTypes) != 1 || w.EventTypes[0] != "issue.status_changed" || w.Mode != "once" {
		t.Fatalf("condition rule = %+v", w)
	}
	// Not yet true: no run, no matter how often it is evaluated.
	wakeTick(t, f, s, w.ID)
	wakeSetStatus(t, f, issue, "in_progress")
	wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 0 {
		t.Fatalf("unmet condition started %d runs", n)
	}
	// The matching status change is a hint; the platform evaluates it.
	wakeSetStatus(t, f, issue, "in_review")
	got := wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 1 {
		t.Fatalf("met condition started %d runs, want 1", n)
	}
	if got.Enabled {
		t.Fatal("a once condition must be consumed")
	}
	task, err := f.q.GetAgentTask(context.Background(), got.LastTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(task.HandoffNote.String, "condition.met") || !strings.Contains(task.HandoffNote.String, `"status":"in_review"`) {
		t.Fatalf("trigger facts missing the observation: %s", task.HandoffNote.String)
	}
	// Status-change hints never reach the agent as separate facts.
	if strings.Contains(task.HandoffNote.String, "issue.status_changed") {
		t.Fatalf("hint leaked into the run: %s", task.HandoffNote.String)
	}
	if n := f.Count(t, "SELECT count(*) FROM activity_log WHERE issue_id=$1 AND action='wakeup_triggered'", issue); n != 1 {
		t.Fatalf("trigger timeline entries = %d, want 1", n)
	}
	if n := f.Count(t, "SELECT count(*) FROM activity_log WHERE issue_id=$1 AND action='wakeup_created'", issue); n != 1 {
		t.Fatalf("created timeline entries = %d, want 1", n)
	}
}

// Cross-issue conditions have no event hints. Their stored predicates must be
// checked through ListReadyWakeups, including after an unmet scheduled check.
func TestWakeupConditionOtherIssueUsesScheduledChecks(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	ctx := context.Background()
	workspace := parseTestUUID(t, f.WorkspaceID)
	other := f.Issue(t, "Waiting for review", testutil.Cols{"status": "in_progress"})
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Continue after review",
		Condition: condition(t, map[string]any{"type": "other_issue", "issue_id": other, "state": "in_review"})})
	if len(w.EventTypes) != 0 || !w.NextFireAt.Valid {
		t.Fatalf("poll-only rule: events=%v next_fire_at=%+v", w.EventTypes, w.NextFireAt)
	}
	tick := func() db.IssueWakeup {
		t.Helper()
		if err := s.TickWorkspaces(ctx, workspace); err != nil {
			t.Fatal(err)
		}
		got, err := f.q.LocklessWakeup(ctx, w.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	got := tick()
	if !got.Enabled || got.FireCount != 0 || wakeRuns(t, f, w.ID) != 0 {
		t.Fatalf("unmet condition: enabled=%t fire_count=%d", got.Enabled, got.FireCount)
	}
	if !got.NextFireAt.Valid || !got.NextFireAt.Time.After(w.NextFireAt.Time) {
		t.Fatalf("unmet condition was not scheduled for another check: %+v", got.NextFireAt)
	}
	// Hold the next check in the future without sleeping or relying on the
	// wall-clock poll interval. Changing the watched issue emits no hint here.
	f.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()+interval '1 hour' WHERE id=$1", w.ID)
	wakeSetStatus(t, f, parseTestUUID(t, other), "in_review")
	if n := f.Count(t, "SELECT count(*) FROM issue_wakeup_receipt WHERE wakeup_id=$1 AND processed_at IS NULL", w.ID); n != 0 {
		t.Fatalf("cross-issue change created %d event hints", n)
	}
	if active, err := f.q.HasActiveTaskForIssue(ctx, issue); err != nil || active {
		t.Fatalf("waiting issue has active work: active=%t err=%v", active, err)
	}
	got = tick()
	if got.FireCount != 0 || wakeRuns(t, f, w.ID) != 0 {
		t.Fatal("condition fired before its scheduled check")
	}
	// Only the persisted due time can make this rule a scheduler candidate.
	f.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()-interval '1 second' WHERE id=$1", w.ID)
	got = tick()
	if got.Enabled || got.FireCount != 1 || got.LastError.Valid || wakeRuns(t, f, w.ID) != 1 {
		t.Fatalf("due condition: enabled=%t fire_count=%d last_error=%+v", got.Enabled, got.FireCount, got.LastError)
	}
	task, err := f.q.GetAgentTask(ctx, got.LastTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(task.HandoffNote.String, "condition.met") || !strings.Contains(task.HandoffNote.String, other) || !strings.Contains(task.HandoffNote.String, `"status":"in_review"`) {
		t.Fatalf("cross-issue observation missing from the run: %s", task.HandoffNote.String)
	}
	got = tick()
	if got.FireCount != 1 || wakeRuns(t, f, w.ID) != 1 {
		t.Fatal("once condition fired again on a repeated scheduler pass")
	}
}

// A coordinator can wait for a child's blocker handoff: state=blocked matches
// only the blocked status, so a review handoff on the same child does not fire it.
func TestWakeupConditionOtherIssueBlocked(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	ctx := context.Background()
	workspace := parseTestUUID(t, f.WorkspaceID)
	other := f.Issue(t, "Child that may block", testutil.Cols{"status": "in_progress"})
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Resolve the child's blocker",
		Condition: condition(t, map[string]any{"type": "other_issue", "issue_id": other, "state": "blocked"})})
	tick := func() db.IssueWakeup {
		t.Helper()
		f.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()-interval '1 second' WHERE id=$1 AND next_fire_at IS NOT NULL", w.ID)
		if err := s.TickWorkspaces(ctx, workspace); err != nil {
			t.Fatal(err)
		}
		got, err := f.q.LocklessWakeup(ctx, w.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	wakeSetStatus(t, f, parseTestUUID(t, other), "in_review")
	if got := tick(); !got.Enabled || got.FireCount != 0 || wakeRuns(t, f, w.ID) != 0 {
		t.Fatalf("in_review fired a blocked rule: enabled=%t fire_count=%d", got.Enabled, got.FireCount)
	}
	wakeSetStatus(t, f, parseTestUUID(t, other), "blocked")
	got := tick()
	if got.Enabled || got.FireCount != 1 || wakeRuns(t, f, w.ID) != 1 {
		t.Fatalf("blocked child: enabled=%t fire_count=%d last_error=%+v", got.Enabled, got.FireCount, got.LastError)
	}
	task, err := f.q.GetAgentTask(ctx, got.LastTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(task.HandoffNote.String, other) || !strings.Contains(task.HandoffNote.String, `"status":"blocked"`) {
		t.Fatalf("blocked observation missing from the run: %s", task.HandoffNote.String)
	}
}

func TestWakeupConditionOtherIssueRejectsUnknownState(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	other := f.Issue(t, "Watched", testutil.Cols{"status": "todo"})
	_, err := s.Create(context.Background(), issue, parseTestUUID(t, f.UserID), pgtype.UUID{}, WakeupInput{AgentID: agent, Kind: "event", Instruction: "x",
		Condition: condition(t, map[string]any{"type": "other_issue", "issue_id": other, "state": "in_progress"})})
	if err == nil || !strings.Contains(err.Error(), "state must be done, ended, in_review or blocked") {
		t.Fatalf("unknown state error = %v", err)
	}
}

func TestWakeupConditionRepeatsOnlyWhenItBecomesTrueAgain(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	label := f.Insert(t, "issue_label", testutil.Cols{"workspace_id": f.WorkspaceID, "name": "approved", "color": "#123456"})
	f.Cleanup(t, "DELETE FROM issue_to_label WHERE label_id=$1", label)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", Instruction: "Ship it",
		Condition: condition(t, map[string]any{"type": "issue_field", "field": "label", "label_id": label})})
	if !w.MaxFires.Valid || w.MaxFires.Int32 != wakeupDefaultMaxFires {
		t.Fatalf("repeating rule cap = %+v, want default", w.MaxFires)
	}
	f.Exec(t, "INSERT INTO issue_to_label(issue_id,label_id) VALUES($1,$2)", issue, label)
	wakeTick(t, f, s, w.ID)
	wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 1 {
		t.Fatalf("a condition that stays true fired %d times, want 1", n)
	}
	f.Exec(t, "UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE context->>'wakeup_id'=$1", util.UUIDToString(w.ID))
	f.Exec(t, "DELETE FROM issue_to_label WHERE issue_id=$1 AND label_id=$2", issue, label)
	wakeTick(t, f, s, w.ID)
	f.Exec(t, "INSERT INTO issue_to_label(issue_id,label_id) VALUES($1,$2)", issue, label)
	got := wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 2 {
		t.Fatalf("re-satisfied condition fired %d times total, want 2", n)
	}
	if !got.Enabled || got.FireCount != 2 {
		t.Fatalf("continuous rule after two fires: enabled=%t fire_count=%d", got.Enabled, got.FireCount)
	}
}

func TestWakeupConditionPRConflictsWakesOnEachNewConflict(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	s.Tasks.PRSnapshotsEnabled = true
	pr := f.Insert(t, "github_pull_request", testutil.Cols{
		"workspace_id": f.WorkspaceID, "installation_id": 1,
		"repo_owner": "fixture", "repo_name": "conflicts", "pr_number": 7,
		"title": "PR", "state": "open", "html_url": "https://example.test/pr/7",
		"pr_created_at": "2026-01-01T00:00:00Z", "pr_updated_at": "2026-01-01T00:00:00Z",
		"head_sha": "a", "snapshot_head_sha": "a", "api_mergeable": "MERGEABLE",
	})
	f.Cleanup(t, "DELETE FROM issue_pull_request WHERE pull_request_id=$1", pr)
	f.Exec(t, "INSERT INTO issue_pull_request(issue_id,pull_request_id) VALUES($1,$2)", issue, pr)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", Instruction: "Resolve the conflict",
		Condition: condition(t, map[string]any{"type": "pull_request", "event": "conflicts"})})
	finishRuns := func() {
		f.Exec(t, "UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE context->>'wakeup_id'=$1 AND status<>'completed'", util.UUIDToString(w.ID))
	}
	snapshot := func(head, snapshotHead, mergeable string) db.IssueWakeup {
		t.Helper()
		f.Exec(t, "UPDATE github_pull_request SET head_sha=$2,snapshot_head_sha=$3,api_mergeable=$4 WHERE id=$1", pr, head, snapshotHead, mergeable)
		return wakeTick(t, f, s, w.ID)
	}
	expectRuns := func(want int, step string) {
		t.Helper()
		if n := wakeRuns(t, f, w.ID); n != want {
			t.Fatalf("%s: %d runs, want %d", step, n, want)
		}
	}

	snapshot("a", "a", "MERGEABLE")
	expectRuns(0, "mergeable")
	got := snapshot("a", "a", "CONFLICTING")
	expectRuns(1, "conflict")
	task, err := f.q.GetAgentTask(context.Background(), got.LastTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(task.HandoffNote.String, `"mergeable":"conflicting"`) || !strings.Contains(task.HandoffNote.String, `"head_sha":"a"`) {
		t.Fatalf("conflict facts missing from the run: %s", task.HandoffNote.String)
	}
	finishRuns()
	// GitHub recomputing after another base move is not a new conflict.
	snapshot("a", "a", "UNKNOWN")
	snapshot("a", "a", "CONFLICTING")
	expectRuns(1, "recompute")
	// The agent pushed a fix; until the new head is fetched the old verdict
	// does not count, and the pending snapshot is visible on the rule.
	got = snapshot("b", "a", "CONFLICTING")
	expectRuns(1, "pushed head awaiting a snapshot")
	if !got.LastError.Valid || got.LastError.String != conditionDiagnosticPrefix+errPRSnapshotPending.Error() {
		t.Fatalf("pending snapshot diagnostic = %+v", got.LastError)
	}
	snapshot("b", "b", "MERGEABLE")
	expectRuns(1, "resolved")
	got = snapshot("b", "b", "CONFLICTING")
	expectRuns(2, "conflict again")
	if !got.Enabled || got.FireCount != 2 || got.LastError.Valid {
		t.Fatalf("continuous rule after two conflicts: enabled=%t fire_count=%d error=%+v", got.Enabled, got.FireCount, got.LastError)
	}
	finishRuns()
	f.Exec(t, "UPDATE github_pull_request SET state='merged' WHERE id=$1", pr)
	wakeTick(t, f, s, w.ID)
	expectRuns(2, "merged PR")
}

func TestWakeupConditionPRConflictsAcrossSeveralPRs(t *testing.T) {
	for _, tc := range []struct {
		name       string
		sameNumber bool
		wantRuns   int
	}{
		{"resolving one PR does not re-wake for the other", false, 1},
		{"PRs in different repositories keep distinct identities", true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, s, issue, agent := conditionFixture(t)
			s.Tasks.PRSnapshotsEnabled = true
			insertPR := func(repo string, number int, mergeable string) string {
				pr := f.Insert(t, "github_pull_request", testutil.Cols{
					"workspace_id": f.WorkspaceID, "installation_id": 1,
					"repo_owner": "fixture", "repo_name": repo, "pr_number": number,
					"title": "PR", "state": "open", "html_url": "https://example.test/" + repo,
					"pr_created_at": "2026-01-01T00:00:00Z", "pr_updated_at": "2026-01-01T00:00:00Z",
					"head_sha": "shared-head", "snapshot_head_sha": "shared-head", "api_mergeable": mergeable,
				})
				f.Cleanup(t, "DELETE FROM issue_pull_request WHERE pull_request_id=$1", pr)
				f.Exec(t, "INSERT INTO issue_pull_request(issue_id,pull_request_id) VALUES($1,$2)", issue, pr)
				return pr
			}
			a := insertPR("repo-a", 1, "CONFLICTING")
			bNumber, bMergeable := 2, "CONFLICTING"
			if tc.sameNumber {
				// A fork's PR can share both the number and the head commit.
				bNumber, bMergeable = 1, "MERGEABLE"
			}
			b := insertPR("repo-b", bNumber, bMergeable)
			w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", Instruction: "Resolve conflicts",
				Condition: condition(t, map[string]any{"type": "pull_request", "event": "conflicts"})})
			wakeTick(t, f, s, w.ID)
			if n := wakeRuns(t, f, w.ID); n != 1 {
				t.Fatalf("initial conflict runs = %d, want 1", n)
			}
			f.Exec(t, "UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE context->>'wakeup_id'=$1", util.UUIDToString(w.ID))
			f.Exec(t, "UPDATE github_pull_request SET api_mergeable='MERGEABLE' WHERE id=$1", a)
			if tc.sameNumber {
				f.Exec(t, "UPDATE github_pull_request SET api_mergeable='CONFLICTING' WHERE id=$1", b)
			}
			wakeTick(t, f, s, w.ID)
			if n := wakeRuns(t, f, w.ID); n != tc.wantRuns {
				t.Fatalf("after the conflict moved runs = %d, want %d", n, tc.wantRuns)
			}
		})
	}
}

// Checks and merged keys carry only the PR number, so same-numbered PRs from
// two repositories share a key. The second one satisfying still wakes.
func TestWakeupConditionPRSharedNumberWakesForEachPR(t *testing.T) {
	for _, event := range []string{"merged", "checks_finished"} {
		t.Run(event, func(t *testing.T) {
			f, s, issue, agent := conditionFixture(t)
			s.Tasks.PRSnapshotsEnabled = true
			insertPR := func(repo string) string {
				pr := f.Insert(t, "github_pull_request", testutil.Cols{
					"workspace_id": f.WorkspaceID, "installation_id": 1,
					"repo_owner": "fixture", "repo_name": repo, "pr_number": 7,
					"title": "PR", "state": "open", "html_url": "https://example.test/" + repo,
					"pr_created_at": "2026-01-01T00:00:00Z", "pr_updated_at": "2026-01-01T00:00:00Z",
					"head_sha": "shared-head", "snapshot_head_sha": "shared-head", "checks_rollup_state": "PENDING",
				})
				f.Cleanup(t, "DELETE FROM issue_pull_request WHERE pull_request_id=$1", pr)
				f.Exec(t, "INSERT INTO issue_pull_request(issue_id,pull_request_id) VALUES($1,$2)", issue, pr)
				return pr
			}
			a, b := insertPR("repo-a"), insertPR("repo-b")
			w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", Instruction: "Follow each pull request",
				Condition: condition(t, map[string]any{"type": "pull_request", "event": event})})
			wakeTick(t, f, s, w.ID)
			if n := wakeRuns(t, f, w.ID); n != 0 {
				t.Fatalf("before either PR satisfies: %d runs, want 0", n)
			}
			satisfy := func(pr string) {
				if event == "merged" {
					f.Exec(t, "UPDATE github_pull_request SET state='merged' WHERE id=$1", pr)
				} else {
					f.Exec(t, "UPDATE github_pull_request SET checks_rollup_state='SUCCESS' WHERE id=$1", pr)
				}
				wakeTick(t, f, s, w.ID)
			}
			satisfy(a)
			if n := wakeRuns(t, f, w.ID); n != 1 {
				t.Fatalf("after the first PR: %d runs, want 1", n)
			}
			f.Exec(t, "UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE context->>'wakeup_id'=$1", util.UUIDToString(w.ID))
			satisfy(b)
			if n := wakeRuns(t, f, w.ID); n != 2 {
				t.Fatalf("after the second PR: %d runs, want 2", n)
			}
		})
	}
}

func TestWakeupConditionChildrenAndOtherIssue(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	first := f.Issue(t, "Stage 1 child", testutil.Cols{"parent_issue_id": issue, "status": "todo", "stage": 1})
	f.Issue(t, "Stage 2 child", testutil.Cols{"parent_issue_id": issue, "status": "todo", "stage": 2})
	other := f.Issue(t, "Blocking issue", testutil.Cols{"status": "in_progress"})
	stage := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Start stage 2",
		Condition: condition(t, map[string]any{"type": "children_done", "stage": 1})})
	watch := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Continue after the blocker",
		Condition: condition(t, map[string]any{"type": "other_issue", "issue_id": other, "state": "done"})})
	wakeTick(t, f, s, stage.ID)
	wakeTick(t, f, s, watch.ID)
	if wakeRuns(t, f, stage.ID)+wakeRuns(t, f, watch.ID) != 0 {
		t.Fatal("unmet conditions started runs")
	}
	var watched WakeupCondition
	_ = json.Unmarshal(watch.Condition, &watched)
	if !strings.Contains(watched.Identifier, "-") {
		t.Fatalf("watched issue identifier not recorded: %s", watch.Condition)
	}
	f.Exec(t, "UPDATE issue SET status='cancelled' WHERE id=$1", first)
	f.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", other)
	wakeTick(t, f, s, stage.ID)
	wakeTick(t, f, s, watch.ID)
	// Both hold for the same agent: the second waits for the first one's run
	// and joins it when it is claimed, instead of queuing another behind it.
	if wakeRuns(t, f, stage.ID) != 1 || wakeRuns(t, f, watch.ID) != 0 {
		t.Fatalf("stage runs=%d, other-issue runs=%d, want one shared run", wakeRuns(t, f, stage.ID), wakeRuns(t, f, watch.ID))
	}
	var shared string
	f.QueryRow(t, "SELECT id FROM agent_task_queue WHERE context->>'wakeup_id'=$1", util.UUIDToString(stage.ID)).Scan(&shared)
	if notes := wakeClaim(t, f, s, shared); !strings.Contains(notes, "Continue after the blocker") || !strings.Contains(notes, `"state":"done"`) {
		t.Fatalf("shared run lacks the joined rule: %q", notes)
	}
}

func TestWakeupConditionPullRequestChecksWaitForNewResult(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	s.Tasks.PRSnapshotsEnabled = true
	ctx := context.Background()
	var pr string
	if err := f.Pool.QueryRow(ctx, `INSERT INTO github_pull_request(workspace_id,installation_id,repo_owner,repo_name,pr_number,title,state,html_url,pr_created_at,pr_updated_at,snapshot_head_sha,checks_rollup_state)
		VALUES($1,1,'multica-ai','wakeup-test',(extract(epoch from clock_timestamp())*1000)::bigint % 100000,'PR','open','https://example.test/pr',now(),now(),'aaa','SUCCESS') RETURNING id`, f.WorkspaceID).Scan(&pr); err != nil {
		t.Fatal(err)
	}
	f.Cleanup(t, "DELETE FROM github_pull_request WHERE id=$1", pr)
	f.Cleanup(t, "DELETE FROM issue_pull_request WHERE pull_request_id=$1", pr)
	f.Exec(t, "INSERT INTO issue_pull_request(issue_id,pull_request_id) VALUES($1,$2)", issue, pr)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Look at CI",
		Condition: condition(t, map[string]any{"type": "pull_request", "event": "checks_finished"})})
	// Finished checks from before registration belong to an older push.
	wakeTick(t, f, s, w.ID)
	f.Exec(t, "UPDATE github_pull_request SET snapshot_head_sha='bbb',checks_rollup_state='PENDING' WHERE id=$1", pr)
	wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 0 {
		t.Fatalf("stale or pending checks started %d runs", n)
	}
	f.Exec(t, "UPDATE github_pull_request SET checks_rollup_state='FAILURE' WHERE id=$1", pr)
	got := wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 1 {
		t.Fatalf("finished checks started %d runs, want 1", n)
	}
	task, err := f.q.GetAgentTask(ctx, got.LastTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(task.HandoffNote.String, `"checks":"failure"`) || !strings.Contains(task.HandoffNote.String, `"head_sha":"bbb"`) {
		t.Fatalf("PR facts missing: %s", task.HandoffNote.String)
	}
}

func TestWakeupConditionValidation(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	ctx := context.Background()
	member := parseTestUUID(t, f.UserID)
	for name, in := range map[string]WakeupInput{
		"unknown status":    {Condition: condition(t, map[string]any{"type": "issue_field", "field": "status", "value": "nope"})},
		"this issue":        {Condition: condition(t, map[string]any{"type": "other_issue", "issue_id": util.UUIDToString(issue), "state": "done"})},
		"unknown field":     {Condition: condition(t, map[string]any{"type": "issue_field", "field": "title", "value": "x"})},
		"extra key":         {Condition: condition(t, map[string]any{"type": "children_done", "when": "soon"})},
		"with events":       {EventTypes: []string{"comment.created"}, Condition: condition(t, map[string]any{"type": "children_done"})},
		"time kind":         {Kind: "at", AfterSeconds: 60, Condition: condition(t, map[string]any{"type": "children_done"})},
		"cap on once":       {MaxFires: 3, Condition: condition(t, map[string]any{"type": "children_done"})},
		"foreign assignee":  {Condition: condition(t, map[string]any{"type": "issue_field", "field": "assignee", "assignee_type": "agent", "assignee_id": util.UUIDToString(parseTestUUID(t, "00000000-0000-0000-0000-000000000001"))})},
		"property archived": {Condition: condition(t, map[string]any{"type": "issue_field", "field": "property", "property_id": "00000000-0000-0000-0000-000000000002", "value": "x"})},
	} {
		in.AgentID, in.Instruction = agent, "x"
		if in.Kind == "" {
			in.Kind = "event"
		}
		if _, err := s.Create(ctx, issue, member, pgtype.UUID{}, in); !errors.Is(err, ErrWakeupInput) {
			t.Errorf("%s: err = %v, want ErrWakeupInput", name, err)
		}
	}
}

// A check that loses its lock wait records an error; the next unmet check that commits clears it.
func TestWakeupConditionCheckClearsAnEarlierFailure(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	ctx := context.Background()
	workspace := parseTestUUID(t, f.WorkspaceID)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Review when ready",
		Condition: condition(t, map[string]any{"type": "issue_field", "field": "status", "value": "in_review"})})
	due := func() {
		f.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()-interval '1 second' WHERE id=$1", w.ID)
	}

	due()
	tx, err := f.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "SELECT 1 FROM issue WHERE id=$1 FOR UPDATE", issue); err != nil {
		t.Fatal(err)
	}
	tickErr := s.TickWorkspaces(ctx, workspace)
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if tickErr == nil {
		t.Fatal("a check blocked on its issue row reported no error")
	}
	failed, err := f.q.LocklessWakeup(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !failed.LastError.Valid {
		t.Fatal("the failed check recorded no error")
	}

	due()
	if err = s.TickWorkspaces(ctx, workspace); err != nil {
		t.Fatal(err)
	}
	got, err := f.q.LocklessWakeup(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastError.Valid {
		t.Fatalf("the check that committed kept the earlier error %q", got.LastError.String)
	}
}

type wakeupCommitFailureStarter struct {
	TxStarter
	err error
}

func (s wakeupCommitFailureStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return wakeupCommitFailureTx{Tx: tx, err: s.err}, nil
}

type wakeupCommitFailureTx struct {
	pgx.Tx
	err error
}

func (tx wakeupCommitFailureTx) Commit(context.Context) error { return tx.err }

func TestWakeupConditionFailedCommitPreservesFailure(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	ctx := context.Background()
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Review when ready",
		Condition: condition(t, map[string]any{"type": "issue_field", "field": "status", "value": "in_review"})})
	f.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()-interval '1 second',last_error='earlier dispatch failure' WHERE id=$1", w.ID)
	commitErr := errors.New("dispatch commit failed")
	s.Tasks.TxStarter = wakeupCommitFailureStarter{TxStarter: f.Pool, err: commitErr}
	if err := s.dispatch(ctx, w); !errors.Is(err, commitErr) {
		t.Fatalf("dispatch error=%v, want %v", err, commitErr)
	}
	got, err := f.q.LocklessWakeup(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastError.Valid || got.LastError.String != "earlier dispatch failure" {
		t.Fatalf("failed commit cleared the earlier failure: %+v", got.LastError)
	}
	if err := s.TickWorkspaces(ctx, parseTestUUID(t, f.WorkspaceID)); !errors.Is(err, commitErr) {
		t.Fatalf("tick error=%v, want %v", err, commitErr)
	}
	got, err = f.q.LocklessWakeup(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastError.Valid || got.LastError.String != commitErr.Error() {
		t.Fatalf("failed pass did not record the current failure: %+v", got.LastError)
	}
	s.Tasks.TxStarter = f.Pool
	if got := wakeTick(t, f, s, w.ID); got.LastError.Valid {
		t.Fatalf("successful retry kept the commit failure: %+v", got.LastError)
	}
}

func TestWakeupConditionPRDiagnosticsRecover(t *testing.T) {
	for _, event := range []string{"checks_finished", "merged"} {
		t.Run(event, func(t *testing.T) {
			f, s, issue, agent := conditionFixture(t)
			ctx := context.Background()
			parent := f.Issue(t, "PR owner")
			f.Exec(t, "UPDATE issue SET parent_issue_id=$2 WHERE id=$1", issue, parent)
			pr := f.Insert(t, "github_pull_request", testutil.Cols{
				"workspace_id": f.WorkspaceID, "installation_id": 1,
				"repo_owner": "fixture", "repo_name": "wakeup", "pr_number": 1,
				"title": "PR", "state": "open", "html_url": "https://example.test/pr/1",
				"pr_created_at": "2026-01-01T00:00:00Z", "pr_updated_at": "2026-01-01T00:00:00Z",
			})
			f.Cleanup(t, "DELETE FROM issue_pull_request WHERE pull_request_id=$1", pr)
			f.Exec(t, "INSERT INTO issue_pull_request(issue_id,pull_request_id) VALUES($1,$2)", parent, pr)
			w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Review PR",
				Condition: condition(t, map[string]any{"type": "pull_request", "event": event})})
			assertWaiting := func(w db.IssueWakeup, diagnostic error) {
				t.Helper()
				if !w.Enabled || !w.LastError.Valid || w.LastError.String != conditionDiagnosticPrefix+diagnostic.Error() || w.FireCount != 0 || wakeRuns(t, f, w.ID) != 0 {
					t.Fatalf("expected enabled, unfired rule with %q; got enabled=%t fires=%d error=%+v", diagnostic, w.Enabled, w.FireCount, w.LastError)
				}
				if !w.NextFireAt.Valid {
					t.Fatal("diagnostic stopped condition polling")
				}
			}
			assertRecoveredFailure := func(diagnostic error) {
				t.Helper()
				f.Exec(t, "UPDATE issue_wakeup SET last_error='earlier dispatch failure' WHERE id=$1", w.ID)
				assertWaiting(wakeTick(t, f, s, w.ID), diagnostic)
			}
			if event == "checks_finished" {
				// A disabled snapshot client is visible even in the create response.
				assertWaiting(w, errPRSnapshotsUnavailable)
				assertWaiting(wakeTick(t, f, s, w.ID), errPRSnapshotsUnavailable)
				assertRecoveredFailure(errPRSnapshotsUnavailable)
				s.Tasks.PRSnapshotsEnabled = true
			}
			// Linking to the parent must never satisfy a child issue's rule.
			assertWaiting(wakeTick(t, f, s, w.ID), errPRNotLinked)
			assertRecoveredFailure(errPRNotLinked)
			f.Exec(t, "INSERT INTO issue_pull_request(issue_id,pull_request_id) VALUES($1,$2)", issue, pr)
			if event == "checks_finished" {
				assertWaiting(wakeTick(t, f, s, w.ID), errPRSnapshotPending)
				assertRecoveredFailure(errPRSnapshotPending)
				// Re-enabling also accepts a transiently missing snapshot.
				disabled, err := s.Disable(ctx, issue, w.ID, parseTestUUID(t, f.UserID))
				if err != nil {
					t.Fatal(err)
				}
				w, err = s.Enable(ctx, issue, parseTestUUID(t, f.UserID), pgtype.UUID{}, w.ID, WakeupEnableInput{Revision: disabled.Revision})
				if err != nil {
					t.Fatal(err)
				}
				assertWaiting(w, errPRSnapshotPending)
				f.Exec(t, "UPDATE github_pull_request SET snapshot_head_sha='new',checks_rollup_state='PENDING' WHERE id=$1", pr)
			}
			got := wakeTick(t, f, s, w.ID)
			if got.LastError.Valid || !got.Enabled || wakeRuns(t, f, w.ID) != 0 {
				t.Fatalf("normal pending condition did not clear diagnostic: %+v", got.LastError)
			}
			f.Exec(t, "UPDATE github_pull_request SET state='merged',checks_rollup_state='SUCCESS' WHERE id=$1", pr)
			got = wakeTick(t, f, s, w.ID)
			if got.Enabled || got.LastError.Valid || wakeRuns(t, f, w.ID) != 1 {
				t.Fatalf("recovered condition failed to fire once: enabled=%t error=%+v", got.Enabled, got.LastError)
			}
		})
	}
}

func TestWakeupConditionPullRequestChecksCoversVCSProviders(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	ctx := context.Background()
	var pr, connection string
	if err := f.Pool.QueryRow(ctx, `INSERT INTO vcs_pull_request(workspace_id,connection_id,provider,repo_owner,repo_name,pr_number,title,state,html_url,pr_created_at,pr_updated_at,head_sha)
		VALUES($1,gen_random_uuid(),'forgejo','selfhosted','wakeup-test',(extract(epoch from clock_timestamp())*1000)::bigint % 100000,'PR','open','https://forgejo.example.test/pr',now(),now(),'aaa') RETURNING id, connection_id`, f.WorkspaceID).Scan(&pr, &connection); err != nil {
		t.Fatal(err)
	}
	f.Cleanup(t, "DELETE FROM vcs_pull_request WHERE id=$1", pr)
	f.Cleanup(t, "DELETE FROM issue_vcs_pull_request WHERE pull_request_id=$1", pr)
	f.Exec(t, "INSERT INTO issue_vcs_pull_request(issue_id,pull_request_id) VALUES($1,$2)", issue, pr)
	f.Exec(t, `INSERT INTO vcs_commit_status(connection_id,sha,context,state) VALUES($1,'aaa','ci/test','pending')`, connection)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Look at CI",
		Condition: condition(t, map[string]any{"type": "pull_request", "event": "checks_finished"})})
	// Pending statuses from before registration are not a finished check.
	wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 0 {
		t.Fatalf("pending statuses started %d runs", n)
	}
	f.Exec(t, "UPDATE vcs_pull_request SET head_sha='bbb' WHERE id=$1", pr)
	f.Exec(t, `INSERT INTO vcs_commit_status(connection_id,sha,context,state) VALUES($1,'bbb','ci/test','failed')`, connection)
	got := wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 1 {
		t.Fatalf("failed statuses on the new head started %d runs, want 1", n)
	}
	task, err := f.q.GetAgentTask(ctx, got.LastTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(task.HandoffNote.String, `"checks":"failure"`) || !strings.Contains(task.HandoffNote.String, `"head_sha":"bbb"`) {
		t.Fatalf("VCS PR facts missing: %s", task.HandoffNote.String)
	}
	if !strings.Contains(task.HandoffNote.String, `"provider":"forgejo"`) {
		t.Fatalf("provider missing from PR facts: %s", task.HandoffNote.String)
	}
}

func TestWakeupConditionPullRequestMergedCoversVCSProviders(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	ctx := context.Background()
	var pr string
	if err := f.Pool.QueryRow(ctx, `INSERT INTO vcs_pull_request(workspace_id,connection_id,provider,repo_owner,repo_name,pr_number,title,state,html_url,pr_created_at,pr_updated_at,head_sha)
		VALUES($1,gen_random_uuid(),'gitea','selfhosted','wakeup-test',(extract(epoch from clock_timestamp())*1000)::bigint % 100000,'PR','open','https://gitea.example.test/pr',now(),now(),'ccc') RETURNING id`, f.WorkspaceID).Scan(&pr); err != nil {
		t.Fatal(err)
	}
	f.Cleanup(t, "DELETE FROM vcs_pull_request WHERE id=$1", pr)
	f.Cleanup(t, "DELETE FROM issue_vcs_pull_request WHERE pull_request_id=$1", pr)
	f.Exec(t, "INSERT INTO issue_vcs_pull_request(issue_id,pull_request_id) VALUES($1,$2)", issue, pr)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Note the merge",
		Condition: condition(t, map[string]any{"type": "pull_request", "event": "merged"})})
	wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 0 {
		t.Fatalf("open PR started %d runs", n)
	}
	f.Exec(t, "UPDATE vcs_pull_request SET state='merged' WHERE id=$1", pr)
	wakeTick(t, f, s, w.ID)
	if wakeRuns(t, f, w.ID) != 1 {
		t.Fatalf("merged VCS PR did not start exactly 1 run")
	}
}

// linkVCSPR links a self-hosted VCS pull request to issue and returns its id.
func linkVCSPR(t *testing.T, f principalFixture, issue pgtype.UUID, connection, provider string, number int, head string) string {
	t.Helper()
	var pr string
	if err := f.Pool.QueryRow(context.Background(), `INSERT INTO vcs_pull_request(workspace_id,connection_id,provider,repo_owner,repo_name,pr_number,title,state,html_url,pr_created_at,pr_updated_at,head_sha)
		VALUES($1,$2,$3,'selfhosted','wakeup-test',$4,'PR','open','https://vcs.example.test/pr',now(),now(),$5) RETURNING id`,
		f.WorkspaceID, connection, provider, number, head).Scan(&pr); err != nil {
		t.Fatal(err)
	}
	f.Cleanup(t, "DELETE FROM vcs_pull_request WHERE id=$1", pr)
	f.Cleanup(t, "DELETE FROM issue_vcs_pull_request WHERE pull_request_id=$1", pr)
	f.Cleanup(t, "DELETE FROM vcs_commit_status WHERE connection_id=$1", connection)
	f.Exec(t, "INSERT INTO issue_vcs_pull_request(issue_id,pull_request_id) VALUES($1,$2)", issue, pr)
	return pr
}

func setVCSStatus(t *testing.T, f principalFixture, connection, sha, state string) {
	t.Helper()
	f.Exec(t, `INSERT INTO vcs_commit_status(connection_id,sha,context,state) VALUES($1,$2,'ci/test',$3)
		ON CONFLICT (connection_id,sha,context) DO UPDATE SET state=EXCLUDED.state`, connection, sha, state)
}

func newConnection(t *testing.T, f principalFixture) string {
	t.Helper()
	var id string
	if err := f.Pool.QueryRow(context.Background(), "SELECT gen_random_uuid()::text").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestWakeupConditionPullRequestCoversEveryVCSProvider(t *testing.T) {
	for _, provider := range []string{"forgejo", "gitea", "gitlab"} {
		t.Run(provider+"/checks_finished", func(t *testing.T) {
			f, s, issue, agent := conditionFixture(t)
			s.Tasks.PRSnapshotsEnabled = true
			connection := newConnection(t, f)
			pr := linkVCSPR(t, f, issue, connection, provider, 7, "old")
			setVCSStatus(t, f, connection, "old", "passed")
			w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Look at CI",
				Condition: condition(t, map[string]any{"type": "pull_request", "event": "checks_finished"})})
			// A result already finished at registration belongs to the old head.
			wakeTick(t, f, s, w.ID)
			// A new head with no statuses yet is not finished.
			f.Exec(t, "UPDATE vcs_pull_request SET head_sha='new' WHERE id=$1", pr)
			wakeTick(t, f, s, w.ID)
			// A late result for the replaced head must not count either.
			setVCSStatus(t, f, connection, "old", "failed")
			wakeTick(t, f, s, w.ID)
			setVCSStatus(t, f, connection, "new", "pending")
			wakeTick(t, f, s, w.ID)
			if n := wakeRuns(t, f, w.ID); n != 0 {
				t.Fatalf("unfinished current head started %d runs", n)
			}
			// The same head going from pending to passed wakes once.
			setVCSStatus(t, f, connection, "new", "passed")
			got := wakeTick(t, f, s, w.ID)
			if n := wakeRuns(t, f, w.ID); n != 1 || got.LastError.Valid {
				t.Fatalf("finished checks started %d runs (error %+v), want 1", n, got.LastError)
			}
		})
		t.Run(provider+"/merged", func(t *testing.T) {
			f, s, issue, agent := conditionFixture(t)
			pr := linkVCSPR(t, f, issue, newConnection(t, f), provider, 7, "head")
			w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Note the merge",
				Condition: condition(t, map[string]any{"type": "pull_request", "event": "merged"})})
			wakeTick(t, f, s, w.ID)
			if n := wakeRuns(t, f, w.ID); n != 0 {
				t.Fatalf("open PR started %d runs", n)
			}
			f.Exec(t, "UPDATE vcs_pull_request SET state='merged' WHERE id=$1", pr)
			wakeTick(t, f, s, w.ID)
			if n := wakeRuns(t, f, w.ID); n != 1 {
				t.Fatalf("merged PR started %d runs, want 1", n)
			}
		})
	}
}

// A server without the GitHub snapshot client still evaluates VCS PRs linked
// next to a GitHub PR; the GitHub diagnostic only explains the wait.
func TestWakeupConditionVCSChecksNotBlockedByGitHubSnapshotGate(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	if s.Tasks.PRSnapshotsEnabled {
		t.Fatal("fixture must start without the snapshot client")
	}
	gh := f.Insert(t, "github_pull_request", testutil.Cols{
		"workspace_id": f.WorkspaceID, "installation_id": 1,
		"repo_owner": "fixture", "repo_name": "wakeup", "pr_number": 1,
		"title": "PR", "state": "open", "html_url": "https://example.test/pr/1",
		"pr_created_at": "2026-01-01T00:00:00Z", "pr_updated_at": "2026-01-01T00:00:00Z",
	})
	f.Cleanup(t, "DELETE FROM issue_pull_request WHERE pull_request_id=$1", gh)
	f.Exec(t, "INSERT INTO issue_pull_request(issue_id,pull_request_id) VALUES($1,$2)", issue, gh)
	connection := newConnection(t, f)
	linkVCSPR(t, f, issue, connection, "forgejo", 2, "sha")
	setVCSStatus(t, f, connection, "sha", "pending")
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Look at CI",
		Condition: condition(t, map[string]any{"type": "pull_request", "event": "checks_finished"})})
	got := wakeTick(t, f, s, w.ID)
	if wakeRuns(t, f, w.ID) != 0 || !got.LastError.Valid || got.LastError.String != conditionDiagnosticPrefix+errPRSnapshotsUnavailable.Error() {
		t.Fatalf("pending VCS checks: runs=%d error=%+v", wakeRuns(t, f, w.ID), got.LastError)
	}
	setVCSStatus(t, f, connection, "sha", "passed")
	got = wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 1 || got.LastError.Valid {
		t.Fatalf("passed VCS checks started %d runs (error %+v), want 1 and a cleared diagnostic", n, got.LastError)
	}
}

// PR numbers and shas repeat across connections: each PR rolls up its own
// statuses and has its own fingerprint.
func TestWakeupConditionVCSChecksKeyedByPullRequest(t *testing.T) {
	setup := func(t *testing.T) (principalFixture, *IssueWakeupService, pgtype.UUID, string, string, string) {
		f, s, issue, agent := conditionFixture(t)
		s.Tasks.PRSnapshotsEnabled = true
		a, b := newConnection(t, f), newConnection(t, f)
		linkVCSPR(t, f, issue, a, "forgejo", 5, "same")
		linkVCSPR(t, f, issue, b, "forgejo", 5, "same")
		return f, s, issue, agent, a, b
	}
	create := func(t *testing.T, f principalFixture, s *IssueWakeupService, issue pgtype.UUID, agent string) db.IssueWakeup {
		return wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Look at CI",
			Condition: condition(t, map[string]any{"type": "pull_request", "event": "checks_finished"})})
	}
	t.Run("rollup", func(t *testing.T) {
		f, s, issue, agent, a, b := setup(t)
		setVCSStatus(t, f, a, "same", "pending")
		setVCSStatus(t, f, b, "same", "pending")
		w := create(t, f, s, issue, agent)
		// One PR finishing satisfies the rule while the other still runs.
		setVCSStatus(t, f, a, "same", "passed")
		wakeTick(t, f, s, w.ID)
		if n := wakeRuns(t, f, w.ID); n != 1 {
			t.Fatalf("one finished PR started %d runs, want 1", n)
		}
	})
	t.Run("fingerprint", func(t *testing.T) {
		f, s, issue, agent, a, b := setup(t)
		setVCSStatus(t, f, a, "same", "passed")
		setVCSStatus(t, f, b, "same", "pending")
		w := create(t, f, s, issue, agent)
		// A reruns while B finishes before the next evaluation: a new result.
		setVCSStatus(t, f, a, "same", "pending")
		setVCSStatus(t, f, b, "same", "passed")
		wakeTick(t, f, s, w.ID)
		if n := wakeRuns(t, f, w.ID); n != 1 {
			t.Fatalf("a different PR finishing started %d runs, want 1", n)
		}
	})
}
