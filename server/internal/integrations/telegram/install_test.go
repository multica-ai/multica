package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeTelegramInstallQueries struct {
	upsertCalled bool
	upsert       db.UpsertChannelInstallationParams
	rowID        pgtype.UUID
	appIDTaken   bool
	owner        db.GetChannelInstallationOwnerByAppIDRow
	ownerErr     error
	listed       []db.ChannelInstallation
	listParams   db.ListChannelInstallationsByWorkspaceParams
	listErr      error
	got          db.ChannelInstallation
	getParams    db.GetChannelInstallationInWorkspaceParams
	getErr       error
	statusParams db.SetChannelInstallationStatusParams
	statusErr    error
}

func (f *fakeTelegramInstallQueries) WithTx(pgx.Tx) installQueries { return f }
func (f *fakeTelegramInstallQueries) ReclaimDeadChannelInstallationByAppID(context.Context, db.ReclaimDeadChannelInstallationByAppIDParams) (pgtype.UUID, error) {
	return pgtype.UUID{}, pgx.ErrNoRows
}
func (f *fakeTelegramInstallQueries) UpsertChannelInstallation(_ context.Context, p db.UpsertChannelInstallationParams) (db.ChannelInstallation, error) {
	f.upsertCalled, f.upsert = true, p
	if f.appIDTaken {
		return db.ChannelInstallation{}, &pgconn.PgError{Code: pgUniqueViolation}
	}
	return db.ChannelInstallation{ID: f.rowID, WorkspaceID: p.WorkspaceID, AgentID: p.AgentID, ChannelType: p.ChannelType, Config: p.Config, InstallerUserID: p.InstallerUserID, Status: "active"}, nil
}
func (f *fakeTelegramInstallQueries) GetChannelInstallationOwnerByAppID(context.Context, db.GetChannelInstallationOwnerByAppIDParams) (db.GetChannelInstallationOwnerByAppIDRow, error) {
	return f.owner, f.ownerErr
}
func (f *fakeTelegramInstallQueries) ListChannelInstallationsByWorkspace(_ context.Context, p db.ListChannelInstallationsByWorkspaceParams) ([]db.ChannelInstallation, error) {
	f.listParams = p
	return f.listed, f.listErr
}
func (f *fakeTelegramInstallQueries) GetChannelInstallationInWorkspace(_ context.Context, p db.GetChannelInstallationInWorkspaceParams) (db.ChannelInstallation, error) {
	f.getParams = p
	return f.got, f.getErr
}
func (f *fakeTelegramInstallQueries) SetChannelInstallationStatus(_ context.Context, p db.SetChannelInstallationStatusParams) error {
	f.statusParams = p
	return f.statusErr
}

type fakeTelegramTx struct {
	pgx.Tx
	committed bool
}

func (t *fakeTelegramTx) Commit(context.Context) error   { t.committed = true; return nil }
func (t *fakeTelegramTx) Rollback(context.Context) error { return nil }

type fakeTelegramTxStarter struct{ tx *fakeTelegramTx }

func (f fakeTelegramTxStarter) Begin(context.Context) (pgx.Tx, error) { return f.tx, nil }

func telegramInstallTestBox(t *testing.T) *secretbox.Box {
	t.Helper()
	key := make([]byte, secretbox.KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func newTelegramInstallTestService(t *testing.T, q installQueries) *InstallService {
	t.Helper()
	svc, err := newInstallService(q, fakeTelegramTxStarter{tx: &fakeTelegramTx{}}, telegramInstallTestBox(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func telegramInstallAPIServer(t *testing.T, webhookURL string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":12345,"is_bot":true,"first_name":"Multica","username":"multica_test_bot"}}`))
		case strings.HasSuffix(r.URL.Path, "/getWebhookInfo"):
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"url": webhookURL}})
		default:
			t.Errorf("unexpected Telegram method %s", r.URL.Path)
		}
	}))
}

