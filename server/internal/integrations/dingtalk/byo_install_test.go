package dingtalk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// dingtalkMockServer stubs the single Open-API call RegisterBYO makes:
// /v1.0/oauth2/accessToken (mint a token from AppKey/AppSecret). tokenOK=false
// makes it reject the credentials with a 400, as DingTalk does for a bad pair.
func dingtalkMockServer(t *testing.T, tokenOK bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != accessTokenPath {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"unknownPath","message":"unknown"}`))
			return
		}
		if !tokenOK {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"InvalidAuthentication","message":"appKey or appSecret is invalid"}`))
			return
		}
		_, _ = w.Write([]byte(`{"accessToken":"tok-abc","expireIn":7200}`))
	}))
}

func byoParams(ws, agent string) RegisterBYOParams {
	return RegisterBYOParams{
		WorkspaceID: pgtypeUUID(ws),
		AgentID:     pgtypeUUID(agent),
		InitiatorID: pgtypeUUID("33333333-3333-3333-3333-333333333333"),
		AppKey:      "ding-app-key-xyz",
		AppSecret:   "ding-app-secret-xyz",
	}
}

// pgtypeUUID is a test-local UUID parse that panics on bad input (test data is
// always valid), so byoParams stays a plain literal.
func pgtypeUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		panic(err)
	}
	return u
}

func TestRegisterBYO_PersistsEncryptedSecretKeyedByAppID(t *testing.T) {
	srv := dingtalkMockServer(t, true)
	defer srv.Close()

	q := &fakeInstallQueries{rowID: mustUUID(t, "44444444-4444-4444-4444-444444444444")}
	svc := newTestInstallService(t, q)
	svc.apiBase = srv.URL

	row, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	))
	if err != nil {
		t.Fatalf("RegisterBYO: %v", err)
	}
	if row.ID != q.rowID {
		t.Errorf("row id = %v, want %v", row.ID, q.rowID)
	}
	if !q.createCalled || !q.grantCalled {
		t.Fatalf("connector/grant persist not called: create=%t grant=%t", q.createCalled, q.grantCalled)
	}

	var cfg installConfig
	if err := json.Unmarshal(q.createParams.Config, &cfg); err != nil {
		t.Fatalf("decode upserted config: %v", err)
	}
	// Routing key is the AppKey (== robotCode for a Stream-mode robot).
	if cfg.AppID != "ding-app-key-xyz" || cfg.RobotCode != "ding-app-key-xyz" {
		t.Errorf("config app_id/robot_code = %q/%q, want ding-app-key-xyz", cfg.AppID, cfg.RobotCode)
	}
	// AppSecret stored encrypted (never plaintext) and decrypts back. AppKey is
	// not a secret and lives in app_id in the clear (like Feishu's app_id).
	if cfg.AppSecretEncrypted == "" {
		t.Fatalf("app secret must be stored: %+v", cfg)
	}
	if strings.Contains(cfg.AppSecretEncrypted, "ding-app-secret-xyz") {
		t.Error("app secret must be stored encrypted, not plaintext")
	}
	secret, err := decryptToken(cfg.AppSecretEncrypted, svc.box.Open)
	if err != nil || secret != "ding-app-secret-xyz" {
		t.Errorf("decrypted app secret = %q, %v", secret, err)
	}
}

func TestRegisterBYO_MissingCredentials(t *testing.T) {
	q := &fakeInstallQueries{}
	svc := newTestInstallService(t, q)

	// Empty AppKey — rejected before any network call or upsert.
	p := byoParams("11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	p.AppKey = "   "
	if _, err := svc.RegisterBYO(context.Background(), p); err != ErrInvalidAppKey {
		t.Errorf("empty app key = %v, want ErrInvalidAppKey", err)
	}
	// Empty AppSecret.
	p = byoParams("11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	p.AppSecret = ""
	if _, err := svc.RegisterBYO(context.Background(), p); err != ErrInvalidAppSecret {
		t.Errorf("empty app secret = %v, want ErrInvalidAppSecret", err)
	}
	if q.createCalled || q.grantCalled {
		t.Error("missing credentials must be rejected before connector/grant persistence")
	}
}

func TestRegisterBYO_AccessTokenFailure(t *testing.T) {
	srv := dingtalkMockServer(t, false) // DingTalk rejects the credentials
	defer srv.Close()
	q := &fakeInstallQueries{}
	svc := newTestInstallService(t, q)
	svc.apiBase = srv.URL

	if _, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	)); err == nil {
		t.Fatal("expected an error when the access-token mint rejects the credentials")
	}
	if q.createCalled || q.grantCalled {
		t.Error("a failed credential validation must not persist an installation")
	}
}

func TestRegisterBYO_CredentialValidationTimesOut(t *testing.T) {
	q := &fakeInstallQueries{}
	svc := newTestInstallService(t, q)
	svc.validationTimeout = 10 * time.Millisecond
	svc.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}

	_, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	))
	if !errors.Is(err, ErrCredentialValidation) {
		t.Fatalf("timeout error = %v, want ErrCredentialValidation", err)
	}
	if q.createCalled || q.grantCalled {
		t.Fatal("timed-out credential validation persisted an installation")
	}
}

