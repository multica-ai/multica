package handler

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/authority"
)

func TestUpsertIssueExternalIdentityRejectsInvalidAliasBeforeDB(t *testing.T) {
	req := newRequest(http.MethodPost, "/api/issues/upsert-external", map[string]any{
		"aliases": []map[string]any{{"namespace": "GitHub", "external_id": "123"}},
		"create":  map[string]any{"title": "Imported"},
	})
	w := httptest.NewRecorder()

	(&Handler{}).UpsertIssueExternalIdentity(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", w.Code, w.Body.String())
	}
}

func TestUpsertIssueExternalIdentityRejectsTrailingJSONBeforeDB(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/issues/upsert-external", strings.NewReader(
		`{"aliases":[{"namespace":"github","external_id":"123"}],"create":{"title":"Imported"}} {}`,
	))
	w := httptest.NewRecorder()

	(&Handler{}).UpsertIssueExternalIdentity(w, req)

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid request body") {
		t.Fatalf("status = %d body=%s, want 400 invalid request body", w.Code, w.Body.String())
	}
}

func TestUpsertIssueExternalIdentityRejectsUnknownFieldBeforeDB(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/issues/upsert-external", strings.NewReader(
		`{"nonce":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","aliases":[{"namespace":"github-node","external_id":"123"}],"unexpected":true}`,
	))
	w := httptest.NewRecorder()
	(&Handler{}).UpsertIssueExternalIdentity(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", w.Code, w.Body.String())
	}
}

func TestUpsertIssueExternalIdentityConcurrentIndependentRequestsConverge(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	oldCfg, oldSigner, oldCommit := testHandler.cfg, testHandler.AuthoritySigner, testHandler.ServerCommit
	testHandler.cfg.ExternalUpsertPrincipalID = testUserID
	testHandler.cfg.ExternalUpsertNamespaces = []string{"github-node"}
	testHandler.AuthoritySigner = &authority.Signer{AuthorityID: "test-authority", PrivateKey: priv, PublicKey: pub}
	testHandler.ServerCommit = "test-commit"
	t.Cleanup(func() {
		testHandler.cfg = oldCfg
		testHandler.AuthoritySigner = oldSigner
		testHandler.ServerCommit = oldCommit
	})
	externalID := "endpoint-concurrent-" + strings.ReplaceAll(t.Name(), "/", "-")
	title := "Endpoint concurrent external upsert"
	t.Cleanup(func() {
		_, _ = testPool.Exec(t.Context(), `DELETE FROM issue WHERE workspace_id=$1 AND title=$2`, testWorkspaceID, title)
	})
	type result struct {
		id   string
		code int
		body string
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			nonce, _ := authority.GenerateNonce(nil)
			req := newRequest(http.MethodPost, "/api/issues/upsert-external", map[string]any{
				"nonce": nonce, "aliases": []map[string]any{{"namespace": "github-node", "external_id": externalID}}, "create": map[string]any{"title": title},
			})
			w := httptest.NewRecorder()
			testHandler.UpsertIssueExternalIdentity(w, req)
			var env struct {
				Issue   IssueResponse          `json:"issue"`
				Receipt authority.WriteReceipt `json:"receipt"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &env)
			results <- result{id: env.Issue.ID, code: w.Code, body: w.Body.String()}
		}()
	}
	close(start)
	first, second := <-results, <-results
	if first.code != http.StatusCreated && first.code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", first.code, first.body)
	}
	if second.code != http.StatusCreated && second.code != http.StatusOK {
		t.Fatalf("second status=%d body=%s", second.code, second.body)
	}
	if first.id == "" || first.id != second.id {
		t.Fatalf("issue ids = %q/%q, want same non-empty", first.id, second.id)
	}
	var issueCount, aliasCount int
	if err := testPool.QueryRow(t.Context(), `SELECT count(*) FROM issue WHERE workspace_id=$1 AND title=$2`, testWorkspaceID, title).Scan(&issueCount); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(t.Context(), `SELECT count(*) FROM issue_external_identity WHERE workspace_id=$1 AND namespace='github-node' AND external_id=$2`, testWorkspaceID, externalID).Scan(&aliasCount); err != nil {
		t.Fatal(err)
	}
	if issueCount != 1 || aliasCount != 1 {
		t.Fatalf("issues=%d aliases=%d, want 1/1", issueCount, aliasCount)
	}
}

func TestExternalUpsertAuthorizationFailClosedAndRejectsTaskTokens(t *testing.T) {
	const principal = "11111111-1111-1111-1111-111111111111"
	aliases := []externalIdentityAliasRequest{{Namespace: "github-node", ExternalID: "123"}}
	cases := []struct {
		name        string
		cfg         Config
		userID      string
		actorSource string
		want        int
	}{
		{name: "unset", userID: principal, want: http.StatusForbidden},
		{name: "wrong principal", cfg: Config{ExternalUpsertPrincipalID: principal, ExternalUpsertNamespaces: []string{"github-node"}}, userID: "22222222-2222-2222-2222-222222222222", want: http.StatusForbidden},
		{name: "task token", cfg: Config{ExternalUpsertPrincipalID: principal, ExternalUpsertNamespaces: []string{"github-node"}}, userID: principal, actorSource: "task_token", want: http.StatusForbidden},
		{name: "allowed", cfg: Config{ExternalUpsertPrincipalID: principal, ExternalUpsertNamespaces: []string{" GitHub-Node ", "github-node"}}, userID: principal, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{cfg: tc.cfg}
			r := httptest.NewRequest(http.MethodPost, "/api/issues/upsert-external", nil)
			r.Header.Set("X-User-ID", tc.userID)
			r.Header.Set("X-Actor-Source", tc.actorSource)
			got, _ := h.externalUpsertAuthorizationError(r, aliases)
			if got != tc.want {
				t.Fatalf("status = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestExternalUpsertAuthorizationRejectsNamespaceOutsideAllowlist(t *testing.T) {
	h := &Handler{cfg: Config{ExternalUpsertPrincipalID: "11111111-1111-1111-1111-111111111111", ExternalUpsertNamespaces: []string{"github-node"}}}
	r := httptest.NewRequest(http.MethodPost, "/api/issues/upsert-external", nil)
	r.Header.Set("X-User-ID", "11111111-1111-1111-1111-111111111111")
	status, _ := h.externalUpsertAuthorizationError(r, []externalIdentityAliasRequest{{Namespace: "github", ExternalID: "123"}})
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", status)
	}
}
