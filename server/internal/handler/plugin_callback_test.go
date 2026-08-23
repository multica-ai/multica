package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

// The identity model, exercised through the path that actually decides it.
//
// A hook handler calls back with a token, and the token — not the request body,
// not a header — says who the resulting writes belong to. This is the whole
// reason the callback token exists instead of handing a plugin's server the
// install token and trusting it to say who it is acting for.

func callbackRequest(token, method, path string, body any, params map[string]string) *http.Request {
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	// No X-User-ID: a plugin's own server has no session. The token is the
	// entire credential, which is what these tests are about.
	request.Header.Set("Authorization", "Bearer "+token)
	routeContext := chi.NewRouteContext()
	for key, value := range params {
		routeContext.URLParams.Add(key, value)
	}
	return request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
}

func issueCallbackToken(t *testing.T, installationID string, actor service.HookActor) string {
	t.Helper()
	installation, err := testHandler.PluginService.InstallationForWorkspace(
		context.Background(), parseUUID(testWorkspaceID), installationID)
	if err != nil {
		t.Fatalf("load installation: %v", err)
	}
	token, err := testHandler.PluginService.Callbacks.Issue(context.Background(), service.HookInvocation{
		Installation: installation,
		Hook:         plugincontract.Hook{Key: "summarize"},
		Trigger:      plugincontract.TriggerManual,
		Actor:        actor,
	})
	if err != nil {
		t.Fatalf("issue callback token: %v", err)
	}
	return token
}

func withCallbackTokens(t *testing.T) {
	t.Helper()
	previous := testHandler.PluginService.Callbacks
	testHandler.PluginService.Callbacks = service.NewCallbackTokens()
	t.Cleanup(func() { testHandler.PluginService.Callbacks = previous })
}

// A ui/manual hook ran because a person asked. Its writes stay that person's,
// with via_plugin_id recording that a plugin produced them — permission-wise
// the write is the user's, audit-wise it stays traceable to the plugin.
func TestCallbackFromAUserTriggeredHookWritesAsTheUser(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	withCallbackTokens(t)
	installationID := installHookPlugin(t)
	issueID := createTestIssue(t, "Callback attribution member", "todo", "none")

	token := issueCallbackToken(t, installationID, service.HookActor{Type: "member", ID: parseUUID(testUserID)})
	recorder := httptest.NewRecorder()
	testHandler.CreatePluginComment(recorder, callbackRequest(token, http.MethodPost,
		"/v1/issues/"+issueID+"/comments",
		map[string]any{"content": "posted by a manual hook"},
		map[string]string{"id": issueID}))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	comment := latestComment(t, issueID)
	if comment.AuthorType != "member" {
		t.Fatalf("author_type = %q, want member: a hook the user triggered must stay attributed to them", comment.AuthorType)
	}
	if uuidToString(comment.AuthorID) != testUserID {
		t.Fatalf("author_id = %s, want the triggering user %s", uuidToString(comment.AuthorID), testUserID)
	}
	if uuidToString(comment.ViaPluginID) != installationID {
		t.Fatalf("via_plugin_id = %s, want %s: the plugin must stay traceable", uuidToString(comment.ViaPluginID), installationID)
	}
}

// An event hook has nobody behind it. Attributing its writes to the last member
// who happened to touch the issue would be a lie the audit trail cannot undo.
func TestCallbackFromAnEventHookWritesAsThePlugin(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	withCallbackTokens(t)
	installationID := installHookPlugin(t)
	issueID := createTestIssue(t, "Callback attribution plugin", "todo", "none")

	token := issueCallbackToken(t, installationID, service.HookActor{Type: "plugin", ID: parseUUID(installationID)})
	recorder := httptest.NewRecorder()
	testHandler.CreatePluginComment(recorder, callbackRequest(token, http.MethodPost,
		"/v1/issues/"+issueID+"/comments",
		map[string]any{"content": "posted by an event hook"},
		map[string]string{"id": issueID}))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	comment := latestComment(t, issueID)
	if comment.AuthorType != "plugin" {
		t.Fatalf("author_type = %q, want plugin: an event hook must not borrow a person's identity", comment.AuthorType)
	}
	if uuidToString(comment.AuthorID) != installationID {
		t.Fatalf("author_id = %s, want the installation %s", uuidToString(comment.AuthorID), installationID)
	}
}

