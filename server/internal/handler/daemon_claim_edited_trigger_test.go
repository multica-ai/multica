package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// MUL-8218 / GH #9492: the user writes X, the agent starts, the user edits the
// comment to Y. The edit cancels that run and retriggers from Y, but the new
// run resumes the cancelled run's session — whose history still quotes X. The
// claim must tell the daemon the trigger was edited after that run started.

func TestTriggerCommentEditedSince(t *testing.T) {
	start := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	ts := func(d time.Duration) pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: start.Add(d), Valid: true}
	}
	runStarted := ts(0)

	cases := []struct {
		name    string
		comment db.Comment
		started pgtype.Timestamptz
		want    bool
	}{
		{
			name:    "edited after the run started",
			comment: db.Comment{CreatedAt: ts(-time.Minute), UpdatedAt: ts(time.Minute), Revision: 2},
			started: runStarted,
			want:    true,
		},
		{
			name:    "never edited",
			comment: db.Comment{CreatedAt: ts(-time.Minute), UpdatedAt: ts(-time.Minute), Revision: 1},
			started: runStarted,
		},
		{
			name:    "edited before the run started: the run already read the new text",
			comment: db.Comment{CreatedAt: ts(-2 * time.Minute), UpdatedAt: ts(-time.Minute), Revision: 2},
			started: runStarted,
		},
		{
			name:    "created after the run started: the session never saw any version",
			comment: db.Comment{CreatedAt: ts(time.Minute), UpdatedAt: ts(2 * time.Minute), Revision: 2},
			started: runStarted,
		},
		{
			name: "only change is a single resolve",
			comment: db.Comment{
				CreatedAt: ts(-time.Minute), UpdatedAt: ts(time.Minute), Revision: 2,
				ResolvedAt: ts(time.Minute),
			},
			started: runStarted,
		},
		{
			name: "edit then resolve: the resolve must not hide the edit",
			comment: db.Comment{
				CreatedAt: ts(-time.Minute), UpdatedAt: ts(2 * time.Minute), Revision: 3,
				ResolvedAt: ts(2 * time.Minute),
			},
			started: runStarted,
			want:    true,
		},
		{
			name: "edited after an earlier resolve",
			comment: db.Comment{
				CreatedAt: ts(-time.Minute), UpdatedAt: ts(2 * time.Minute), Revision: 3,
				ResolvedAt: ts(time.Minute),
			},
			started: runStarted,
			want:    true,
		},
		{
			name:    "resumed run never started",
			comment: db.Comment{CreatedAt: ts(-time.Minute), UpdatedAt: ts(time.Minute), Revision: 2},
			started: pgtype.Timestamptz{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := triggerCommentEditedSince(tc.comment, tc.started); got != tc.want {
				t.Fatalf("triggerCommentEditedSince = %v, want %v", got, tc.want)
			}
		})
	}
}

type claimEditedTriggerResp struct {
	Task *struct {
		PriorSessionID         string `json:"prior_session_id"`
		TriggerCommentID       string `json:"trigger_comment_id"`
		TriggerCommentContent  string `json:"trigger_comment_content"`
		TriggerCommentEditedAt string `json:"trigger_comment_edited_at"`
		TriggerCommentRevision int64  `json:"trigger_comment_revision"`
	} `json:"task"`
}

func claimEditedTriggerTask(t *testing.T, runtimeID string) claimEditedTriggerResp {
	t.Helper()
	req := newOwnerDaemonRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil)
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)
	var resp claimEditedTriggerResp
	w.JSON(&resp)
	if resp.Task == nil {
		t.Fatalf("expected a claimed task, got nil: %s", w.Body.String())
	}
	return resp
}

