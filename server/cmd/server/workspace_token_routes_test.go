package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
)

// routesOpenToWorkspaceTokens lists the user-scoped routes that answer a
// workspace token, keyed "METHOD pattern", each with what keeps the token
// inside its workspace there. Every other route behind Auth must either
// refuse a workspace token or require membership in its workspace; see
// TestWorkspaceTokenRoutes.
var routesOpenToWorkspaceTokens = map[string]string{
	"GET /api/me":          "returns the token owner's own profile and nothing from any workspace",
	"GET /api/workspaces/": "ListWorkspaces lists only the workspace a bound credential is confined to",
	"POST /api/upload-file": "resolves the workspace through ResolveWorkspaceIDFromRequest, which pins a bound " +
		"credential to its workspace, then checks membership in the handler",
	"GET /api/attachments/{id}/download":          "loadAttachmentForRequest refuses an attachment outside the bound workspace",
	"GET /api/attachments/{id}/thumbnail":         "loadAttachmentForRequest refuses an attachment outside the bound workspace",
	"GET /api/attachments/{id}/original":          "loadAttachmentForRequest refuses an attachment outside the bound workspace",
	"GET /api/attachments/{id}/download-metadata": "loadAttachmentForRequest refuses an attachment outside the bound workspace",
}

const (
	authMissingCredentialBody     = `{"error":"missing authorization"}`
	workspaceTokenRefusedBody     = `{"error":"workspace tokens cannot access account-level endpoints"}`
	workspaceMembershipDeniedBody = `{"error":"workspace not found"}`
)

// A workspace token must reach nothing outside its workspace. Routes that act
// on the account refuse it; the rest hold the caller to a workspace. This
// walks the real router with a workspace token whose owner is NOT a member of
// the token's workspace, so the only acceptable answers are the account-route
// refusal and the membership denial: any other answer comes from a route that
// let the token through on the strength of its owner alone. A new route is
// covered the moment it is registered.
func TestWorkspaceTokenRoutes(t *testing.T) {
	db := dbfx.New(testPool, testWorkspaceID, testUserID)
	workspaceID := db.Workspace(t, "Workspace token routes", "ws-token-routes-"+uuid.NewString())
	owner := db.User(t, "Workspace token owner", "ws-token-owner-"+uuid.NewString()+"@example.test")
	token := auth.WorkspaceTokenPrefix + strings.ReplaceAll(uuid.NewString(), "-", "")
	db.InsertNoID(t, "personal_access_token", dbfx.Cols{
		"user_id": owner, "name": "routes", "token_hash": auth.HashToken(token),
		"token_prefix": token[:12], "workspace_id": workspaceID,
	}, "token_hash = $1", auth.HashToken(token))

	router, _ := NewRouterWithOptions(testPool, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil, RouterOptions{})

	call := func(method, path, bearer string) (int, string) {
		req := httptest.NewRequest(method, path, nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code, strings.TrimSpace(rec.Body.String())
	}

	// Every URL parameter is filled with the token's workspace, so a route
	// that takes the workspace from its path is asked for the one workspace
	// the token may name.
	fill := func(pattern string) string {
		return chiRouteParam.ReplaceAllString(strings.ReplaceAll(pattern, "/*", ""), workspaceID)
	}

	type route struct{ method, pattern string }
	var routes []route
	err := chi.Walk(router, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, route{method, pattern})
		return nil
	})
	if err != nil {
		t.Fatalf("walk router: %v", err)
	}
	sort.Slice(routes, func(i, j int) bool {
		return routes[i].method+" "+routes[i].pattern < routes[j].method+" "+routes[j].pattern
	})

	behindAuth, refused, membership := 0, 0, 0
	open := map[string]bool{}
	for _, rt := range routes {
		key := rt.method + " " + rt.pattern
		path := fill(rt.pattern)
		// Auth is what answers a request with no credential on the routes it
		// guards; anything else is public or behind another scheme.
		if status, body := call(rt.method, path, ""); status != http.StatusUnauthorized || body != authMissingCredentialBody {
			continue
		}
		behindAuth++

		status, body := call(rt.method, path, token)
		switch {
		case status == http.StatusForbidden && body == workspaceTokenRefusedBody:
			refused++
		case status == http.StatusNotFound && body == workspaceMembershipDeniedBody:
			membership++
		default:
			open[key] = true
			if _, listed := routesOpenToWorkspaceTokens[key]; !listed {
				t.Errorf("%s answered a workspace token whose owner is not in its workspace (status %d: %s). "+
					"Register it on the router that refuses workspace tokens, or behind the workspace middleware; "+
					"if it holds the caller to a workspace in its handler, add it to routesOpenToWorkspaceTokens with the reason.",
					key, status, body)
			}
		}
	}

	for key := range routesOpenToWorkspaceTokens {
		if !open[key] {
			t.Errorf("routesOpenToWorkspaceTokens lists %q, which no longer answers a workspace token", key)
		}
	}
	// Guards against the walk silently stopping to recognize Auth's routes.
	if refused == 0 || membership == 0 {
		t.Fatalf("walk found %d routes behind Auth: %d refused, %d membership-gated; expected both kinds", behindAuth, refused, membership)
	}
	t.Logf("%d routes behind Auth: %d refuse workspace tokens, %d require workspace membership, %d open", behindAuth, refused, membership, len(open))
}

