package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/ghsnapshot"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func reviewConditionPR(t *testing.T, f principalFixture, issue pgtype.UUID) string {
	t.Helper()
	pr := f.Insert(t, "github_pull_request", testutil.Cols{
		"workspace_id": f.WorkspaceID, "installation_id": 1, "repo_owner": "fixture", "repo_name": "reviews", "pr_number": 7,
		"title": "PR", "state": "open", "html_url": "https://example.test/pr/7", "head_sha": "a",
		"pr_created_at": "2026-01-01T00:00:00Z", "pr_updated_at": "2026-01-01T00:00:00Z",
	})
	f.Cleanup(t, "DELETE FROM issue_pull_request WHERE pull_request_id=$1", pr)
	f.Exec(t, "INSERT INTO issue_pull_request(issue_id,pull_request_id) VALUES($1,$2)", issue, pr)
	return pr
}

func writeReviewSnapshot(t *testing.T, f principalFixture, pr string, reviews ...ghsnapshot.PRReview) {
	t.Helper()
	if reviews == nil {
		reviews = []ghsnapshot.PRReview{}
	}
	raw, err := json.Marshal(reviews)
	if err != nil {
		t.Fatal(err)
	}
	f.Exec(t, "UPDATE github_pull_request SET review_snapshot=$2 WHERE id=$1", pr, raw)
}

func TestPRReviewConditionsWakeOnNewDecisions(t *testing.T) {
	for _, event := range []string{"reviewed", "approved", "changes_requested"} {
		t.Run(event, func(t *testing.T) {
			f, s, issue, agent := conditionFixture(t)
			s.Tasks.PRSnapshotsEnabled = true
			pr := reviewConditionPR(t, f, issue)
			old := ghsnapshot.PRReview{ID: 1, State: "APPROVED", SubmittedAt: time.Now().Add(-time.Hour)}
			writeReviewSnapshot(t, f, pr, old)
			w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Handle the review",
				Condition: condition(t, map[string]any{"type": "pull_request", "event": event})})
			wakeTick(t, f, s, w.ID)
			if n := wakeRuns(t, f, w.ID); n != 0 {
				t.Fatalf("replayed %d historical reviews", n)
			}
			decision := "APPROVED"
			if event == "changes_requested" {
				decision = "CHANGES_REQUESTED"
			}
			newReview := ghsnapshot.PRReview{ID: 2, State: decision, SubmittedAt: time.Now().Add(time.Second)}
			writeReviewSnapshot(t, f, pr, old, newReview)
			got := wakeTick(t, f, s, w.ID)
			if n := wakeRuns(t, f, w.ID); n != 1 || got.Enabled {
				t.Fatalf("new %s: runs=%d enabled=%t", event, n, got.Enabled)
			}
			task, err := f.q.GetAgentTask(context.Background(), got.LastTaskID)
			if err != nil || !strings.Contains(task.HandoffNote.String, `"review_id":2`) || !strings.Contains(task.HandoffNote.String, strings.ToLower(decision)) {
				t.Fatalf("review evidence missing: %s %v", task.HandoffNote.String, err)
			}
		})
	}
}

func TestPRReviewConditionsPreserveBaselineAndDeduplicate(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	s.Tasks.PRSnapshotsEnabled = true
	pr := reviewConditionPR(t, f, issue)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Mode: "continuous", Instruction: "Handle new approvals",
		Condition: condition(t, map[string]any{"type": "pull_request", "event": "approved"})})
	if !w.LastError.Valid || w.ConditionState == "" {
		t.Fatalf("missing snapshot lost baseline or diagnostic: %+v", w)
	}
	old := ghsnapshot.PRReview{ID: 1, State: "APPROVED", SubmittedAt: time.Now().Add(-time.Hour)}
	writeReviewSnapshot(t, f, pr, old)
	w = wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 0 || w.LastError.Valid {
		t.Fatalf("delayed first fetch replayed history or kept diagnostic: runs=%d error=%+v", n, w.LastError)
	}
	newReview := ghsnapshot.PRReview{ID: 2, State: "APPROVED", SubmittedAt: time.Now().Add(time.Second)}
	writeReviewSnapshot(t, f, pr, old, newReview)
	w = wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 1 {
		t.Fatalf("first approval: %d runs", n)
	}
	f.Exec(t, "UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1", w.LastTaskID)
	// Duplicate delivery, push, dismissal, unlink/relink, and a temporary
	// snapshot failure must neither replay the same ID nor erase the baseline.
	wakeTick(t, f, s, w.ID)
	f.Exec(t, "UPDATE github_pull_request SET head_sha='b' WHERE id=$1", pr)
	wakeTick(t, f, s, w.ID)
	writeReviewSnapshot(t, f, pr)
	wakeTick(t, f, s, w.ID)
	f.Exec(t, "DELETE FROM issue_pull_request WHERE issue_id=$1 AND pull_request_id=$2", issue, pr)
	wakeTick(t, f, s, w.ID)
	f.Exec(t, "INSERT INTO issue_pull_request(issue_id,pull_request_id) VALUES($1,$2)", issue, pr)
	f.Exec(t, "UPDATE github_pull_request SET review_snapshot=NULL WHERE id=$1", pr)
	wakeTick(t, f, s, w.ID)
	writeReviewSnapshot(t, f, pr, old, newReview)
	wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 1 {
		t.Fatalf("same review replayed: %d runs", n)
	}
	// A different review on the unchanged head is new work.
	nextReview := ghsnapshot.PRReview{ID: 3, State: "APPROVED", SubmittedAt: newReview.SubmittedAt.Add(time.Second)}
	writeReviewSnapshot(t, f, pr, old, newReview, nextReview)
	wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 2 {
		t.Fatalf("second approval on same head: %d runs", n)
	}
}