func TestRegisterBYO_SameRobotAcrossWorkspacesSharesConnector(t *testing.T) {
	srv := dingtalkMockServer(t, true)
	defer srv.Close()
	q := &fakeInstallQueries{
		rowID: mustUUID(t, "44444444-4444-4444-4444-444444444444"),
	}
	svc := newTestInstallService(t, q)
	svc.apiBase = srv.URL

	first, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	))
	if err != nil {
		t.Fatalf("register first workspace: %v", err)
	}
	second, err := svc.RegisterBYO(context.Background(), byoParams(
		"99999999-9999-9999-9999-999999999999",
		"88888888-8888-8888-8888-888888888888",
	))
	if err != nil {
		t.Fatalf("register second workspace: %v", err)
	}
	if first.ID != second.ID || first.ID != q.rowID {
		t.Fatalf("connector ids = %v and %v, want shared %v", first.ID, second.ID, q.rowID)
	}
	if !q.createCalled || !q.updateCalled || len(q.grantParams) != 2 {
		t.Fatalf("persistence calls create=%t update=%t grants=%d, want true, true, 2", q.createCalled, q.updateCalled, len(q.grantParams))
	}
}

func TestRegisterBYO_SameWorkspaceUpdatesDefaultAgentGrant(t *testing.T) {
	srv := dingtalkMockServer(t, true)
	defer srv.Close()
	q := &fakeInstallQueries{rowID: mustUUID(t, "44444444-4444-4444-4444-444444444444")}
	svc := newTestInstallService(t, q)
	svc.apiBase = srv.URL

	if _, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	)); err != nil {
		t.Fatalf("register first default agent: %v", err)
	}
	if _, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"77777777-7777-7777-7777-777777777777",
	)); err != nil {
		t.Fatalf("update default agent: %v", err)
	}
	last := q.grantParams[len(q.grantParams)-1]
	if last.DefaultAgentID != pgtypeUUID("77777777-7777-7777-7777-777777777777") {
		t.Fatalf("default agent = %v, want updated agent", last.DefaultAgentID)
	}
}

func TestRegisterBYO_ReactivatesRevokedConnector(t *testing.T) {
	srv := dingtalkMockServer(t, true)
	defer srv.Close()
	connectorID := mustUUID(t, "44444444-4444-4444-4444-444444444444")
	q := &fakeInstallQueries{
		rowID: connectorID,
		connector: &db.DingtalkConnector{
			ID: connectorID, AppID: "ding-app-key-xyz", Status: "revoked",
		},
	}
	svc := newTestInstallService(t, q)
	svc.apiBase = srv.URL

	row, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	))
	if err != nil {
		t.Fatalf("RegisterBYO after revoke: %v", err)
	}
	if !q.updateCalled || !q.grantCalled {
		t.Fatalf("reactivation calls update=%t grant=%t, want both", q.updateCalled, q.grantCalled)
	}
	if row.ID != connectorID {
		t.Errorf("row id = %v, want preserved connector %v", row.ID, connectorID)
	}
}

func TestRegisterBYO_SameAgentReconnect_PreservesConnectorIdentity(t *testing.T) {
	srv := dingtalkMockServer(t, true)
	defer srv.Close()
	existing := &db.DingtalkConnector{
		ID:    mustUUID(t, "44444444-4444-4444-4444-444444444444"),
		AppID: "ding-app-key-xyz", Status: "active",
	}
	q := &fakeInstallQueries{connector: existing, rowID: existing.ID}
	svc := newTestInstallService(t, q)
	svc.apiBase = srv.URL

	row, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	))
	if err != nil {
		t.Fatalf("RegisterBYO same-agent reconnect: %v", err)
	}
	if row.ID != existing.ID {
		t.Errorf("reconnect row id = %v, want in-place %v", row.ID, existing.ID)
	}
	if q.createCalled || !q.updateCalled {
		t.Fatalf("same-AppKey reconnect create=%t update=%t, want false/true", q.createCalled, q.updateCalled)
	}
}

func TestRegisterBYO_DifferentAppKeyCreatesDistinctConnectorIdentity(t *testing.T) {
	srv := dingtalkMockServer(t, true)
	defer srv.Close()
	oldID := mustUUID(t, "44444444-4444-4444-4444-444444444444")
	newID := mustUUID(t, "55555555-5555-5555-5555-555555555555")
	existing := &db.DingtalkConnector{
		ID: oldID, AppID: "ding-old-app-key", Status: "active",
	}
	q := &fakeInstallQueries{
		connector: existing,
		rowID:     newID,
	}
	svc := newTestInstallService(t, q)
	svc.apiBase = srv.URL

	row, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	))
	if err != nil {
		t.Fatalf("RegisterBYO different-AppKey replacement: %v", err)
	}
	if !q.lockCalled {
		t.Fatal("replacement decision was not serialized")
	}
	if !q.createCalled || q.updateCalled {
		t.Fatalf("different AppKey create=%t update=%t, want true/false", q.createCalled, q.updateCalled)
	}
	if row.ID != newID || row.ID == oldID {
		t.Fatalf("replacement row id = %v, want fresh %v (old %v)", row.ID, newID, oldID)
	}
}

func TestRevokeDingTalkInstallationScopesToWorkspaceGrant(t *testing.T) {
	q := &fakeInstallQueries{}
	svc := newTestInstallService(t, q)
	connectorID := pgtypeUUID("44444444-4444-4444-4444-444444444444")
	workspaceID := pgtypeUUID("11111111-1111-1111-1111-111111111111")
	if err := svc.Revoke(context.Background(), connectorID, workspaceID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if q.revokeParams.ConnectorID != connectorID || q.revokeParams.WorkspaceID != workspaceID {
		t.Fatalf("revoke params = %+v, want connector/workspace scoped", q.revokeParams)
	}
}
