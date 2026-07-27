package telegram

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func testBox(t *testing.T) *secretbox.Box {
	t.Helper()
	key := make([]byte, secretbox.KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	return box
}

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}

// fakeInstallQueries mirrors slack/install_test.go's fake so RegisterBYO can be
// exercised without a real DB.
type fakeInstallQueries struct {
	existing     *db.ChannelInstallation
	appIDTaken   bool
	upsertParams db.UpsertChannelInstallationParams
	upsertCalled bool
	rowID        pgtype.UUID

	reclaimedID      *pgtype.UUID
	reclaimCalled    bool
	ownerWorkspaceID pgtype.UUID
	ownerArchived    bool
	ownerMissing     bool

	statusCalled bool
	statusParams db.SetChannelInstallationStatusParams
}

func (f *fakeInstallQueries) WithTx(_ pgx.Tx) installQueries { return f }

func (f *fakeInstallQueries) ReclaimDeadChannelInstallationByAppID(_ context.Context, _ db.ReclaimDeadChannelInstallationByAppIDParams) (pgtype.UUID, error) {
	f.reclaimCalled = true
	if f.reclaimedID != nil {
		return *f.reclaimedID, nil
	}
	return pgtype.UUID{}, pgx.ErrNoRows
}

func (f *fakeInstallQueries) GetChannelInstallationOwnerByAppID(_ context.Context, _ db.GetChannelInstallationOwnerByAppIDParams) (db.GetChannelInstallationOwnerByAppIDRow, error) {
	if f.ownerMissing {
		return db.GetChannelInstallationOwnerByAppIDRow{}, pgx.ErrNoRows
	}
	return db.GetChannelInstallationOwnerByAppIDRow{
		WorkspaceID:     f.ownerWorkspaceID,
		AgentArchivedAt: pgtype.Timestamptz{Valid: f.ownerArchived},
	}, nil
}

func (f *fakeInstallQueries) UpsertChannelInstallation(_ context.Context, arg db.UpsertChannelInstallationParams) (db.ChannelInstallation, error) {
	f.upsertCalled = true
	f.upsertParams = arg
	if f.appIDTaken {
		return db.ChannelInstallation{}, &pgconn.PgError{Code: "23505"}
	}
	id := f.rowID
	if f.existing != nil {
		id = f.existing.ID
	}
	return db.ChannelInstallation{
		ID:              id,
		WorkspaceID:     arg.WorkspaceID,
		AgentID:         arg.AgentID,
		ChannelType:     arg.ChannelType,
		Config:          arg.Config,
		InstallerUserID: arg.InstallerUserID,
		Status:          "active",
	}, nil
}

func (f *fakeInstallQueries) ListChannelInstallationsByWorkspace(_ context.Context, _ db.ListChannelInstallationsByWorkspaceParams) ([]db.ChannelInstallation, error) {
	return nil, nil
}

func (f *fakeInstallQueries) GetChannelInstallationInWorkspace(_ context.Context, _ db.GetChannelInstallationInWorkspaceParams) (db.ChannelInstallation, error) {
	return db.ChannelInstallation{}, nil
}

func (f *fakeInstallQueries) SetChannelInstallationStatus(_ context.Context, arg db.SetChannelInstallationStatusParams) error {
	f.statusCalled = true
	f.statusParams = arg
	return nil
}

// fakeTx is a no-op pgx.Tx: embedding the interface satisfies it, and the
// install path only ever calls Commit / Rollback.
type fakeTx struct {
	pgx.Tx
	committed bool
}

func (t *fakeTx) Commit(context.Context) error   { t.committed = true; return nil }
func (t *fakeTx) Rollback(context.Context) error { return nil }

type fakeTxStarter struct{ tx *fakeTx }

func (f *fakeTxStarter) Begin(context.Context) (pgx.Tx, error) { return f.tx, nil }

