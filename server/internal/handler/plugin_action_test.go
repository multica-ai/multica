package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	publicapiv1 "github.com/multica-ai/multica/server/pkg/publicapi/v1"
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
	versionID := withLocalPluginSource(t, manifest)

	body, _ := json.Marshal(map[string]any{"version_id": versionID, "granted_scopes": scopes})
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

func pluginInstallTokenRequest(method, path, token string, body any, params map[string]string) *http.Request {
	request := pluginActionRequest(method, path, "", body, params)
	request.Header.Del("X-User-ID")
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

func TestPluginInstallTokenRunsIssueCommentWorkflow(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read", "issues:write", "comments:read", "comments:write"})
	token, err := testHandler.PluginService.IssueInstallToken(context.Background(), parseUUID(installationID))
	if err != nil {
		t.Fatalf("issue install token: %v", err)
	}
	issueID := createTestIssue(t, "Install token workflow", "todo", "none")

	get := httptest.NewRecorder()
	testHandler.GetPluginIssue(get, pluginInstallTokenRequest(http.MethodGet, "/v1/issues/"+issueID, token, nil,
		map[string]string{"issue_ref": issueID}))
	if get.Code != http.StatusOK {
		t.Fatalf("install-token issue read status=%d body=%s", get.Code, get.Body.String())
	}

	patchRequest := pluginInstallTokenRequest(http.MethodPatch, "/v1/issues/"+issueID, token,
		map[string]any{"title": "Updated by install token"}, map[string]string{"issue_ref": issueID})
	patchRequest.Header.Set("If-Match", get.Header().Get("ETag"))
	patch := httptest.NewRecorder()
	testHandler.PatchPluginIssue(patch, patchRequest)
	if patch.Code != http.StatusOK {
		t.Fatalf("install-token issue patch status=%d body=%s", patch.Code, patch.Body.String())
	}

	create := httptest.NewRecorder()
	testHandler.CreatePluginComment(create, pluginInstallTokenRequest(http.MethodPost, "/v1/issues/"+issueID+"/comments", token,
		map[string]any{"content": "created with a real mpi token"}, map[string]string{"issue_ref": issueID}))
	if create.Code != http.StatusCreated {
		t.Fatalf("install-token comment create status=%d body=%s", create.Code, create.Body.String())
	}
	var comment publicapiv1.Comment
	if err := json.Unmarshal(create.Body.Bytes(), &comment); err != nil {
		t.Fatalf("decode install-token comment: %v", err)
	}
	if comment.AuthorType != "plugin" || comment.AuthorID != installationID {
		t.Fatalf("install-token comment attribution = %+v", comment)
	}

	list := httptest.NewRecorder()
	testHandler.ListPluginComments(list, pluginInstallTokenRequest(http.MethodGet, "/v1/issues/"+issueID+"/comments", token, nil,
		map[string]string{"issue_ref": issueID}))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), comment.ID) {
		t.Fatalf("install-token comment list status=%d body=%s", list.Code, list.Body.String())
	}
}

func TestPluginInstallTokenEnforcesGrantedScope(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read"})
	token, err := testHandler.PluginService.IssueInstallToken(context.Background(), parseUUID(installationID))
	if err != nil {
		t.Fatalf("issue install token: %v", err)
	}
	issueID := createTestIssue(t, "Install token scope", "todo", "none")

	request := pluginInstallTokenRequest(http.MethodPatch, "/v1/issues/"+issueID, token,
		map[string]any{"title": "must not update"}, map[string]string{"issue_ref": issueID})
	response := httptest.NewRecorder()
	testHandler.PatchPluginIssue(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("ungranted install-token patch status=%d body=%s", response.Code, response.Body.String())
	}
	var problem publicapiv1.Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode scope problem: %v", err)
	}
	if problem.Code != "forbidden" || !strings.Contains(problem.Detail, "issues:write") {
		t.Fatalf("unexpected scope problem: %+v", problem)
	}
}

func TestPluginActionRequiresAGrantedScope(t *testing.T) {
	// Installed with issues:read only. Everything else must be refused, and the
	// refusal must name the missing scope so an admin can act on it.
	installationID := installPluginForAction(t, []string{"issues:read"})
	issueID := createTestIssue(t, "Plugin action scope test", "todo", "none")

	recorder := httptest.NewRecorder()
	testHandler.GetPluginIssue(recorder, pluginActionRequest(http.MethodGet, "/issues", installationID, nil,
		map[string]string{"issue_ref": issueID}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("granted scope was refused: status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	testHandler.CreatePluginComment(recorder, pluginActionRequest(http.MethodPost, "/comments", installationID,
		map[string]any{"content": "hello"}, map[string]string{"issue_ref": issueID}))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("ungranted scope status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "comments:write") {
		t.Fatalf("refusal does not name the missing scope: %s", recorder.Body.String())
	}
	var problem publicapiv1.Problem
	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode stable problem: %v", err)
	}
	if problem.Code != "forbidden" || problem.Error == "" || problem.RequestID == "" {
		t.Fatalf("unexpected stable problem: %+v", problem)
	}
}

