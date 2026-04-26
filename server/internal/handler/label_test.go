package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// withURLParams is defined in daemon_test.go — we reuse it here.

// --- Helpers ---------------------------------------------------------------

// seedLabelTestWorkspace creates a fresh workspace + owner user and returns IDs.
// It uses the shared testPool and registers a cleanup that removes the workspace.
// Tests use this instead of the shared testWorkspaceID so parallel runs don't
// collide on label names and counts.
func seedLabelTestWorkspace(t *testing.T) (workspaceID, userID string) {
	t.Helper()
	if testPool == nil {
		t.Skip("database not reachable")
	}
	ctx := context.Background()
	userID = uuid.NewString()
	slug := "label-" + uuid.NewString()[:8]
	workspaceID = uuid.NewString()

	if _, err := testPool.Exec(ctx,
		`INSERT INTO "user" (id, name, email) VALUES ($1, 'Label Test', $2)`,
		userID, fmt.Sprintf("label-test-%s@multica.ai", userID[:8])); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO workspace (id, slug, name, issue_prefix, description) VALUES ($1, $2, 'Label Tests', 'LAB', '')`,
		workspaceID, slug); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		workspaceID, userID); err != nil {
		t.Fatalf("seed member: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return
}

// labelReq builds a request authenticated as userID, with X-Workspace-ID set.
// Pass chi URL params as pairs: key1, value1, key2, value2, ...
func labelReq(method, path, userID, workspaceID string, body any, params ...string) *http.Request {
	req := newRequest(method, path, body)
	req.Header.Set("X-User-ID", userID)
	if workspaceID != "" {
		req.Header.Set("X-Workspace-ID", workspaceID)
	}
	if len(params) > 0 {
		req = withURLParams(req, params...)
	}
	return req
}

// --- Tests -----------------------------------------------------------------

func TestCreateWorkspaceLabel(t *testing.T) {
	workspaceID, userID := seedLabelTestWorkspace(t)

	w := httptest.NewRecorder()
	req := labelReq("POST",
		fmt.Sprintf("/api/workspaces/%s/labels", workspaceID),
		userID, workspaceID,
		map[string]string{"name": "Bug", "color": "red"},
		"id", workspaceID,
	)
	testHandler.CreateWorkspaceLabel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp IssueLabelResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Name != "Bug" || resp.Color != "red" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestCreateLabelRejectsInvalidColor(t *testing.T) {
	workspaceID, userID := seedLabelTestWorkspace(t)

	w := httptest.NewRecorder()
	req := labelReq("POST",
		fmt.Sprintf("/api/workspaces/%s/labels", workspaceID),
		userID, workspaceID,
		map[string]string{"name": "Neon", "color": "neon"},
		"id", workspaceID,
	)
	testHandler.CreateWorkspaceLabel(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateLabelRejectsDuplicateNameCaseInsensitive(t *testing.T) {
	workspaceID, userID := seedLabelTestWorkspace(t)

	w := httptest.NewRecorder()
	req := labelReq("POST",
		fmt.Sprintf("/api/workspaces/%s/labels", workspaceID),
		userID, workspaceID,
		map[string]string{"name": "Backend", "color": "blue"},
		"id", workspaceID,
	)
	testHandler.CreateWorkspaceLabel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first create: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = labelReq("POST",
		fmt.Sprintf("/api/workspaces/%s/labels", workspaceID),
		userID, workspaceID,
		map[string]string{"name": "backend", "color": "teal"},
		"id", workspaceID,
	)
	testHandler.CreateWorkspaceLabel(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate create: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListWorkspaceLabels(t *testing.T) {
	workspaceID, userID := seedLabelTestWorkspace(t)

	for _, body := range []map[string]string{
		{"name": "Zeta", "color": "purple"},
		{"name": "Alpha", "color": "green"},
		{"name": "Middle", "color": "amber"},
	} {
		w := httptest.NewRecorder()
		req := labelReq("POST",
			fmt.Sprintf("/api/workspaces/%s/labels", workspaceID),
			userID, workspaceID, body,
			"id", workspaceID,
		)
		testHandler.CreateWorkspaceLabel(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("seed label %s: %d %s", body["name"], w.Code, w.Body.String())
		}
	}

	w := httptest.NewRecorder()
	req := labelReq("GET",
		fmt.Sprintf("/api/workspaces/%s/labels", workspaceID),
		userID, workspaceID, nil,
		"id", workspaceID,
	)
	testHandler.ListWorkspaceLabels(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	var resp []IssueLabelResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 3 {
		t.Fatalf("expected 3 labels, got %d", len(resp))
	}
	// Labels come back alphabetically case-insensitive (ListIssueLabels ORDER BY LOWER(name)).
	if resp[0].Name != "Alpha" || resp[1].Name != "Middle" || resp[2].Name != "Zeta" {
		t.Fatalf("wrong order: %v %v %v", resp[0].Name, resp[1].Name, resp[2].Name)
	}
}

func TestUpdateAndDeleteLabel(t *testing.T) {
	workspaceID, userID := seedLabelTestWorkspace(t)

	// Create.
	w := httptest.NewRecorder()
	req := labelReq("POST",
		fmt.Sprintf("/api/workspaces/%s/labels", workspaceID),
		userID, workspaceID,
		map[string]string{"name": "Old", "color": "gray"},
		"id", workspaceID,
	)
	testHandler.CreateWorkspaceLabel(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created IssueLabelResponse
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	// Update name + color.
	newName := "New"
	newColor := "indigo"
	w = httptest.NewRecorder()
	req = labelReq("PATCH",
		fmt.Sprintf("/api/workspaces/%s/labels/%s", workspaceID, created.ID),
		userID, workspaceID,
		map[string]*string{"name": &newName, "color": &newColor},
		"id", workspaceID, "labelId", created.ID,
	)
	testHandler.UpdateWorkspaceLabel(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	var updated IssueLabelResponse
	_ = json.Unmarshal(w.Body.Bytes(), &updated)
	if updated.Name != "New" || updated.Color != "indigo" {
		t.Fatalf("update payload wrong: %+v", updated)
	}

	// Delete.
	w = httptest.NewRecorder()
	req = labelReq("DELETE",
		fmt.Sprintf("/api/workspaces/%s/labels/%s", workspaceID, created.ID),
		userID, workspaceID, nil,
		"id", workspaceID, "labelId", created.ID,
	)
	testHandler.DeleteWorkspaceLabel(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
}

func TestAttachAndDetachLabel(t *testing.T) {
	workspaceID, userID := seedLabelTestWorkspace(t)
	ctx := context.Background()

	// Seed a label directly.
	var labelID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO issue_label (workspace_id, name, color, creator_type, creator_id)
		 VALUES ($1, 'Frontend', 'teal', 'member', $2) RETURNING id`,
		workspaceID, userID,
	).Scan(&labelID); err != nil {
		t.Fatalf("seed label: %v", err)
	}

	// Seed an issue directly. Issue.number is workspace-unique so we pick 1.
	var issueID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		 VALUES ($1, 'Test issue', 'todo', 'medium', 'member', $2, 1) RETURNING id`,
		workspaceID, userID,
	).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	// Attach.
	w := httptest.NewRecorder()
	req := labelReq("POST",
		fmt.Sprintf("/api/issues/%s/labels", issueID),
		userID, workspaceID,
		map[string]string{"label_id": labelID},
		"id", issueID,
	)
	testHandler.AttachLabelToIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("attach: %d %s", w.Code, w.Body.String())
	}

	var n int
	testPool.QueryRow(ctx,
		`SELECT count(*) FROM issue_to_label WHERE issue_id = $1 AND label_id = $2`,
		issueID, labelID).Scan(&n)
	if n != 1 {
		t.Fatalf("expected 1 join row after attach, got %d", n)
	}

	// Detach.
	w = httptest.NewRecorder()
	req = labelReq("DELETE",
		fmt.Sprintf("/api/issues/%s/labels/%s", issueID, labelID),
		userID, workspaceID, nil,
		"id", issueID, "labelId", labelID,
	)
	testHandler.DetachLabelFromIssue(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
		t.Fatalf("detach: %d %s", w.Code, w.Body.String())
	}
	testPool.QueryRow(ctx,
		`SELECT count(*) FROM issue_to_label WHERE issue_id = $1 AND label_id = $2`,
		issueID, labelID).Scan(&n)
	if n != 0 {
		t.Fatalf("expected 0 join rows after detach, got %d", n)
	}
}

func TestAgentCannotCreateLabel(t *testing.T) {
	workspaceID, userID := seedLabelTestWorkspace(t)
	ctx := context.Background()

	// Seed an agent runtime (required FK) + agent owned by userID.
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, 'online', $4, '{}'::jsonb, now())
		RETURNING id
	`, workspaceID, "Label Test Runtime", "label_test_runtime", "Label test runtime").Scan(&runtimeID); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}

	agentID := uuid.NewString()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id, owner_id)
		 VALUES ($1, $2, 'TestBot', 'cloud', $3, $4)`,
		agentID, workspaceID, runtimeID, userID); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := labelReq("POST",
		fmt.Sprintf("/api/workspaces/%s/labels", workspaceID),
		userID, workspaceID,
		map[string]string{"name": "AgentTag", "color": "blue"},
		"id", workspaceID,
	)
	req.Header.Set("X-Agent-ID", agentID)

	testHandler.CreateWorkspaceLabel(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
