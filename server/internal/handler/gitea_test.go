package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestVerifyGiteaSignature(t *testing.T) {
	secret := "gitea-shared-secret"
	body := []byte(`{"action":"opened"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	good := hex.EncodeToString(mac.Sum(nil)) // raw hex, no "sha256=" prefix

	if !verifyGiteaSignature(secret, good, body) {
		t.Error("expected valid signature to verify")
	}
	if verifyGiteaSignature(secret, "sha256="+good, body) {
		t.Error("expected the GitHub-style prefixed form to fail (Gitea is raw hex)")
	}
	if verifyGiteaSignature(secret, "deadbeef", body) {
		t.Error("expected wrong digest to fail")
	}
	if verifyGiteaSignature(secret, "nothex", body) {
		t.Error("expected non-hex to fail")
	}
	if verifyGiteaSignature(secret, "", body) {
		t.Error("expected empty header to fail")
	}
	if verifyGiteaSignature("other-secret", good, body) {
		t.Error("expected wrong secret to fail")
	}
}

func TestGiteaMergeableState(t *testing.T) {
	yes, no := true, false
	if got := giteaMergeableState(nil); got != "" {
		t.Errorf("nil mergeable = %q, want empty", got)
	}
	if got := giteaMergeableState(&yes); got != "clean" {
		t.Errorf("true mergeable = %q, want clean", got)
	}
	if got := giteaMergeableState(&no); got != "dirty" {
		t.Errorf("false mergeable = %q, want dirty", got)
	}
}

func TestGiteaInstanceBaseURL(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{`{"repository":{"html_url":"https://gitea.example.com/acme/widget"}}`, "https://gitea.example.com"},
		{`{"repository":{"html_url":"http://localhost:3000/o/r"}}`, "http://localhost:3000"},
		{`{"repository":{"html_url":"not a url"}}`, ""},
		{`{"repository":{}}`, ""},
		{`not json`, ""},
	}
	for _, c := range cases {
		if got := giteaInstanceBaseURL([]byte(c.body)); got != c.want {
			t.Errorf("giteaInstanceBaseURL(%q) = %q, want %q", c.body, got, c.want)
		}
	}
}

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

// TestGiteaWebhook_MergedPR_AdvancesLinkedIssueToDone drives the Gitea webhook
// end to end: a workspace connection for the delivering instance, a merged
// pull_request delivery whose body declares closing intent, and the shared
// mirror pipeline. It asserts the PR is mirrored under provider='gitea' and the
// linked issue advances to done — the DoD's merge→Done reuse. It also checks a
// bad signature is rejected and a repeat delivery stays idempotent.
func TestGiteaWebhook_MergedPR_AdvancesLinkedIssueToDone(t *testing.T) {
	if testHandler == nil {
		t.Skip("handler test fixture not initialized (no DB?)")
	}
	ctx := context.Background()
	secret := "gitea-webhook-secret"
	t.Setenv("MULTICA_GITEA_WEBHOOK_SECRET", secret)
	const instance = "http://gitea.local"

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Gitea PR auto-merge test",
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_pull_request WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM github_pull_request WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM gitea_connection WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, created.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID)
	})

	// A connection for the delivering instance so the webhook can attribute the
	// delivery to this workspace. The webhook path never reads the token, so a
	// placeholder sealed value is fine here.
	if _, err := testHandler.Queries.CreateGiteaConnection(ctx, db.CreateGiteaConnectionParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		BaseUrl:        instance,
		TokenEncrypted: []byte("placeholder"),
		AccountLogin:   "cici",
	}); err != nil {
		t.Fatalf("CreateGiteaConnection: %v", err)
	}

	mergeable := true
	body, _ := json.Marshal(map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number":        42,
			"html_url":      instance + "/acme/widget/pulls/42",
			"title":         "Fix login " + created.Identifier,
			"body":          "Closes " + created.Identifier,
			"state":         "closed",
			"draft":         false,
			"merged":        true,
			"mergeable":     mergeable,
			"merged_at":     "2026-04-29T00:00:00Z",
			"closed_at":     "2026-04-29T00:00:00Z",
			"created_at":    "2026-04-28T00:00:00Z",
			"updated_at":    "2026-04-29T00:00:00Z",
			"head":          map[string]any{"ref": "fix/login", "sha": "abc123"},
			"user":          map[string]any{"login": "cici", "avatar_url": ""},
			"additions":     3,
			"deletions":     1,
			"changed_files": 2,
		},
		"repository": map[string]any{
			"name":     "widget",
			"html_url": instance + "/acme/widget",
			"owner":    map[string]any{"login": "acme"},
		},
	})

	fire := func(sig string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		hookReq := httptest.NewRequest("POST", "/api/webhooks/gitea", bytes.NewReader(body))
		hookReq.Header.Set("X-Gitea-Event", "pull_request")
		hookReq.Header.Set("X-Gitea-Signature", sig)
		testHandler.HandleGiteaWebhook(rec, hookReq)
		return rec
	}

	// Bad signature is rejected before any mirroring happens.
	if rec := fire("deadbeef"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature: expected 401, got %d (%s)", rec.Code, rec.Body.String())
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	if rec := fire(sig); rec.Code != http.StatusAccepted {
		t.Fatalf("webhook: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}

	pr, err := testHandler.Queries.GetGitHubPullRequest(ctx, db.GetGitHubPullRequestParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Provider:    "gitea",
		BaseHost:    "gitea.local",
		RepoOwner:   "acme",
		RepoName:    "widget",
		PrNumber:    42,
	})
	if err != nil {
		t.Fatalf("GetGitHubPullRequest: %v", err)
	}
	if pr.State != "merged" {
		t.Errorf("expected pr state merged, got %q", pr.State)
	}
	if pr.MergeableState.String != "clean" {
		t.Errorf("expected mergeable_state clean, got %q", pr.MergeableState.String)
	}

	linked, err := testHandler.Queries.ListPullRequestsByIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("ListPullRequestsByIssue: %v", err)
	}
	if len(linked) != 1 {
		t.Fatalf("expected 1 linked PR, got %d", len(linked))
	}

	updated, err := testHandler.Queries.GetIssue(ctx, parseUUID(created.ID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if updated.Status != "done" {
		t.Errorf("expected issue status 'done', got %q", updated.Status)
	}

	// Repeat delivery is idempotent: still one PR row, one link, still done.
	if rec := fire(sig); rec.Code != http.StatusAccepted {
		t.Fatalf("replay webhook: expected 202, got %d", rec.Code)
	}
	var prCount int
	testPool.QueryRow(ctx, `SELECT count(*) FROM github_pull_request WHERE workspace_id = $1 AND provider = 'gitea'`, testWorkspaceID).Scan(&prCount)
	if prCount != 1 {
		t.Errorf("expected 1 gitea PR row after replay, got %d", prCount)
	}
}

func TestSplitOwnerRepo(t *testing.T) {
	cases := []struct {
		in, owner, repo string
		ok              bool
	}{
		{"acme/app", "acme", "app", true},
		{"  acme/app  ", "acme", "app", true},
		{"/acme/app/", "acme", "app", true},
		{"acme", "", "", false},
		{"acme/app/extra", "", "", false},
		{"acme/", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		o, r, ok := splitOwnerRepo(c.in)
		if ok != c.ok || o != c.owner || r != c.repo {
			t.Errorf("splitOwnerRepo(%q) = (%q,%q,%v), want (%q,%q,%v)", c.in, o, r, ok, c.owner, c.repo, c.ok)
		}
	}
}

func TestCreateGiteaRepoWebhook(t *testing.T) {
	const target = "https://multica.example/api/webhooks/gitea"

	t.Run("creates when absent", func(t *testing.T) {
		posted := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "token pat-xyz" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write([]byte(`[]`))
			case http.MethodPost:
				posted = true
				w.WriteHeader(http.StatusCreated)
			}
		}))
		defer srv.Close()
		created, err := createGiteaRepoWebhook(context.Background(), srv.URL, "pat-xyz", "acme", "app", target, "sekret")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !created {
			t.Fatal("expected created=true")
		}
		if !posted {
			t.Fatal("expected a POST to be issued")
		}
	})

	t.Run("skips when a hook already targets the URL", func(t *testing.T) {
		posted := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				_, _ = w.Write([]byte(`[{"config":{"url":"` + target + `"}}]`))
			case http.MethodPost:
				posted = true
				w.WriteHeader(http.StatusCreated)
			}
		}))
		defer srv.Close()
		created, err := createGiteaRepoWebhook(context.Background(), srv.URL, "pat", "acme", "app", target, "sekret")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if created {
			t.Fatal("expected created=false (idempotent)")
		}
		if posted {
			t.Fatal("expected no POST when a matching hook already exists")
		}
	})

	t.Run("maps 404 to an error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		if _, err := createGiteaRepoWebhook(context.Background(), srv.URL, "pat", "acme", "app", target, "sekret"); err == nil {
			t.Fatal("expected an error for a 404 repo")
		}
	})
}
