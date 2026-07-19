package handler

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
		id                 string
		receiptWorkspaceID string
		code               int
		body               string
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			nonce, _ := authority.GenerateNonce(nil)
			req := newRequest(http.MethodPost, "/api/issues/upsert-external", map[string]any{
				"nonce": nonce, "write_receipt_protocol": authority.WriteReceiptProtocolV2, "aliases": []map[string]any{{"namespace": "github-node", "external_id": externalID}}, "create": map[string]any{"title": title},
			})
			w := httptest.NewRecorder()
			testHandler.UpsertIssueExternalIdentity(w, req)
			var env struct {
				Issue   IssueResponse          `json:"issue"`
				Receipt authority.WriteReceipt `json:"receipt"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &env)
			results <- result{id: env.Issue.ID, receiptWorkspaceID: env.Receipt.WorkspaceID, code: w.Code, body: w.Body.String()}
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
	if first.receiptWorkspaceID != testWorkspaceID || second.receiptWorkspaceID != testWorkspaceID {
		t.Fatalf("receipt workspace ids = %q/%q, want %q", first.receiptWorkspaceID, second.receiptWorkspaceID, testWorkspaceID)
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

func TestUpsertIssueExternalIdentityOldClientGetsStrictV1Receipt(t *testing.T) {
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

	externalID := "old-client-v1-" + strings.ReplaceAll(t.Name(), "/", "-")
	title := "Old client new server rollout"
	t.Cleanup(func() {
		_, _ = testPool.Exec(t.Context(), `DELETE FROM issue WHERE workspace_id=$1 AND title=$2`, testWorkspaceID, title)
	})
	nonce, err := authority.GenerateNonce(nil)
	if err != nil {
		t.Fatal(err)
	}
	req := newRequest(http.MethodPost, "/api/issues/upsert-external", map[string]any{
		"nonce":   nonce,
		"aliases": []map[string]any{{"namespace": "github-node", "external_id": externalID}},
		"create":  map[string]any{"title": title},
	})
	w := httptest.NewRecorder()
	testHandler.UpsertIssueExternalIdentity(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s, want 201", w.Code, w.Body.String())
	}

	// Model the old client's strict response shape: workspace_id is unknown in v1.
	var oldEnvelope struct {
		Issue   json.RawMessage `json:"issue"`
		Receipt struct {
			Protocol      string               `json:"protocol"`
			Operation     string               `json:"operation"`
			RequestSHA256 string               `json:"request_sha256"`
			ResourceID    string               `json:"resource_id"`
			Nonce         string               `json:"nonce"`
			AuthorityID   string               `json:"authority_id"`
			DBIdentity    authority.DBIdentity `json:"db_identity"`
			IssuedAt      string               `json:"issued_at"`
			ServerCommit  string               `json:"server_commit"`
			Signature     string               `json:"signature"`
		} `json:"receipt"`
	}
	dec := json.NewDecoder(bytes.NewReader(w.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&oldEnvelope); err != nil {
		t.Fatalf("old strict client rejected new-server v1 response after commit: %v; body=%s", err, w.Body.String())
	}
	var oldIssueFields map[string]json.RawMessage
	if err := json.Unmarshal(oldEnvelope.Issue, &oldIssueFields); err != nil {
		t.Fatalf("decode old-client issue response: %v", err)
	}
	if _, ok := oldIssueFields["properties"]; ok {
		t.Fatalf("old strict client would reject new-server v1 issue response after commit: unknown issue field %q; body=%s", "properties", w.Body.String())
	}
	if oldEnvelope.Receipt.Protocol != "multica-authority-write-receipt-v1" {
		t.Fatalf("protocol=%q, want v1", oldEnvelope.Receipt.Protocol)
	}
}

func TestUpsertIssueExternalIdentityRejectsUnsupportedReceiptProtocolBeforeDB(t *testing.T) {
	nonce, err := authority.GenerateNonce(nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/issues/upsert-external", strings.NewReader(
		`{"nonce":"`+nonce+`","write_receipt_protocol":"multica-authority-write-receipt-v999","aliases":[{"namespace":"github-node","external_id":"123"}],"create":{"title":"Imported"}}`,
	))
	w := httptest.NewRecorder()
	(&Handler{}).UpsertIssueExternalIdentity(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "unsupported write receipt protocol") {
		t.Fatalf("status=%d body=%s, want pre-DB 400 unsupported protocol", w.Code, w.Body.String())
	}
}

type failingExternalReceiptDB struct{}

func (failingExternalReceiptDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("injected DB identity failure")
}

func (failingExternalReceiptDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("injected DB identity failure")
}

func (failingExternalReceiptDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return failingExternalReceiptRow{}
}

type failingExternalReceiptRow struct{}

func (failingExternalReceiptRow) Scan(...any) error {
	return errors.New("injected DB identity failure")
}

type failSecondWriteReceiptSigner struct {
	delegate writeReceiptSigner
	calls    int
}

func (s *failSecondWriteReceiptSigner) SignWriteReceipt(stmt authority.WriteReceiptStatement) (authority.WriteReceipt, error) {
	s.calls++
	if s.calls == 2 {
		return authority.WriteReceipt{}, errors.New("injected final receipt signing failure")
	}
	return s.delegate.SignWriteReceipt(stmt)
}

func externalUpsertDBSnapshot(t *testing.T) string {
	t.Helper()
	var snapshot string
	if err := testPool.QueryRow(t.Context(), `
		SELECT jsonb_build_object(
			'workspace', (SELECT to_jsonb(w) FROM workspace w WHERE w.id = $1),
			'issues', COALESCE((SELECT jsonb_agg(to_jsonb(i) ORDER BY i.id) FROM issue i WHERE i.workspace_id = $1), '[]'::jsonb),
			'aliases', COALESCE((SELECT jsonb_agg(to_jsonb(e) ORDER BY e.namespace, e.external_id) FROM issue_external_identity e WHERE e.workspace_id = $1), '[]'::jsonb)
		)::text
	`, testWorkspaceID).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot external upsert database state: %v", err)
	}
	return snapshot
}

func TestUpsertIssueExternalIdentityReceiptFailuresLeaveCompleteDBSnapshotUnchanged(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name      string
		configure func(*Handler)
	}{
		{
			name: "database identity query",
			configure: func(h *Handler) {
				h.DB = failingExternalReceiptDB{}
				h.AuthoritySigner = &authority.Signer{AuthorityID: "test-authority", PrivateKey: priv, PublicKey: pub}
			},
		},
		{
			name: "receipt signing",
			configure: func(h *Handler) {
				signer := &authority.Signer{AuthorityID: "test-authority", PrivateKey: priv, PublicKey: pub}
				h.AuthoritySigner = signer
				h.writeReceiptSigner = &failSecondWriteReceiptSigner{delegate: signer}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := *testHandler
			h.cfg.ExternalUpsertPrincipalID = testUserID
			h.cfg.ExternalUpsertNamespaces = []string{"github-node"}
			h.ServerCommit = "test-commit"
			h.writeReceiptSigner = nil
			tc.configure(&h)

			suffix := strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
			externalID := "receipt-fault-" + suffix
			title := "Receipt fault " + suffix
			t.Cleanup(func() {
				_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_external_identity WHERE workspace_id=$1 AND namespace='github-node' AND external_id=$2`, testWorkspaceID, externalID)
				_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id=$1 AND title=$2`, testWorkspaceID, title)
			})

			before := externalUpsertDBSnapshot(t)
			nonce, err := authority.GenerateNonce(nil)
			if err != nil {
				t.Fatal(err)
			}
			req := newRequest(http.MethodPost, "/api/issues/upsert-external", map[string]any{
				"nonce":                  nonce,
				"write_receipt_protocol": authority.WriteReceiptProtocolV2,
				"aliases":                []map[string]any{{"namespace": "github-node", "external_id": externalID}},
				"create":                 map[string]any{"title": title},
			})
			w := httptest.NewRecorder()
			h.UpsertIssueExternalIdentity(w, req)

			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status=%d body=%s, want 500", w.Code, w.Body.String())
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode failure response: %v", err)
			}
			if _, ok := body["receipt"]; ok {
				t.Fatalf("failure response contains success-like receipt: %s", w.Body.String())
			}
			if _, ok := body["issue"]; ok {
				t.Fatalf("failure response contains success-like issue: %s", w.Body.String())
			}
			after := externalUpsertDBSnapshot(t)
			if after != before {
				t.Fatalf("database snapshot changed after %s failure\nbefore=%s\nafter=%s", tc.name, before, after)
			}
		})
	}
}

func TestDeleteIssueCleansExternalIdentitiesInApplicationTransaction(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	title := "Delete external aliases " + strings.ReplaceAll(t.Name(), "/", "-")
	externalID := "delete-alias-" + strings.ReplaceAll(t.Name(), "/", "-")
	var issueID string
	if err := testPool.QueryRow(t.Context(), `
		WITH next_number AS (
			UPDATE workspace SET issue_counter = issue_counter + 1 WHERE id = $1 RETURNING issue_counter
		)
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number)
		SELECT $1, $2, 'todo', 'none', 'member', $3, -1, issue_counter FROM next_number
		RETURNING id
	`, testWorkspaceID, title, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_external_identity WHERE issue_id=$1`, issueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id=$1`, issueID)
	})
	if _, err := testPool.Exec(t.Context(), `
		INSERT INTO issue_external_identity(workspace_id, namespace, external_id, issue_id)
		VALUES($1, 'github-node', $2, $3)
	`, testWorkspaceID, externalID, issueID); err != nil {
		t.Fatalf("create alias fixture: %v", err)
	}

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodDelete, "/api/issues/"+issueID, nil), "id", issueID)
	testHandler.DeleteIssue(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s, want 204", w.Code, w.Body.String())
	}
	var issues, aliases int
	if err := testPool.QueryRow(t.Context(), `SELECT count(*) FROM issue WHERE id=$1`, issueID).Scan(&issues); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(t.Context(), `SELECT count(*) FROM issue_external_identity WHERE issue_id=$1`, issueID).Scan(&aliases); err != nil {
		t.Fatal(err)
	}
	if issues != 0 || aliases != 0 {
		t.Fatalf("application cleanup left issues=%d aliases=%d, want 0/0", issues, aliases)
	}
}

func TestBatchDeleteIssuesCleansExternalIdentitiesInApplicationTransaction(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	issueID := createTestIssue(t, "Batch delete external aliases "+strings.ReplaceAll(t.Name(), "/", "-"), "todo", "none")
	externalID := "batch-delete-alias-" + strings.ReplaceAll(t.Name(), "/", "-")
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_external_identity WHERE issue_id=$1`, issueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id=$1`, issueID)
	})
	if _, err := testPool.Exec(t.Context(), `
		INSERT INTO issue_external_identity(workspace_id, namespace, external_id, issue_id)
		VALUES($1, 'github-node', $2, $3)
	`, testWorkspaceID, externalID, issueID); err != nil {
		t.Fatalf("create alias fixture: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/issues/batch-delete", map[string]any{"issue_ids": []string{issueID}})
	testHandler.BatchDeleteIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", w.Code, w.Body.String())
	}
	var response struct {
		Deleted int `json:"deleted"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Deleted != 1 {
		t.Fatalf("deleted=%d, want 1", response.Deleted)
	}
	var aliases int
	if err := testPool.QueryRow(t.Context(), `SELECT count(*) FROM issue_external_identity WHERE issue_id=$1`, issueID).Scan(&aliases); err != nil {
		t.Fatal(err)
	}
	if aliases != 0 {
		t.Fatalf("batch application cleanup left aliases=%d, want 0", aliases)
	}
}

