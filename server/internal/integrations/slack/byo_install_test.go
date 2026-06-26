package slack

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

// authTestServer stubs Slack auth.test. ok=false drives the bad-token path.
func authTestServer(t *testing.T, ok bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"team_id":"T999","user_id":"UBOTBYO","team":"Acme Inc","url":"https://acme.slack.com/"}`))
	}))
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
	// A BYO paste has no authed_user, so the installer is NOT auto-bound, and a
	// fresh install retires nothing.
	if q.bindCalled {
		t.Error("BYO must not auto-bind an installer (no authed_user from a paste)")
	}
	if q.deleteCalled {
		t.Error("a fresh install must not retire chat-session bindings")
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

func TestRegisterBYO_CrossWorkspace_Rejected(t *testing.T) {
	srv := authTestServer(t, true)
	defer srv.Close()
	// This app id is already owned by workspace W1; registering it under W2 must
	// be refused by the upsert's atomic guard.
	q := &fakeInstallQueries{
		rowID: mustUUID(t, "44444444-4444-4444-4444-444444444444"),
		existing: &db.ChannelInstallation{
			WorkspaceID: mustUUID(t, "11111111-1111-1111-1111-111111111111"), // W1
			AgentID:     mustUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		},
	}
	svc := newTestInstallService(t, q)
	svc.apiURL = srv.URL + "/"

	if _, err := svc.RegisterBYO(context.Background(), byoParams(
		"99999999-9999-9999-9999-999999999999", // W2
		"22222222-2222-2222-2222-222222222222",
	)); err != ErrTeamOwnedByAnotherWorkspace {
		t.Fatalf("cross-workspace BYO = %v, want ErrTeamOwnedByAnotherWorkspace", err)
	}
}

func TestRegisterBYO_AgentMove_RetiresStaleSessionBindings(t *testing.T) {
	srv := authTestServer(t, true)
	defer srv.Close()
	// Same app already installed for agent A in this workspace; re-registering it
	// for agent B must retire the stale chat-session bindings.
	q := &fakeInstallQueries{
		rowID: mustUUID(t, "44444444-4444-4444-4444-444444444444"),
		existing: &db.ChannelInstallation{
			WorkspaceID: mustUUID(t, "11111111-1111-1111-1111-111111111111"),
			AgentID:     mustUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		},
	}
	svc := newTestInstallService(t, q)
	svc.apiURL = srv.URL + "/"

	if _, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222", // agent B
	)); err != nil {
		t.Fatalf("RegisterBYO: %v", err)
	}
	if !q.deleteCalled {
		t.Fatal("an agent change must retire the installation's chat-session bindings")
	}
}
