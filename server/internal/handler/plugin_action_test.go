package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// These are the Action API's security properties, not its happy path. Each one
// is a way a plugin could exceed what the person using it can do, and none of
// them is visible by reading the handler — they only hold if the three checks
// run in the right order against the right inputs.

// installPluginForAction installs the reference plugin and returns its id,
// leaving the caller free to vary scopes.
func installPluginForAction(t *testing.T, scopes []string) string {
	t.Helper()
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)

	manifest := handlerTestManifest
	if scopes != nil {
		encoded, err := json.Marshal(scopes)
		if err != nil {
			t.Fatalf("encode scopes: %v", err)
		}
		manifest = strings.Replace(manifest,
			`"scopes": ["issues:read", "comments:write", "storage:user"]`,
			`"scopes": `+string(encoded), 1)
	}
	source := withLocalPluginSource(t, manifest)

	body, _ := json.Marshal(map[string]any{"source_url": source, "granted_scopes": scopes})
	recorder := httptest.NewRecorder()
	testHandler.InstallPlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins", body, map[string]string{"id": testWorkspaceID}))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("install status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var installed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &installed); err != nil {
		t.Fatalf("decode installation: %v", err)
	}
	return installed.ID
}

func pluginActionRequest(method, path, installationID string, body any, params map[string]string) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	request := httptest.NewRequest(method, path, &buf)
	request.Header.Set("X-User-ID", testUserID)
	request.Header.Set("Content-Type", "application/json")
	if installationID != "" {
		request.Header.Set(pluginInstallationHeader, installationID)
	}
	routeContext := chi.NewRouteContext()
	for key, value := range params {
		routeContext.URLParams.Add(key, value)
	}
	return request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
}

func TestPluginActionRequiresAGrantedScope(t *testing.T) {
	// Installed with issues:read only. Everything else must be refused, and the
	// refusal must name the missing scope so an admin can act on it.
	installationID := installPluginForAction(t, []string{"issues:read"})
	issueID := createTestIssue(t, "Plugin action scope test", "todo", "none")

	recorder := httptest.NewRecorder()
	testHandler.GetPluginIssue(recorder, pluginActionRequest(http.MethodGet, "/issues", installationID, nil,
		map[string]string{"id": issueID}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("granted scope was refused: status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	testHandler.CreatePluginComment(recorder, pluginActionRequest(http.MethodPost, "/comments", installationID,
		map[string]any{"content": "hello"}, map[string]string{"id": issueID}))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("ungranted scope status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "comments:write") {
		t.Fatalf("refusal does not name the missing scope: %s", recorder.Body.String())
	}
}

func TestPluginActionRefusedWithoutAnInstallation(t *testing.T) {
	installPluginForAction(t, []string{"issues:read"})
	issueID := createTestIssue(t, "Plugin action installation test", "todo", "none")

	// No header at all: the endpoint must not fall back to "any installed
	// plugin" or to an unscoped call.
	recorder := httptest.NewRecorder()
	testHandler.GetPluginIssue(recorder, pluginActionRequest(http.MethodGet, "/issues", "", nil,
		map[string]string{"id": issueID}))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing installation header status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	// An installation id that does not exist is a 404, never a pass-through.
	recorder = httptest.NewRecorder()
	testHandler.GetPluginIssue(recorder, pluginActionRequest(http.MethodGet, "/issues",
		"11111111-1111-1111-1111-111111111111", nil, map[string]string{"id": issueID}))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown installation status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestPluginActionStopsWhenTheInstallationIsDisabled(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read"})
	issueID := createTestIssue(t, "Plugin action disable test", "todo", "none")

	recorder := httptest.NewRecorder()
	testHandler.DisablePlugin(recorder, pluginHandlerRequest(http.MethodPost, "/plugins/disable", nil,
		map[string]string{"id": testWorkspaceID, "installationId": installationID}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	// A surface left open in a stale tab keeps its bridge; disabling has to cut
	// it off server-side or "disabled" only means "hidden".
	recorder = httptest.NewRecorder()
	testHandler.GetPluginIssue(recorder, pluginActionRequest(http.MethodGet, "/issues", installationID, nil,
		map[string]string{"id": issueID}))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("disabled installation status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestPluginCommentIsAuthoredByTheUserAndMarkedWithThePlugin(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read", "comments:write"})
	issueID := createTestIssue(t, "Plugin action attribution test", "todo", "none")

	recorder := httptest.NewRecorder()
	testHandler.CreatePluginComment(recorder, pluginActionRequest(http.MethodPost, "/comments", installationID,
		map[string]any{"content": "posted through a plugin"}, map[string]string{"id": issueID}))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create comment status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var created struct {
		ID         string `json:"id"`
		AuthorType string `json:"author_type"`
		AuthorID   string `json:"author_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	// Permission-wise the write is the user's...
	if created.AuthorType != "member" || created.AuthorID != testUserID {
		t.Fatalf("comment was not authored by the calling user: %+v", created)
	}
	// ...and audit-wise it stays attributable to the plugin that produced it.
	var viaPlugin *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT via_plugin_id::text FROM comment WHERE id = $1`, created.ID).Scan(&viaPlugin); err != nil {
		t.Fatalf("read via_plugin_id: %v", err)
	}
	if viaPlugin == nil || *viaPlugin != installationID {
		t.Fatalf("via_plugin_id = %v, want %s", viaPlugin, installationID)
	}
}

func TestPluginActionCannotReachAnotherWorkspacesIssue(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read"})

	// An issue id from a workspace this user is not a member of must 404 — the
	// plugin inherits the user's reach and nothing more, so this is the same
	// answer the ordinary issue endpoint gives.
	otherWorkspaceIssue := createIssueInForeignWorkspace(t)
	recorder := httptest.NewRecorder()
	testHandler.GetPluginIssue(recorder, pluginActionRequest(http.MethodGet, "/issues", installationID, nil,
		map[string]string{"id": otherWorkspaceIssue}))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace read status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

// createIssueInForeignWorkspace seeds a workspace the test user is NOT a member
// of, so a cross-tenant read has something real to fail against rather than a
// made-up uuid that would 404 for the wrong reason.
func createIssueInForeignWorkspace(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	var ownerID string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Foreign Owner', $1) RETURNING id`,
		"foreign-plugin-"+testUserID+"@multica.test").Scan(&ownerID); err != nil {
		t.Fatalf("seed foreign user: %v", err)
	}
	var workspaceID string
	if err := testPool.QueryRow(ctx, `INSERT INTO workspace (name, slug) VALUES ('Foreign', $1) RETURNING id`,
		"foreign-plugin-"+testUserID).Scan(&workspaceID); err != nil {
		t.Fatalf("seed foreign workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		workspaceID, ownerID); err != nil {
		t.Fatalf("seed foreign member: %v", err)
	}
	var issueID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id)
		 VALUES ($1, 'Foreign issue', 'todo', 'none', 'member', $2) RETURNING id`,
		workspaceID, ownerID).Scan(&issueID); err != nil {
		t.Fatalf("seed foreign issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1`, workspaceID)
		testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, ownerID)
	})
	return issueID
}
