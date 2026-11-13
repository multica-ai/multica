package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func newServiceAccountRequest(t *testing.T, userID, method, path string, body any, extraURLParams ...string) *http.Request {
	t.Helper()
	req := newRequest(method, path, body)
	req.Header.Set("X-User-ID", userID)
	// WithURLParams builds a fresh route context, so "id" and the extras
	// must be set together.
	params := append([]string{"id", testWorkspaceID}, extraURLParams...)
	return testutil.WithURLParams(req, params...)
}

func createServiceAccountAs(t *testing.T, requesterID, name string) (ServiceAccountIssuedResponse, *httptest.ResponseRecorder) {
	t.Helper()
	req := newServiceAccountRequest(t, requesterID, "POST", "/api/workspaces/"+testWorkspaceID+"/service-accounts", CreateServiceAccountRequest{Name: name})
	w := httptest.NewRecorder()
	testHandler.CreateServiceAccount(w, req)
	var issued ServiceAccountIssuedResponse
	if w.Code == http.StatusCreated {
		if err := json.NewDecoder(w.Body).Decode(&issued); err != nil {
			t.Fatalf("decode created service account: %v", err)
		}
	}
	return issued, w
}

func assertServiceAccountPersisted(t *testing.T, issued ServiceAccountIssuedResponse, ownerID string) {
	t.Helper()
	ctx := context.Background()

	var kind, email string
	if err := testPool.QueryRow(ctx, `SELECT kind, email FROM "user" WHERE id = $1`,
		parseUUID(issued.ID)).Scan(&kind, &email); err != nil {
		t.Fatalf("service account user row missing: %v", err)
	}
	if kind != "service" {
		t.Fatalf("user.kind = %q, want service", kind)
	}
	if !strings.HasSuffix(email, ServiceAccountEmailSuffix) {
		t.Fatalf("generated email %q lacks reserved suffix %q", email, ServiceAccountEmailSuffix)
	}
	if !strings.HasPrefix(email, "service-") {
		t.Fatalf("generated email %q lacks service- prefix", email)
	}

	var role string
	if err := testPool.QueryRow(ctx, `SELECT role FROM member WHERE user_id = $1 AND workspace_id = $2`,
		parseUUID(issued.ID), parseUUID(testWorkspaceID)).Scan(&role); err != nil {
		t.Fatalf("service account membership missing: %v", err)
	}
	if role != "member" {
		t.Fatalf("service account role = %q, want member", role)
	}

	var hash string
	if err := testPool.QueryRow(ctx, `SELECT token_hash FROM personal_access_token WHERE user_id = $1`,
		parseUUID(issued.ID)).Scan(&hash); err != nil {
		t.Fatalf("service account credential missing: %v", err)
	}
	if issued.Token == "" {
		t.Fatal("created response must carry the raw token once")
	}
}

