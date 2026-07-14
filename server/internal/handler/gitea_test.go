package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

func TestNormalizeGiteaBaseURL(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"https://gitea.example.com", "https://gitea.example.com", false},
		{"  https://gitea.example.com/  ", "https://gitea.example.com", false},
		{"https://gitea.example.com/gitea/", "https://gitea.example.com/gitea", false},
		{"https://gitea.example.com/?x=1#f", "https://gitea.example.com", false},
		{"http://localhost:3000", "http://localhost:3000", false},
		{"", "", true},
		{"ftp://gitea.example.com", "", true},
		{"gitea.example.com", "", true}, // no scheme → host lands in Path
		{"https://", "", true},
	}
	for _, c := range cases {
		got, err := normalizeGiteaBaseURL(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("normalizeGiteaBaseURL(%q): expected error, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeGiteaBaseURL(%q): unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("normalizeGiteaBaseURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestVerifyGiteaPAT(t *testing.T) {
	const goodToken = "gitea-pat-abcdef"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/user" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "token "+goodToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"login": "octo", "avatar_url": "https://ex/a.png"})
	}))
	defer srv.Close()

	t.Run("valid token returns account", func(t *testing.T) {
		acct, err := verifyGiteaPAT(context.Background(), srv.URL, goodToken)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if acct.Login != "octo" || acct.AvatarURL != "https://ex/a.png" {
			t.Fatalf("unexpected account: %+v", acct)
		}
	})

	t.Run("bad token rejected", func(t *testing.T) {
		if _, err := verifyGiteaPAT(context.Background(), srv.URL, "wrong"); err == nil {
			t.Fatal("expected error for bad token")
		}
	})

	t.Run("unreachable base url", func(t *testing.T) {
		if _, err := verifyGiteaPAT(context.Background(), "http://127.0.0.1:1", goodToken); err == nil {
			t.Fatal("expected error for unreachable host")
		}
	})

	t.Run("non-gitea payload rejected", func(t *testing.T) {
		other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"unrelated":true}`))
		}))
		defer other.Close()
		if _, err := verifyGiteaPAT(context.Background(), other.URL, goodToken); err == nil {
			t.Fatal("expected error when login is absent")
		}
	})
}

// TestGiteaConnect_PersistsEncrypted drives the full connect handler against a
// stub Gitea API and asserts the PAT is stored sealed (never plaintext) and
// that a missing box yields 503. DB-gated like the rest of the handler suite.
func TestGiteaConnect_PersistsEncrypted(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	const token = "gitea-secret-pat-999"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"login": "cici", "avatar_url": "https://ex/c.png"})
	}))
	defer srv.Close()

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM gitea_connection WHERE workspace_id = $1`, testWorkspaceID)
	})

	// No box → 503, nothing persisted.
	testHandler.GiteaSecretBox = nil
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/gitea", giteaConnectRequest{BaseURL: srv.URL, Token: token}), "id", testWorkspaceID)
	testHandler.GiteaConnect(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without box, got %d", w.Code)
	}

	// Configure the box and connect for real.
	key := make([]byte, secretbox.KeySize)
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	testHandler.GiteaSecretBox = box
	t.Cleanup(func() { testHandler.GiteaSecretBox = nil })

	w = httptest.NewRecorder()
	req = withURLParam(newRequest("POST", "/gitea", giteaConnectRequest{BaseURL: srv.URL + "/", Token: token}), "id", testWorkspaceID)
	testHandler.GiteaConnect(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("connect: %d %s", w.Code, w.Body.String())
	}
	var resp GiteaConnectionResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.AccountLogin != "cici" {
		t.Fatalf("unexpected login %q", resp.AccountLogin)
	}
	if resp.BaseURL != srv.URL {
		t.Fatalf("base_url not normalized: %q", resp.BaseURL)
	}

	// The stored token must be sealed bytes that decrypt back to the PAT, and
	// must not equal the plaintext.
	var stored []byte
	if err := testPool.QueryRow(ctx, `SELECT token_encrypted FROM gitea_connection WHERE workspace_id = $1`, testWorkspaceID).Scan(&stored); err != nil {
		t.Fatalf("load stored token: %v", err)
	}
	if string(stored) == token {
		t.Fatal("token stored in plaintext")
	}
	opened, err := box.Open(stored)
	if err != nil || string(opened) != token {
		t.Fatalf("stored token did not decrypt to PAT: %v", err)
	}

	// Re-connect same base_url updates in place (one row).
	w = httptest.NewRecorder()
	req = withURLParam(newRequest("POST", "/gitea", giteaConnectRequest{BaseURL: srv.URL, Token: "rotated-pat"}), "id", testWorkspaceID)
	testHandler.GiteaConnect(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("reconnect: %d %s", w.Code, w.Body.String())
	}
	var count int
	testPool.QueryRow(ctx, `SELECT count(*) FROM gitea_connection WHERE workspace_id = $1`, testWorkspaceID).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 connection after reconnect, got %d", count)
	}
}
