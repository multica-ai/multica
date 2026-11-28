package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// targetPRFixture mirrors one GitHub pull request into the fixture workspace,
// linked to issue when link is true.
func targetPRFixture(t *testing.T, f principalFixture, issue pgtype.UUID, repo string, number int, state string, link bool) string {
	t.Helper()
	pr := f.Insert(t, "github_pull_request", testutil.Cols{
		"workspace_id": f.WorkspaceID, "installation_id": 1,
		"repo_owner": "fixture", "repo_name": repo, "pr_number": number,
		"title": "PR", "state": state, "html_url": "https://example.test/" + repo,
		"pr_created_at": "2026-01-01T00:00:00Z", "pr_updated_at": "2026-01-01T00:00:00Z",
		"head_sha": "head-1", "snapshot_head_sha": "head-1", "checks_rollup_state": "PENDING",
	})
	f.Cleanup(t, "DELETE FROM issue_pull_request WHERE pull_request_id=$1", pr)
	if link {
		f.Exec(t, "INSERT INTO issue_pull_request(issue_id,pull_request_id) VALUES($1,$2)", issue, pr)
	}
	return pr
}

func targetPRCondition(t *testing.T, repo string, number int, event string) WakeupInput {
	return WakeupInput{Kind: "event", Instruction: "Continue after the PR",
		Condition: condition(t, map[string]any{"type": "pull_request", "repo": repo, "number": number, "event": event})}
}

// An issue that already has a merged PR can still wait for a different one.
func TestWakeupConditionTargetPRIgnoresOtherLinkedPRs(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	targetPRFixture(t, f, issue, "earlier", 1, "merged", true)
	target := targetPRFixture(t, f, issue, "blocking", 2, "open", false)
	in := targetPRCondition(t, "fixture/blocking", 2, "merged")
	in.AgentID = agent
	w := wakeCreate(t, f, s, issue, in)
	if got := wakeTick(t, f, s, w.ID); got.LastError.Valid || wakeRuns(t, f, w.ID) != 0 {
		t.Fatalf("an unrelated merged PR fired the rule: runs=%d error=%+v", wakeRuns(t, f, w.ID), got.LastError)
	}
	f.Exec(t, "UPDATE github_pull_request SET state='merged' WHERE id=$1", target)
	wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 1 {
		t.Fatalf("after the target merged: %d runs, want 1", n)
	}
}

// A PR closed without merging ends a :closed wait but not a :merged one.
func TestWakeupConditionTargetPRClosedWithoutMerge(t *testing.T) {
	for event, want := range map[string]int{"closed": 1, "merged": 0} {
		t.Run(event, func(t *testing.T) {
			f, s, issue, agent := conditionFixture(t)
			targetPRFixture(t, f, issue, "abandoned", 3, "closed", true)
			in := targetPRCondition(t, "Fixture/Abandoned", 3, event)
			in.AgentID = agent
			w := wakeCreate(t, f, s, issue, in)
			wakeTick(t, f, s, w.ID)
			if n := wakeRuns(t, f, w.ID); n != want {
				t.Fatalf("%d runs, want %d", n, want)
			}
		})
	}
}

// The named PR does not have to be linked to the issue, and the issue does not
// need any linked PR at all.
func TestWakeupConditionTargetPRUnlinked(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	target := targetPRFixture(t, f, issue, "other-team", 4, "open", false)
	in := targetPRCondition(t, "fixture/other-team", 4, "merged")
	in.AgentID = agent
	w := wakeCreate(t, f, s, issue, in)
	if got := wakeTick(t, f, s, w.ID); got.LastError.Valid {
		t.Fatalf("waiting on an unlinked PR reported %q", got.LastError.String)
	}
	f.Exec(t, "UPDATE github_pull_request SET state='merged' WHERE id=$1", target)
	wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 1 {
		t.Fatalf("%d runs, want 1", n)
	}
}

