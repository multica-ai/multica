package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func installAutopilotSubscriberInsertFailure(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	functionName := fmt.Sprintf("autopilot_subscriber_fail_fn_%d", suffix)
	triggerName := fmt.Sprintf("autopilot_subscriber_fail_%d", suffix)
	t.Cleanup(func() {
		testPool.Exec(ctx, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON autopilot_subscriber`, triggerName))
		testPool.Exec(ctx, fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, functionName))
	})

	if _, err := testPool.Exec(ctx, fmt.Sprintf(`
CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	RAISE EXCEPTION 'forced autopilot subscriber insert failure';
END;
$$;
`, functionName)); err != nil {
		t.Fatalf("install failure function: %v", err)
	}
	if _, err := testPool.Exec(ctx, fmt.Sprintf(`
CREATE TRIGGER %s
BEFORE INSERT ON autopilot_subscriber
FOR EACH ROW EXECUTE FUNCTION %s();
`, triggerName, functionName)); err != nil {
		t.Fatalf("install failure trigger: %v", err)
	}
}

// TestCreateAutopilotPersistsMemberSubscribers covers the happy path:
// supplying a non-empty `subscribers` array on POST /api/autopilots stores
// the rows and the response echoes them back. This is the create half of the
// MUL-2533 RFC ("autopilot default subscriber template").
func TestCreateAutopilotPersistsMemberSubscribers(t *testing.T) {
	ctx := context.Background()
	var autopilotID string
	defer func() {
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "Subscriber template autopilot",
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": testUserID},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	autopilotID = resp.ID
	if len(resp.Subscribers) != 1 {
		t.Fatalf("subscribers in response = %d, want 1", len(resp.Subscribers))
	}
	if resp.Subscribers[0].UserType != "member" || resp.Subscribers[0].UserID != testUserID {
		t.Fatalf("subscribers[0] = %+v, want member/%s", resp.Subscribers[0], testUserID)
	}

	// Confirm the row landed in the DB. Belt-and-braces: the response could
	// in principle be assembled from the request without writing.
	var count int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM autopilot_subscriber WHERE autopilot_id = $1
	`, autopilotID).Scan(&count); err != nil {
		t.Fatalf("count subscribers: %v", err)
	}
	if count != 1 {
		t.Fatalf("autopilot_subscriber rows = %d, want 1", count)
	}
}

// TestCreateAutopilotRejectsNonMemberSubscriberType locks in the first-version
// constraint: only user_type='member' is accepted on the API. The DB CHECK
// would also reject anything else; the 400 here exists so the client gets a
// clear message instead of a 500 with a constraint-name leak.
func TestCreateAutopilotRejectsNonMemberSubscriberType(t *testing.T) {
	var agentID string
	if err := testPool.QueryRow(context.Background(), `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "Bad subscriber type",
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
		"subscribers": []map[string]any{
			{"user_type": "agent", "user_id": agentID},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateAutopilot: expected 400 for non-member subscriber, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateAutopilotRejectsForeignSubscriber covers the boundary check:
// supplying a UUID that does not belong to this workspace must 400, not
// silently leak inside the autopilot row.
func TestCreateAutopilotRejectsForeignSubscriber(t *testing.T) {
	var agentID string
	if err := testPool.QueryRow(context.Background(), `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "Foreign subscriber",
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": "00000000-0000-0000-0000-000000000000"},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateAutopilot: expected 400 for foreign member subscriber, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAutopilotRollsBackWhenSubscriberInsertFails(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Subscriber rollback create %d", time.Now().UnixNano())

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	installAutopilotSubscriberInsertFailure(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          title,
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": testUserID},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("CreateAutopilot: expected 500 for forced subscriber insert failure, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM autopilot
		WHERE workspace_id = $1 AND title = $2
	`, testWorkspaceID, title).Scan(&count); err != nil {
		t.Fatalf("count rolled-back autopilots: %v", err)
	}
	if count != 0 {
		t.Fatalf("autopilot rows after failed subscriber insert = %d, want 0", count)
	}
}

// TestUpdateAutopilotFullReplaceSubscribers covers the PATCH semantics from
// the RFC: sending `subscribers` wipes whatever was there and re-inserts the
// new set. Omitting the field would leave the previous template untouched;
// that branch is exercised separately by TestUpdateAutopilotPreservesSubscribersWhenOmitted.
func TestUpdateAutopilotFullReplaceSubscribers(t *testing.T) {
	ctx := context.Background()
	var autopilotID string
	defer func() {
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "Replace subscribers autopilot",
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": testUserID},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	autopilotID = created.ID

	// PATCH with an empty array → expect zero subscribers afterward.
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID, map[string]any{
		"subscribers": []map[string]any{},
	})
	req = withURLParam(req, "id", autopilotID)
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilot: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated: %v", err)
	}
	if len(updated.Subscribers) != 0 {
		t.Fatalf("subscribers after empty replace = %d, want 0", len(updated.Subscribers))
	}

	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM autopilot_subscriber WHERE autopilot_id = $1`, autopilotID).Scan(&count); err != nil {
		t.Fatalf("count after replace: %v", err)
	}
	if count != 0 {
		t.Fatalf("DB rows after empty replace = %d, want 0", count)
	}
}

func TestUpdateAutopilotRollsBackWhenSubscriberInsertFails(t *testing.T) {
	ctx := context.Background()
	originalTitle := fmt.Sprintf("Subscriber rollback update %d", time.Now().UnixNano())
	updatedTitle := originalTitle + " changed"
	var autopilotID string
	defer func() {
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          originalTitle,
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": testUserID},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	autopilotID = created.ID

	installAutopilotSubscriberInsertFailure(t)

	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID, map[string]any{
		"title": updatedTitle,
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": testUserID},
		},
	})
	req = withURLParam(req, "id", autopilotID)
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("UpdateAutopilot: expected 500 for forced subscriber insert failure, got %d: %s", w.Code, w.Body.String())
	}

	var gotTitle string
	if err := testPool.QueryRow(ctx, `SELECT title FROM autopilot WHERE id = $1`, autopilotID).Scan(&gotTitle); err != nil {
		t.Fatalf("load autopilot title after rollback: %v", err)
	}
	if gotTitle != originalTitle {
		t.Fatalf("autopilot title after failed subscriber replace = %q, want %q", gotTitle, originalTitle)
	}

	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM autopilot_subscriber WHERE autopilot_id = $1`, autopilotID).Scan(&count); err != nil {
		t.Fatalf("count subscribers after rollback: %v", err)
	}
	if count != 1 {
		t.Fatalf("subscriber rows after failed replace = %d, want 1", count)
	}
}

// TestUpdateAutopilotPreservesSubscribersWhenOmitted asserts the
// "omit the field to leave it alone" contract — a previously-set template
// must NOT be wiped just because the client sent a partial PATCH.
func TestUpdateAutopilotPreservesSubscribersWhenOmitted(t *testing.T) {
	ctx := context.Background()
	var autopilotID string
	defer func() {
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "Preserve subscribers autopilot",
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": testUserID},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	autopilotID = created.ID

	// PATCH a different field, leave subscribers out → row count unchanged.
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Preserve subscribers autopilot (renamed)",
	})
	req = withURLParam(req, "id", autopilotID)
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilot: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM autopilot_subscriber WHERE autopilot_id = $1`, autopilotID).Scan(&count); err != nil {
		t.Fatalf("count after omitted PATCH: %v", err)
	}
	if count != 1 {
		t.Fatalf("DB rows after omitted PATCH = %d, want 1 (subscribers must not have been touched)", count)
	}
}

