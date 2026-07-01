package dingtalk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

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
	if !q.upsertCalled || q.upsertParams.ChannelType != string(TypeDingTalk) {
		t.Fatalf("upsert not called for dingtalk: %+v", q.upsertParams)
	}

	var cfg installConfig
	if err := json.Unmarshal(q.upsertParams.Config, &cfg); err != nil {
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
	if q.upsertCalled {
		t.Error("missing credentials must be rejected before the upsert")
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
	if q.upsertCalled {
		t.Error("a failed credential validation must not persist an installation")
	}
}

func TestRegisterBYO_AppAlreadyConnected_Rejected(t *testing.T) {
	srv := dingtalkMockServer(t, true)
	defer srv.Close()
	// The pasted robot is already connected to another agent / workspace, so the
	// (channel_type, app_id) routing index rejects the upsert (unique violation).
	// We must refuse, not steal it.
	q := &fakeInstallQueries{
		rowID:      mustUUID(t, "44444444-4444-4444-4444-444444444444"),
		appIDTaken: true,
	}
	svc := newTestInstallService(t, q)
	svc.apiBase = srv.URL

	if _, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	)); err != ErrAppOwnedByAnotherWorkspace {
		t.Fatalf("app already connected = %v, want ErrAppOwnedByAnotherWorkspace", err)
	}
}
