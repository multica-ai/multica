package weixin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		pool, err := pgxpool.New(ctx, dsn)
		if err == nil {
			if pool.Ping(ctx) == nil {
				testPool = pool
			} else {
				pool.Close()
			}
		}
		cancel()
	}
	code := m.Run()
	if testPool != nil {
		testPool.Close()
	}
	os.Exit(code)
}

func testBox(t *testing.T) *secretbox.Box {
	t.Helper()
	box, err := secretbox.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func testUUID(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	id, err := util.ParseUUID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type databaseFixture struct {
	f         *dbfx.Fixture
	q         *db.Queries
	svc       *InstallService
	agent     pgtype.UUID
	workspace pgtype.UUID
	user      pgtype.UUID
	status    atomic.Value
	provider  *httptest.Server
	block     func()
}

func newDatabaseFixture(t *testing.T) *databaseFixture {
	t.Helper()
	if testPool == nil {
		t.Skip("DATABASE_URL pointing to a migrated test database is required")
	}
	f := dbfx.New(testPool, "", "")
	suffix := util.UUIDToString(dbid.NewV7())
	f.UserID = f.User(t, "WeChat test", suffix+"@example.test")
	f.WorkspaceID = f.Workspace(t, "WeChat test", "wx-"+suffix)
	f.Member(t, f.WorkspaceID, f.UserID, "admin")
	agent := f.Agent(t, "WeChat test agent", "")
	x := &databaseFixture{f: f, q: db.New(testPool), agent: testUUID(t, agent), workspace: testUUID(t, f.WorkspaceID), user: testUUID(t, f.UserID)}
	x.status.Store(qrStatus{Status: "confirmed", BotID: "bot-" + suffix, UserID: "scanner-" + suffix, Token: "test-token", BaseURL: DefaultBaseURL})
	x.provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ilink/bot/get_bot_qrcode":
			_ = json.NewEncoder(w).Encode(map[string]string{"qrcode": "private-qr", "qrcode_img_content": "https://weixin.qq.com/test-qr"})
		case "/ilink/bot/get_qrcode_status":
			if x.block != nil {
				x.block()
			}
			_ = json.NewEncoder(w).Encode(x.status.Load().(qrStatus))
		default:
			t.Errorf("unexpected provider path %s", r.URL.Path)
		}
	}))
	t.Cleanup(x.provider.Close)
	svc, err := NewInstallService(x.q, testPool, testBox(t))
	if err != nil {
		t.Fatal(err)
	}
	svc.apiBase = x.provider.URL
	x.svc = svc
	f.Cleanup(t, `DELETE FROM channel_installation WHERE workspace_id = $1`, f.WorkspaceID)
	f.Cleanup(t, `DELETE FROM channel_user_binding WHERE workspace_id = $1`, f.WorkspaceID)
	f.Cleanup(t, `DELETE FROM weixin_login_session WHERE workspace_id = $1`, f.WorkspaceID)
	f.Cleanup(t, `DELETE FROM weixin_account_state WHERE installation_id IN (SELECT id FROM channel_installation WHERE workspace_id = $1)`, f.WorkspaceID)
	return x
}

func (x *databaseFixture) begin(t *testing.T) pgtype.UUID {
	t.Helper()
	login, err := x.svc.Begin(context.Background(), x.workspace, x.agent, x.user)
	if err != nil {
		t.Fatal(err)
	}
	return testUUID(t, login.ID)
}

func (x *databaseFixture) connect(t *testing.T) db.ChannelInstallation {
	t.Helper()
	login, err := x.svc.Poll(context.Background(), x.begin(t), x.workspace, x.user, "")
	if err != nil || login.Status != "confirmed" {
		t.Fatalf("login: %v, %v", login.Status, err)
	}
	inst, err := x.q.GetChannelInstallation(context.Background(), db.GetChannelInstallationParams{ID: testUUID(t, login.InstallationID), ChannelType: string(TypeWeixin)})
	if err != nil {
		t.Fatal(err)
	}
	return inst
}