// A grant is good for the whole invocation, not one request. A handler that
// reads an issue and then comments on it makes two calls, and the single-use
// version of this refused the second — the failure a live run surfaced.
func TestCallbackTokenServesTheWholeInvocation(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	withCallbackTokens(t)
	installationID := installHookPlugin(t)
	issueID := createTestIssue(t, "Callback single use", "todo", "none")

	token := issueCallbackToken(t, installationID, service.HookActor{Type: "member", ID: parseUUID(testUserID)})
	first := httptest.NewRecorder()
	testHandler.GetPluginIssue(first, callbackRequest(token, http.MethodGet,
		"/v1/issues/"+issueID, nil, map[string]string{"id": issueID}))
	if first.Code != http.StatusOK {
		t.Fatalf("first use: status=%d body=%s", first.Code, first.Body.String())
	}

	// The read-then-write shape every non-trivial handler has.
	second := httptest.NewRecorder()
	testHandler.CreatePluginComment(second, callbackRequest(token, http.MethodPost,
		"/v1/issues/"+issueID+"/comments",
		map[string]any{"content": "and now a comment about what I read"},
		map[string]string{"id": issueID}))
	if second.Code != http.StatusCreated {
		t.Fatalf("a handler must be able to write after reading: status=%d body=%s", second.Code, second.Body.String())
	}

	// Revoked once the invocation is over.
	testHandler.PluginService.Callbacks.Revoke(token)
	third := httptest.NewRecorder()
	testHandler.GetPluginIssue(third, callbackRequest(token, http.MethodGet,
		"/v1/issues/"+issueID, nil, map[string]string{"id": issueID}))
	if third.Code != http.StatusForbidden {
		t.Fatalf("a revoked grant must be refused: status=%d body=%s", third.Code, third.Body.String())
	}
}

// storage:user is per-member state. A caller with no member has no such scope
// to resolve, and falling through would key every plugin-actor write to the
// zero UUID — one shared bucket masquerading as somebody's private one.
func TestPluginActorCannotReachUserStorage(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	withCallbackTokens(t)
	installationID := installPluginForAction(t, []string{"issues:read", "comments:write", "storage:user"})

	token := issueCallbackToken(t, installationID, service.HookActor{Type: "plugin", ID: parseUUID(installationID)})
	recorder := httptest.NewRecorder()
	testHandler.PutPluginStorage(recorder, callbackRequest(token, http.MethodPut,
		"/v1/storage/user/pref",
		map[string]any{"value": "x"},
		map[string]string{"scope": "user", "key": "pref"}))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s, want 403", recorder.Code, recorder.Body.String())
	}
}