func TestRegisterValidatesAndEncryptsBotToken(t *testing.T) {
	srv := telegramInstallAPIServer(t, "")
	defer srv.Close()
	q := &fakeTelegramInstallQueries{rowID: telegramTestUUID(9)}
	svc := newTelegramInstallTestService(t, q)
	svc.apiBase, svc.httpClient = srv.URL, srv.Client()
	p := RegisterParams{WorkspaceID: telegramTestUUID(1), AgentID: telegramTestUUID(2), InitiatorID: telegramTestUUID(3), BotToken: " 12345:secret "}
	row, err := svc.Register(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if !q.upsertCalled || row.ID != q.rowID || q.upsert.ChannelType != string(TypeTelegram) {
		t.Fatalf("persisted row = %+v params = %+v", row, q.upsert)
	}
	var cfg installConfig
	if err := json.Unmarshal(q.upsert.Config, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.AppID != "12345" || cfg.BotUsername != "multica_test_bot" || strings.Contains(cfg.BotTokenEncrypted, "secret") {
		t.Fatalf("unsafe or incorrect config = %+v", cfg)
	}
	plain, err := decryptToken(cfg.BotTokenEncrypted, svc.box.Open)
	if err != nil || plain != "12345:secret" {
		t.Fatalf("decrypted token = %q, %v", plain, err)
	}
}

func TestRegisterRejectsMalformedTokenBeforeNetwork(t *testing.T) {
	q := &fakeTelegramInstallQueries{}
	svc := newTelegramInstallTestService(t, q)
	for _, token := range []string{"", "12345:", ":secret", "abc:secret"} {
		if _, err := svc.Register(context.Background(), RegisterParams{BotToken: token}); !errors.Is(err, ErrInvalidBotToken) {
			t.Errorf("token %q error = %v", token, err)
		}
	}
	if q.upsertCalled {
		t.Fatal("malformed token reached persistence")
	}
}

func TestRegisterRefusesConfiguredWebhook(t *testing.T) {
	srv := telegramInstallAPIServer(t, "https://other.example/telegram")
	defer srv.Close()
	q := &fakeTelegramInstallQueries{}
	svc := newTelegramInstallTestService(t, q)
	svc.apiBase, svc.httpClient = srv.URL, srv.Client()
	_, err := svc.Register(context.Background(), RegisterParams{BotToken: "12345:secret"})
	if !errors.Is(err, ErrWebhookConfigured) || q.upsertCalled {
		t.Fatalf("error = %v, persisted = %v", err, q.upsertCalled)
	}
}

func TestInstallationManagementStaysWorkspaceAndChannelScoped(t *testing.T) {
	q := &fakeTelegramInstallQueries{
		listed: []db.ChannelInstallation{{ID: telegramTestUUID(9)}},
		got:    db.ChannelInstallation{ID: telegramTestUUID(8)},
	}
	svc := newTelegramInstallTestService(t, q)
	workspaceID := telegramTestUUID(1)
	installationID := telegramTestUUID(2)

	rows, err := svc.ListByWorkspace(context.Background(), workspaceID)
	if err != nil || len(rows) != 1 || rows[0].ID != telegramTestUUID(9) {
		t.Fatalf("ListByWorkspace = %+v, %v", rows, err)
	}
	if q.listParams.WorkspaceID != workspaceID || q.listParams.ChannelType != string(TypeTelegram) {
		t.Fatalf("list params = %+v", q.listParams)
	}

	row, err := svc.GetInWorkspace(context.Background(), installationID, workspaceID)
	if err != nil || row.ID != telegramTestUUID(8) {
		t.Fatalf("GetInWorkspace = %+v, %v", row, err)
	}
	if q.getParams.ID != installationID || q.getParams.WorkspaceID != workspaceID ||
		q.getParams.ChannelType != string(TypeTelegram) {
		t.Fatalf("get params = %+v", q.getParams)
	}

	if err := svc.Revoke(context.Background(), installationID); err != nil {
		t.Fatal(err)
	}
	if q.statusParams.ID != installationID || q.statusParams.Status != "revoked" {
		t.Fatalf("status params = %+v", q.statusParams)
	}
}

func TestGetInstallationMapsMissingRowWithoutLeakingScope(t *testing.T) {
	q := &fakeTelegramInstallQueries{getErr: pgx.ErrNoRows}
	svc := newTelegramInstallTestService(t, q)
	_, err := svc.GetInWorkspace(context.Background(), telegramTestUUID(2), telegramTestUUID(1))
	if !errors.Is(err, ErrInstallationNotFound) {
		t.Fatalf("GetInWorkspace error = %v, want ErrInstallationNotFound", err)
	}
}

func TestLiveBotOwnerConflictIsClassified(t *testing.T) {
	workspaceID := telegramTestUUID(1)
	tests := []struct {
		name  string
		owner db.GetChannelInstallationOwnerByAppIDRow
		err   error
		want  error
	}{
		{
			name:  "same workspace active agent",
			owner: db.GetChannelInstallationOwnerByAppIDRow{WorkspaceID: workspaceID},
			want:  ErrBotOwnedBySameWorkspace,
		},
		{
			name: "same workspace archived agent",
			owner: db.GetChannelInstallationOwnerByAppIDRow{
				WorkspaceID:     workspaceID,
				AgentArchivedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
			},
			want: ErrBotOwnedByArchivedAgent,
		},
		{
			name:  "different workspace",
			owner: db.GetChannelInstallationOwnerByAppIDRow{WorkspaceID: telegramTestUUID(2)},
			want:  ErrBotOwnedByAnotherWorkspace,
		},
		{
			name: "owner lookup failure is privacy preserving",
			err:  errors.New("database unavailable"),
			want: ErrBotOwnedByAnotherWorkspace,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := &fakeTelegramInstallQueries{owner: tc.owner, ownerErr: tc.err}
			svc := newTelegramInstallTestService(t, q)
			if err := svc.liveOwnerConflictErr(context.Background(), workspaceID, "12345"); !errors.Is(err, tc.want) {
				t.Fatalf("liveOwnerConflictErr = %v, want %v", err, tc.want)
			}
		})
	}
}
