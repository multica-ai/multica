package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestGitHubSetupRejectsUnverifiedInstallation(t *testing.T) {
	if testHandler == nil {
		t.Fatal("handler fixture requires an isolated database")
	}
	t.Setenv("GITHUB_WEBHOOK_SECRET", "ownership-test-secret")
	t.Setenv("GITHUB_APP_CLIENT_ID", "test-client")
	t.Setenv("GITHUB_APP_CLIENT_SECRET", "test-client-secret")
	t.Setenv("GITHUB_APP_ID", "")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"account":{"login":"unrelated-org","type":"Organization"}}`)
	}))
	t.Cleanup(srv.Close)
	oldBase := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = oldBase })
	installationID := time.Now().UnixNano()
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
	})
	state, err := signState(testWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/github/setup?installation_id=%d&state=%s", installationID, url.QueryEscape(state)), nil)
	rec := httptest.NewRecorder()
	testHandler.GitHubSetupCallback(rec, req)
	rows, err := testHandler.Queries.ListGitHubInstallationsByInstallationID(req.Context(), installationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("unverified installation was persisted: %d bindings", len(rows))
	}
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "github_error=missing_authorization") {
		t.Fatalf("callback = %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func stubGitHubOwnership(t *testing.T, serve http.HandlerFunc) {
	t.Helper()
	t.Setenv("GITHUB_APP_CLIENT_ID", "test-client")
	t.Setenv("GITHUB_APP_CLIENT_SECRET", "test-client-secret")
	srv := httptest.NewServer(serve)
	t.Cleanup(srv.Close)
	oldAPI, oldOAuth := githubAPIBase, githubOAuthBase
	githubAPIBase, githubOAuthBase = srv.URL, srv.URL
	t.Cleanup(func() { githubAPIBase, githubOAuthBase = oldAPI, oldOAuth })
}

func serveGitHubOwnership(t *testing.T, w http.ResponseWriter, r *http.Request, installationID int64) bool {
	t.Helper()
	switch r.URL.Path {
	case "/login/oauth/access_token":
		if r.Method != http.MethodPost || r.FormValue("code") != "valid-code" || r.FormValue("client_id") != "test-client" || r.FormValue("client_secret") != "test-client-secret" {
			t.Error("incorrect OAuth exchange request")
		}
		fmt.Fprint(w, `{"access_token":"test-user-token"}`)
	case "/user/installations":
		if r.Header.Get("Authorization") != "Bearer test-user-token" {
			t.Error("installation access must be checked with the user token")
		}
		fmt.Fprintf(w, `{"installations":[{"id":%d,"account":{"id":123,"type":"User"}}]}`, installationID)
	case "/user":
		if r.Header.Get("Authorization") != "Bearer test-user-token" {
			t.Error("identity must use the user token")
		}
		fmt.Fprint(w, `{"id":123}`)
	case "/applications/test-client/token":
		id, secret, _ := r.BasicAuth()
		var body struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.Method != http.MethodDelete || id != "test-client" || secret != "test-client-secret" || body.AccessToken != "test-user-token" {
			t.Error("incorrect token revocation request")
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		return false
	}
	return true
}

func TestGitHubSetupOwnershipFailuresNeverBind(t *testing.T) {
	t.Setenv("GITHUB_WEBHOOK_SECRET", "ownership-test-secret")
	for _, mode := range []string{"foreign_installation", "exchange_rejected", "api_unavailable", "malformed_api", "missing_credentials"} {
		t.Run(mode, func(t *testing.T) {
			installationID := time.Now().UnixNano()
			victimWS := dbfx.Workspace(t, "Victim", fmt.Sprintf("victim-%d", installationID))
			victimID := dbfx.Insert(t, "github_installation", testutil.Cols{
				"workspace_id": victimWS, "installation_id": installationID,
				"account_login": "victim-org", "account_type": "Organization",
			})
			var revoked atomic.Int32
			stubGitHubOwnership(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/applications/test-client/token" {
					revoked.Add(1)
				}
				if mode == "exchange_rejected" && r.URL.Path == "/login/oauth/access_token" {
					fmt.Fprint(w, `{"error":"bad_verification_code"}`)
					return
				}
				if r.URL.Path == "/user/installations" {
					switch mode {
					case "api_unavailable":
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					case "malformed_api":
						fmt.Fprint(w, `{`)
						return
					}
				}
				if !serveGitHubOwnership(t, w, r, installationID-1) {
					t.Errorf("unverified callback fetched installation metadata: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			})
			if mode == "missing_credentials" {
				t.Setenv("GITHUB_APP_CLIENT_SECRET", "")
			}
			state, err := signState(testWorkspaceID)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/github/setup?installation_id=%d&code=valid-code&state=%s", installationID, url.QueryEscape(state)), nil)
			rec := httptest.NewRecorder()
			testHandler.GitHubSetupCallback(rec, req)
			wantError := "verification_failed"
			if mode == "foreign_installation" {
				wantError = "installation_not_authorized"
			}
			if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "github_error="+wantError) {
				t.Fatalf("callback = %d %s", rec.Code, rec.Header().Get("Location"))
			}
			rows, err := testHandler.Queries.ListGitHubInstallationsByInstallationID(req.Context(), installationID)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || uuidToString(rows[0].ID) != victimID || uuidToString(rows[0].WorkspaceID) != victimWS {
				t.Fatalf("foreign installation bindings changed: %+v", rows)
			}
			wantRevocations := int32(1)
			if mode == "exchange_rejected" || mode == "missing_credentials" {
				wantRevocations = 0
			}
			if revoked.Load() != wantRevocations {
				t.Fatalf("revocations = %d, want %d", revoked.Load(), wantRevocations)
			}
		})
	}
}

func TestGitHubSetupExistingBindingNeedsNoCode(t *testing.T) {
	t.Setenv("GITHUB_WEBHOOK_SECRET", "ownership-test-secret")
	t.Setenv("GITHUB_APP_CLIENT_ID", "")
	t.Setenv("GITHUB_APP_CLIENT_SECRET", "")
	installationID := time.Now().UnixNano()
	rowID := dbfx.Insert(t, "github_installation", testutil.Cols{
		"workspace_id": testWorkspaceID, "installation_id": installationID,
		"account_login": "existing-org", "account_type": "Organization", "connected_by_id": testUserID,
	})
	state, err := signState(testWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/github/setup?installation_id=%d&state=%s", installationID, url.QueryEscape(state)), nil)
	rec := httptest.NewRecorder()
	testHandler.GitHubSetupCallback(rec, req)
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "github_connected=1") {
		t.Fatalf("update = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	row, err := testHandler.Queries.GetGitHubInstallationByID(req.Context(), parseUUID(rowID))
	if err != nil {
		t.Fatal(err)
	}
	if row.AccountLogin != "existing-org" || uuidToString(row.ConnectedByID) != testUserID {
		t.Fatalf("update changed existing binding: %+v", row)
	}
}

func TestGitHubSetupRejectsReadOnlyCollaborator(t *testing.T) {
	t.Setenv("GITHUB_WEBHOOK_SECRET", "collaborator-test-secret")
	pemBytes, _ := generateTestRSAKeyPEM(t)
	t.Setenv("GITHUB_APP_ID", "11111")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", string(pemBytes))
	installationID := time.Now().UnixNano()
	stubGitHubOwnership(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user/installations":
			fmt.Fprintf(w, `{"installations":[{"id":%d,"account":{"id":123,"type":"Organization"}}]}`, installationID)
		case fmt.Sprintf("/user/installations/%d/repositories", installationID):
			fmt.Fprint(w, `{"total_count":1,"repositories":[{"id":1,"permissions":{"admin":false,"pull":true}}]}`)
		default:
			if !serveGitHubOwnership(t, w, r, installationID) {
				fmt.Fprint(w, `{"account":{"login":"victim-org","type":"Organization"}}`)
			}
		}
	})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
	})
	state, err := signState(testWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/github/setup?installation_id=%d&code=valid-code&state=%s", installationID, url.QueryEscape(state)), nil)
	rec := httptest.NewRecorder()
	testHandler.GitHubSetupCallback(rec, req)
	rows, err := testHandler.Queries.ListGitHubInstallationsByInstallationID(req.Context(), installationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("read-only collaborator created %d installation bindings", len(rows))
	}
	if !strings.Contains(rec.Header().Get("Location"), "github_error=installation_not_authorized") {
		t.Fatalf("callback = %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestGitHubOwnershipPaginationAndRevocation(t *testing.T) {
	var pages, revoked atomic.Int32
	stubGitHubOwnership(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/applications/test-client/token" {
			revoked.Add(1)
		}
		if r.URL.Path == "/user/installations" {
			pages.Add(1)
			if r.URL.Query().Get("per_page") != "100" {
				t.Error("missing page size")
			}
			if r.URL.Query().Get("page") == "1" {
				installations := make([]map[string]int64, 100)
				for i := range installations {
					installations[i] = map[string]int64{"id": int64(i + 1)}
				}
				writeJSON(w, http.StatusOK, map[string]any{"installations": installations})
				return
			}
		}
		if !serveGitHubOwnership(t, w, r, 999) {
			http.NotFound(w, r)
		}
	})
	if err := verifyGitHubInstallationOwnership(context.Background(), "valid-code", 999); err != nil {
		t.Fatal(err)
	}
	if pages.Load() != 2 || revoked.Load() != 1 {
		t.Fatalf("pages=%d revocations=%d", pages.Load(), revoked.Load())
	}
}

func TestGitHubSetupRequiresAdministrationOfAllInstallationRepositories(t *testing.T) {
	for _, mode := range []string{"all_admin", "partial_access", "different_repositories", "missing_admin", "empty_installation", "admin_on_later_page", "read_on_later_page", "app_unavailable", "missing_app_credentials"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("GITHUB_WEBHOOK_SECRET", "repository-admin-secret")
			pemBytes, _ := generateTestRSAKeyPEM(t)
			t.Setenv("GITHUB_APP_ID", "11111")
			t.Setenv("GITHUB_APP_PRIVATE_KEY", string(pemBytes))
			installationID := time.Now().UnixNano()
			var userRevoked, appRevoked, userPages, appPages atomic.Int32
			stubGitHubOwnership(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/user/installations":
					fmt.Fprintf(w, `{"installations":[{"id":%d,"account":{"id":123,"type":"Organization"}}]}`, installationID)
				case fmt.Sprintf("/app/installations/%d/access_tokens", installationID):
					var body struct {
						Permissions map[string]string `json:"permissions"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if len(body.Permissions) != 1 || body.Permissions["metadata"] != "read" {
						t.Errorf("proof token permissions: %+v", body.Permissions)
					}
					if r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ey") {
						t.Error("metadata token must use App JWT")
					}
					writeJSON(w, http.StatusCreated, map[string]any{"token": "app-proof-token"})
				case "/installation/token":
					if r.Method != http.MethodDelete || r.Header.Get("Authorization") != "Bearer app-proof-token" {
						t.Error("incorrect App token revocation")
					}
					appRevoked.Add(1)
					w.WriteHeader(http.StatusNoContent)
				case fmt.Sprintf("/user/installations/%d/repositories", installationID), "/installation/repositories":
					isUser := strings.HasPrefix(r.URL.Path, "/user/")
					wantToken := "Bearer app-proof-token"
					if isUser {
						userPages.Add(1)
						wantToken = "Bearer test-user-token"
					} else {
						appPages.Add(1)
					}
					if r.Header.Get("Authorization") != wantToken {
						t.Errorf("incorrect repository-list token for %s", r.URL.Path)
					}
					if !isUser && mode == "app_unavailable" {
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					}
					count := 1
					if mode == "empty_installation" {
						count = 0
					}
					if !isUser && mode == "partial_access" {
						count = 2
					}
					if mode == "admin_on_later_page" || mode == "read_on_later_page" {
						count = 101
					}
					start := 1
					if r.URL.Query().Get("page") == "2" {
						start = 101
					}
					repos := []map[string]any{}
					for id := start; id <= count && id < start+100; id++ {
						repoID := id
						if !isUser && mode == "different_repositories" {
							repoID = 2
						}
						item := map[string]any{"id": repoID}
						if isUser && mode != "missing_admin" {
							item["permissions"] = map[string]bool{"admin": mode != "read_on_later_page" || id != 101, "pull": true}
						}
						repos = append(repos, item)
					}
					writeJSON(w, http.StatusOK, map[string]any{"total_count": count, "repositories": repos})
				case "/applications/test-client/token":
					userRevoked.Add(1)
					serveGitHubOwnership(t, w, r, installationID)
				default:
					if !serveGitHubOwnership(t, w, r, installationID) {
						writeJSON(w, http.StatusOK, map[string]any{"account": map[string]any{"login": "admin-org", "type": "Organization"}})
					}
				}
			})
			if mode == "missing_app_credentials" {
				t.Setenv("GITHUB_APP_PRIVATE_KEY", "")
			}
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
			})
			state, err := signState(testWorkspaceID)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/github/setup?installation_id=%d&code=valid-code&state=%s", installationID, url.QueryEscape(state)), nil)
			rec := httptest.NewRecorder()
			testHandler.GitHubSetupCallback(rec, req)
			rows, err := testHandler.Queries.ListGitHubInstallationsByInstallationID(req.Context(), installationID)
			if err != nil {
				t.Fatal(err)
			}
			allowed := mode == "all_admin" || mode == "admin_on_later_page"
			wantRows := 0
			wantQuery := "github_error=installation_not_authorized"
			if allowed {
				wantRows = 1
				wantQuery = "github_connected=1"
			}
			if mode == "app_unavailable" || mode == "missing_app_credentials" {
				wantQuery = "github_error=verification_failed"
			}
			if len(rows) != wantRows || !strings.Contains(rec.Header().Get("Location"), wantQuery) {
				t.Fatalf("bindings=%d callback=%d %s", len(rows), rec.Code, rec.Header().Get("Location"))
			}
			if userRevoked.Load() != 1 {
				t.Fatalf("user token revocations = %d", userRevoked.Load())
			}
			wantAppRevoked := int32(1)
			if mode == "missing_admin" || mode == "empty_installation" || mode == "read_on_later_page" || mode == "missing_app_credentials" {
				wantAppRevoked = 0
			}
			if appRevoked.Load() != wantAppRevoked {
				t.Fatalf("App token revocations=%d want=%d", appRevoked.Load(), wantAppRevoked)
			}
			if mode == "admin_on_later_page" && (userPages.Load() != 2 || appPages.Load() != 2) {
				t.Fatalf("user pages=%d App pages=%d", userPages.Load(), appPages.Load())
			}
		})
	}
}