// Checks on the named PR wait for a result newer than the one at creation.
func TestWakeupConditionTargetPRChecks(t *testing.T) {
	t.Run("snapshots unavailable", func(t *testing.T) {
		f, s, issue, agent := conditionFixture(t)
		targetPRFixture(t, f, issue, "ci", 5, "open", false)
		in := targetPRCondition(t, "fixture/ci", 5, "checks_finished")
		in.AgentID = agent
		w := wakeCreate(t, f, s, issue, in)
		if w.LastError.String != conditionDiagnosticPrefix+errPRSnapshotsUnavailable.Error() || !w.NextFireAt.Valid {
			t.Fatalf("error %+v, want the unavailable diagnostic while polling continues", w.LastError)
		}
	})
	f, s, issue, agent := conditionFixture(t)
	s.Tasks.PRSnapshotsEnabled = true
	target := targetPRFixture(t, f, issue, "ci", 5, "open", false)
	f.Exec(t, "UPDATE github_pull_request SET checks_rollup_state='FAILURE' WHERE id=$1", target)
	in := targetPRCondition(t, "fixture/ci", 5, "checks_finished")
	in.AgentID = agent
	w := wakeCreate(t, f, s, issue, in)
	wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 0 {
		t.Fatalf("a result from before registration fired: %d runs", n)
	}
	f.Exec(t, "UPDATE github_pull_request SET head_sha='head-2',snapshot_head_sha='head-1',checks_rollup_state='SUCCESS' WHERE id=$1", target)
	if got := wakeTick(t, f, s, w.ID); got.LastError.String != conditionDiagnosticPrefix+errPRTargetSnapshotPending.Error() || wakeRuns(t, f, w.ID) != 0 {
		t.Fatalf("stale-head snapshot: runs=%d error=%+v", wakeRuns(t, f, w.ID), got.LastError)
	}
	f.Exec(t, "UPDATE github_pull_request SET snapshot_head_sha='head-2' WHERE id=$1", target)
	wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 1 {
		t.Fatalf("after a new result: %d runs, want 1", n)
	}
}

// Without a named PR the rule still watches every linked PR.
func TestWakeupConditionLinkedPRDefaultUnchanged(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	targetPRFixture(t, f, issue, "linked", 6, "merged", true)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "After any linked PR merges",
		Condition: condition(t, map[string]any{"type": "pull_request", "event": "merged"})})
	wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 1 {
		t.Fatalf("%d runs, want 1", n)
	}
}

func TestWakeupConditionTargetPRValidation(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	targetPRFixture(t, f, issue, "known", 7, "open", false)
	ctx := context.Background()
	for name, c := range map[string]map[string]any{
		"unknown pr":          {"type": "pull_request", "repo": "fixture/known", "number": 8, "event": "merged"},
		"other workspace":     {"type": "pull_request", "repo": "elsewhere/known", "number": 7, "event": "merged"},
		"conflicts on target": {"type": "pull_request", "repo": "fixture/known", "number": 7, "event": "conflicts"},
		"repo without number": {"type": "pull_request", "repo": "fixture/known", "event": "merged"},
		"number without repo": {"type": "pull_request", "number": 7, "event": "merged"},
		"bad repo":            {"type": "pull_request", "repo": "known", "number": 7, "event": "merged"},
		"closed without repo": {"type": "pull_request", "event": "closed"},
	} {
		in := WakeupInput{AgentID: agent, Kind: "event", Instruction: "x", Condition: condition(t, c)}
		if _, err := s.Create(ctx, issue, parseTestUUID(t, f.UserID), pgtype.UUID{}, in); !errors.Is(err, ErrWakeupInput) {
			t.Errorf("%s: err = %v, want ErrWakeupInput", name, err)
		}
	}
	// The stored repository keeps the spelling GitHub reported.
	in := targetPRCondition(t, "FIXTURE/KNOWN", 7, "closed")
	in.AgentID = agent
	w := wakeCreate(t, f, s, issue, in)
	var got WakeupCondition
	if err := json.Unmarshal(w.Condition, &got); err != nil || got.Type != "pull_request" || got.Event != "closed" || got.Repo != "fixture/known" || got.Number != 7 {
		t.Fatalf("stored condition %s (%v)", w.Condition, err)
	}
}
