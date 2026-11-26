package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func createLabeledTestAutopilot(t *testing.T, body map[string]any) AutopilotResponse {
	t.Helper()
	ctx := context.Background()
	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}
	body["assignee_id"] = agentID
	body["execution_mode"] = "create_issue"
	w := httptest.NewRecorder()
	testHandler.CreateAutopilot(w, newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, body))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode autopilot: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, resp.ID)
	})
	return resp
}

func patchAutopilotLabels(t *testing.T, id string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/autopilots/"+id+"?workspace_id="+testWorkspaceID, body)
	testHandler.UpdateAutopilot(w, withURLParam(req, "id", id))
	return w
}

func storedAutopilotLabelIDs(t *testing.T, id string) []string {
	t.Helper()
	ap, err := db.New(testPool).GetAutopilot(context.Background(), parseUUID(id))
	if err != nil {
		t.Fatalf("GetAutopilot: %v", err)
	}
	return uuidsToStrings(ap.LabelIds)
}

func TestAutopilotLabelIDsCreateAndUpdate(t *testing.T) {
	suffix := time.Now().UnixNano()
	feedback := createTestIssueLabel(t, fmt.Sprintf("feedback-%d", suffix))
	bug := createTestIssueLabel(t, fmt.Sprintf("bug-%d", suffix))

	created := createLabeledTestAutopilot(t, map[string]any{
		"title":     "Labeled autopilot",
		"label_ids": []string{feedback, bug, feedback},
	})
	if want := []string{feedback, bug}; !slices.Equal(created.LabelIDs, want) {
		t.Fatalf("created label_ids = %v, want %v", created.LabelIDs, want)
	}

	// Omitting label_ids leaves them untouched.
	if w := patchAutopilotLabels(t, created.ID, map[string]any{"title": "Renamed"}); w.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilot (title): %d %s", w.Code, w.Body.String())
	}
	if got := storedAutopilotLabelIDs(t, created.ID); !slices.Equal(got, []string{feedback, bug}) {
		t.Fatalf("label_ids after unrelated update = %v", got)
	}

	// An empty list clears them, and the response stays a non-null array.
	w := patchAutopilotLabels(t, created.ID, map[string]any{"label_ids": []string{}})
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilot (clear): %d %s", w.Code, w.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if string(raw["label_ids"]) != "[]" {
		t.Fatalf("label_ids after clear = %s, want []", raw["label_ids"])
	}
	if got := storedAutopilotLabelIDs(t, created.ID); len(got) != 0 {
		t.Fatalf("stored label_ids after clear = %v", got)
	}
}

func TestAutopilotLabelIDsRejectUnknownLabel(t *testing.T) {
	created := createLabeledTestAutopilot(t, map[string]any{"title": "Unlabeled autopilot"})
	if created.LabelIDs == nil || len(created.LabelIDs) != 0 {
		t.Fatalf("label_ids without labels = %#v, want empty array", created.LabelIDs)
	}

	unknown := "00000000-0000-7000-8000-000000000001"
	if w := patchAutopilotLabels(t, created.ID, map[string]any{"label_ids": []string{unknown}}); w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateAutopilot (unknown label): expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if w := patchAutopilotLabels(t, created.ID, map[string]any{"label_ids": []string{"not-a-uuid"}}); w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateAutopilot (malformed label): expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAutopilotDispatchAttachesLabels(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	kept := createTestIssueLabel(t, fmt.Sprintf("kept-%d", suffix))
	deleted := createTestIssueLabel(t, fmt.Sprintf("deleted-%d", suffix))

	created := createLabeledTestAutopilot(t, map[string]any{
		"title":                "Labeled dispatch autopilot",
		"issue_title_template": fmt.Sprintf("Labeled dispatch %d", suffix),
		"label_ids":            []string{kept, deleted},
	})

	// A label deleted after the autopilot was saved leaves the autopilot.
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/labels/"+deleted, nil)
	testHandler.DeleteLabel(w, withURLParam(req, "id", deleted))
	if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
		t.Fatalf("DeleteLabel: %d %s", w.Code, w.Body.String())
	}
	if got := storedAutopilotLabelIDs(t, created.ID); !slices.Equal(got, []string{kept}) {
		t.Fatalf("label_ids after label delete = %v, want [%s]", got, kept)
	}

	// Even if a stale id is still stored, dispatch skips it instead of failing.
	if _, err := testPool.Exec(ctx, `UPDATE autopilot SET label_ids = ARRAY[$2::uuid, $3::uuid] WHERE id = $1`, created.ID, kept, deleted); err != nil {
		t.Fatalf("restore stale label id: %v", err)
	}

	ap, err := db.New(testPool).GetAutopilot(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetAutopilot: %v", err)
	}
	run, _, err := testHandler.AutopilotService.DispatchAutopilotManual(ctx, ap, pgtype.UUID{}, nil, parseUUID(testUserID))
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	if run == nil || !run.IssueID.Valid {
		t.Fatalf("dispatch run = %+v, want linked issue", run)
	}
	issueID := uuidToString(run.IssueID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	if n := countIssueLabel(t, issueID, kept); n != 1 {
		t.Fatalf("kept label on issue = %d, want 1", n)
	}
	if n := countIssueLabel(t, issueID, deleted); n != 0 {
		t.Fatalf("deleted label on issue = %d, want 0", n)
	}
}