// TestClaimTask_FlagsTriggerEditedAfterResumedCancelledRun is the reported
// scenario: the resumed session belongs to the CANCELLED run that read X.
func TestClaimTask_FlagsTriggerEditedAfterResumedCancelledRun(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Edited trigger runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Edited trigger agent")

	commentID := dbfx.Comment(t, issueID, "Now it connect", testutil.Cols{
		"created_at": testutil.Raw("now() - interval '10 minutes'"),
		"updated_at": testutil.Raw("now() - interval '10 minutes'"),
	})
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":         runtimeID,
		"issue_id":           issueID,
		"trigger_comment_id": commentID,
		"status":             "cancelled",
		"session_id":         "edited-trigger-cancelled-session",
		"started_at":         testutil.Raw("now() - interval '9 minutes'"),
		"completed_at":       testutil.Raw("now() - interval '5 minutes'"),
	})
	// The edit: new body, revision bump, updated_at after the run started.
	dbfx.Exec(t, `UPDATE comment SET content = 'Now it connects over TLS', revision = revision + 1, updated_at = now() - interval '5 minutes' WHERE id = $1`, commentID)
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":         runtimeID,
		"issue_id":           issueID,
		"trigger_comment_id": commentID,
	})

	resp := claimEditedTriggerTask(t, runtimeID)
	if resp.Task.PriorSessionID != "edited-trigger-cancelled-session" {
		t.Fatalf("PriorSessionID = %q, want the cancelled run's session", resp.Task.PriorSessionID)
	}
	if resp.Task.TriggerCommentContent != "Now it connects over TLS" {
		t.Fatalf("TriggerCommentContent = %q, want the edited body", resp.Task.TriggerCommentContent)
	}
	if resp.Task.TriggerCommentEditedAt == "" {
		t.Fatal("TriggerCommentEditedAt is empty, want the edit time: the resumed history quotes the old text")
	}
	if resp.Task.TriggerCommentRevision != 2 {
		t.Fatalf("TriggerCommentRevision = %d, want 2", resp.Task.TriggerCommentRevision)
	}
}

// TestClaimTask_NoEditFlagWhenTriggerUnchangedSinceResumedRun guards the
// common follow-up: nothing was edited, so nothing is flagged.
func TestClaimTask_NoEditFlagWhenTriggerUnchangedSinceResumedRun(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Unedited trigger runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Unedited trigger agent")

	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":   runtimeID,
		"issue_id":     issueID,
		"status":       "completed",
		"session_id":   "unedited-trigger-session",
		"started_at":   testutil.Raw("now() - interval '1 hour'"),
		"completed_at": testutil.Raw("now() - interval '50 minutes'"),
	})
	createCommentTriggeredClaimTask(t, ctx, agentID, runtimeID, issueID, nil)

	resp := claimEditedTriggerTask(t, runtimeID)
	if resp.Task.PriorSessionID != "unedited-trigger-session" {
		t.Fatalf("PriorSessionID = %q, want unedited-trigger-session", resp.Task.PriorSessionID)
	}
	if resp.Task.TriggerCommentEditedAt != "" || resp.Task.TriggerCommentRevision != 0 {
		t.Fatalf("edit fields = (%q, %d), want empty for a new, unedited comment",
			resp.Task.TriggerCommentEditedAt, resp.Task.TriggerCommentRevision)
	}
}

// TestUpdateComment_EditedTriggerReachesResumedRun drives the reported flow
// through the real edit path, twice: claim X, run, edit to Y (the edit cancels
// the run and retriggers), claim — then run, edit to Z, claim again. Each
// follow-up must resume the cancelled run's session, carry the newest body,
// and flag the edit with the matching revision.
func TestUpdateComment_EditedTriggerReachesResumedRun(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fixture := createCommentDeliveryFixture(t, "Edited trigger resumed run")
	makeCommentDeliverySingleThread(t, fixture)
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, fixture.issueID, fixture.agentID)
	triggerID := fixture.commentID[2]

	first := claimCommentDeliveryFixture(t, fixture, "")
	if first.TriggerCommentEditedAt != "" {
		t.Fatalf("first claim flagged an edit (%q) on a never-edited trigger", first.TriggerCommentEditedAt)
	}

	runningTaskID := first.ID
	var revision int64 = 1
	previous := "latest instruction"
	for i, body := range []string{"edited instruction Y", "edited instruction Z"} {
		session := "edited-trigger-session-" + string(rune('a'+i))
		// The daemon started the run and pinned its session before the edit.
		dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', started_at = now() - interval '30 seconds', session_id = $2 WHERE id = $1`, runningTaskID, session)

		w := httptest.NewRecorder()
		req := newRequest(http.MethodPut, "/api/comments/"+triggerID, map[string]any{"content": body})
		req = withURLParam(req, "commentId", triggerID)
		testHandler.UpdateComment(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("edit %d: UpdateComment got %d: %s", i, w.Code, w.Body.String())
		}
		revision++
		if got := taskStatus(t, runningTaskID); got != "cancelled" {
			t.Fatalf("edit %d: running task status = %q, want cancelled by the edit", i, got)
		}

		next := claimCommentDeliveryFixture(t, fixture, "")
		if next.PriorSessionID != session {
			t.Fatalf("edit %d: PriorSessionID = %q, want %q (the cancelled run's session)", i, next.PriorSessionID, session)
		}
		// The fixture's batch is folded into one legacy bundle here; the
		// current body must be in it and the superseded one must not.
		if !strings.Contains(next.TriggerCommentContent, body) || strings.Contains(next.TriggerCommentContent, previous) {
			t.Fatalf("edit %d: TriggerCommentContent = %q, want the current body %q only", i, next.TriggerCommentContent, body)
		}
		if next.TriggerCommentEditedAt == "" {
			t.Fatalf("edit %d: TriggerCommentEditedAt empty, want the edit flagged", i)
		}
		if next.TriggerCommentRevision != revision {
			t.Fatalf("edit %d: TriggerCommentRevision = %d, want %d", i, next.TriggerCommentRevision, revision)
		}
		runningTaskID = next.ID
		previous = body
	}
}