func TestCreateServiceAccount_AsOwnerAdmin(t *testing.T) {
	ownerID := createTestUserAndMember(t, "owner")

	issued, w := createServiceAccountAs(t, ownerID, "ci-runner")
	if w.Code != http.StatusCreated {
		t.Fatalf("owner create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	assertServiceAccountPersisted(t, issued, ownerID)

	// Admins can mint too.
	adminIssued, w2 := createServiceAccountAs(t, adminFixtureID(t), "bot-second")
	if w2.Code != http.StatusCreated {
		t.Fatalf("admin create: expected 201, got %d: %s", w2.Code, w2.Body.String())
	}
	assertServiceAccountPersisted(t, adminIssued, ownerID)
}

func TestCreateServiceAccount_MemberAndAnonymousDenied(t *testing.T) {
	memberID := createTestUserAndMember(t, "member")
	req := newServiceAccountRequest(t, memberID, "POST", "/api/workspaces/"+testWorkspaceID+"/service-accounts", CreateServiceAccountRequest{Name: "x"})
	w := httptest.NewRecorder()
	testHandler.CreateServiceAccount(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("member create: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// A user with no membership in the workspace is a 404, not a 403,
	// mirroring the other workspace-scoped endpoints.
	outsiderID := createTestUserAndMember(t, "")
	req2 := newServiceAccountRequest(t, outsiderID, "POST", "/api/workspaces/"+testWorkspaceID+"/service-accounts", CreateServiceAccountRequest{Name: "x"})
	w2 := httptest.NewRecorder()
	testHandler.CreateServiceAccount(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("outsider create: expected 404, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestCreateServiceAccount_Validation(t *testing.T) {
	ownerID := createTestUserAndMember(t, "owner")

	req := newServiceAccountRequest(t, ownerID, "POST", "/api/workspaces/"+testWorkspaceID+"/service-accounts", CreateServiceAccountRequest{Name: ""})
	w := httptest.NewRecorder()
	testHandler.CreateServiceAccount(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing name: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	badBody := []byte(`not json`)
	req2 := newServiceAccountRequest(t, ownerID, "POST", "/api/workspaces/"+testWorkspaceID+"/service-accounts", badBody)
	w2 := httptest.NewRecorder()
	testHandler.CreateServiceAccount(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("malformed body: expected 400, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestListServiceAccounts_ReportsTokenState(t *testing.T) {
	ownerID := createTestUserAndMember(t, "owner")

	first, w := createServiceAccountAs(t, ownerID, "list-a")
	if w.Code != http.StatusCreated {
		t.Fatalf("create a: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	second, w2 := createServiceAccountAs(t, ownerID, "list-b")
	if w2.Code != http.StatusCreated {
		t.Fatalf("create b: expected 201, got %d: %s", w2.Code, w2.Body.String())
	}

	req := newServiceAccountRequest(t, ownerID, "GET", "/api/workspaces/"+testWorkspaceID+"/service-accounts", nil)
	w3 := httptest.NewRecorder()
	testHandler.ListServiceAccounts(w3, req)
	if w3.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", w3.Code, w3.Body.String())
	}
	var accounts []ServiceAccountResponse
	if err := json.NewDecoder(w3.Body).Decode(&accounts); err != nil {
		t.Fatalf("decode list: %v", err)
	}

	byID := map[string]ServiceAccountResponse{}
	for _, a := range accounts {
		byID[a.ID] = a
	}
	a, ok := byID[first.ID]
	if !ok {
		t.Fatalf("created account %s missing from list (%d accounts)", first.ID, len(accounts))
	}
	if a.Name != first.Name || a.ActiveTokens != 1 {
		t.Fatalf("account %s = name %q active %d, want %q/1", first.ID, a.Name, a.ActiveTokens, first.Name)
	}
	if b, ok := byID[second.ID]; !ok || b.ActiveTokens != 1 {
		t.Fatalf("account %s missing or active tokens = %d, want 1", second.ID, b.ActiveTokens)
	}
}

func TestCreateServiceAccountToken_Rotates(t *testing.T) {
	ownerID := createTestUserAndMember(t, "owner")
	issued, w := createServiceAccountAs(t, ownerID, "rotate-me")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	req := newServiceAccountRequest(t, ownerID, "POST", "/api/workspaces/"+testWorkspaceID+"/service-accounts/"+issued.ID+"/tokens", CreateServiceAccountTokenRequest{}, "accountId", issued.ID)
	w2 := httptest.NewRecorder()
	testHandler.CreateServiceAccountToken(w2, req)
	if w2.Code != http.StatusCreated {
		t.Fatalf("mint: expected 201, got %d: %s", w2.Code, w2.Body.String())
	}
	var rotated ServiceAccountIssuedResponse
	if err := json.NewDecoder(w2.Body).Decode(&rotated); err != nil {
		t.Fatalf("decode minted token: %v", err)
	}
	if rotated.Token == "" || rotated.Token == issued.Token {
		t.Fatal("mint must return a fresh credential")
	}

	var active int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM personal_access_token WHERE user_id = $1 AND revoked = FALSE AND (expires_at IS NULL OR expires_at > now())`,
		parseUUID(issued.ID)).Scan(&active); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if active != 2 {
		t.Fatalf("active tokens = %d, want 2 (old one still valid until revoked)", active)
	}
}

func TestCreateServiceAccountToken_UnknownAccount(t *testing.T) {
	ownerID := createTestUserAndMember(t, "owner")
	foreignID := createTestUserAndMember(t, "") // human, not a service account

	req := newServiceAccountRequest(t, ownerID, "POST", "/api/workspaces/"+testWorkspaceID+"/service-accounts/"+foreignID+"/tokens", CreateServiceAccountTokenRequest{}, "accountId", foreignID)
	w := httptest.NewRecorder()
	testHandler.CreateServiceAccountToken(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("mint for non-service-account user: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRevokeServiceAccountToken(t *testing.T) {
	ownerID := createTestUserAndMember(t, "owner")
	issued, w := createServiceAccountAs(t, ownerID, "revoke-me")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var tokenID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM personal_access_token WHERE user_id = $1`, parseUUID(issued.ID)).Scan(&tokenID); err != nil {
		t.Fatalf("find minted token: %v", err)
	}

	req := newServiceAccountRequest(t, ownerID, "DELETE", "/api/workspaces/"+testWorkspaceID+"/service-accounts/tokens/"+tokenID, nil, "tokenId", tokenID)
	w2 := httptest.NewRecorder()
	testHandler.RevokeServiceAccountToken(w2, req)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("revoke: expected 204, got %d: %s", w2.Code, w2.Body.String())
	}

	var revoked bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT revoked FROM personal_access_token WHERE id = $1`, parseUUID(tokenID)).Scan(&revoked); err != nil {
		t.Fatalf("reload token: %v", err)
	}
	if !revoked {
		t.Fatal("token still active after revoke")
	}

	// Second revoke misses and reports 404.
	req3 := newServiceAccountRequest(t, ownerID, "DELETE", "/api/workspaces/"+testWorkspaceID+"/service-accounts/tokens/"+tokenID, nil, "tokenId", tokenID)
	w3 := httptest.NewRecorder()
	testHandler.RevokeServiceAccountToken(w3, req3)
	if w3.Code != http.StatusNotFound {
		t.Fatalf("second revoke: expected 404, got %d: %s", w3.Code, w3.Body.String())
	}
}

func TestRevokeServiceAccountToken_ForeignWorkspaceRejected(t *testing.T) {
	ownerID := createTestUserAndMember(t, "owner")
	issued, w := createServiceAccountAs(t, ownerID, "cross-ws-target")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var tokenID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM personal_access_token WHERE user_id = $1`, parseUUID(issued.ID)).Scan(&tokenID); err != nil {
		t.Fatalf("find minted token: %v", err)
	}

	// A second workspace whose owner/admin attacker calls the same endpoint.
	otherOwnerID := createTestUserAndMember(t, "")
	ctx := context.Background()
	var otherWorkspaceID string
	otherSlug := fmt.Sprintf("svc-acct-cross-ws-%d", testSeq.Add(1))
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Service Account Cross-WS', $1, '', 'SACW')
		RETURNING id
	`, otherSlug).Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE slug = $1`, otherSlug)
	})
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, parseUUID(otherWorkspaceID), parseUUID(otherOwnerID)); err != nil {
		t.Fatalf("add other member: %v", err)
	}

	otherOwner := newRequest("DELETE", "/api/workspaces/"+otherWorkspaceID+"/service-accounts/tokens/"+tokenID, nil)
	otherOwner.Header.Set("X-User-ID", otherOwnerID)
	otherOwner = testutil.WithURLParams(otherOwner, "id", otherWorkspaceID, "tokenId", tokenID)
	w2 := httptest.NewRecorder()
	testHandler.RevokeServiceAccountToken(w2, otherOwner)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace revoke: expected 404, got %d: %s", w2.Code, w2.Body.String())
	}

	var revoked bool
	if err := testPool.QueryRow(ctx, `SELECT revoked FROM personal_access_token WHERE id = $1`, parseUUID(tokenID)).Scan(&revoked); err != nil {
		t.Fatalf("reload token: %v", err)
	}
	if revoked {
		t.Fatal("cross-workspace caller revoked the token; EXISTS guard failed")
	}
}

func adminFixtureID(t *testing.T) string {
	t.Helper()
	return createTestUserAndMember(t, "admin")
}
