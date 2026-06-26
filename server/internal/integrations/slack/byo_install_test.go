package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// slackMock parameterizes the install-time Slack API stub. botAppID defaults to
// the app id embedded in byoParams' xapp token (so the same-app check passes).
type slackMock struct {
	authOK     bool   // auth.test result
	botAppID   string // bots.info -> bot.app_id
	appTokenOK bool   // apps.connections.open result
}

// slackMockServer stubs the three Web API calls RegisterBYO makes: auth.test
// (bot token), bots.info (bot id -> owning app id), apps.connections.open (app
// token live check).
func slackMockServer(t *testing.T, m slackMock) *httptest.Server {
	t.Helper()
	if m.botAppID == "" {
		m.botAppID = "A0BCXGVCS7R"
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth.test":
			if !m.authOK {
				_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
				return
			}
			_, _ = w.Write([]byte(`{"ok":true,"team_id":"T999","user_id":"UBOTBYO","bot_id":"B0BOT","team":"Acme Inc","url":"https://acme.slack.com/"}`))
		case "/bots.info":
			_, _ = w.Write([]byte(fmt.Sprintf(`{"ok":true,"bot":{"id":"B0BOT","app_id":%q,"user_id":"UBOTBYO"}}`, m.botAppID)))
		case "/apps.connections.open":
			if !m.appTokenOK {
				_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
				return
			}
			_, _ = w.Write([]byte(`{"ok":true,"url":"wss://example.test/link"}`))
		default:
			_, _ = w.Write([]byte(`{"ok":false,"error":"unknown_method"}`))
		}
	}))
}

// authTestServer is the happy-path stub (valid bot token, matching app id, live
// app token) unless ok=false, which makes auth.test reject the bot token.
func authTestServer(t *testing.T, ok bool) *httptest.Server {
	return slackMockServer(t, slackMock{authOK: ok, appTokenOK: true})
}

func byoParams(ws, agent string) RegisterBYOParams {
	return RegisterBYOParams{
		WorkspaceID: pgtypeUUID(ws),
		AgentID:     pgtypeUUID(agent),
		InitiatorID: pgtypeUUID("33333333-3333-3333-3333-333333333333"),
		BotToken:    "xoxb-real-bot-token",
		AppToken:    "xapp-1-A0BCXGVCS7R-111-appsecret",
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

func TestParseSlackAppID(t *testing.T) {
	cases := []struct {
		token   string
		want    string
		wantErr bool
	}{
		{"xapp-1-A0BCXGVCS7R-111-secret", "A0BCXGVCS7R", false},
		{"xapp-1-A12345-9-abc", "A12345", false},
		{"xoxb-not-an-app-token", "", true},
		{"xapp-1-", "", true},
		{"xapp-1-B123-9-abc", "", true}, // app ids start with A
		{"", "", true},
	}
	for _, c := range cases {
		got, err := parseSlackAppID(c.token)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseSlackAppID(%q) = %q, want error", c.token, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("parseSlackAppID(%q) = %q, %v; want %q", c.token, got, err, c.want)
		}
	}
}

func TestRegisterBYO_PersistsEncryptedTokensKeyedByAppID(t *testing.T) {
	srv := authTestServer(t, true)
	defer srv.Close()

	q := &fakeInstallQueries{rowID: mustUUID(t, "44444444-4444-4444-4444-444444444444")}
	svc := newTestInstallService(t, q) // BYO needs NO OAuth creds
	svc.apiURL = srv.URL + "/"

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
	if !q.upsertCalled || q.upsertParams.ChannelType != string(TypeSlack) {
		t.Fatalf("upsert not called for slack: %+v", q.upsertParams)
	}

	var cfg installConfig
	if err := json.Unmarshal(q.upsertParams.Config, &cfg); err != nil {
		t.Fatalf("decode upserted config: %v", err)
	}
	// Keyed by the REAL app id (parsed from the xapp token), NOT the team id —
	// this is what lets several BYO apps share one Slack workspace.
	if cfg.AppID != "A0BCXGVCS7R" {
		t.Errorf("config app_id = %q, want the real app id A0BCXGVCS7R", cfg.AppID)
	}
	if cfg.TeamID != "T999" || cfg.BotUserID != "UBOTBYO" {
		t.Errorf("config team/bot = %q/%q, want T999/UBOTBYO", cfg.TeamID, cfg.BotUserID)
	}
	// Both tokens stored encrypted (never plaintext) and both decrypt back.
	if cfg.BotTokenEncrypted == "" || cfg.AppTokenEncrypted == "" {
		t.Fatalf("both tokens must be stored: %+v", cfg)
	}
	if strings.Contains(cfg.BotTokenEncrypted, "xoxb-") || strings.Contains(cfg.AppTokenEncrypted, "xapp-") {
		t.Error("tokens must be stored encrypted, not plaintext")
	}
	botTok, err := decryptToken(cfg.BotTokenEncrypted, svc.box.Open)
	if err != nil || botTok != "xoxb-real-bot-token" {
		t.Errorf("decrypted bot token = %q, %v", botTok, err)
	}
	appTok, err := decryptToken(cfg.AppTokenEncrypted, svc.box.Open)
	if err != nil || appTok != "xapp-1-A0BCXGVCS7R-111-appsecret" {
		t.Errorf("decrypted app token = %q, %v", appTok, err)
	}
}

func TestRegisterBYO_InvalidTokens(t *testing.T) {
	q := &fakeInstallQueries{}
	svc := newTestInstallService(t, q)

	// Bad bot token prefix — rejected before any network call or upsert.
	p := byoParams("11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	p.BotToken = "nope-not-a-bot-token"
	if _, err := svc.RegisterBYO(context.Background(), p); err != ErrInvalidBotToken {
		t.Errorf("bad bot token = %v, want ErrInvalidBotToken", err)
	}
	// Bad app token.
	p = byoParams("11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222")
	p.AppToken = "xapp-broken"
	if _, err := svc.RegisterBYO(context.Background(), p); err != ErrInvalidAppToken {
		t.Errorf("bad app token = %v, want ErrInvalidAppToken", err)
	}
	if q.upsertCalled {
		t.Error("malformed tokens must be rejected before the upsert")
	}
}

func TestRegisterBYO_AuthTestFailure(t *testing.T) {
	srv := authTestServer(t, false) // Slack rejects the bot token
	defer srv.Close()
	q := &fakeInstallQueries{}
	svc := newTestInstallService(t, q)
	svc.apiURL = srv.URL + "/"

	if _, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	)); err == nil {
		t.Fatal("expected an error when auth.test rejects the bot token")
	}
	if q.upsertCalled {
		t.Error("a failed auth.test must not persist an installation")
	}
}