// /context has no scope requirement, but it must not invent a user for a caller
// that has none — a handler reading `user` would believe it is acting for
// somebody.
func TestPluginContextOmitsTheUserForAPluginActor(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	withCallbackTokens(t)
	installationID := installHookPlugin(t)

	token := issueCallbackToken(t, installationID, service.HookActor{Type: "plugin", ID: parseUUID(installationID)})
	recorder := httptest.NewRecorder()
	testHandler.GetPluginContext(recorder, callbackRequest(token, http.MethodGet, "/v1/context", nil, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode context: %v", err)
	}
	if _, present := payload["user"]; present {
		t.Fatalf("context carried a user for a plugin actor: %s", recorder.Body.String())
	}
	if payload["actor"] != "plugin" {
		t.Fatalf("actor = %v, want plugin", payload["actor"])
	}
}

// latestComment reads the row directly. The response body reports what the
// handler decided to return; the row is what was actually written, and
// attribution is only meaningful as stored.
func latestComment(t *testing.T, issueID string) db.Comment {
	t.Helper()
	var comment db.Comment
	err := testPool.QueryRow(context.Background(),
		`SELECT id, author_type, author_id, via_plugin_id FROM comment WHERE issue_id = $1 ORDER BY created_at DESC LIMIT 1`,
		issueID,
	).Scan(&comment.ID, &comment.AuthorType, &comment.AuthorID, &comment.ViaPluginID)
	if err != nil {
		t.Fatalf("read the written comment: %v", err)
	}
	return comment
}

// The narrowing that was only a comment until Fabel 5's review.
//
// A hook called about one issue has no business reaching another. Without this
// check the grant was worth every issue in the workspace its actor could see,
// for the whole five minutes it lived — and both the type's doc comment and the
// PR description claimed otherwise.
func TestCallbackTokenCannotReachAnotherIssue(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	withCallbackTokens(t)
	installationID := installHookPlugin(t)
	granted := createTestIssue(t, "Callback scope granted issue", "todo", "none")
	other := createTestIssue(t, "Callback scope other issue", "todo", "none")

	installation, err := testHandler.PluginService.InstallationForWorkspace(
		context.Background(), parseUUID(testWorkspaceID), installationID)
	if err != nil {
		t.Fatalf("load installation: %v", err)
	}
	token, err := testHandler.PluginService.Callbacks.Issue(context.Background(), service.HookInvocation{
		Installation: installation,
		Hook:         plugincontract.Hook{Key: "summarize"},
		Trigger:      plugincontract.TriggerManual,
		Actor:        service.HookActor{Type: "member", ID: parseUUID(testUserID)},
		IssueID:      parseUUID(granted),
	})
	if err != nil {
		t.Fatalf("issue callback token: %v", err)
	}

	// The issue it was issued for: allowed.
	ok := httptest.NewRecorder()
	testHandler.GetPluginIssue(ok, callbackRequest(token, http.MethodGet,
		"/v1/issues/"+granted, nil, map[string]string{"id": granted}))
	if ok.Code != http.StatusOK {
		t.Fatalf("the granted issue must be reachable: status=%d body=%s", ok.Code, ok.Body.String())
	}

	// Any other issue in the same workspace: refused, on both read and write.
	refusedRead := httptest.NewRecorder()
	testHandler.GetPluginIssue(refusedRead, callbackRequest(token, http.MethodGet,
		"/v1/issues/"+other, nil, map[string]string{"id": other}))
	if refusedRead.Code != http.StatusNotFound {
		t.Fatalf("reading another issue must be refused: status=%d body=%s", refusedRead.Code, refusedRead.Body.String())
	}

	refusedWrite := httptest.NewRecorder()
	testHandler.CreatePluginComment(refusedWrite, callbackRequest(token, http.MethodPost,
		"/v1/issues/"+other+"/comments",
		map[string]any{"content": "should never land"},
		map[string]string{"id": other}))
	if refusedWrite.Code != http.StatusNotFound {
		t.Fatalf("commenting on another issue must be refused: status=%d body=%s", refusedWrite.Code, refusedWrite.Body.String())
	}
}

// An invocation with no issue produces an unscoped grant. That is not a hole —
// it simply means there was no issue to narrow to — and the workspace and
// membership checks still apply.
func TestCallbackTokenWithoutAnIssueIsNotNarrowed(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	cleanupPluginInstallations(t)
	withCallbackTokens(t)
	installationID := installHookPlugin(t)
	issueID := createTestIssue(t, "Callback unscoped grant", "todo", "none")

	token := issueCallbackToken(t, installationID, service.HookActor{Type: "member", ID: parseUUID(testUserID)})
	recorder := httptest.NewRecorder()
	testHandler.GetPluginIssue(recorder, callbackRequest(token, http.MethodGet,
		"/v1/issues/"+issueID, nil, map[string]string{"id": issueID}))
	if recorder.Code != http.StatusOK {
		t.Fatalf("an unscoped grant must still reach the workspace: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