func TestGitHubPersonalInstallationRequiresAccountOwner(t *testing.T) {
	for _, mode := range []string{"owner", "collaborator", "missing_identity", "identity_unavailable"} {
		t.Run(mode, func(t *testing.T) {
			stubGitHubOwnership(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/user" {
					switch mode {
					case "collaborator":
						fmt.Fprint(w, `{"id":456}`)
					case "missing_identity":
						fmt.Fprint(w, `{}`)
					case "identity_unavailable":
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
					default:
						fmt.Fprint(w, `{"id":123}`)
					}
					return
				}
				if !serveGitHubOwnership(t, w, r, 999) {
					t.Errorf("unexpected endpoint: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			})
			err := verifyGitHubInstallationOwnership(context.Background(), "valid-code", 999)
			if (err == nil) != (mode == "owner") {
				t.Fatalf("owner verification: %v", err)
			}
		})
	}
}

func TestGitHubStateExpiry(t *testing.T) {
	t.Setenv("GITHUB_WEBHOOK_SECRET", "expiry-test-secret")
	for _, tc := range []struct {
		nonce string
		want  bool
	}{
		{fmt.Sprintf("%d-nonce", time.Now().Add(-14*time.Minute).Unix()), true},
		{fmt.Sprintf("desktop-%d-nonce", time.Now().Add(-14*time.Minute).Unix()), true},
		{fmt.Sprintf("%d-nonce", time.Now().Add(-16*time.Minute).Unix()), false},
		{fmt.Sprintf("%d-nonce", time.Now().Add(2*time.Minute).Unix()), false},
		{"old-nonce", false},
	} {
		payload := testWorkspaceID + "." + tc.nonce
		mac := hmac.New(sha256.New, []byte("expiry-test-secret"))
		mac.Write([]byte(payload))
		if _, _, ok := verifyGitHubState(payload + "." + hex.EncodeToString(mac.Sum(nil))); ok != tc.want {
			t.Errorf("nonce %q: ok=%v", tc.nonce, ok)
		}
	}
}

func TestGitHubConnectRequiresOAuthCredentials(t *testing.T) {
	t.Setenv("GITHUB_APP_SLUG", "test-app")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "test-secret")
	t.Setenv("GITHUB_APP_CLIENT_ID", "test-client")
	t.Setenv("GITHUB_APP_CLIENT_SECRET", "test-client-secret")
	t.Setenv("GITHUB_APP_ID", "11111")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "configured-key")
	if !isGitHubConfigured() {
		t.Fatal("fully configured Connect disabled")
	}
	for _, key := range []string{"GITHUB_APP_CLIENT_ID", "GITHUB_APP_CLIENT_SECRET", "GITHUB_APP_ID", "GITHUB_APP_PRIVATE_KEY"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "")
			if isGitHubConfigured() {
				t.Fatal("Connect enabled without OAuth credentials")
			}
		})
	}
}
