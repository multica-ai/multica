package dingtalk

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

// reclaimOwnerRow builds a GetChannelInstallationReclaimByAppIDRow for the robot's
// current app_id owner, so tests can drive each reclaim branch.
func reclaimOwnerRow(t *testing.T, ws, agent, status string, exists bool) *db.GetChannelInstallationReclaimByAppIDRow {
	t.Helper()
	return &db.GetChannelInstallationReclaimByAppIDRow{
		ID:          mustUUID(t, "99999999-9999-9999-9999-999999999999"),
		WorkspaceID: mustUUID(t, ws),
		AgentID:     mustUUID(t, agent),
		Status:      status,
		AgentExists: exists,
	}
}

func TestRegisterBYO_AppOwnedByLiveAgent_Rejected(t *testing.T) {
	srv := dingtalkMockServer(t, true)
	defer srv.Close()
	// The pasted robot is already connected to a LIVE agent (present agent,
	// active installation — archived or not) — a genuine conflict. Refuse, don't
	// steal it.
	q := &fakeInstallQueries{
		rowID: mustUUID(t, "44444444-4444-4444-4444-444444444444"),
		reclaimOwner: reclaimOwnerRow(t,
			"55555555-5555-5555-5555-555555555555",
			"66666666-6666-6666-6666-666666666666",
			"active", true),
	}
	svc := newTestInstallService(t, q)
	svc.apiBase = srv.URL

	if _, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	)); err != ErrAppOwnedByAnotherAgent {
		t.Fatalf("app owned by live agent = %v, want ErrAppOwnedByAnotherAgent", err)
	}
	if q.deleteCalled {
		t.Error("a live owner must never be deleted/reclaimed")
	}
	if q.upsertCalled {
		t.Error("a live owner conflict must be refused before the upsert")
	}
}

// A DEAD prior binding — orphaned (deleted workspace/agent), or revoked in the
// SAME workspace — is reclaimed so the robot can move to the new agent. An
// archived agent, or a revoked binding owned by ANOTHER workspace, is NOT dead
// (see TestRegisterBYO_AppOwnedByLiveAgent_Rejected and
// TestRegisterBYO_CrossWorkspaceRevoked_Refused) and is refused as LIVE.
func TestRegisterBYO_ReclaimsDeadBinding(t *testing.T) {
	deadOwners := map[string]*db.GetChannelInstallationReclaimByAppIDRow{
		"installation revoked in same workspace": reclaimOwnerRow(t,
			"11111111-1111-1111-1111-111111111111", // same workspace as byoParams
			"88888888-8888-8888-8888-888888888888",
			"revoked", true),
		"orphaned by deleted workspace/agent": reclaimOwnerRow(t,
			"77777777-7777-7777-7777-777777777777", // deleted workspace
			"88888888-8888-8888-8888-888888888888",
			"active", false), // agent row gone
	}
	for name, owner := range deadOwners {
		t.Run(name, func(t *testing.T) {
			srv := dingtalkMockServer(t, true)
			defer srv.Close()
			q := &fakeInstallQueries{
				rowID:        mustUUID(t, "44444444-4444-4444-4444-444444444444"),
				appIDTaken:   true, // routing key is taken until the dead row is deleted
				reclaimOwner: owner,
			}
			svc := newTestInstallService(t, q)
			svc.apiBase = srv.URL

			row, err := svc.RegisterBYO(context.Background(), byoParams(
				"11111111-1111-1111-1111-111111111111",
				"22222222-2222-2222-2222-222222222222",
			))
			if err != nil {
				t.Fatalf("RegisterBYO after reclaim: %v", err)
			}
			if !q.deleteCalled || q.deletedID != owner.ID {
				t.Fatalf("dead binding not reclaimed: deleteCalled=%v deletedID=%v", q.deleteCalled, q.deletedID)
			}
			if !q.upsertCalled {
				t.Error("upsert must run after reclaiming the dead binding")
			}
			if row.ID != q.rowID {
				t.Errorf("row id = %v, want %v", row.ID, q.rowID)
			}
		})
	}
}