func TestPRReviewConditionsIgnoreOtherStatesAndClosedPRs(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	s.Tasks.PRSnapshotsEnabled = true
	pr := reviewConditionPR(t, f, issue)
	writeReviewSnapshot(t, f, pr)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Handle approvals",
		Condition: condition(t, map[string]any{"type": "pull_request", "event": "approved"})})
	for _, state := range []string{"COMMENTED", "PENDING", "DISMISSED", "CHANGES_REQUESTED"} {
		writeReviewSnapshot(t, f, pr, ghsnapshot.PRReview{ID: 2, State: state, SubmittedAt: time.Now().Add(time.Second)})
		wakeTick(t, f, s, w.ID)
	}
	f.Exec(t, "UPDATE github_pull_request SET state='closed' WHERE id=$1", pr)
	writeReviewSnapshot(t, f, pr, ghsnapshot.PRReview{ID: 2, State: "APPROVED", SubmittedAt: time.Now().Add(time.Second)})
	wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 0 {
		t.Fatalf("irrelevant or closed review started %d runs", n)
	}
}

func TestPRReviewReenableDoesNotReplayReviews(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	s.Tasks.PRSnapshotsEnabled = true
	pr := reviewConditionPR(t, f, issue)
	writeReviewSnapshot(t, f, pr)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Handle reviews",
		Condition: condition(t, map[string]any{"type": "pull_request", "event": "reviewed"})})
	member := parseTestUUID(t, f.UserID)
	if _, err := s.Disable(context.Background(), issue, w.ID, member); err != nil {
		t.Fatal(err)
	}
	writeReviewSnapshot(t, f, pr, ghsnapshot.PRReview{ID: 1, State: "APPROVED", SubmittedAt: time.Now().Add(-time.Second)})
	if _, err := s.Enable(context.Background(), issue, member, pgtype.UUID{}, w.ID, WakeupEnableInput{Revision: w.Revision}); err != nil {
		t.Fatal(err)
	}
	wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 0 {
		t.Fatalf("re-enable replayed %d reviews", n)
	}
}

func TestPRReviewConditionDiagnosticsRecover(t *testing.T) {
	for _, event := range []string{"reviewed", "approved", "changes_requested"} {
		t.Run(event, func(t *testing.T) {
			f, s, issue, agent := conditionFixture(t)
			s.Tasks.PRSnapshotsEnabled = false
			w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Handle review",
				Condition: condition(t, map[string]any{"type": "pull_request", "event": event})})
			if w.LastError.String != conditionDiagnosticPrefix+errPRSnapshotsUnavailable.Error() {
				t.Fatalf("missing configuration: %+v", w.LastError)
			}
			s.Tasks.PRSnapshotsEnabled = true
			w = wakeTick(t, f, s, w.ID)
			if w.LastError.String != conditionDiagnosticPrefix+errPRNotLinked.Error() {
				t.Fatalf("missing link: %+v", w.LastError)
			}
			linkVCSPR(t, f, issue, newConnection(t, f), "forgejo", 7, "a")
			w = wakeTick(t, f, s, w.ID)
			if w.LastError.String != conditionDiagnosticPrefix+errPRReviewsUnsupported.Error() {
				t.Fatalf("unsupported provider: %+v", w.LastError)
			}
			pr := reviewConditionPR(t, f, issue)
			w = wakeTick(t, f, s, w.ID)
			if w.LastError.String != conditionDiagnosticPrefix+errPRSnapshotPending.Error() {
				t.Fatalf("missing snapshot: %+v", w.LastError)
			}
			writeReviewSnapshot(t, f, pr)
			w = wakeTick(t, f, s, w.ID)
			if w.LastError.Valid || w.ConditionState == "" {
				t.Fatalf("recovery kept diagnostic or lost baseline: %+v", w)
			}
		})
	}
}

func TestPRReviewConditionsDoNotReadAnotherWorkspace(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	s.Tasks.PRSnapshotsEnabled = true
	pr := reviewConditionPR(t, f, issue)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", Instruction: "Handle reviews",
		Condition: condition(t, map[string]any{"type": "pull_request", "event": "reviewed"})})
	// Even a corrupt cross-workspace link cannot supply a review fact.
	other, _ := newPrincipalFixture(t)
	f.Cleanup(t, "UPDATE github_pull_request SET workspace_id=$2 WHERE id=$1", pr, f.WorkspaceID)
	f.Exec(t, "UPDATE github_pull_request SET workspace_id=$2 WHERE id=$1", pr, other.WorkspaceID)
	writeReviewSnapshot(t, f, pr, ghsnapshot.PRReview{ID: 1, State: "APPROVED", SubmittedAt: time.Now().Add(time.Second)})
	wakeTick(t, f, s, w.ID)
	if n := wakeRuns(t, f, w.ID); n != 0 {
		t.Fatalf("foreign PR started %d runs", n)
	}
}