func newTestInstallService(t *testing.T, q installQueries) *InstallService {
	t.Helper()
	svc, err := newInstallService(q, &fakeTxStarter{tx: &fakeTx{}}, testBox(t), "https://public.example.test", nil)
	if err != nil {
		t.Fatalf("newInstallService: %v", err)
	}
	return svc
}

// telegramMock parameterizes the install-time Telegram Bot API stub.
type telegramMock struct {
	getMeOK      bool
	username     string
	setWebhookOK bool

	setWebhookCalls []map[string]any
}

func telegramMockServer(t *testing.T, m *telegramMock) *httptest.Server {
	t.Helper()
	if m.username == "" {
		m.username = "acme_bot"
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			if !m.getMeOK {
				w.Write([]byte(`{"ok":false,"description":"Unauthorized"}`))
				return
			}
			w.Write([]byte(`{"ok":true,"result":{"id":123456789,"username":"` + m.username + `"}}`))
		case strings.HasSuffix(r.URL.Path, "/setWebhook"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			m.setWebhookCalls = append(m.setWebhookCalls, body)
			if !m.setWebhookOK {
				w.Write([]byte(`{"ok":false,"description":"bad webhook"}`))
				return
			}
			w.Write([]byte(`{"ok":true,"result":true}`))
		case strings.HasSuffix(r.URL.Path, "/deleteWebhook"):
			w.Write([]byte(`{"ok":true,"result":true}`))
		default:
			w.Write([]byte(`{"ok":false,"description":"unknown method"}`))
		}
	}))
}

func byoParams(ws, agent string) RegisterBYOParams {
	return RegisterBYOParams{
		WorkspaceID: mustUUIDPanic(ws),
		AgentID:     mustUUIDPanic(agent),
		InitiatorID: mustUUIDPanic("33333333-3333-3333-3333-333333333333"),
		BotToken:    "123456789:AAExampleSecretToken",
	}
}

func mustUUIDPanic(s string) pgtype.UUID {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		panic(err)
	}
	return u
}

func TestRegisterBYO_PersistsEncryptedTokenAndRegistersWebhook(t *testing.T) {
	m := &telegramMock{getMeOK: true, setWebhookOK: true, username: "acme_bot"}
	srv := telegramMockServer(t, m)
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
	if !q.upsertCalled || q.upsertParams.ChannelType != string(TypeTelegram) {
		t.Fatalf("upsert not called for telegram: %+v", q.upsertParams)
	}

	var cfg installConfig
	if err := json.Unmarshal(q.upsertParams.Config, &cfg); err != nil {
		t.Fatalf("decode upserted config: %v", err)
	}
	if cfg.AppID != "123456789" {
		t.Errorf("config app_id = %q, want the parsed bot id 123456789", cfg.AppID)
	}
	if cfg.BotUsername != "acme_bot" {
		t.Errorf("config bot_username = %q, want acme_bot", cfg.BotUsername)
	}
	if cfg.BotTokenEncrypted == "" {
		t.Fatal("bot token must be stored")
	}
	if strings.Contains(cfg.BotTokenEncrypted, "123456789:AAExampleSecretToken") {
		t.Error("token must be stored encrypted, not plaintext")
	}
	tok, err := decryptToken(cfg.BotTokenEncrypted, svc.box.Open)
	if err != nil || tok != "123456789:AAExampleSecretToken" {
		t.Errorf("decrypted bot token = %q, %v", tok, err)
	}
	if cfg.WebhookSecret == "" {
		t.Fatal("webhook secret must be generated and stored")
	}

	if len(m.setWebhookCalls) != 1 {
		t.Fatalf("setWebhook calls = %d, want 1", len(m.setWebhookCalls))
	}
	wantURL := "https://public.example.test/api/webhooks/telegram/123456789"
	if got, _ := m.setWebhookCalls[0]["url"].(string); got != wantURL {
		t.Errorf("setWebhook url = %q, want %q", got, wantURL)
	}
	if got, _ := m.setWebhookCalls[0]["secret_token"].(string); got != cfg.WebhookSecret {
		t.Errorf("setWebhook secret_token = %q, want stored secret %q", got, cfg.WebhookSecret)
	}
}