func TestLoginPersistsEncryptedCredentialsAndIdentity(t *testing.T) {
	x := newDatabaseFixture(t)
	id := x.begin(t)
	row, err := x.q.GetWeixinLoginSession(context.Background(), db.GetWeixinLoginSessionParams{ID: id, WorkspaceID: x.workspace, InitiatorUserID: x.user})
	if err != nil || bytes.Contains(row.StateEncrypted, []byte("private-qr")) {
		t.Fatalf("QR state not encrypted: %v", err)
	}
	if _, err := x.svc.Get(context.Background(), id, x.workspace, dbid.NewV7()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another user could read QR: %v", err)
	}
	login, err := x.svc.Poll(context.Background(), id, x.workspace, x.user, "")
	if err != nil || login.Status != "confirmed" || login.QRCodeURL != "" {
		t.Fatalf("confirmation: %v, %v", login.Status, err)
	}
	instID := testUUID(t, login.InstallationID)
	inst, err := x.q.GetChannelInstallation(context.Background(), db.GetChannelInstallationParams{ID: instID, ChannelType: string(TypeWeixin)})
	if err != nil || bytes.Contains(inst.Config, []byte("test-token")) {
		t.Fatalf("token not encrypted: %v", err)
	}
	creds, err := decodeCredentials(inst.Config, x.svc.box)
	if err != nil || creds.Token != "test-token" {
		t.Fatalf("credentials cannot be read: %v", err)
	}
	binding, err := x.q.GetChannelUserBindingByUserID(context.Background(), db.GetChannelUserBindingByUserIDParams{InstallationID: inst.ID, ChannelUserID: creds.UserID})
	if err != nil || binding.MulticaUserID != x.user {
		t.Fatalf("scanner not bound to initiator: %v", err)
	}
	row, err = x.q.GetWeixinLoginSession(context.Background(), db.GetWeixinLoginSessionParams{ID: id, WorkspaceID: x.workspace, InitiatorUserID: x.user})
	if err != nil || len(row.StateEncrypted) != 0 {
		t.Fatalf("confirmed QR retained: %v", err)
	}
}

func TestLoginCancelFencesLateConfirmation(t *testing.T) {
	x := newDatabaseFixture(t)
	id := x.begin(t)
	entered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	x.block = func() { close(entered); <-release }
	result := make(chan error, 1)
	go func() { _, err := x.svc.Poll(context.Background(), id, x.workspace, x.user, ""); result <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("poll never reached provider")
	}
	if _, err := x.svc.Poll(context.Background(), id, x.workspace, x.user, ""); !errors.Is(err, ErrBusy) {
		t.Fatalf("concurrent poll not fenced: %v", err)
	}
	if err := x.svc.Cancel(context.Background(), id, x.workspace, x.user); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-result; !errors.Is(err, ErrNotFound) {
		t.Fatalf("late confirmation: %v", err)
	}
	if count := x.f.Count(t, "SELECT count(*) FROM channel_installation WHERE workspace_id=$1", x.f.WorkspaceID); count != 0 {
		t.Fatal("cancelled login installed bot")
	}
}

func TestLoginExpiryReplacementAndRevokedMembership(t *testing.T) {
	x := newDatabaseFixture(t)
	old, next := x.begin(t), x.begin(t)
	view, err := x.svc.Get(context.Background(), old, x.workspace, x.user)
	if err != nil || view.Status != "cancelled" {
		t.Fatalf("old QR remains live: %v", err)
	}
	x.f.Exec(t, `UPDATE weixin_login_session SET expires_at=now()-interval '1 second' WHERE id=$1`, next)
	view, err = x.svc.Poll(context.Background(), next, x.workspace, x.user, "")
	if err != nil || view.Status != "expired" || view.QRCodeURL != "" {
		t.Fatalf("expired QR: %v", err)
	}
	next = x.begin(t)
	x.f.Exec(t, `UPDATE member SET role='member' WHERE workspace_id=$1 AND user_id=$2`, x.workspace, x.user)
	if _, err := x.svc.Poll(context.Background(), next, x.workspace, x.user, ""); !errors.Is(err, ErrAccess) {
		t.Fatalf("demoted admin installed: %v", err)
	}
}