func TestPluginIssueUsesStableDTOAndRevisionETag(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read", "issues:write"})
	issueID := createTestIssue(t, "Plugin public DTO test", "todo", "none")

	get := httptest.NewRecorder()
	testHandler.GetPluginIssue(get, pluginActionRequest(http.MethodGet, "/v1/issues/"+issueID, installationID, nil,
		map[string]string{"issue_ref": issueID}))
	if get.Code != http.StatusOK {
		t.Fatalf("get issue status=%d body=%s", get.Code, get.Body.String())
	}
	var issue publicapiv1.Issue
	if err := json.Unmarshal(get.Body.Bytes(), &issue); err != nil {
		t.Fatalf("decode public issue: %v", err)
	}
	if issue.ID != issueID || issue.Identifier == "" || issue.Revision < 1 || issue.Metadata == nil || issue.Properties == nil {
		t.Fatalf("unexpected public issue: %+v", issue)
	}
	wantETag := fmt.Sprintf(`W/"%d"`, issue.Revision)
	if got := get.Header().Get("ETag"); got != wantETag {
		t.Fatalf("ETag = %q, want %q", got, wantETag)
	}
	var raw map[string]any
	if err := json.Unmarshal(get.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode issue map: %v", err)
	}
	for _, appOnly := range []string{"labels", "attachments", "reactions"} {
		if _, leaked := raw[appOnly]; leaked {
			t.Fatalf("App-only field %q leaked into Public API: %s", appOnly, get.Body.String())
		}
	}

	patchRequest := pluginActionRequest(http.MethodPatch, "/v1/issues/"+issueID, installationID,
		map[string]any{"title": "Updated through Public API"}, map[string]string{"issue_ref": issueID})
	patchRequest.Header.Set("If-Match", wantETag)
	patch := httptest.NewRecorder()
	testHandler.PatchPluginIssue(patch, patchRequest)
	if patch.Code != http.StatusOK {
		t.Fatalf("patch issue status=%d body=%s", patch.Code, patch.Body.String())
	}
	var updated publicapiv1.Issue
	if err := json.Unmarshal(patch.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated issue: %v", err)
	}
	if updated.Title != "Updated through Public API" || updated.Revision <= issue.Revision {
		t.Fatalf("unexpected updated issue: %+v", updated)
	}
	if got := patch.Header().Get("ETag"); got != fmt.Sprintf(`W/"%d"`, updated.Revision) {
		t.Fatalf("updated ETag = %q", got)
	}

	staleRequest := pluginActionRequest(http.MethodPatch, "/v1/issues/"+issueID, installationID,
		map[string]any{"description": "stale"}, map[string]string{"issue_ref": issueID})
	staleRequest.Header.Set("If-Match", wantETag)
	stale := httptest.NewRecorder()
	testHandler.PatchPluginIssue(stale, staleRequest)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale patch status=%d body=%s", stale.Code, stale.Body.String())
	}
	var conflict publicapiv1.Problem
	if err := json.Unmarshal(stale.Body.Bytes(), &conflict); err != nil {
		t.Fatalf("decode revision conflict: %v", err)
	}
	if conflict.Code != "revision_conflict" {
		t.Fatalf("conflict code = %q", conflict.Code)
	}
}

func TestPluginIssueConditionalPatchHasSingleWinner(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read", "issues:write"})
	issueID := createTestIssue(t, "Plugin conditional write test", "todo", "none")

	get := httptest.NewRecorder()
	testHandler.GetPluginIssue(get, pluginActionRequest(http.MethodGet, "/v1/issues/"+issueID, installationID, nil,
		map[string]string{"issue_ref": issueID}))
	if get.Code != http.StatusOK {
		t.Fatalf("get issue status=%d body=%s", get.Code, get.Body.String())
	}
	etag := get.Header().Get("ETag")

	start := make(chan struct{})
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for _, title := range []string{"concurrent winner A", "concurrent winner B"} {
		wg.Add(1)
		go func(title string) {
			defer wg.Done()
			request := pluginActionRequest(http.MethodPatch, "/v1/issues/"+issueID, installationID,
				map[string]any{"title": title}, map[string]string{"issue_ref": issueID})
			request.Header.Set("If-Match", etag)
			<-start
			response := httptest.NewRecorder()
			testHandler.PatchPluginIssue(response, request)
			results <- response.Code
		}(title)
	}
	close(start)
	wg.Wait()
	close(results)

	counts := map[int]int{}
	for status := range results {
		counts[status]++
	}
	if counts[http.StatusOK] != 1 || counts[http.StatusConflict] != 1 {
		t.Fatalf("conditional write statuses = %v, want one 200 and one 409", counts)
	}
}

func TestPluginActionRefusedWithoutAnInstallation(t *testing.T) {
	installPluginForAction(t, []string{"issues:read"})
	issueID := createTestIssue(t, "Plugin action installation test", "todo", "none")

	// No header at all: the endpoint must not fall back to "any installed
	// plugin" or to an unscoped call.
	recorder := httptest.NewRecorder()
	testHandler.GetPluginIssue(recorder, pluginActionRequest(http.MethodGet, "/issues", "", nil,
		map[string]string{"issue_ref": issueID}))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing installation header status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	// An installation id that does not exist is a 404, never a pass-through.
	recorder = httptest.NewRecorder()
	testHandler.GetPluginIssue(recorder, pluginActionRequest(http.MethodGet, "/issues",
		"11111111-1111-1111-1111-111111111111", nil, map[string]string{"issue_ref": issueID}))
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
		map[string]string{"issue_ref": issueID}))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("disabled installation status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestPluginCommentIsAuthoredByTheUserAndMarkedWithThePlugin(t *testing.T) {
	installationID := installPluginForAction(t, []string{"issues:read", "comments:write"})
	issueID := createTestIssue(t, "Plugin action attribution test", "todo", "none")

	recorder := httptest.NewRecorder()
	testHandler.CreatePluginComment(recorder, pluginActionRequest(http.MethodPost, "/comments", installationID,
		map[string]any{"content": "posted through a plugin"}, map[string]string{"issue_ref": issueID}))
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
		map[string]string{"issue_ref": otherWorkspaceIssue}))
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