// The walk above proves which routes a workspace token may reach at all. This
// follows one real token, whose owner belongs to two workspaces, through the
// router: it works in its workspace, and reaches neither the owner's other
// workspace nor the owner's account.
func TestWorkspaceTokenStaysInItsWorkspace(t *testing.T) {
	db := dbfx.New(testPool, testWorkspaceID, testUserID)
	owner := db.User(t, "Workspace token member", "ws-token-member-"+uuid.NewString()+"@example.test")
	otherSlug := "ws-token-e2e-other-" + uuid.NewString()
	bound := db.Workspace(t, "Workspace token e2e bound", "ws-token-e2e-bound-"+uuid.NewString())
	other := db.Workspace(t, "Workspace token e2e other", otherSlug)
	db.Member(t, bound, owner, "owner")
	db.Member(t, other, owner, "owner")
	db.Cleanup(t, `DELETE FROM inbox_item WHERE recipient_id = $1`, owner)
	for _, workspaceID := range []string{bound, other} {
		db.Insert(t, "inbox_item", dbfx.Cols{"workspace_id": workspaceID, "recipient_type": "member",
			"recipient_id": owner, "type": "new_comment", "severity": "info", "title": "Unread"})
	}

	token := auth.WorkspaceTokenPrefix + strings.ReplaceAll(uuid.NewString(), "-", "")
	db.InsertNoID(t, "personal_access_token", dbfx.Cols{
		"user_id": owner, "name": "e2e", "token_hash": auth.HashToken(token),
		"token_prefix": token[:12], "workspace_id": bound,
	}, "token_hash = $1", auth.HashToken(token))

	router, _ := NewRouterWithOptions(testPool, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil, RouterOptions{})
	call := func(method, path string, headers ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	want := func(rec *httptest.ResponseRecorder, what string, status int, body string) {
		t.Helper()
		if rec.Code != status || !strings.Contains(rec.Body.String(), body) {
			t.Errorf("%s: status %d, want %d with %q: %s", what, rec.Code, status, body, rec.Body)
		}
	}
	const mismatch = "workspace token is bound to a different workspace"

	// It works in its own workspace, with no selector and with a matching one.
	want(call(http.MethodGet, "/api/issues"), "issues with no selector", http.StatusOK, "")
	want(call(http.MethodGet, "/api/issues", "X-Workspace-ID", bound), "issues naming its workspace", http.StatusOK, "")
	want(call(http.MethodGet, "/api/workspaces/"+bound), "its workspace by path", http.StatusOK, bound)

	// The owner's other workspace, however it is named.
	want(call(http.MethodGet, "/api/issues", "X-Workspace-ID", other), "other workspace by id header", http.StatusForbidden, mismatch)
	want(call(http.MethodGet, "/api/issues", "X-Workspace-Slug", otherSlug), "other workspace by slug header", http.StatusForbidden, mismatch)
	want(call(http.MethodGet, "/api/issues?workspace_id="+other), "other workspace by id query", http.StatusForbidden, mismatch)
	want(call(http.MethodGet, "/api/issues?workspace_slug="+otherSlug), "other workspace by slug query", http.StatusForbidden, mismatch)
	want(call(http.MethodGet, "/api/workspaces/"+other), "other workspace by path", http.StatusForbidden, mismatch)
	want(call(http.MethodGet, "/api/workspaces/"+other+"/members"), "other workspace's members", http.StatusForbidden, mismatch)

	// Lists that are keyed on the user show its workspace only.
	var workspaces []struct {
		ID string `json:"id"`
	}
	if rec := call(http.MethodGet, "/api/workspaces"); rec.Code != http.StatusOK ||
		json.Unmarshal(rec.Body.Bytes(), &workspaces) != nil || len(workspaces) != 1 || workspaces[0].ID != bound {
		t.Errorf("workspace list: status %d, want only %s: %s", rec.Code, bound, rec.Body)
	}
	var unread []struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if rec := call(http.MethodGet, "/api/inbox/unread-summary"); rec.Code != http.StatusOK ||
		json.Unmarshal(rec.Body.Bytes(), &unread) != nil || len(unread) != 1 || unread[0].WorkspaceID != bound {
		t.Errorf("unread summary: status %d, want only %s: %s", rec.Code, bound, rec.Body)
	}

	// The owner's account: credentials, settings, new workspaces, and the
	// routes that sit among the workspace routes but are keyed on the user.
	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/api/tokens"},
		{http.MethodGet, "/api/tokens"},
		{http.MethodPost, "/api/cli-token"},
		{http.MethodPatch, "/api/me"},
		{http.MethodPost, "/api/workspaces"},
		{http.MethodGet, "/api/invitations"},
		{http.MethodGet, "/api/cloud-runtime/nodes"},
		{http.MethodPost, "/api/notification-preferences/auto-archive-prompt"},
	} {
		want(call(route.method, route.path), route.method+" "+route.path, http.StatusForbidden, "workspace tokens cannot access account-level endpoints")
	}
	if n := db.Count(t, `SELECT count(*) FROM personal_access_token WHERE user_id = $1`, owner); n != 1 {
		t.Errorf("the token's owner now has %d tokens; a workspace token minted another credential", n)
	}
}