// Review regressions for PR #9526 (supplied by the reviewer, adapted): a
// resolve after the edit, and an edited comment folded behind a newer trigger.
func TestClaimTask_EditThenResolveKeepsEditFlag(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database unavailable")
	}
	f := createCommentDeliveryFixture(t, "Review edit then resolve")
	makeCommentDeliverySingleThread(t, f)
	dbfx.Exec(t, `UPDATE issue SET assignee_type='agent', assignee_id=$2 WHERE id=$1`, f.issueID, f.agentID)
	first := claimCommentDeliveryFixture(t, f, "")
	dbfx.Exec(t, `UPDATE agent_task_queue SET status='running', started_at=now()-interval '30 seconds', session_id='review-edit-resolve-session' WHERE id=$1`, first.ID)
	trigger := f.commentID[2]
	req := withURLParam(newRequest(http.MethodPut, "/api/comments/"+trigger, map[string]any{"content": "edited instruction Y"}), "commentId", trigger)
	testutil.Call(t, testHandler.UpdateComment, req).Want(http.StatusOK)
	resolveCommentHTTP(t, trigger)
	next := claimCommentDeliveryFixture(t, f, "")
	if next.PriorSessionID != "review-edit-resolve-session" {
		t.Fatal("unexpected resume session")
	}
	if next.TriggerCommentEditedAt == "" {
		t.Fatal("edit warning lost after resolve although resumed history still holds X")
	}
}

func TestClaimTask_FlagsEditedCoalescedComment(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database unavailable")
	}
	f := createCommentDeliveryFixture(t, "Review edit coalesced")
	makeCommentDeliverySingleThread(t, f)
	dbfx.Exec(t, `UPDATE issue SET assignee_type='agent', assignee_id=$2 WHERE id=$1`, f.issueID, f.agentID)
	first := claimCommentDeliveryFixture(t, f, "coalesced-comments-v1")
	dbfx.Exec(t, `UPDATE agent_task_queue SET status='running', started_at=now()-interval '30 seconds', session_id='review-edit-coalesced-session' WHERE id=$1`, first.ID)
	editedID := f.commentID[0]
	req := withURLParam(newRequest(http.MethodPut, "/api/comments/"+editedID, map[string]any{"content": "edited instruction Y"}), "commentId", editedID)
	testutil.Call(t, testHandler.UpdateComment, req).Want(http.StatusOK)
	newComment := withURLParam(newRequest(http.MethodPost, "/api/issues/"+f.issueID+"/comments", map[string]any{"content": "[@Review](mention://agent/" + f.agentID + ") Please continue with the current instructions.", "parent_id": editedID}), "id", f.issueID)
	testutil.Call(t, testHandler.CreateComment, newComment).Want(http.StatusCreated)
	next := claimCommentDeliveryFixture(t, f, "coalesced-comments-v1")
	if next.PriorSessionID != "review-edit-coalesced-session" {
		t.Fatal("unexpected resume session")
	}
	if next.TriggerCommentEditedAt != "" {
		t.Fatalf("the new, unchanged trigger was flagged as edited (%q)", next.TriggerCommentEditedAt)
	}
	var found bool
	for _, cc := range next.CoalescedComments {
		if cc.ID != editedID {
			if cc.EditedAt != "" {
				t.Fatalf("unchanged coalesced comment %s flagged as edited", cc.ID)
			}
			continue
		}
		found = true
		if cc.Content != "edited instruction Y" || cc.EditedAt == "" || cc.Revision != 2 {
			t.Fatalf("edited coalesced comment = %+v, want current body, edit time and revision 2", cc)
		}
	}
	if !found {
		t.Fatalf("edited comment %s not delivered: %+v", editedID, next.CoalescedComments)
	}
}
