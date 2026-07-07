package dingtalk

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// These tests cover the pure-Go halves of RegistrationService —
// constructor validation, session-id security boundary, GC, event
// publication — plus the polling loop's terminal paths that do not
// touch the database (verifier rejection, DingTalk-side FAIL). The
// success path's DB write (UpsertDingTalkInstallation) requires a real
// Postgres and is covered by the migration-suite integration tests.

func newInstallationServiceForTest(t *testing.T) *InstallationService {
	t.Helper()
	box, err := secretbox.New(make([]byte, 32))
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	svc, err := NewInstallationService(&db.Queries{}, &fakeTxStarter{tx: &fakeTx{}}, box)
	if err != nil {
		t.Fatalf("NewInstallationService: %v", err)
	}
	return svc
}

func newRegistrationServiceForTest(t *testing.T) *RegistrationService {
	t.Helper()
	client := NewRegistrationClient(RegistrationConfig{BaseURL: "http://127.0.0.1:0"})
	svc, err := NewRegistrationService(RegistrationServiceConfig{}, client, newInstallationServiceForTest(t), &db.Queries{}, nil)
	if err != nil {
		t.Fatalf("NewRegistrationService: %v", err)
	}
	return svc
}

func uuidFromStringSvc(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("ParseUUID(%q): %v", s, err)
	}
	return u
}

// stubAuthQueries satisfies authQueriesAdapter without a database.
type stubAuthQueries struct{ err error }

func (s stubAuthQueries) GetAgentInWorkspace(context.Context, db.GetAgentInWorkspaceParams) (db.Agent, error) {
	return db.Agent{}, s.err
}

// stubVerifier rejects or accepts every credential pair.
type stubVerifier struct{ err error }

func (s stubVerifier) VerifyAppCredentials(context.Context, string, string) error { return s.err }

func TestRegistrationServiceConstructorValidatesDeps(t *testing.T) {
	client := NewRegistrationClient(RegistrationConfig{})
	installs := newInstallationServiceForTest(t)
	cases := []struct {
		name   string
		fn     func() error
		needle string
	}{
		{"missing client", func() error {
			_, err := NewRegistrationService(RegistrationServiceConfig{}, nil, installs, &db.Queries{}, nil)
			return err
		}, "RegistrationClient"},
		{"missing installs", func() error {
			_, err := NewRegistrationService(RegistrationServiceConfig{}, client, nil, &db.Queries{}, nil)
			return err
		}, "InstallationService"},
		{"missing queries", func() error {
			_, err := NewRegistrationService(RegistrationServiceConfig{}, client, installs, nil, nil)
			return err
		}, "queries"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			if err == nil {
				t.Fatalf("want constructor error containing %q, got nil", tc.needle)
			}
			if !strings.Contains(err.Error(), tc.needle) {
				t.Errorf("error %q does not mention %q", err, tc.needle)
			}
		})
	}
	// nil verifier is explicitly allowed (check skipped).
	if _, err := NewRegistrationService(RegistrationServiceConfig{}, client, installs, &db.Queries{}, nil); err != nil {
		t.Errorf("nil verifier should be accepted: %v", err)
	}
}

func TestRegistrationGetSessionNotFound(t *testing.T) {
	s := newRegistrationServiceForTest(t)
	ws := uuidFromStringSvc(t, "11111111-1111-1111-1111-111111111111")
	otherWs := uuidFromStringSvc(t, "22222222-2222-2222-2222-222222222222")

	if _, err := s.GetSession(ws, "nope"); !errors.Is(err, ErrRegistrationSessionNotFound) {
		t.Errorf("unknown session: want ErrRegistrationSessionNotFound, got %v", err)
	}

	// Plant a session by hand for the cross-workspace test (BeginInstall
	// requires a live registration endpoint; we are only exercising the
	// lookup boundary).
	s.mu.Lock()
	s.sessions["plant-1"] = &registrationSession{
		id:          "plant-1",
		workspaceID: ws,
		status:      RegistrationStatusPending,
	}
	s.mu.Unlock()

	if _, err := s.GetSession(otherWs, "plant-1"); !errors.Is(err, ErrRegistrationSessionNotFound) {
		t.Errorf("cross-workspace lookup: want ErrRegistrationSessionNotFound, got %v", err)
	}

	state, err := s.GetSession(ws, "plant-1")
	if err != nil {
		t.Fatalf("same-workspace lookup: %v", err)
	}
	if state.Status != RegistrationStatusPending {
		t.Errorf("Status: got %q want pending", state.Status)
	}
}

