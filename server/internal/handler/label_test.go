package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestLabelCRUD exercises label create/list/get/update/delete.
func TestLabelCRUD(t *testing.T) {
	// Create
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/labels", map[string]any{
		"name":  "bug",
		"color": "#ef4444",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateLabel: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created LabelResponse
	json.NewDecoder(w.Body).Decode(&created)
	if created.Name != "bug" || created.Color != "#ef4444" {
		t.Fatalf("CreateLabel: unexpected payload: %+v", created)
	}
	labelID := created.ID

	t.Cleanup(func() {
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/labels/"+labelID, nil)
		req = withURLParam(req, "id", labelID)
		testHandler.DeleteLabel(w, req)
	})

	// Duplicate name → 409
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/labels", map[string]any{
		"name":  "BUG", // case-insensitive unique
		"color": "#000000",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("Duplicate CreateLabel: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	// Invalid color → 400
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/labels", map[string]any{
		"name":  "enhancement",
		"color": "nope",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("Invalid color: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// List
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/labels", nil)
	testHandler.ListLabels(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListLabels: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Labels []LabelResponse `json:"labels"`
		Total  int             `json:"total"`
	}
	json.NewDecoder(w.Body).Decode(&listResp)
	if listResp.Total < 1 {
		t.Fatalf("ListLabels: expected >= 1 label, got %d", listResp.Total)
	}

	// Get
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/labels/"+labelID, nil)
	req = withURLParam(req, "id", labelID)
	testHandler.GetLabel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetLabel: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Update
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/labels/"+labelID, map[string]any{
		"name":  "Bug (P0)",
		"color": "#b91c1c",
	})
	req = withURLParam(req, "id", labelID)
	testHandler.UpdateLabel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateLabel: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated LabelResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Name != "Bug (P0)" || updated.Color != "#b91c1c" {
		t.Fatalf("UpdateLabel: unexpected payload: %+v", updated)
	}
}

