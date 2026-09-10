package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestUpdateIssueActivityExcludesPositionOnlyWrites(t *testing.T) {
	created := createIssueForTest(t, map[string]any{
		"title": "activity contract",
	})
	if created.LastActivityAt == nil {
		t.Fatal("CreateIssue response omitted last_activity_at")
	}
	var initial time.Time
	if err := testPool.QueryRow(context.Background(), `SELECT last_activity_at FROM issue WHERE id = $1`, created.ID).Scan(&initial); err != nil {
		t.Fatalf("read initial last_activity_at: %v", err)
	}

	update := func(body map[string]any) IssueResponse {
		t.Helper()
		w := httptest.NewRecorder()
		r := withURLParam(newRequest(http.MethodPut, "/api/issues/"+created.ID, body), "id", created.ID)
		testHandler.UpdateIssue(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("UpdateIssue: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var response IssueResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("decode UpdateIssue: %v", err)
		}
		return response
	}

	positionOnly := update(map[string]any{"position": created.Position + 1})
	if positionOnly.LastActivityAt == nil {
		t.Fatal("position update response omitted last_activity_at")
	}
	var positionActivity time.Time
	if err := testPool.QueryRow(context.Background(), `SELECT last_activity_at FROM issue WHERE id = $1`, created.ID).Scan(&positionActivity); err != nil {
		t.Fatalf("read position last_activity_at: %v", err)
	}
	if !positionActivity.Equal(initial) {
		t.Fatalf("position-only update changed activity: before=%s after=%s", initial, positionActivity)
	}

	time.Sleep(10 * time.Millisecond)
	titleUpdate := update(map[string]any{"title": "activity contract changed"})
	if titleUpdate.LastActivityAt == nil {
		t.Fatal("title update response omitted last_activity_at")
	}
	var titleActivity time.Time
	if err := testPool.QueryRow(context.Background(), `SELECT last_activity_at FROM issue WHERE id = $1`, created.ID).Scan(&titleActivity); err != nil {
		t.Fatalf("read title last_activity_at: %v", err)
	}
	if !titleActivity.After(positionActivity) {
		t.Fatalf("semantic update did not advance activity: before=%s after=%s", positionActivity, titleActivity)
	}
}

func TestIssueActivityTimestampIsMonotonic(t *testing.T) {
	created := createIssueForTest(t, map[string]any{"title": "monotonic activity"})
	future := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Microsecond)
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET last_activity_at = $2 WHERE id = $1`, created.ID, future); err != nil {
		t.Fatalf("seed future last_activity_at: %v", err)
	}
	updated, err := testHandler.Queries.SetIssueMetadataKey(context.Background(), db.SetIssueMetadataKeyParams{
		Key:         "activity_test",
		Value:       []byte(`"changed"`),
		ID:          parseUUID(created.ID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("SetIssueMetadataKey: %v", err)
	}
	if !updated.LastActivityAt.Valid || !updated.LastActivityAt.Time.Equal(future) {
		t.Fatalf("activity timestamp regressed: got=%v want=%s", updated.LastActivityAt, future)
	}
}

func TestIssueDecorationActivityOnlyAdvancesOnChange(t *testing.T) {
	ctx := context.Background()
	created := createIssueForTest(t, map[string]any{"title": "decoration activity"})
	issueID := parseUUID(created.ID)
	workspaceID := parseUUID(testWorkspaceID)
	labelID := parseUUID(createTestIssueLabel(t, "activity-label-"+created.ID[:8]))
	base := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)

	reset := func() {
		t.Helper()
		if _, err := testPool.Exec(ctx, `UPDATE issue SET last_activity_at = $2 WHERE id = $1`, issueID, base); err != nil {
			t.Fatalf("reset last_activity_at: %v", err)
		}
	}
	read := func() time.Time {
		t.Helper()
		var got time.Time
		if err := testPool.QueryRow(ctx, `SELECT last_activity_at FROM issue WHERE id = $1`, issueID).Scan(&got); err != nil {
			t.Fatalf("read last_activity_at: %v", err)
		}
		return got
	}

	reset()
	if _, err := testHandler.Queries.AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{
		IssueID: issueID, LabelID: labelID, WorkspaceID: workspaceID,
	}); err != nil {
		t.Fatalf("AttachLabelToIssue: %v", err)
	}
	if got := read(); !got.After(base) {
		t.Fatalf("label attach did not advance activity: base=%s got=%s", base, got)
	}
	reset()
	if _, err := testHandler.Queries.AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{
		IssueID: issueID, LabelID: labelID, WorkspaceID: workspaceID,
	}); err != nil {
		t.Fatalf("duplicate AttachLabelToIssue: %v", err)
	}
	if got := read(); !got.Equal(base) {
		t.Fatalf("duplicate label attach changed activity: base=%s got=%s", base, got)
	}

	reset()
	if _, err := testHandler.Queries.SetIssuePropertyValue(ctx, db.SetIssuePropertyValueParams{
		Key:         "00000000-0000-0000-0000-000000000001",
		Value:       []byte(`"selected"`),
		ID:          issueID,
		WorkspaceID: workspaceID,
	}); err != nil {
		t.Fatalf("SetIssuePropertyValue: %v", err)
	}
	if got := read(); !got.After(base) {
		t.Fatalf("property change did not advance activity: base=%s got=%s", base, got)
	}
	reset()
	if _, err := testHandler.Queries.SetIssuePropertyValue(ctx, db.SetIssuePropertyValueParams{
		Key:         "00000000-0000-0000-0000-000000000001",
		Value:       []byte(`"selected"`),
		ID:          issueID,
		WorkspaceID: workspaceID,
	}); err != nil {
		t.Fatalf("duplicate SetIssuePropertyValue: %v", err)
	}
	if got := read(); !got.Equal(base) {
		t.Fatalf("duplicate property write changed activity: base=%s got=%s", base, got)
	}
}

func TestMarkIssueFirstExecutedDoesNotChangeActivity(t *testing.T) {
	created := createIssueForTest(t, map[string]any{"title": "execution is not activity"})
	if created.LastActivityAt == nil {
		t.Fatal("CreateIssue response omitted last_activity_at")
	}
	var before time.Time
	if err := testPool.QueryRow(context.Background(), `SELECT last_activity_at FROM issue WHERE id = $1`, created.ID).Scan(&before); err != nil {
		t.Fatalf("read last_activity_at before: %v", err)
	}
	if _, err := testHandler.Queries.MarkIssueFirstExecuted(context.Background(), parseUUID(created.ID)); err != nil {
		t.Fatalf("MarkIssueFirstExecuted: %v", err)
	}
	var after time.Time
	if err := testPool.QueryRow(context.Background(), `SELECT last_activity_at FROM issue WHERE id = $1`, created.ID).Scan(&after); err != nil {
		t.Fatalf("read last_activity_at: %v", err)
	}
	if !after.Equal(before) {
		t.Fatalf("execution lifecycle changed activity: before=%s after=%s", before, after)
	}
}
