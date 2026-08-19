package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// stubGitHubInstallOwnership points the install-flow HTTP calls at a local
// server that vouches for the `owned` installation ids, and returns the
// authorization code a setup callback should carry. Requests the ownership
// flow does not own fall through to `rest` (nil = 404), so callers can keep
// controlling what fetchInstallationAccount sees.
func stubGitHubInstallOwnership(t *testing.T, owned []int64, rest http.HandlerFunc) string {
	t.Helper()
	const (
		code      = "test-user-auth-code"
		userToken = "ghu_test_user_token"
	)
	t.Setenv("GITHUB_APP_CLIENT_ID", "Iv1.test-client")
	t.Setenv("GITHUB_APP_CLIENT_SECRET", "test-client-secret")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/login/oauth/access_token":
			w.Header().Set("Content-Type", "application/json")
			if err := r.ParseForm(); err != nil || r.Form.Get("code") != code {
				_, _ = w.Write([]byte(`{"error":"bad_verification_code"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"` + userToken + `","token_type":"bearer"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/user/installations":
			if r.Header.Get("Authorization") != "Bearer "+userToken {
				http.Error(w, "bad credentials", http.StatusUnauthorized)
				return
			}
			installations := make([]map[string]any, 0, len(owned))
			for _, id := range owned {
				installations = append(installations, map[string]any{"id": id})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count":   len(installations),
				"installations": installations,
			})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/applications/"):
			w.WriteHeader(http.StatusNoContent)
		case rest != nil:
			rest(w, r)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	oldAPI, oldOAuth := githubAPIBase, githubOAuthBase
	githubAPIBase, githubOAuthBase = srv.URL, srv.URL
	t.Cleanup(func() { githubAPIBase, githubOAuthBase = oldAPI, oldOAuth })
	return code
}

// TestSetupCallback_RejectsInstallationTheUserDoesNotControl is the regression
// test for MUL-6056: a valid state token for the caller's own workspace plus
// an arbitrary installation_id used to be enough to bind someone else's
// installation, exposing that account's private repository names and its PR
// event stream. The bind must not happen, and no row may be written.
func TestSetupCallback_RejectsInstallationTheUserDoesNotControl(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	t.Setenv("GITHUB_WEBHOOK_SECRET", "ownership-secret")
	t.Setenv("FRONTEND_ORIGIN", "https://app.example.test")

	const (
		victimInstallationID   int64 = 91919191
		attackerInstallationID int64 = 91919192
	)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, victimInstallationID)
	})

	// The attacker controls only their own installation.
	code := stubGitHubInstallOwnership(t, []int64{attackerInstallationID}, nil)

	state, err := signState(testWorkspaceID)
	if err != nil {
		t.Fatalf("signState: %v", err)
	}
	req := httptest.NewRequest("GET", fmt.Sprintf(
		"/api/github/setup?installation_id=%d&code=%s&state=%s",
		victimInstallationID, code, state,
	), nil)
	rec := httptest.NewRecorder()
	testHandler.GitHubSetupCallback(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("setup callback: got %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "github_error=installation_not_authorized") {
		t.Fatalf("redirect = %q, want github_error=installation_not_authorized", loc)
	}
	rows, err := testHandler.Queries.ListGitHubInstallationsByInstallationID(ctx, victimInstallationID)
	if err != nil {
		t.Fatalf("list installations: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("callback bound %d row(s) for an installation the user does not control", len(rows))
	}
}

// TestSetupCallback_FailsClosedWithoutOwnershipProof covers the two ways the
// proof can be unavailable. Both must refuse the bind: falling through would
// leave the original hole open for anyone who can suppress the code or hit a
// deployment that never configured the OAuth credentials.
func TestSetupCallback_FailsClosedWithoutOwnershipProof(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	t.Setenv("GITHUB_WEBHOOK_SECRET", "ownership-secret")
	t.Setenv("FRONTEND_ORIGIN", "https://app.example.test")
	const installationID int64 = 91919193
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
	})

	tests := []struct {
		name      string
		clientID  string
		secret    string
		code      string
		wantError string
	}{
		{
			name:      "no user authorization code in the redirect",
			clientID:  "Iv1.test-client",
			secret:    "test-client-secret",
			code:      "",
			wantError: "github_error=missing_authorization",
		},
		{
			name:      "deployment has no user authorization credentials",
			clientID:  "",
			secret:    "",
			code:      "test-user-auth-code",
			wantError: "github_error=verification_failed",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GITHUB_APP_CLIENT_ID", tc.clientID)
			t.Setenv("GITHUB_APP_CLIENT_SECRET", tc.secret)
			state, err := signState(testWorkspaceID)
			if err != nil {
				t.Fatalf("signState: %v", err)
			}
			req := httptest.NewRequest("GET", fmt.Sprintf(
				"/api/github/setup?installation_id=%d&code=%s&state=%s",
				installationID, tc.code, state,
			), nil)
			rec := httptest.NewRecorder()
			testHandler.GitHubSetupCallback(rec, req)

			if rec.Code != http.StatusFound {
				t.Fatalf("setup callback: got %d, want 302", rec.Code)
			}
			if loc := rec.Header().Get("Location"); !strings.Contains(loc, tc.wantError) {
				t.Fatalf("redirect = %q, want %s", loc, tc.wantError)
			}
			rows, err := testHandler.Queries.ListGitHubInstallationsByInstallationID(ctx, installationID)
			if err != nil {
				t.Fatalf("list installations: %v", err)
			}
			if len(rows) != 0 {
				t.Fatalf("callback bound %d row(s) without an ownership proof", len(rows))
			}
		})
	}
}

// TestSetupCallback_RefreshesAnExistingBindingWithoutACode covers GitHub's
// "redirect on update" callback, which fires when someone edits an installed
// App's repository selection and carries no user-authorization code. The
// binding already exists, so re-affirming it grants nothing new and must not
// be treated as a failed connect.
func TestSetupCallback_RefreshesAnExistingBindingWithoutACode(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	t.Setenv("GITHUB_WEBHOOK_SECRET", "ownership-secret")
	t.Setenv("GITHUB_APP_CLIENT_ID", "Iv1.test-client")
	t.Setenv("GITHUB_APP_CLIENT_SECRET", "test-client-secret")
	t.Setenv("GITHUB_APP_ID", "")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "")
	t.Setenv("FRONTEND_ORIGIN", "https://app.example.test")

	const installationID int64 = 91919194
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
	})
	if _, err := testHandler.Queries.CreateGitHubInstallation(ctx, db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "already-connected",
		AccountType:    "Organization",
	}); err != nil {
		t.Fatalf("seed installation: %v", err)
	}

	state, err := signState(testWorkspaceID)
	if err != nil {
		t.Fatalf("signState: %v", err)
	}
	req := httptest.NewRequest("GET", fmt.Sprintf(
		"/api/github/setup?installation_id=%d&setup_action=update&state=%s",
		installationID, state,
	), nil)
	rec := httptest.NewRecorder()
	testHandler.GitHubSetupCallback(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("setup callback: got %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "github_connected=1") {
		t.Fatalf("redirect = %q, want github_connected=1", loc)
	}
}

// TestVerifyGitHubInstallationOwnership_WalksPagesAndRevokes pins the two
// details a single-page happy path would hide: an account with more than one
// page of installations must still be matched, and the short-lived user token
// must be handed back to GitHub once the check is done.
func TestVerifyGitHubInstallationOwnership_WalksPagesAndRevokes(t *testing.T) {
	t.Setenv("GITHUB_APP_CLIENT_ID", "Iv1.test-client")
	t.Setenv("GITHUB_APP_CLIENT_SECRET", "test-client-secret")
	const wantedInstallationID int64 = 4242

	var revoked atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/login/oauth/access_token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"ghu_paged"}`))
		case r.URL.Path == "/user/installations":
			installations := make([]map[string]any, 0, githubUserInstallationsPageSize)
			if r.URL.Query().Get("page") == "1" {
				// A full page of unrelated installations forces a second fetch.
				for i := 0; i < githubUserInstallationsPageSize; i++ {
					installations = append(installations, map[string]any{"id": int64(1000 + i)})
				}
			} else {
				installations = append(installations, map[string]any{"id": wantedInstallationID})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"installations": installations})
		case r.Method == http.MethodDelete && r.URL.Path == "/applications/"+url.PathEscape("Iv1.test-client")+"/token":
			revoked.Store(true)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	oldAPI, oldOAuth := githubAPIBase, githubOAuthBase
	githubAPIBase, githubOAuthBase = srv.URL, srv.URL
	t.Cleanup(func() { githubAPIBase, githubOAuthBase = oldAPI, oldOAuth })

	if err := verifyGitHubInstallationOwnership(context.Background(), "code", wantedInstallationID); err != nil {
		t.Fatalf("ownership check rejected an installation on page 2: %v", err)
	}
	if !revoked.Load() {
		t.Error("user access token was not revoked after the ownership check")
	}

	err := verifyGitHubInstallationOwnership(context.Background(), "code", 999999)
	if !errors.Is(err, errGitHubInstallationNotAuthorized) {
		t.Fatalf("unlisted installation: err = %v, want errGitHubInstallationNotAuthorized", err)
	}
}

