package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// insertTestProject creates a project in the test workspace and registers a
// cleanup that removes it. status defaults to 'planned' at the DB level.
func insertTestProject(t *testing.T, title string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id`,
		testWorkspaceID, title).Scan(&id); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, id) })
	return id
}

// newTestEidetixBox builds a secretbox with a throwaway in-test key. NEVER use
// a real Eidetix token in tests; "fake-token" below is not a secret.
func newTestEidetixBox(t *testing.T) *secretbox.Box {
	t.Helper()
	key := make([]byte, secretbox.KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	return box
}

func TestEidetixTokenRoundTrip(t *testing.T) {
	box := newTestEidetixBox(t)
	const plain = "fake-token-not-a-secret"

	sealed, err := box.Seal([]byte(plain))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if string(sealed) == plain {
		t.Fatalf("sealed bytes must not equal plaintext")
	}
	opened, err := box.Open(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(opened) != plain {
		t.Errorf("round-trip = %q, want %q", opened, plain)
	}
}

func TestSetAndShowEidetixConfig_TokenNeverReturned(t *testing.T) {
	prev := testHandler.EidetixSecrets
	testHandler.EidetixSecrets = newTestEidetixBox(t)
	t.Cleanup(func() { testHandler.EidetixSecrets = prev })

	projectID := insertTestProject(t, "Eidetix Marketing")

	// PUT set
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPut, "/api/projects/"+projectID+"/eidetix", map[string]any{
		"token":       "fake-token-not-a-secret",
		"graph_label": "Marketing",
	})
	req = withURLParam(req, "id", projectID)
	testHandler.SetEidetixConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("set: status = %d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "fake-token-not-a-secret") {
		t.Fatalf("set response leaked the token: %s", w.Body.String())
	}

	// GET show
	w = httptest.NewRecorder()
	req = newRequest(http.MethodGet, "/api/projects/"+projectID+"/eidetix", nil)
	req = withURLParam(req, "id", projectID)
	testHandler.ShowEidetixConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("show: status = %d body=%s", w.Code, w.Body.String())
	}
	var show map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &show); err != nil {
		t.Fatalf("show body not JSON: %v", err)
	}
	if show["configured"] != true {
		t.Errorf("configured = %v, want true", show["configured"])
	}
	if show["enabled"] != true {
		t.Errorf("enabled = %v, want true", show["enabled"])
	}
	if show["graph_label"] != "Marketing" {
		t.Errorf("graph_label = %v, want Marketing", show["graph_label"])
	}
	if _, present := show["token"]; present {
		t.Errorf("show response must never include a token field")
	}
}

// TestEidetixConfigRejectsNonOwner proves the owner/admin write gate actually
// BLOCKS a workspace member who is neither owner nor admin: a plain 'member'
// PUT must return 403, never reaching the handler body.
func TestEidetixConfigRejectsNonOwner(t *testing.T) {
	prev := testHandler.EidetixSecrets
	testHandler.EidetixSecrets = newTestEidetixBox(t)
	t.Cleanup(func() { testHandler.EidetixSecrets = prev })

	projectID := insertTestProject(t, "Eidetix Gate")

	// Create a second user who is only a 'member' of the test workspace.
	// member.user_id REFERENCES "user"(id) ON DELETE CASCADE, so deleting the
	// user removes the member row too.
	var memberUserID string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Eidetix Member", "eidetix-member@example.test").Scan(&memberUserID); err != nil {
		t.Fatalf("insert member user: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberUserID) })
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, memberUserID); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequestAs(memberUserID, http.MethodPut, "/api/projects/"+projectID+"/eidetix", map[string]any{"token": "fake-token-not-a-secret"})
	req = withURLParam(req, "id", projectID)
	testHandler.SetEidetixConfig(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-owner PUT: status = %d, want 403; body=%s", w.Code, w.Body.String())
	}
}

func TestDisableThenClearEidetixConfig(t *testing.T) {
	prev := testHandler.EidetixSecrets
	testHandler.EidetixSecrets = newTestEidetixBox(t)
	t.Cleanup(func() { testHandler.EidetixSecrets = prev })

	projectID := insertTestProject(t, "Eidetix Toggle")

	// set first
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPut, "/api/projects/"+projectID+"/eidetix", map[string]any{"token": "fake-token-not-a-secret"})
	req = withURLParam(req, "id", projectID)
	testHandler.SetEidetixConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("set: %d %s", w.Code, w.Body.String())
	}

	// PATCH disable
	w = httptest.NewRecorder()
	req = newRequest(http.MethodPatch, "/api/projects/"+projectID+"/eidetix", map[string]any{"enabled": false})
	req = withURLParam(req, "id", projectID)
	testHandler.PatchEidetixConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	req = newRequest(http.MethodGet, "/api/projects/"+projectID+"/eidetix", nil)
	req = withURLParam(req, "id", projectID)
	testHandler.ShowEidetixConfig(w, req)
	var show map[string]any
	json.Unmarshal(w.Body.Bytes(), &show)
	if show["enabled"] != false {
		t.Errorf("after disable, enabled = %v, want false", show["enabled"])
	}

	// DELETE clear
	w = httptest.NewRecorder()
	req = newRequest(http.MethodDelete, "/api/projects/"+projectID+"/eidetix", nil)
	req = withURLParam(req, "id", projectID)
	testHandler.ClearEidetixConfig(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
		t.Fatalf("clear: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	req = newRequest(http.MethodGet, "/api/projects/"+projectID+"/eidetix", nil)
	req = withURLParam(req, "id", projectID)
	testHandler.ShowEidetixConfig(w, req)
	json.Unmarshal(w.Body.Bytes(), &show)
	if show["configured"] != false {
		t.Errorf("after clear, configured = %v, want false", show["configured"])
	}
}