func TestRegisterBYO_AppAlreadyConnected_Rejected(t *testing.T) {
	srv := authTestServer(t, true)
	defer srv.Close()
	// The pasted app is already connected to another agent / workspace, so the
	// (channel_type, app_id) routing index rejects the upsert (unique violation).
	// We must refuse, not steal it.
	q := &fakeInstallQueries{
		rowID:      mustUUID(t, "44444444-4444-4444-4444-444444444444"),
		appIDTaken: true,
	}
	svc := newTestInstallService(t, q)
	svc.apiURL = srv.URL + "/"

	if _, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	)); err != ErrTeamOwnedByAnotherWorkspace {
		t.Fatalf("app already connected = %v, want ErrTeamOwnedByAnotherWorkspace", err)
	}
}

func TestRegisterBYO_ReconnectSameAgent_UpdatesRowInPlace(t *testing.T) {
	srv := authTestServer(t, true)
	defer srv.Close()
	// The agent already has a Slack row (e.g. a previously-disconnected app).
	// Re-connecting it — even with a NEW app — must UPDATE that same row in place
	// (keyed by workspace+agent), not error on the (workspace, agent, channel)
	// unique. The fake returns the existing row id on the upsert.
	existingID := mustUUID(t, "55555555-5555-5555-5555-555555555555")
	q := &fakeInstallQueries{
		rowID: mustUUID(t, "44444444-4444-4444-4444-444444444444"),
		existing: &db.ChannelInstallation{
			ID:          existingID,
			WorkspaceID: mustUUID(t, "11111111-1111-1111-1111-111111111111"),
			AgentID:     mustUUID(t, "22222222-2222-2222-2222-222222222222"),
		},
	}
	svc := newTestInstallService(t, q)
	svc.apiURL = srv.URL + "/"

	row, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	))
	if err != nil {
		t.Fatalf("RegisterBYO: %v", err)
	}
	if row.ID != existingID {
		t.Errorf("reconnect should reuse the agent's existing row %v, got %v", existingID, row.ID)
	}
}

func TestRegisterBYO_TokenAppMismatch(t *testing.T) {
	// The bot token belongs to a DIFFERENT app (bots.info -> A0OTHER) than the
	// app id embedded in the xapp token (A0BCXGVCS7R) — must be rejected so we
	// never persist a broken installation (Niko review).
	srv := slackMockServer(t, slackMock{authOK: true, botAppID: "A0OTHERAPP", appTokenOK: true})
	defer srv.Close()
	q := &fakeInstallQueries{}
	svc := newTestInstallService(t, q)
	svc.apiURL = srv.URL + "/"

	if _, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	)); err != ErrTokenAppMismatch {
		t.Fatalf("mismatched tokens = %v, want ErrTokenAppMismatch", err)
	}
	if q.upsertCalled {
		t.Error("mismatched bot/app tokens must be rejected before the upsert")
	}
}

func TestRegisterBYO_AppTokenNotLive(t *testing.T) {
	// auth.test + same-app check pass, but apps.connections.open rejects the app
	// token — we must not persist a token that will never receive events.
	srv := slackMockServer(t, slackMock{authOK: true, appTokenOK: false})
	defer srv.Close()
	q := &fakeInstallQueries{}
	svc := newTestInstallService(t, q)
	svc.apiURL = srv.URL + "/"

	if _, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	)); err == nil {
		t.Fatal("expected an error when the app-level token is not live")
	}
	if q.upsertCalled {
		t.Error("an invalid app token must not persist an installation")
	}
}
