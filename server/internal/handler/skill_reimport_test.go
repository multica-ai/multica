package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// insertReimportSkill seeds a skill owned by testUserID with a clawhub origin
// pointing at the given slug, plus an outdated body, and returns its id.
func insertReimportSkill(t *testing.T, slug, content string) string {
	t.Helper()
	return insertReimportSkillCreatedBy(t, slug, content, testUserID)
}

// insertReimportSkillCreatedBy is insertReimportSkill with an explicit creator,
// so tests can seed a skill owned by someone other than the caller.
func insertReimportSkillCreatedBy(t *testing.T, slug, content, creatorID string) string {
	t.Helper()
	name := "reimport-" + t.Name()
	config := `{"origin":{"type":"clawhub","source_url":"https://clawhub.ai/acme/` + slug + `","slug":"` + slug + `"}}`
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)
		RETURNING id
	`, testWorkspaceID, name, "old description", content, config, creatorID).Scan(&id); err != nil {
		t.Fatalf("insert skill: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill_file WHERE skill_id = $1`, id)
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, id)
	})
	return id
}

// createTestUser inserts a real user row (the dev DB enforces the created_by /
// member user_id foreign keys) with a unique email, and returns its id.
func createTestUser(t *testing.T) string {
	t.Helper()
	var userID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email)
		VALUES ($1, gen_random_uuid()::text || '@test.local')
		RETURNING id
	`, "reimport-test-user").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

// addWorkspaceMember seeds a real user as a member of testWorkspaceID with the
// given role and returns its user id. Used to exercise role-based authorization.
func addWorkspaceMember(t *testing.T, role string) string {
	t.Helper()
	userID := createTestUser(t)
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, $3)
	`, testWorkspaceID, userID, role); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID)
	})
	return userID
}

// withMockClawHubReimport stands up a mock ClawHub API for the given slug and
// points clawHubAPIBase at it for the duration of the test.
func withMockClawHubReimport(t *testing.T, slug, displayName, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/skills/" + slug:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"skill": map[string]any{
					"slug":        slug,
					"displayName": displayName,
					"summary":     "fresh summary",
					"tags":        map[string]string{"latest": "2.0.0"},
				},
			})
		case "/api/v1/skills/" + slug + "/versions/2.0.0":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"version": map[string]any{
					"version": "2.0.0",
					"files":   []map[string]any{{"path": "SKILL.md", "size": len(body)}},
				},
			})
		case "/api/v1/skills/" + slug + "/file":
			_, _ = w.Write([]byte(body))
		default:
			t.Fatalf("unexpected ClawHub path: %s", r.URL.String())
		}
	}))
	prev := clawHubAPIBase
	clawHubAPIBase = srv.URL + "/api/v1"
	t.Cleanup(func() {
		clawHubAPIBase = prev
		srv.Close()
	})
}

// withGatedMockClawHubReimport is withMockClawHubReimport with the file
// download held open. It returns a channel that closes once the handler enters
// the download, plus a release channel the test closes to let it finish. A test
// can then change DB state while the upstream fetch is in flight.
func withGatedMockClawHubReimport(t *testing.T, slug, displayName, body string) (fetching <-chan struct{}, release chan<- struct{}) {
	t.Helper()
	reached := make(chan struct{})
	gate := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/skills/" + slug:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"skill": map[string]any{
					"slug":        slug,
					"displayName": displayName,
					"summary":     "fresh summary",
					"tags":        map[string]string{"latest": "2.0.0"},
				},
			})
		case "/api/v1/skills/" + slug + "/versions/2.0.0":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"version": map[string]any{
					"version": "2.0.0",
					"files":   []map[string]any{{"path": "SKILL.md", "size": len(body)}},
				},
			})
		case "/api/v1/skills/" + slug + "/file":
			once.Do(func() { close(reached) })
			<-gate
			_, _ = w.Write([]byte(body))
		default:
			t.Errorf("unexpected ClawHub path: %s", r.URL.String())
		}
	}))
	prev := clawHubAPIBase
	clawHubAPIBase = srv.URL + "/api/v1"
	t.Cleanup(func() {
		clawHubAPIBase = prev
		srv.Close()
	})
	return reached, gate
}

func callReimport(t *testing.T, userID, skillID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequestAsUser(userID, http.MethodPost, "/api/skills/"+skillID+"/reimport", nil)
	req = withURLParam(req, "id", skillID)
	testHandler.ReimportSkill(w, req)
	return w
}

func TestReimportSkill_OverwritesFromSource(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test DB not configured")
	}
	slug := "review-helper"
	id := insertReimportSkill(t, slug, "# Old body\n")
	// The overwrite guard requires the target's name to equal the imported name,
	// so the mock's displayName must match the seeded skill name.
	var name string
	if err := testPool.QueryRow(context.Background(), `SELECT name FROM skill WHERE id = $1`, id).Scan(&name); err != nil {
		t.Fatalf("read name: %v", err)
	}
	withMockClawHubReimport(t, slug, name, "# Fresh body\n")

	w := callReimport(t, testUserID, id)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp SkillWithFilesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID != id {
		t.Fatalf("id changed: got %s want %s", resp.ID, id)
	}
	if resp.Content != "# Fresh body\n" {
		t.Fatalf("content = %q, want fresh body", resp.Content)
	}
	if resp.Name != name {
		t.Fatalf("name = %q, want preserved %q", resp.Name, name)
	}
}

