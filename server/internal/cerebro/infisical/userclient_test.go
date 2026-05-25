package infisical

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUserClientFetchFolderSecretsHappyPath(t *testing.T) {
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/universal-auth/login", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["clientId"] != "uc-id" || body["clientSecret"] != "uc-secret" {
			t.Fatalf("login body = %#v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"accessToken": "ut", "expiresIn": 1800})
	})
	mux.HandleFunc("/api/v3/secrets/raw", func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got := r.Header.Get("Authorization"); got != "Bearer ut" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.URL.Query().Get("workspaceId"); got != "proj-1" {
			t.Fatalf("workspaceId = %q", got)
		}
		env := r.URL.Query().Get("environment")
		path := r.URL.Query().Get("secretPath")
		if env == "prod" && path == "/app" {
			_ = json.NewEncoder(w).Encode(map[string]any{"secrets": []map[string]string{
				{"secretKey": "API_KEY", "secretValue": "k1"},
			}})
			return
		}
		if env == "prod" && path == "/cache" {
			_ = json.NewEncoder(w).Encode(map[string]any{"secrets": []map[string]string{
				{"secretKey": "REDIS_URL", "secretValue": "r1"},
			}})
			return
		}
		http.Error(w, "unexpected folder", http.StatusForbidden)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	uc := &UserClient{
		Domain:       srv.URL,
		ProjectID:    "proj-1",
		ClientID:     "uc-id",
		ClientSecret: "uc-secret",
		HTTP:         srv.Client(),
	}
	env, err := uc.FetchFolderSecrets(context.Background(), []FolderRef{
		{Environment: "prod", SecretPath: "/app"},
		{Environment: "prod", SecretPath: "/cache"},
	})
	if err != nil {
		t.Fatalf("FetchFolderSecrets: %v", err)
	}
	if env["API_KEY"] != "k1" || env["REDIS_URL"] != "r1" {
		t.Fatalf("missing secrets: %#v", env)
	}
	if calls != 2 {
		t.Fatalf("expected 2 folder fetches, got %d", calls)
	}
}

func TestUserClientForbiddenFolderSurfacesAsError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/universal-auth/login", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"accessToken": "ut", "expiresIn": 1800})
	})
	mux.HandleFunc("/api/v3/secrets/raw", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	uc := &UserClient{
		Domain:       srv.URL,
		ProjectID:    "proj-1",
		ClientID:     "uc-id",
		ClientSecret: "uc-secret",
		HTTP:         srv.Client(),
	}
	_, err := uc.FetchFolderSecrets(context.Background(), []FolderRef{{Environment: "prod", SecretPath: "/forbidden"}})
	if err == nil {
		t.Fatalf("expected error for 403, got nil")
	}
}

func TestUserClientEmptyFoldersIsNoOp(t *testing.T) {
	uc := &UserClient{
		Domain:       "https://example",
		ProjectID:    "proj-1",
		ClientID:     "uc-id",
		ClientSecret: "uc-secret",
	}
	env, err := uc.FetchFolderSecrets(context.Background(), nil)
	if err != nil {
		t.Fatalf("FetchFolderSecrets nil: %v", err)
	}
	if len(env) != 0 {
		t.Fatalf("expected empty map, got %#v", env)
	}
}