// TestIssueLabelAttachDetach exercises attach/detach + the issue-scoped endpoints.
func TestIssueLabelAttachDetach(t *testing.T) {
	// Create issue
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "Issue for label attach test",
		"status":   "todo",
		"priority": "medium",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	issueID := issue.ID

	// Create label
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/labels", map[string]any{
		"name":  "feature",
		"color": "#3b82f6",
	})
	testHandler.CreateLabel(w, req)
	var label LabelResponse
	json.NewDecoder(w.Body).Decode(&label)
	labelID := label.ID

	t.Cleanup(func() {
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/labels/"+labelID, nil)
		req = withURLParam(req, "id", labelID)
		testHandler.DeleteLabel(w, req)
	})

	// Attach
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issueID+"/labels", map[string]any{
		"label_id": labelID,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.AttachLabel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("AttachLabel: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Attach again (idempotent — ON CONFLICT DO NOTHING)
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issueID+"/labels", map[string]any{
		"label_id": labelID,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.AttachLabel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("AttachLabel (second): expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// List labels for issue
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issueID+"/labels", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListLabelsForIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListLabelsForIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var issueLabels struct {
		Labels []LabelResponse `json:"labels"`
	}
	json.NewDecoder(w.Body).Decode(&issueLabels)
	if len(issueLabels.Labels) != 1 {
		t.Fatalf("ListLabelsForIssue: expected 1 label, got %d", len(issueLabels.Labels))
	}
	if issueLabels.Labels[0].ID != labelID {
		t.Fatalf("ListLabelsForIssue: wrong label returned: %+v", issueLabels.Labels[0])
	}

	// Detach
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/issues/"+issueID+"/labels/"+labelID, nil)
	req = withURLParams(req, "id", issueID, "labelId", labelID)
	testHandler.DetachLabel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DetachLabel: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Confirm detached
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issueID+"/labels", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListLabelsForIssue(w, req)
	json.NewDecoder(w.Body).Decode(&issueLabels)
	if len(issueLabels.Labels) != 0 {
		t.Fatalf("after Detach: expected 0 labels, got %d", len(issueLabels.Labels))
	}
}

func TestProjectScopedLabelListAndUniqueness(t *testing.T) {
	projectA := createTestProject(t, "Label Scope A")
	projectB := createTestProject(t, "Label Scope B")

	global := createTestLabel(t, "scope-global", "#64748b", nil)
	projectALabel := createTestLabel(t, "scope-project", "#2563eb", &projectA)
	projectBLabel := createTestLabel(t, "scope-project", "#16a34a", &projectB)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/labels?project_id="+projectA, nil)
	testHandler.ListLabels(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListLabels scoped: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var scoped struct {
		Labels []LabelResponse `json:"labels"`
	}
	json.NewDecoder(w.Body).Decode(&scoped)
	assertLabelPresent(t, scoped.Labels, global.ID)
	assertLabelPresent(t, scoped.Labels, projectALabel.ID)
	assertLabelAbsent(t, scoped.Labels, projectBLabel.ID)

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/labels", nil)
	testHandler.ListLabels(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListLabels workspace: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var all struct {
		Labels []LabelResponse `json:"labels"`
	}
	json.NewDecoder(w.Body).Decode(&all)
	assertLabelPresent(t, all.Labels, global.ID)
	assertLabelPresent(t, all.Labels, projectALabel.ID)
	assertLabelPresent(t, all.Labels, projectBLabel.ID)

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/labels", map[string]any{
		"name":       projectALabel.Name,
		"color":      "#0f172a",
		"project_id": projectA,
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate project label: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProjectScopedLabelAttachAndCreateIssueValidation(t *testing.T) {
	projectA := createTestProject(t, "Attach Scope A")
	projectB := createTestProject(t, "Attach Scope B")
	labelA := createTestLabel(t, "attach-project-a", "#2563eb", &projectA)

	issueB := createTestIssueWithProject(t, "Issue in project B", projectB)
	t.Cleanup(func() { deleteTestIssue(t, issueB.ID) })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/"+issueB.ID+"/labels", map[string]any{
		"label_id": labelA.ID,
	})
	req = withURLParam(req, "id", issueB.ID)
	testHandler.AttachLabel(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("AttachLabel wrong project: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	var before int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM issue WHERE workspace_id = $1`, testWorkspaceID).Scan(&before); err != nil {
		t.Fatalf("count issues before: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "Create issue with wrong project label",
		"project_id": projectB,
		"label_ids":  []string{labelA.ID},
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateIssue wrong project label: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	var after int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM issue WHERE workspace_id = $1`, testWorkspaceID).Scan(&after); err != nil {
		t.Fatalf("count issues after: %v", err)
	}
	if after != before {
		t.Fatalf("CreateIssue wrong project label should not create issue, count before=%d after=%d", before, after)
	}
}

func TestUpdateIssueProjectChangeRemovesIncompatibleLabels(t *testing.T) {
	projectA := createTestProject(t, "Update Scope A")
	projectB := createTestProject(t, "Update Scope B")
	global := createTestLabel(t, "update-global", "#64748b", nil)
	labelA := createTestLabel(t, "update-project-a", "#2563eb", &projectA)
	issue := createTestIssueWithProjectAndLabels(t, "Update issue project label cleanup", projectA, []string{global.ID, labelA.ID})
	defer deleteTestIssue(t, issue.ID)

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issue.ID, map[string]any{
		"project_id": projectB,
	})
	req = withURLParam(req, "id", issue.ID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue project change: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated IssueResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.ProjectID == nil || *updated.ProjectID != projectB {
		t.Fatalf("UpdateIssue project_id: got %+v, want %s", updated.ProjectID, projectB)
	}
	if updated.Labels == nil {
		t.Fatalf("UpdateIssue expected labels payload after cleanup")
	}
	assertLabelPresent(t, *updated.Labels, global.ID)
	assertLabelAbsent(t, *updated.Labels, labelA.ID)

	labels := listIssueLabels(t, issue.ID)
	assertLabelPresent(t, labels, global.ID)
	assertLabelAbsent(t, labels, labelA.ID)
}

func TestBatchUpdateIssueProjectChangeRemovesIncompatibleLabels(t *testing.T) {
	projectA := createTestProject(t, "Batch Scope A")
	projectB := createTestProject(t, "Batch Scope B")
	global := createTestLabel(t, "batch-global", "#64748b", nil)
	labelA := createTestLabel(t, "batch-project-a", "#2563eb", &projectA)
	issue := createTestIssueWithProjectAndLabels(t, "Batch issue project label cleanup", projectA, []string{global.ID, labelA.ID})
	defer deleteTestIssue(t, issue.ID)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{issue.ID},
		"updates": map[string]any{
			"project_id": projectB,
		},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateIssues project change: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Updated int `json:"updated"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Updated != 1 {
		t.Fatalf("BatchUpdateIssues updated: got %d, want 1", resp.Updated)
	}

	labels := listIssueLabels(t, issue.ID)
	assertLabelPresent(t, labels, global.ID)
	assertLabelAbsent(t, labels, labelA.ID)
}

// TestLabelNotFoundAcrossWorkspaces ensures GET with a foreign workspace
// header returns 404 — the query's `WHERE workspace_id = $2` does the work.
func TestLabelNotFoundAcrossWorkspaces(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/labels", map[string]any{
		"name":  "cross-ws-test",
		"color": "#a855f7",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateLabel: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var label LabelResponse
	json.NewDecoder(w.Body).Decode(&label)
	labelID := label.ID

	t.Cleanup(func() {
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/labels/"+labelID, nil)
		req = withURLParam(req, "id", labelID)
		testHandler.DeleteLabel(w, req)
	})

	// GET with a different workspace ID → 404
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/labels/"+labelID, nil)
	req.Header.Set("X-Workspace-ID", "00000000-0000-0000-0000-000000000000")
	req = withURLParam(req, "id", labelID)
	testHandler.GetLabel(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetLabel cross-workspace: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateLabelCrossWorkspace — PUT with a foreign workspace header must not
// allow updating a label in another workspace (404 via pgx.ErrNoRows from the
// UPDATE ... WHERE id = $1 AND workspace_id = $2 clause).
func TestUpdateLabelCrossWorkspace(t *testing.T) {
	// Create in real workspace
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/labels", map[string]any{
		"name":  "cross-ws-update-test",
		"color": "#10b981",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateLabel: expected 201, got %d", w.Code)
	}
	var label LabelResponse
	json.NewDecoder(w.Body).Decode(&label)
	labelID := label.ID

	t.Cleanup(func() {
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/labels/"+labelID, nil)
		req = withURLParam(req, "id", labelID)
		testHandler.DeleteLabel(w, req)
	})

	// PUT with a foreign workspace ID → 404
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/labels/"+labelID, map[string]any{"name": "hacked"})
	req.Header.Set("X-Workspace-ID", "00000000-0000-0000-0000-000000000000")
	req = withURLParam(req, "id", labelID)
	testHandler.UpdateLabel(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("UpdateLabel cross-workspace: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// Sanity: the label wasn't renamed.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/labels/"+labelID, nil)
	req = withURLParam(req, "id", labelID)
	testHandler.GetLabel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetLabel after failed cross-workspace PUT: expected 200, got %d", w.Code)
	}
	var after LabelResponse
	json.NewDecoder(w.Body).Decode(&after)
	if after.Name != "cross-ws-update-test" {
		t.Fatalf("label name changed despite cross-workspace PUT: got %q", after.Name)
	}
}

// TestAttachLabelCrossWorkspaceLabel — an attach request whose label_id
// belongs to a different workspace must return 404, not silently no-op.
// Directly exercises the GetLabel workspace precheck and the SQL-layer
// defense-in-depth guard.
func TestAttachLabelCrossWorkspaceLabel(t *testing.T) {
	// Issue in the test workspace
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "cross-ws-attach-issue",
		"status":   "todo",
		"priority": "medium",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d", w.Code)
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)

	// Label in a second workspace — insert directly via the pool to avoid
	// the public API (which would require creating a full second workspace
	// fixture). The defense-in-depth is exactly that the handler refuses
	// even labels that exist *somewhere* but not in the current workspace.
	otherWorkspaceID := createOtherTestWorkspace(t)
	var otherLabelID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue_label (workspace_id, name, color)
		VALUES ($1, 'foreign-label', '#000000')
		RETURNING id
	`, otherWorkspaceID).Scan(&otherLabelID)
	if err != nil {
		t.Fatalf("insert foreign label: %v", err)
	}

	// Try to attach the foreign label to the test-workspace issue.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issue.ID+"/labels", map[string]any{
		"label_id": otherLabelID,
	})
	req = withURLParam(req, "id", issue.ID)
	testHandler.AttachLabel(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("AttachLabel cross-workspace label: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// Confirm nothing was attached.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issue.ID+"/labels", nil)
	req = withURLParam(req, "id", issue.ID)
	testHandler.ListLabelsForIssue(w, req)
	var list struct {
		Labels []LabelResponse `json:"labels"`
	}
	json.NewDecoder(w.Body).Decode(&list)
	if len(list.Labels) != 0 {
		t.Fatalf("expected 0 labels on issue, got %d", len(list.Labels))
	}
}

// TestLabelNameTooLong — names longer than 64 chars must return 400.
func TestLabelNameTooLong(t *testing.T) {
	longName := strings.Repeat("a", 33)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/labels", map[string]any{
		"name":  longName,
		"color": "#123456",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateLabel too-long name: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Exactly 32 chars is fine.
	okName := strings.Repeat("b", 32)
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/labels", map[string]any{
		"name":  okName,
		"color": "#123456",
	})
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateLabel 64-char name: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created LabelResponse
	json.NewDecoder(w.Body).Decode(&created)
	t.Cleanup(func() {
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/labels/"+created.ID, nil)
		req = withURLParam(req, "id", created.ID)
		testHandler.DeleteLabel(w, req)
	})
}

// TestColorCaseNormalization — input `#ABCDEF` must be stored as `#abcdef`
// so the case-insensitive uniqueness and downstream CSS rendering are
// consistent. Also accepts a bare `ABCDEF` (no leading #).
func TestColorCaseNormalization(t *testing.T) {
	cases := []struct {
		nameSuffix string
		input      string
		want       string
	}{
		{"upper", "#ABCDEF", "#abcdef"},
		{"mixed", "#AbCdEf", "#abcdef"},
		{"bare", "ABCDEF", "#abcdef"},
		{"lower", "#123abc", "#123abc"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		name := "color-norm-" + tc.nameSuffix // unique & case-independent
		req := newRequest("POST", "/api/labels", map[string]any{
			"name":  name,
			"color": tc.input,
		})
		testHandler.CreateLabel(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateLabel %q: expected 201, got %d: %s", tc.input, w.Code, w.Body.String())
		}
		var got LabelResponse
		json.NewDecoder(w.Body).Decode(&got)
		if got.Color != tc.want {
			t.Errorf("color normalization %q: got %q, want %q", tc.input, got.Color, tc.want)
		}
		t.Cleanup(func() {
			w := httptest.NewRecorder()
			req := newRequest("DELETE", "/api/labels/"+got.ID, nil)
			req = withURLParam(req, "id", got.ID)
			testHandler.DeleteLabel(w, req)
		})
	}
}

// createOtherTestWorkspace inserts a second workspace + owner membership for
// cross-workspace tests. Returns the new workspace id; cleanup registered.
func createOtherTestWorkspace(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var wsID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Other Handler Tests", handlerTestWorkspaceSlug+"-other", "temp second workspace", "OTH").Scan(&wsID)
	if err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
	`, wsID, testUserID); err != nil {
		t.Fatalf("add member to other workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})
	return wsID
}

func createTestProject(t *testing.T, title string) string {
	t.Helper()
	ctx := context.Background()
	var projectID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, $2)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("%s %s", title, strings.ReplaceAll(t.Name(), "/", "-"))).Scan(&projectID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})
	return projectID
}

func createTestLabel(t *testing.T, name, color string, projectID *string) LabelResponse {
	t.Helper()
	body := map[string]any{
		"name":  fmt.Sprintf("%s-%08x", name, crc32.ChecksumIEEE([]byte(t.Name()))),
		"color": color,
	}
	if projectID != nil {
		body["project_id"] = *projectID
	}
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/labels", body)
	testHandler.CreateLabel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateLabel: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var label LabelResponse
	json.NewDecoder(w.Body).Decode(&label)
	t.Cleanup(func() {
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/labels/"+label.ID, nil)
		req = withURLParam(req, "id", label.ID)
		testHandler.DeleteLabel(w, req)
	})
	return label
}

func createTestIssueWithProject(t *testing.T, title, projectID string) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      title,
		"status":     "todo",
		"priority":   "medium",
		"project_id": projectID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	return issue
}

func createTestIssueWithProjectAndLabels(t *testing.T, title, projectID string, labelIDs []string) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      title,
		"status":     "todo",
		"priority":   "medium",
		"project_id": projectID,
		"label_ids":  labelIDs,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	return issue
}

func listIssueLabels(t *testing.T, issueID string) []LabelResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/labels", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListLabelsForIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListLabelsForIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Labels []LabelResponse `json:"labels"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	return resp.Labels
}

func assertLabelPresent(t *testing.T, labels []LabelResponse, id string) {
	t.Helper()
	for _, label := range labels {
		if label.ID == id {
			return
		}
	}
	t.Fatalf("expected label %s to be present in %+v", id, labels)
}

func assertLabelAbsent(t *testing.T, labels []LabelResponse, id string) {
	t.Helper()
	for _, label := range labels {
		if label.ID == id {
			t.Fatalf("expected label %s to be absent from %+v", id, labels)
		}
	}
}