func TestRegistrationGetSessionGCsExpiredEntries(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := newRegistrationServiceForTest(t)
	s.cfg.Now = func() time.Time { return now }
	ws := uuidFromStringSvc(t, "11111111-1111-1111-1111-111111111111")

	s.mu.Lock()
	s.sessions["expired"] = &registrationSession{
		id:          "expired",
		workspaceID: ws,
		status:      RegistrationStatusError,
		gcAfter:     now.Add(-1 * time.Minute),
	}
	s.sessions["live"] = &registrationSession{
		id:          "live",
		workspaceID: ws,
		status:      RegistrationStatusSuccess,
		gcAfter:     now.Add(10 * time.Minute),
	}
	s.mu.Unlock()

	if _, err := s.GetSession(ws, "live"); err != nil {
		t.Errorf("live session lookup: %v", err)
	}
	if _, err := s.GetSession(ws, "expired"); !errors.Is(err, ErrRegistrationSessionNotFound) {
		t.Errorf("expired session lookup: want not-found, got %v", err)
	}
}

func TestRegistrationSessionMarkErrorIsIdempotent(t *testing.T) {
	sess := &registrationSession{id: "x", status: RegistrationStatusPending}
	deadline := time.Unix(1_700_001_000, 0)
	sess.markError(RegistrationReasonInstallFailed, "user denied", deadline)
	sess.markError(RegistrationReasonExpired, "qr expired", deadline) // second mark — should no-op
	st := sess.snapshot()
	if st.ErrorReason != RegistrationReasonInstallFailed {
		t.Errorf("first reason should win; got %q", st.ErrorReason)
	}
}

func TestRandomSessionIDUnique(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 1024; i++ {
		id, err := randomSessionID()
		if err != nil {
			t.Fatalf("randomSessionID: %v", err)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate after %d iterations: %q", i, id)
		}
		seen[id] = struct{}{}
	}
}

func TestRegistrationServicePublishInstalledEmitsCreatedEvent(t *testing.T) {
	bus := events.New()
	var caught []events.Event
	bus.Subscribe(protocol.EventDingTalkInstallationCreated, func(e events.Event) {
		caught = append(caught, e)
	})

	svc := newRegistrationServiceForTest(t)
	svc.SetEventBus(bus)

	ws := uuidFromStringSvc(t, "11111111-1111-1111-1111-111111111111")
	inst := uuidFromStringSvc(t, "22222222-2222-2222-2222-222222222222")
	svc.publishInstalled(ws, inst)

	if len(caught) != 1 {
		t.Fatalf("expected exactly 1 dingtalk_installation:created event, got %d", len(caught))
	}
	got := caught[0]
	if got.WorkspaceID != uuidString(ws) {
		t.Errorf("workspace_id = %q, want %q", got.WorkspaceID, uuidString(ws))
	}
	if got.ActorType != "system" {
		t.Errorf("actor_type = %q, want \"system\"", got.ActorType)
	}
	payload, ok := got.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", got.Payload)
	}
	if payload["installation_id"] != uuidString(inst) {
		t.Errorf("installation_id = %v, want %q", payload["installation_id"], uuidString(inst))
	}
}

func TestRegistrationServicePublishInstalledNilBusIsNoOp(t *testing.T) {
	svc := newRegistrationServiceForTest(t) // no SetEventBus
	svc.publishInstalled(
		uuidFromStringSvc(t, "33333333-3333-3333-3333-333333333333"),
		uuidFromStringSvc(t, "44444444-4444-4444-4444-444444444444"),
	)
}