// TestAutopilotDispatchFansOutSubscribersToIssue is the integration check
// for the dispatch path: an autopilot with a default subscriber list must
// auto-subscribe each entry to the issue it spawns, with reason='autopilot'.
// Belt-and-braces: also confirms that the creator-of-the-issue (the assignee
// agent — see TestAutopilotCreatedIssueCreatorIsAssigneeAgent) gets a row
// with reason='creator', and the two reasons don't fight (PK is one row per
// (issue, user_type, user_id), so the first one wins on conflict).
func TestAutopilotDispatchFansOutSubscribersToIssue(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Autopilot subscriber fanout %d", time.Now().UnixNano())
	var autopilotID, issueID string
	defer func() {
		if issueID != "" {
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		}
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":                "Subscriber fanout autopilot",
		"assignee_id":          agentID,
		"execution_mode":       "create_issue",
		"issue_title_template": title,
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": testUserID},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var autopilot AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&autopilot); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	autopilotID = autopilot.ID

	queries := db.New(testPool)
	ap, err := queries.GetAutopilot(ctx, parseUUID(autopilotID))
	if err != nil {
		t.Fatalf("GetAutopilot: %v", err)
	}
	run, err := testHandler.AutopilotService.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil || !run.IssueID.Valid {
		t.Fatalf("dispatch run = %+v, want linked issue", run)
	}
	issueID = uuidToString(run.IssueID)

	var subscriberReason string
	if err := testPool.QueryRow(ctx, `
		SELECT reason
		FROM issue_subscriber
		WHERE issue_id = $1 AND user_type = 'member' AND user_id = $2
	`, issueID, testUserID).Scan(&subscriberReason); err != nil {
		t.Fatalf("query autopilot-fanned subscriber: %v", err)
	}
	if subscriberReason != "autopilot" {
		t.Fatalf("subscriber reason = %q, want %q", subscriberReason, "autopilot")
	}
}

// TestAutopilotDispatchNotifiesSubscribersOnCreate locks in the OQ3 promise
// from the RFC ("reason='autopilot' 与 reason='manual' 一致，订阅事件全收"):
// when an autopilot creates an issue, each template subscriber must land in
// the recipient's inbox with type='issue_subscribed' pointing at the new
// issue. Without this, subscribers would only see comment/status updates
// after the fact and miss the creation event itself — flagged in PR #3060
// review by the Emacs agent.
func TestAutopilotDispatchNotifiesSubscribersOnCreate(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Autopilot subscriber inbox %d", time.Now().UnixNano())
	var autopilotID, issueID string
	defer func() {
		if issueID != "" {
			testPool.Exec(ctx, `DELETE FROM inbox_item WHERE issue_id = $1`, issueID)
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		}
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":                "Subscriber inbox autopilot",
		"assignee_id":          agentID,
		"execution_mode":       "create_issue",
		"issue_title_template": title,
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": testUserID},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var autopilot AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&autopilot); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	autopilotID = autopilot.ID

	queries := db.New(testPool)
	ap, err := queries.GetAutopilot(ctx, parseUUID(autopilotID))
	if err != nil {
		t.Fatalf("GetAutopilot: %v", err)
	}
	run, err := testHandler.AutopilotService.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil || !run.IssueID.Valid {
		t.Fatalf("dispatch run = %+v, want linked issue", run)
	}
	issueID = uuidToString(run.IssueID)

	var inboxCount int
	var inboxType, inboxTitle string
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM inbox_item
		WHERE issue_id = $1 AND recipient_id = $2 AND type = 'issue_subscribed'
	`, issueID, testUserID).Scan(&inboxCount); err != nil {
		t.Fatalf("count inbox rows: %v", err)
	}
	if inboxCount != 1 {
		t.Fatalf("inbox_item rows for subscriber = %d, want 1", inboxCount)
	}

	if err := testPool.QueryRow(ctx, `
		SELECT type, title FROM inbox_item
		WHERE issue_id = $1 AND recipient_id = $2 AND type = 'issue_subscribed'
	`, issueID, testUserID).Scan(&inboxType, &inboxTitle); err != nil {
		t.Fatalf("load inbox row: %v", err)
	}
	if inboxType != "issue_subscribed" {
		t.Fatalf("inbox type = %q, want issue_subscribed", inboxType)
	}
	if inboxTitle != title {
		t.Fatalf("inbox title = %q, want %q (issue title)", inboxTitle, title)
	}
}

// TestAutopilotDispatchSkipsInboxWhenNoSubscribers asserts the no-op path:
// an autopilot with an empty subscriber template must NOT create any inbox
// rows on dispatch — otherwise we'd be paging the workspace on every quiet
// autopilot run. The corresponding issue_subscriber rows are also expected
// to be absent (other-reason rows like creator/assignee are filtered out by
// the WHERE type = 'issue_subscribed' clause).
func TestAutopilotDispatchSkipsInboxWhenNoSubscribers(t *testing.T) {
	ctx := context.Background()
	title := fmt.Sprintf("Autopilot no-subscriber inbox %d", time.Now().UnixNano())
	var autopilotID, issueID string
	defer func() {
		if issueID != "" {
			testPool.Exec(ctx, `DELETE FROM inbox_item WHERE issue_id = $1`, issueID)
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		}
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":                "No-subscriber autopilot",
		"assignee_id":          agentID,
		"execution_mode":       "create_issue",
		"issue_title_template": title,
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var autopilot AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&autopilot); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	autopilotID = autopilot.ID

	queries := db.New(testPool)
	ap, err := queries.GetAutopilot(ctx, parseUUID(autopilotID))
	if err != nil {
		t.Fatalf("GetAutopilot: %v", err)
	}
	run, err := testHandler.AutopilotService.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil || !run.IssueID.Valid {
		t.Fatalf("dispatch run = %+v, want linked issue", run)
	}
	issueID = uuidToString(run.IssueID)

	var inboxCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM inbox_item
		WHERE issue_id = $1 AND type = 'issue_subscribed'
	`, issueID).Scan(&inboxCount); err != nil {
		t.Fatalf("count inbox rows: %v", err)
	}
	if inboxCount != 0 {
		t.Fatalf("issue_subscribed inbox rows = %d, want 0 (no subscribers)", inboxCount)
	}
}

// TestDeleteAutopilotRemovesSubscribers guards the app-layer cleanup that
// replaced the dropped autopilot_subscriber → autopilot ON DELETE CASCADE:
// deleting an autopilot must also delete its subscriber template rows in the
// same transaction, leaving no orphans behind.
func TestDeleteAutopilotRemovesSubscribers(t *testing.T) {
	ctx := context.Background()
	var autopilotID string
	defer func() {
		if autopilotID != "" {
			testPool.Exec(ctx, `DELETE FROM autopilot_subscriber WHERE autopilot_id = $1`, autopilotID)
			testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		}
	}()

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "Delete-with-subscribers autopilot",
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
		"subscribers": []map[string]any{
			{"user_type": "member", "user_id": testUserID},
		},
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	autopilotID = created.ID

	var before int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM autopilot_subscriber WHERE autopilot_id = $1`, autopilotID).Scan(&before); err != nil {
		t.Fatalf("count subscribers before delete: %v", err)
	}
	if before != 1 {
		t.Fatalf("subscriber rows before delete = %d, want 1", before)
	}

	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID, nil)
	req = withURLParam(req, "id", autopilotID)
	testHandler.DeleteAutopilot(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteAutopilot: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var after int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM autopilot_subscriber WHERE autopilot_id = $1`, autopilotID).Scan(&after); err != nil {
		t.Fatalf("count subscribers after delete: %v", err)
	}
	if after != 0 {
		t.Fatalf("subscriber rows after delete = %d, want 0 (app-layer cleanup)", after)
	}

	var autopilotRows int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM autopilot WHERE id = $1`, autopilotID).Scan(&autopilotRows); err != nil {
		t.Fatalf("count autopilot after delete: %v", err)
	}
	if autopilotRows != 0 {
		t.Fatalf("autopilot rows after delete = %d, want 0", autopilotRows)
	}
}