func TestDeleteWorkspaceCleansExternalIdentitiesInApplicationTransaction(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	const slug = "handler-tests-delete-external-identities"
	ctx := t.Context()
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug=$1`, slug)
	var workspaceID, issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace(name, slug, description)
		VALUES('Handler Test Delete External Identities', $1, 'application cleanup test')
		RETURNING id
	`, slug).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_external_identity WHERE workspace_id=$1`, workspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id=$1`, workspaceID)
	})
	if _, err := testPool.Exec(ctx, `INSERT INTO member(workspace_id, user_id, role) VALUES($1, $2, 'owner')`, workspaceID, testUserID); err != nil {
		t.Fatalf("create owner fixture: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		WITH next_number AS (
			UPDATE workspace SET issue_counter=issue_counter+1 WHERE id=$1 RETURNING issue_counter
		)
		INSERT INTO issue(workspace_id, title, status, priority, creator_type, creator_id, position, number)
		SELECT $1, 'Workspace delete alias fixture', 'todo', 'none', 'member', $2, -1, issue_counter FROM next_number
		RETURNING id
	`, workspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue fixture: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue_external_identity(workspace_id, namespace, external_id, issue_id)
		VALUES($1, 'github-node', 'workspace-delete-alias', $2)
	`, workspaceID, issueID); err != nil {
		t.Fatalf("create alias fixture: %v", err)
	}

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodDelete, "/api/workspaces/"+workspaceID, nil), "id", workspaceID)
	testHandler.DeleteWorkspace(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s, want 204", w.Code, w.Body.String())
	}
	var aliases int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM issue_external_identity WHERE workspace_id=$1`, workspaceID).Scan(&aliases); err != nil {
		t.Fatal(err)
	}
	if aliases != 0 {
		t.Fatalf("workspace application cleanup left aliases=%d, want 0", aliases)
	}
}

func TestUpdateIssueRejectsWorkspaceMoveBeforeMutatingExternalIdentityRelationship(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	title := "Reject workspace move " + strings.ReplaceAll(t.Name(), "/", "-")
	externalID := "workspace-move-" + strings.ReplaceAll(t.Name(), "/", "-")
	var issueID string
	if err := testPool.QueryRow(t.Context(), `
		WITH next_number AS (
			UPDATE workspace SET issue_counter = issue_counter + 1 WHERE id = $1 RETURNING issue_counter
		)
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number)
		SELECT $1, $2, 'todo', 'none', 'member', $3, -1, issue_counter FROM next_number
		RETURNING id
	`, testWorkspaceID, title, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_external_identity WHERE issue_id=$1`, issueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id=$1`, issueID)
	})
	if _, err := testPool.Exec(t.Context(), `
		INSERT INTO issue_external_identity(workspace_id, namespace, external_id, issue_id)
		VALUES($1, 'github-node', $2, $3)
	`, testWorkspaceID, externalID, issueID); err != nil {
		t.Fatalf("create alias fixture: %v", err)
	}

	before := externalUpsertDBSnapshot(t)
	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPatch, "/api/issues/"+issueID, map[string]any{
		"workspace_id": "11111111-1111-1111-1111-111111111111",
	}), "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400 for forbidden workspace move", w.Code, w.Body.String())
	}
	if after := externalUpsertDBSnapshot(t); after != before {
		t.Fatalf("workspace move attempt changed database snapshot\nbefore=%s\nafter=%s", before, after)
	}
}