func TestRegisterBYO_InvalidToken(t *testing.T) {
	q := &fakeInstallQueries{}
	svc := newTestInstallService(t, q)

	p := byoParams("11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	p.BotToken = "nocolon"

	if _, err := svc.RegisterBYO(context.Background(), p); err != ErrInvalidBotToken {
		t.Errorf("bad bot token = %v, want ErrInvalidBotToken", err)
	}
	if q.upsertCalled {
		t.Error("a malformed token must be rejected before the upsert")
	}
}

func TestRegisterBYO_GetMeFailure(t *testing.T) {
	m := &telegramMock{getMeOK: false}
	srv := telegramMockServer(t, m)
	defer srv.Close()

	q := &fakeInstallQueries{}
	svc := newTestInstallService(t, q)
	svc.apiBase = srv.URL

	if _, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	)); err == nil {
		t.Fatal("expected an error when getMe rejects the bot token")
	}
	if q.upsertCalled {
		t.Error("a failed getMe must not persist an installation")
	}
	if len(m.setWebhookCalls) != 0 {
		t.Error("setWebhook must not be called when getMe fails")
	}
}

func TestRegisterBYO_SetWebhookFailure_RevokesJustPersistedRow(t *testing.T) {
	m := &telegramMock{getMeOK: true, setWebhookOK: false, username: "acme_bot"}
	srv := telegramMockServer(t, m)
	defer srv.Close()

	q := &fakeInstallQueries{rowID: mustUUID(t, "44444444-4444-4444-4444-444444444444")}
	svc := newTestInstallService(t, q)
	svc.apiBase = srv.URL

	if _, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	)); err == nil {
		t.Fatal("expected an error when setWebhook fails")
	}
	if !q.upsertCalled {
		t.Error("the row must have been persisted before setWebhook was attempted")
	}
	if !q.statusCalled || q.statusParams.Status != "revoked" {
		t.Errorf("expected the just-persisted row to be revoked, statusCalled=%v params=%+v", q.statusCalled, q.statusParams)
	}
}

func TestListByWorkspace_GetInWorkspace_Revoke(t *testing.T) {
	srv := telegramMockServer(t, &telegramMock{})
	defer srv.Close()

	q := &fakeInstallQueries{}
	svc := newTestInstallService(t, q)
	svc.apiBase = srv.URL

	if _, err := svc.ListByWorkspace(context.Background(), mustUUID(t, "11111111-1111-1111-1111-111111111111")); err != nil {
		t.Errorf("ListByWorkspace: %v", err)
	}
	if _, err := svc.GetInWorkspace(context.Background(), mustUUID(t, "44444444-4444-4444-4444-444444444444"), mustUUID(t, "11111111-1111-1111-1111-111111111111")); err != nil {
		t.Errorf("GetInWorkspace: %v", err)
	}

	inst := db.ChannelInstallation{
		ID:          mustUUID(t, "44444444-4444-4444-4444-444444444444"),
		WorkspaceID: mustUUID(t, "11111111-1111-1111-1111-111111111111"),
	}
	sealed, err := svc.box.Seal([]byte("123456789:AAExampleSecretToken"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	cfg, _ := json.Marshal(installConfig{
		AppID:             "123456789",
		BotTokenEncrypted: base64.StdEncoding.EncodeToString(sealed),
	})
	inst.Config = cfg

	if err := svc.Revoke(context.Background(), inst); err != nil {
		t.Errorf("Revoke: %v", err)
	}
	if !q.statusCalled || q.statusParams.Status != "revoked" {
		t.Errorf("Revoke did not flip status: called=%v params=%+v", q.statusCalled, q.statusParams)
	}
}