// beginInstallForTest runs BeginInstall against the supplied fake
// registration server with a stubbed agent-ownership check, returning
// the session id. The fake's begin response advertises a 1s interval so
// the polling loop ticks fast enough for the tests below.
func beginInstallForTest(t *testing.T, f *fakeRegistrationServer, svc *RegistrationService) string {
	t.Helper()
	f.beginBody = map[string]any{
		"errcode": 0, "errmsg": "ok",
		"device_code":               "dc_test",
		"verification_uri_complete": "https://x.example/qr",
		"expires_in":                60,
		"interval":                  1,
	}
	svc.authQueries = stubAuthQueries{}
	res, err := svc.BeginInstall(context.Background(), BeginInstallParams{
		WorkspaceID: uuidFromStringSvc(t, "11111111-1111-1111-1111-111111111111"),
		AgentID:     uuidFromStringSvc(t, "22222222-2222-2222-2222-222222222222"),
		InitiatorID: uuidFromStringSvc(t, "33333333-3333-3333-3333-333333333333"),
	})
	if err != nil {
		t.Fatalf("BeginInstall: %v", err)
	}
	if res.QRCodeURL != "https://x.example/qr" {
		t.Errorf("QRCodeURL = %q", res.QRCodeURL)
	}
	return res.SessionID
}

// waitForTerminal polls GetSession until the session leaves pending or
// the timeout elapses.
func waitForTerminal(t *testing.T, svc *RegistrationService, ws pgtype.UUID, sessionID string) RegistrationSessionState {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		state, err := svc.GetSession(ws, sessionID)
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if state.Status != RegistrationStatusPending {
			return state
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("session %s never left pending", sessionID)
	return RegistrationSessionState{}
}

// TestRegistrationPollingVerifierRejectionEndsSession drives the full
// begin → poll(SUCCESS) → verifier-rejects path over a fake HTTP server,
// pinning that bad credentials surface as credentials_check_failed and
// never reach the DB write (the nil *db.Queries would panic if they did).
func TestRegistrationPollingVerifierRejectionEndsSession(t *testing.T) {
	f, client := newFakeRegistration(t)
	f.pollBody = map[string]any{
		"errcode": 0, "errmsg": "ok", "status": "SUCCESS",
		"client_id": "dingabc", "client_secret": "s3cret",
	}
	svc, err := NewRegistrationService(
		RegistrationServiceConfig{},
		client,
		newInstallationServiceForTest(t),
		&db.Queries{},
		stubVerifier{err: errors.New("token exchange refused")},
	)
	if err != nil {
		t.Fatalf("NewRegistrationService: %v", err)
	}
	ws := uuidFromStringSvc(t, "11111111-1111-1111-1111-111111111111")
	sessionID := beginInstallForTest(t, f, svc)

	state := waitForTerminal(t, svc, ws, sessionID)
	if state.Status != RegistrationStatusError {
		t.Fatalf("status = %q, want error", state.Status)
	}
	if state.ErrorReason != RegistrationReasonCredentialsCheckFailed {
		t.Errorf("reason = %q, want %q", state.ErrorReason, RegistrationReasonCredentialsCheckFailed)
	}
}

// TestRegistrationPollingFailEndsSession drives begin → poll(FAIL) and
// pins the install_failed reason with the DingTalk fail_reason retained
// in the message for diagnostics.
func TestRegistrationPollingFailEndsSession(t *testing.T) {
	f, client := newFakeRegistration(t)
	f.pollBody = map[string]any{
		"errcode": 0, "errmsg": "ok", "status": "FAIL",
		"fail_reason": "用户拒绝授权",
	}
	svc, err := NewRegistrationService(
		RegistrationServiceConfig{},
		client,
		newInstallationServiceForTest(t),
		&db.Queries{},
		nil,
	)
	if err != nil {
		t.Fatalf("NewRegistrationService: %v", err)
	}
	ws := uuidFromStringSvc(t, "11111111-1111-1111-1111-111111111111")
	sessionID := beginInstallForTest(t, f, svc)

	state := waitForTerminal(t, svc, ws, sessionID)
	if state.Status != RegistrationStatusError {
		t.Fatalf("status = %q, want error", state.Status)
	}
	if state.ErrorReason != RegistrationReasonInstallFailed {
		t.Errorf("reason = %q, want %q", state.ErrorReason, RegistrationReasonInstallFailed)
	}
	if !strings.Contains(state.ErrorMessage, "用户拒绝授权") {
		t.Errorf("message %q should retain fail_reason", state.ErrorMessage)
	}
}
