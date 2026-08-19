package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/tagaccess"
	"github.com/multica-ai/multica/server/internal/vibeshandoff"
)

type recordingTagSessionGrantor struct {
	grants []tagaccess.SessionGrant
	err    error
}

func (g *recordingTagSessionGrantor) GrantSession(_ context.Context, grant tagaccess.SessionGrant) error {
	g.grants = append(g.grants, grant)
	return g.err
}

func TestVIBESHandoffCreatesStableDistinctMirrorsAndCookieSession(t *testing.T) {
	const secret = "local-service-secret-at-least-32-bytes"
	identities := map[string]vibeshandoff.Identity{
		"first": {
			UserID:                     "vibes-handler-user-1",
			SessionID:                  "vibes-handler-session-1",
			WorkspaceID:                "vibes-handler-workspace-1",
			WorkspaceSlug:              "vibes-handler-workspace",
			WorkspaceName:              "VIBES Handler Workspace",
			Name:                       "First VIBES User",
			Email:                      "same-profile@example.test",
			Role:                       "owner",
			AccountEpoch:               1,
			SessionWorkspaceGeneration: 1,
			AuthorityVersion:           1,
			MembershipGeneration:       1,
		},
		"second": {
			UserID:                     "vibes-handler-user-2",
			SessionID:                  "vibes-handler-session-2",
			WorkspaceID:                "vibes-handler-workspace-1",
			WorkspaceSlug:              "vibes-handler-workspace",
			WorkspaceName:              "VIBES Handler Workspace",
			Name:                       "Second VIBES User",
			Email:                      "same-profile@example.test",
			Role:                       "member",
			AccountEpoch:               1,
			SessionWorkspaceGeneration: 1,
			AuthorityVersion:           1,
			MembershipGeneration:       1,
		},
	}
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		identity, ok := identities[body["code"]]
		if !ok {
			http.Error(w, "rejected", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(identity)
	}))
	defer issuer.Close()

	previous := testHandler.cfg
	previousGrantor := testHandler.TagSessionGrantor
	grantor := &recordingTagSessionGrantor{}
	testHandler.cfg.VIBESHandoffConsumeURL = issuer.URL
	testHandler.cfg.VIBESHandoffServiceSecret = secret
	testHandler.TagSessionGrantor = grantor
	t.Cleanup(func() {
		testHandler.cfg = previous
		testHandler.TagSessionGrantor = previousGrantor
	})
	cleanupVIBESMirrorTest(t)
	t.Cleanup(func() { cleanupVIBESMirrorTest(t) })

	firstCode := strings.Repeat("A", 43)
	secondCode := strings.Repeat("B", 43)
	identities[firstCode] = identities["first"]
	identities[secondCode] = identities["second"]
	call := func(code string) *httptest.ResponseRecorder {
		body := `{"code":"` + code + `","audience":"vibes-tag-local","workspaceSlug":"vibes-handler-workspace"}`
		request := httptest.NewRequest(http.MethodPost, "/api/auth/vibes-handoff", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		testHandler.VIBESHandoff(response, request)
		return response
	}

	for _, code := range []string{firstCode, firstCode, secondCode} {
		response := call(code)
		if response.Code != http.StatusNoContent {
			t.Fatalf("code %q returned %d: %s", code, response.Code, response.Body.String())
		}
		foundAuthCookie := false
		for _, cookie := range response.Result().Cookies() {
			if cookie.Name == auth.AuthCookieName && cookie.Value != "" {
				foundAuthCookie = true
			}
		}
		if !foundAuthCookie {
			t.Fatal("normal Multica session cookie was not set")
		}
	}

	var userCount, distinctUserCount, workspaceCount int
	if err := testPool.QueryRow(t.Context(), `
		SELECT count(*), count(DISTINCT multica_user_id)
		FROM vibes_user_mirror
		WHERE vibes_user_id LIKE 'vibes-handler-user-%'
	`).Scan(&userCount, &distinctUserCount); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(t.Context(), `
		SELECT count(*) FROM vibes_workspace_mirror
		WHERE vibes_workspace_id = 'vibes-handler-workspace-1'
	`).Scan(&workspaceCount); err != nil {
		t.Fatal(err)
	}
	if userCount != 2 || distinctUserCount != 2 || workspaceCount != 1 {
		t.Fatalf("wrong mirror cardinality: users=%d distinct=%d workspaces=%d", userCount, distinctUserCount, workspaceCount)
	}
	if len(grantor.grants) != 3 {
		t.Fatalf("handoff grants = %d, want 3", len(grantor.grants))
	}
	firstGrant := grantor.grants[0]
	if firstGrant.TagSessionID != tagaccess.BrowserTagSessionID(identities["first"].UserID, identities["first"].SessionID) ||
		firstGrant.VIBESSessionID != identities["first"].SessionID ||
		firstGrant.VIBESUserID != identities["first"].UserID || firstGrant.WorkspaceID != identities["first"].WorkspaceID ||
		firstGrant.AccountEpoch != 1 || firstGrant.SessionWorkspaceGeneration != 1 ||
		firstGrant.AuthorityVersion != 1 || firstGrant.MembershipGeneration != 1 ||
		!firstGrant.SessionExpiresAt.Equal(firstGrant.GrantExpiresAt) {
		t.Fatalf("first handoff grant = %#v", firstGrant)
	}
}

func TestNativeLoginRejectsMirroredVIBESIdentity(t *testing.T) {
	cleanupVIBESMirrorTest(t)
	t.Cleanup(func() { cleanupVIBESMirrorTest(t) })
	identity := vibeshandoff.Identity{
		UserID: "vibes-handler-user-native-login", SessionID: "vibes-handler-session-native-login",
		WorkspaceID: "vibes-handler-workspace-1", WorkspaceSlug: "vibes-handler-workspace",
		WorkspaceName: "VIBES Handler Workspace", Name: "VIBES User", Email: "profile@example.test",
		Role: "member", AccountEpoch: 1, SessionWorkspaceGeneration: 1, AuthorityVersion: 1, MembershipGeneration: 1,
	}
	user, err := testHandler.mirrorVIBESIdentity(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if user.Email == identity.Email {
		t.Fatal("test requires a synthetic local email distinct from VIBES profile metadata")
	}
	request := httptest.NewRequest(http.MethodPost, "/auth/send-code", strings.NewReader(`{"email":"`+identity.Email+`"}`))
	response := httptest.NewRecorder()
	testHandler.SendCode(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
}

func cleanupVIBESMirrorTest(t *testing.T) {
	t.Helper()
	_, err := testPool.Exec(context.Background(), `
		WITH users AS (
			SELECT multica_user_id FROM vibes_user_mirror
			WHERE vibes_user_id LIKE 'vibes-handler-user-%'
		), workspaces AS (
			SELECT multica_workspace_id FROM vibes_workspace_mirror
			WHERE vibes_workspace_id = 'vibes-handler-workspace-1'
		), deleted_members AS (
			DELETE FROM member WHERE user_id IN (SELECT multica_user_id FROM users)
		), deleted_user_mirrors AS (
			DELETE FROM vibes_user_mirror WHERE vibes_user_id LIKE 'vibes-handler-user-%'
		), deleted_workspace_mirrors AS (
			DELETE FROM vibes_workspace_mirror WHERE vibes_workspace_id = 'vibes-handler-workspace-1'
		), deleted_users AS (
			DELETE FROM "user" WHERE id IN (SELECT multica_user_id FROM users)
		)
		DELETE FROM workspace WHERE id IN (SELECT multica_workspace_id FROM workspaces)
	`)
	if err != nil {
		t.Fatal(err)
	}
}
