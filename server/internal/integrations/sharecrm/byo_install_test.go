package sharecrm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// sharecrmMockServer stubs the auth/token call RegisterBYO makes.
func sharecrmMockServer(t *testing.T, tokenOK bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.Contains(r.URL.Path, "auth/token") && !strings.HasSuffix(r.URL.Path, "/token") {
			// Accept both /im-gateway/auth/token and bare path variants.
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusNotFound)
				return
			}
		}
		if !tokenOK {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":400,"msg":"invalid credentials"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"accessToken":"tok-abc","expireIn":7200,"expiresIn":7200}}`))
	}))
}

func byoParams(ws, agent, appID string) RegisterBYOParams {
	return RegisterBYOParams{
		WorkspaceID: pgtypeUUID(ws),
		AgentID:     pgtypeUUID(agent),
		InitiatorID: pgtypeUUID("33333333-3333-3333-3333-333333333333"),
		AppID:       appID,
		AppSecret:   "sharecrm-app-secret-xyz",
	}
}

func pgtypeUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		panic(err)
	}
	return u
}

func TestRegisterBYO_PersistsEncryptedSecretKeyedByAppID(t *testing.T) {
	srv := sharecrmMockServer(t, true)
	defer srv.Close()

	q := &fakeInstallQueries{rowID: mustUUID(t, "44444444-4444-4444-4444-444444444444")}
	svc := newTestInstallService(t, q)
	svc.httpClient = srv.Client()

	// Point validation at the mock by overriding the base URL on the request.
	p := byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
		"sharecrm-app-id-xyz",
	)
	p.GatewayBaseURL = srv.URL

	row, err := svc.RegisterBYO(context.Background(), p)
	if err != nil {
		t.Fatalf("RegisterBYO: %v", err)
	}
	if row.ID != q.rowID {
		t.Errorf("row id = %v, want %v", row.ID, q.rowID)
	}
	if !q.upsertCalled || q.upsertParams.ChannelType != string(TypeShareCRM) {
		t.Fatalf("upsert not called for sharecrm: %+v", q.upsertParams)
	}
	if !q.lockCalled {
		t.Error("install must lock the owner slot")
	}
	if !q.reclaimCalled {
		t.Error("install must run the dead-owner reclaim before the upsert")
	}

	var cfg installConfig
	if err := json.Unmarshal(q.upsertParams.Config, &cfg); err != nil {
		t.Fatalf("decode upserted config: %v", err)
	}
	if cfg.AppID != "sharecrm-app-id-xyz" {
		t.Fatalf("config app_id = %q", cfg.AppID)
	}
	if cfg.AppSecretEncrypted == "" {
		t.Fatalf("app secret must be stored: %+v", cfg)
	}
	if strings.Contains(cfg.AppSecretEncrypted, "sharecrm-app-secret-xyz") {
		t.Error("app secret must be stored encrypted, not plaintext")
	}
	secret, err := decryptToken(cfg.AppSecretEncrypted, svc.box.Open)
	if err != nil || secret != "sharecrm-app-secret-xyz" {
		t.Fatalf("decrypted app secret = %q, %v", secret, err)
	}
}

func TestRegisterBYO_SameAgentReconnect_UpdatesRowInPlace(t *testing.T) {
	srv := sharecrmMockServer(t, true)
	defer srv.Close()
	existing := &db.ChannelInstallation{
		ID:          mustUUID(t, "44444444-4444-4444-4444-444444444444"),
		WorkspaceID: mustUUID(t, "11111111-1111-1111-1111-111111111111"),
		AgentID:     mustUUID(t, "22222222-2222-2222-2222-222222222222"),
	}
	q := &fakeInstallQueries{existing: existing, existingAppID: "sharecrm-app-id-xyz"}
	svc := newTestInstallService(t, q)
	svc.httpClient = srv.Client()

	p := byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
		"sharecrm-app-id-xyz",
	)
	p.GatewayBaseURL = srv.URL

	row, err := svc.RegisterBYO(context.Background(), p)
	if err != nil {
		t.Fatalf("RegisterBYO same-agent reconnect: %v", err)
	}
	if row.ID != existing.ID {
		t.Fatalf("reconnect row id = %v, want in-place %v", row.ID, existing.ID)
	}
	if q.replaceCalled {
		t.Fatal("same-AppID reconnect retired the installation identity")
	}
}

// Replacing an agent's robot with a DIFFERENT App ID must establish a fresh
// installation identity. Keeping the old installation_id would carry
// prior user bindings and chat sessions into the new bot.
func TestRegisterBYO_DifferentAppID_ReplacesInstallationIdentity(t *testing.T) {
	srv := sharecrmMockServer(t, true)
	defer srv.Close()
	oldID := mustUUID(t, "44444444-4444-4444-4444-444444444444")
	newID := mustUUID(t, "55555555-5555-5555-5555-555555555555")
	existing := &db.ChannelInstallation{
		ID:          oldID,
		WorkspaceID: mustUUID(t, "11111111-1111-1111-1111-111111111111"),
		AgentID:     mustUUID(t, "22222222-2222-2222-2222-222222222222"),
	}
	q := &fakeInstallQueries{
		existing:      existing,
		existingAppID: "sharecrm-old-app-id",
		rowID:         newID,
	}
	svc := newTestInstallService(t, q)
	svc.httpClient = srv.Client()

	p := byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
		"sharecrm-new-app-id",
	)
	p.GatewayBaseURL = srv.URL

	row, err := svc.RegisterBYO(context.Background(), p)
	if err != nil {
		t.Fatalf("RegisterBYO different-AppID replacement: %v", err)
	}
	if !q.lockCalled {
		t.Fatal("replacement decision was not serialized")
	}
	if !q.replaceCalled || q.replaceParams.InstallationID != oldID {
		t.Fatalf("retired installation = (%v, %+v), want old id %v", q.replaceCalled, q.replaceParams, oldID)
	}
	if row.ID != newID || row.ID == oldID {
		t.Fatalf("replacement row id = %v, want fresh %v (old %v)", row.ID, newID, oldID)
	}
}

func TestRegisterBYO_MissingCredentials(t *testing.T) {
	q := &fakeInstallQueries{}
	svc := newTestInstallService(t, q)

	p := byoParams("11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "")
	if _, err := svc.RegisterBYO(context.Background(), p); err != ErrInvalidAppID {
		t.Fatalf("empty app id = %v, want ErrInvalidAppID", err)
	}
	p = byoParams("11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "app")
	p.AppSecret = ""
	if _, err := svc.RegisterBYO(context.Background(), p); err != ErrInvalidAppSecret {
		t.Fatalf("empty app secret = %v, want ErrInvalidAppSecret", err)
	}
}