func TestLoginBotOwnershipAndRevoke(t *testing.T) {
	x := newDatabaseFixture(t)
	inst := x.connect(t)
	y := newDatabaseFixture(t)
	y.status.Store(x.status.Load().(qrStatus))
	if _, err := y.svc.Poll(context.Background(), y.begin(t), y.workspace, y.user, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("bot taken by another workspace: %v", err)
	}
	if err := x.svc.Revoke(context.Background(), inst.ID, y.workspace); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace revoke: %v", err)
	}
	if err := x.svc.Revoke(context.Background(), inst.ID, x.workspace); err != nil {
		t.Fatal(err)
	}
	y.connect(t)
	if count := x.f.Count(t, "SELECT count(*) FROM weixin_account_state WHERE installation_id=$1", inst.ID); count != 0 {
		t.Fatal("revoked context retained")
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func providerClient(server *httptest.Server) *http.Client {
	target, _ := url.Parse(server.URL)
	return &http.Client{Transport: roundTripper(func(req *http.Request) (*http.Response, error) {
		copy := req.Clone(req.Context())
		copy.URL.Scheme, copy.URL.Host = target.Scheme, target.Host
		return server.Client().Transport.RoundTrip(copy)
	})}
}

func TestOutboundDedupUnknownAndRouting(t *testing.T) {
	for _, scenario := range []string{"accepted", "response-lost", "web-origin", "revoked", "rebound", "retry-pending"} {
		t.Run(scenario, func(t *testing.T) {
			x := newDatabaseFixture(t)
			inst := x.connect(t)
			info := DecodePublicConfig(inst.Config)
			ctx := context.Background()
			sealed, _ := x.svc.box.Seal([]byte("reply-context"))
			if _, err := x.q.SaveWeixinContext(ctx, db.SaveWeixinContextParams{InstallationID: inst.ID, BotID: info.BotID, UserID: info.UserID, ContextTokenEncrypted: sealed, ContextTimeMs: 123}); err != nil {
				t.Fatal(err)
			}
			session := x.f.ChatSession(t, util.UUIDToString(x.agent))
			taskID := x.f.Task(t, util.UUIDToString(x.agent), dbfx.Cols{"chat_session_id": session, "status": "completed", "runtime_id": x.f.Runtime(t, "test runtime")})
			x.f.Exec(t, `UPDATE agent_task_queue SET chat_input_task_id=id WHERE id=$1`, taskID)
			x.f.Insert(t, "chat_message", dbfx.Cols{"chat_session_id": session, "role": "user", "content": "hello", "task_id": taskID, "channel_ingested": scenario != "web-origin"})
			bindingID := x.f.Insert(t, "channel_chat_session_binding", dbfx.Cols{"chat_session_id": session, "installation_id": inst.ID, "channel_type": "weixin", "channel_chat_id": info.UserID, "chat_type": "p2p"})
			route, _ := json.Marshal(weixinBindingConfig{BotID: info.BotID, UserID: info.UserID})
			x.f.InsertNoID(t, "channel_task_delivery", dbfx.Cols{"task_id": taskID, "binding_id": bindingID, "installation_id": inst.ID, "channel_type": "weixin", "channel_chat_id": info.UserID, "chat_type": "p2p", "route_revision": 1, "config": route}, "task_id=$1", taskID)
			x.f.Cleanup(t, `DELETE FROM channel_reply_delivery WHERE turn_id=$1`, taskID)
			x.f.Cleanup(t, `DELETE FROM channel_outbound_message WHERE installation_id=$1`, inst.ID)
			if scenario == "revoked" {
				if err := x.svc.Revoke(ctx, inst.ID, x.workspace); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "rebound" {
				x.f.Exec(t, `UPDATE channel_task_delivery SET config=jsonb_set(config, '{bot_id}', '"previous-bot"') WHERE task_id=$1`, taskID)
			}
			var sends atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				var body struct {
					Msg message `json:"msg"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Msg.To != info.UserID || body.Msg.ContextToken != "reply-context" {
					t.Error("wrong reply context")
				}
				if scenario == "response-lost" {
					_, _ = w.Write([]byte("truncated"))
					return
				}
				_, _ = w.Write([]byte(`{"ret":0}`))
			}))
			defer server.Close()
			o := NewOutbound(x.q, x.svc.box, nil)
			o.client = providerClient(server)
			event := events.Event{Type: protocol.EventChatDone, WorkspaceID: x.f.WorkspaceID, ChatSessionID: session, TaskID: taskID, Payload: map[string]any{"content": strings.Repeat("好", 4001), "retry_pending": scenario == "retry-pending"}}
			for i := 0; i < 2; i++ {
				err := o.processEvent(ctx, event)
				if scenario == "accepted" && err != nil {
					t.Fatal(err)
				}
			}
			want := int32(0)
			if scenario == "accepted" {
				want = 2
			}
			if scenario == "response-lost" {
				want = 1
			}
			if sends.Load() != want {
				t.Fatalf("sends=%d, want %d", sends.Load(), want)
			}
		})
	}
}