// TestState_ExpiresAfterMaxAge keeps a captured install URL from staying a
// usable "bind into this workspace" credential forever.
func TestState_ExpiresAfterMaxAge(t *testing.T) {
	t.Setenv("GITHUB_WEBHOOK_SECRET", "state-ttl-secret")
	wsID := "11111111-2222-3333-4444-555555555555"

	fresh, err := signStateForReturnAt(wsID, githubReturnToGitHub, time.Now())
	if err != nil {
		t.Fatalf("signStateForReturnAt: %v", err)
	}
	if _, ok := verifyState(fresh); !ok {
		t.Fatal("freshly signed state was rejected")
	}

	stale, err := signStateForReturnAt(wsID, githubReturnToGitHub, time.Now().Add(-githubStateMaxAge-time.Minute))
	if err != nil {
		t.Fatalf("signStateForReturnAt: %v", err)
	}
	if _, ok := verifyState(stale); ok {
		t.Error("expired state token was accepted")
	}

	future, err := signStateForReturnAt(wsID, githubReturnToGitHub, time.Now().Add(githubStateClockSkew+time.Minute))
	if err != nil {
		t.Fatalf("signStateForReturnAt: %v", err)
	}
	if _, ok := verifyState(future); ok {
		t.Error("state token issued beyond the tolerated clock skew was accepted")
	}
}

// TestIsGitHubConfigured_RequiresUserAuthorizationCredentials pins that the
// Connect button stays hidden on deployments that cannot prove ownership,
// rather than offering a flow the callback would refuse.
func TestIsGitHubConfigured_RequiresUserAuthorizationCredentials(t *testing.T) {
	t.Setenv("GITHUB_APP_SLUG", "multica-test")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "test-secret-123")
	t.Setenv("GITHUB_APP_CLIENT_ID", "")
	t.Setenv("GITHUB_APP_CLIENT_SECRET", "")
	if isGitHubConfigured() {
		t.Error("integration reported configured without user authorization credentials")
	}

	t.Setenv("GITHUB_APP_CLIENT_ID", "Iv1.test-client")
	if isGitHubConfigured() {
		t.Error("integration reported configured with only half of the OAuth credentials")
	}

	t.Setenv("GITHUB_APP_CLIENT_SECRET", "test-client-secret")
	if !isGitHubConfigured() {
		t.Error("integration reported not configured with every credential set")
	}
}