// A revoked binding owned by ANOTHER workspace is recoverable data (Revoke
// preserves the row so that workspace can re-install and restore its member and
// session bindings), so a different workspace pasting the same robot is refused
// with a 409 rather than silently hard-deleting the owner's install.
func TestRegisterBYO_CrossWorkspaceRevoked_Refused(t *testing.T) {
	srv := dingtalkMockServer(t, true)
	defer srv.Close()
	q := &fakeInstallQueries{
		rowID: mustUUID(t, "44444444-4444-4444-4444-444444444444"),
		reclaimOwner: reclaimOwnerRow(t,
			"55555555-5555-5555-5555-555555555555", // a DIFFERENT workspace
			"66666666-6666-6666-6666-666666666666",
			"revoked", true),
	}
	svc := newTestInstallService(t, q)
	svc.apiBase = srv.URL

	if _, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	)); err != ErrAppOwnedByAnotherAgent {
		t.Fatalf("cross-workspace revoked = %v, want ErrAppOwnedByAnotherAgent", err)
	}
	if q.deleteCalled {
		t.Error("another workspace's revoked install must never be hard-deleted")
	}
	if q.upsertCalled {
		t.Error("a cross-workspace revoked conflict must be refused before the upsert")
	}
}

// Re-connecting the SAME (workspace, agent) is an in-place update, never a
// reclaim — the same-target check precedes the dead/live decision, so an
// archived agent can still re-connect its own robot.
func TestRegisterBYO_SameAgentReconnect_NotReclaimed(t *testing.T) {
	srv := dingtalkMockServer(t, true)
	defer srv.Close()
	existing := &db.ChannelInstallation{ID: mustUUID(t, "44444444-4444-4444-4444-444444444444")}
	q := &fakeInstallQueries{
		existing: existing,
		reclaimOwner: reclaimOwnerRow(t,
			"11111111-1111-1111-1111-111111111111", // same workspace
			"22222222-2222-2222-2222-222222222222", // same agent as byoParams
			"active", true),
	}
	svc := newTestInstallService(t, q)
	svc.apiBase = srv.URL

	row, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	))
	if err != nil {
		t.Fatalf("RegisterBYO same-agent reconnect: %v", err)
	}
	if q.deleteCalled {
		t.Error("same-agent reconnect must not delete its own row")
	}
	if row.ID != existing.ID {
		t.Errorf("reconnect row id = %v, want in-place %v", row.ID, existing.ID)
	}
}

// A LIVE owner can race in between the reclaim probe (which sees the app_id as
// free) and the upsert, so the (channel_type, app_id) routing index is the
// last-resort guard: its unique violation must map to ErrAppOwnedByAnotherAgent
// (HTTP 409), not surface as an opaque 500.
func TestRegisterBYO_UpsertRace_MappedToConflict(t *testing.T) {
	srv := dingtalkMockServer(t, true)
	defer srv.Close()
	// reclaimOwner nil => the probe reports the app_id free (no dead row to
	// reclaim), but appIDTaken makes the upsert trip the routing unique index,
	// mimicking a concurrent registrant that won the race.
	q := &fakeInstallQueries{
		rowID:      mustUUID(t, "44444444-4444-4444-4444-444444444444"),
		appIDTaken: true,
	}
	svc := newTestInstallService(t, q)
	svc.apiBase = srv.URL

	if _, err := svc.RegisterBYO(context.Background(), byoParams(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	)); err != ErrAppOwnedByAnotherAgent {
		t.Fatalf("upsert race = %v, want ErrAppOwnedByAnotherAgent", err)
	}
	if q.deleteCalled {
		t.Error("a free app_id has no dead row to reclaim/delete")
	}
	if !q.upsertCalled {
		t.Error("the upsert must run and trip the routing unique index")
	}
}
