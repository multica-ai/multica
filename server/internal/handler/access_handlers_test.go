package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EVAL for Phase 2 — server-side filtering. Two members in the same workspace
// (one a project member, one not) probe ListIssues, GetIssue, GetProject,
// SearchIssues. Outsider must see no trace of the restricted resources.

type filterFixture struct {
	memberUserID  string
	outsideUserID string
	openProjectID string
	restrictedID  string
	openIssueID   string
	restrictIssID string
	memberMember  db.Member
	outsideMember db.Member
}

func setupFilterFixture(t *testing.T) filterFixture {
	t.Helper()
	ctx := context.Background()

	mkUser := func(name, email string) string {
		var id string
		if err := testPool.QueryRow(ctx,
			`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`, name, email,
		).Scan(&id); err != nil {
			t.Fatalf("create user: %v", err)
		}
		return id
	}
	mkMember := func(userID, role string) {
		if _, err := testPool.Exec(ctx,
			`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3)`,
			testWorkspaceID, userID, role,
		); err != nil {
			t.Fatalf("create member: %v", err)
		}
	}

	memberID := mkUser("Filter Member", "filter-member@multica.test")
	outsideID := mkUser("Filter Outside", "filter-outside@multica.test")
	mkMember(memberID, "member")
	mkMember(outsideID, "member")

	mkProject := func(title, access string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO project (workspace_id, title, status, priority, access)
			VALUES ($1, $2, 'in_progress', 'medium', $3)
			RETURNING id
		`, testWorkspaceID, title, access).Scan(&id); err != nil {
			t.Fatalf("create project: %v", err)
		}
		return id
	}
	openID := mkProject("filter-open", "workspace")
	restrID := mkProject("filter-restricted", "restricted")

	if _, err := testPool.Exec(ctx,
		`INSERT INTO project_member (workspace_id, project_id, user_id) VALUES ($1, $2, $3)`,
		testWorkspaceID, restrID, memberID,
	); err != nil {
		t.Fatalf("project_member: %v", err)
	}

	mkIssue := func(title, projectID string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number, project_id)
			VALUES ($1, $2, 'todo', 'medium', 'member', $3, 0, nextval('pg_catalog.pg_dist_node_seq'::regclass) % 100000, $4)
			RETURNING id
		`, testWorkspaceID, title, memberID, projectID).Scan(&id); err != nil {
			// Fallback: use a unique-enough number.
			if err2 := testPool.QueryRow(ctx, `
				INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number, project_id)
				VALUES ($1, $2, 'todo', 'medium', 'member', $3, 0, 50000 + floor(random() * 49000)::int, $4)
				RETURNING id
			`, testWorkspaceID, title, memberID, projectID).Scan(&id); err2 != nil {
				t.Fatalf("create issue: %v / %v", err, err2)
			}
		}
		return id
	}

	openIssueID := mkIssue("filter-open-issue", openID)
	restrictIssID := mkIssue("filter-restricted-issue", restrID)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = ANY($1)`, []string{openIssueID, restrictIssID})
		testPool.Exec(ctx, `DELETE FROM project_member WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM project WHERE id = ANY($1)`, []string{openID, restrID})
		testPool.Exec(ctx, `DELETE FROM member WHERE user_id = ANY($1)`, []string{memberID, outsideID})
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = ANY($1)`, []string{memberID, outsideID})
	})

	memberMember, err := testHandler.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      util.ParseUUID(memberID),
		WorkspaceID: util.ParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load member: %v", err)
	}
	outsideMember, err := testHandler.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      util.ParseUUID(outsideID),
		WorkspaceID: util.ParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load outside member: %v", err)
	}

	return filterFixture{
		memberUserID:  memberID,
		outsideUserID: outsideID,
		openProjectID: openID,
		restrictedID:  restrID,
		openIssueID:   openIssueID,
		restrictIssID: restrictIssID,
		memberMember:  memberMember,
		outsideMember: outsideMember,
	}
}

func newReqAs(method, path, userID string, member db.Member) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-User-ID", userID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	ctx := middleware.SetMemberContext(req.Context(), testWorkspaceID, member)
	return req.WithContext(ctx)
}

func TestListIssues_FiltersRestrictedForOutsider(t *testing.T) {
	f := setupFilterFixture(t)

	req := newReqAs("GET", "/api/issues", f.outsideUserID, f.outsideMember)
	rec := httptest.NewRecorder()
	testHandler.ListIssues(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Issues []map[string]any `json:"issues"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, iss := range body.Issues {
		if id, _ := iss["id"].(string); id == f.restrictIssID {
			t.Fatalf("outsider got restricted issue %s in ListIssues result", id)
		}
	}
}

func TestListIssues_IncludesRestrictedForMember(t *testing.T) {
	f := setupFilterFixture(t)

	req := newReqAs("GET", "/api/issues?limit=200", f.memberUserID, f.memberMember)
	rec := httptest.NewRecorder()
	testHandler.ListIssues(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var body struct {
		Issues []map[string]any `json:"issues"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	found := false
	for _, iss := range body.Issues {
		if id, _ := iss["id"].(string); id == f.restrictIssID {
			found = true
		}
	}
	if !found {
		t.Fatalf("project member did not see restricted issue in ListIssues")
	}
}

func TestGetProject_OutsiderGets404OnRestricted(t *testing.T) {
	f := setupFilterFixture(t)

	req := newReqAs("GET", "/api/projects/"+f.restrictedID, f.outsideUserID, f.outsideMember)
	req = withURLParam(req, "id", f.restrictedID)
	rec := httptest.NewRecorder()
	testHandler.GetProject(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("outsider on restricted project: want 404 got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetProject_MemberOK(t *testing.T) {
	f := setupFilterFixture(t)

	req := newReqAs("GET", "/api/projects/"+f.restrictedID, f.memberUserID, f.memberMember)
	req = withURLParam(req, "id", f.restrictedID)
	rec := httptest.NewRecorder()
	testHandler.GetProject(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("project member on restricted project: want 200 got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestListProjects_FiltersRestrictedForOutsider(t *testing.T) {
	f := setupFilterFixture(t)

	req := newReqAs("GET", "/api/projects", f.outsideUserID, f.outsideMember)
	rec := httptest.NewRecorder()
	testHandler.ListProjects(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Projects []map[string]any `json:"projects"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	for _, p := range body.Projects {
		if id, _ := p["id"].(string); id == f.restrictedID {
			t.Fatalf("outsider saw restricted project in ListProjects")
		}
	}
}