// A workspace owner/admin who is NOT the skill's creator may re-import it —
// anyone who can edit a skill can update it from its source (canManageSkill).
func TestReimportSkill_WorkspaceOwnerNonCreatorAllowed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test DB not configured")
	}
	slug := "review-helper"
	// Skill created by someone else; testUserID is the workspace owner.
	otherCreator := createTestUser(t)
	id := insertReimportSkillCreatedBy(t, slug, "# Old body\n", otherCreator)
	var name string
	if err := testPool.QueryRow(context.Background(), `SELECT name FROM skill WHERE id = $1`, id).Scan(&name); err != nil {
		t.Fatalf("read name: %v", err)
	}
	withMockClawHubReimport(t, slug, name, "# Fresh body\n")

	w := callReimport(t, testUserID, id)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (owner may re-import a skill they didn't create): %s", w.Code, w.Body.String())
	}
	var resp SkillWithFilesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Content != "# Fresh body\n" {
		t.Fatalf("content = %q, want fresh body", resp.Content)
	}
}

// A plain member who is neither an admin nor the creator cannot re-import —
// they can't edit the skill, so they can't update it from source either (403).
func TestReimportSkill_NonAdminMemberForbidden(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test DB not configured")
	}
	id := insertReimportSkill(t, "review-helper", "# Old\n") // created by testUserID
	memberID := addWorkspaceMember(t, "member")
	w := callReimport(t, memberID, id)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

// The upstream fetch runs for up to importFetchTimeout between the
// request-time permission check and the write. A caller who loses access in
// that window must not land the overwrite. The write tx re-reads membership,
// so revoking it mid-fetch returns 403 and leaves the skill untouched.
func TestReimportSkill_RoleRevokedDuringFetchForbidden(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test DB not configured")
	}
	slug := "review-helper"
	// The workspace owner created the skill. The caller is a separate admin, so
	// only their role authorizes them.
	id := insertReimportSkillCreatedBy(t, slug, "# Old body\n", testUserID)
	adminID := addWorkspaceMember(t, "admin")
	var name string
	if err := testPool.QueryRow(context.Background(), `SELECT name FROM skill WHERE id = $1`, id).Scan(&name); err != nil {
		t.Fatalf("read name: %v", err)
	}
	fetching, release := withGatedMockClawHubReimport(t, slug, name, "# Fresh body\n")

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- callReimport(t, adminID, id) }()

	select {
	case <-fetching:
	case <-time.After(10 * time.Second):
		t.Fatal("upstream fetch never started")
	}
	if _, err := testPool.Exec(context.Background(),
		`DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, adminID); err != nil {
		t.Fatalf("revoke membership: %v", err)
	}
	close(release)

	var w *httptest.ResponseRecorder
	select {
	case w = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("reimport never returned")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 after membership revoked mid-fetch: %s", w.Code, w.Body.String())
	}
	var content string
	if err := testPool.QueryRow(context.Background(), `SELECT content FROM skill WHERE id = $1`, id).Scan(&content); err != nil {
		t.Fatalf("read content: %v", err)
	}
	if content != "# Old body\n" {
		t.Fatalf("content = %q, want the original body left unchanged", content)
	}
}

// An edit that lands while the upstream fetch is in flight must survive. The
// write tx compares updated_at against the row the request read, so the
// overwrite fails with 409 and the edit stays.
func TestReimportSkill_ConcurrentEditConflicts(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test DB not configured")
	}
	slug := "review-helper"
	id := insertReimportSkill(t, slug, "# Old body\n")
	var name string
	if err := testPool.QueryRow(context.Background(), `SELECT name FROM skill WHERE id = $1`, id).Scan(&name); err != nil {
		t.Fatalf("read name: %v", err)
	}
	fetching, release := withGatedMockClawHubReimport(t, slug, name, "# Fresh body\n")

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- callReimport(t, testUserID, id) }()

	select {
	case <-fetching:
	case <-time.After(10 * time.Second):
		t.Fatal("upstream fetch never started")
	}
	// Another member edits the body without renaming the skill.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE skill SET content = $2, updated_at = now() WHERE id = $1`, id, "# Someone else's edit\n"); err != nil {
		t.Fatalf("concurrent edit: %v", err)
	}
	close(release)

	var w *httptest.ResponseRecorder
	select {
	case w = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("reimport never returned")
	}
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 after a concurrent edit: %s", w.Code, w.Body.String())
	}
	var content string
	if err := testPool.QueryRow(context.Background(), `SELECT content FROM skill WHERE id = $1`, id).Scan(&content); err != nil {
		t.Fatalf("read content: %v", err)
	}
	if content != "# Someone else's edit\n" {
		t.Fatalf("content = %q, want the concurrent edit preserved", content)
	}
}

func TestReimportSkill_ManualSkillNotUpdatable(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test DB not configured")
	}
	// insertHandlerTestSkill seeds config '{}' (manual, no origin), created by testUserID.
	id := insertHandlerTestSkill(t, "manual", "# Manual\n")
	w := callReimport(t, testUserID, id)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", w.Code, w.Body.String())
	}
}
