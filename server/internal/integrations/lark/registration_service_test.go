package lark

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// These tests cover the pure-Go halves of RegistrationService —
// constructor validation, session-id security boundary, status code
// mapping — without touching the database. The polling goroutine's
// DB-write paths (UpsertLarkInstallation + BindInstallerTx in one tx)
// require a real Postgres + sqlc-generated *db.Queries and are
// covered by an integration test against the migration suite.

// TestRegistrationServiceConstructorValidatesDeps pins that every
// required dependency surfaces as a constructor error rather than a
// runtime panic inside BeginInstall — a half-init at startup would
// otherwise leave the install button returning 500s with no signal in
// the logs.
func TestRegistrationServiceConstructorValidatesDeps(t *testing.T) {
	client := NewRegistrationClient(RegistrationConfig{})
	api := NewStubAPIClient(nil)
	cases := []struct {
		name   string
		fn     func() error
		needle string
	}{
		{"missing client", func() error {
			_, err := NewRegistrationService(RegistrationServiceConfig{}, nil, api, &db.Queries{}, fakeTxStarter{}, &InstallationService{}, &fakeInstallerBinder{})
			return err
		}, "RegistrationClient"},
		{"missing api", func() error {
			_, err := NewRegistrationService(RegistrationServiceConfig{}, client, nil, &db.Queries{}, fakeTxStarter{}, &InstallationService{}, &fakeInstallerBinder{})
			return err
		}, "APIClient"},
		{"missing queries", func() error {
			_, err := NewRegistrationService(RegistrationServiceConfig{}, client, api, nil, fakeTxStarter{}, &InstallationService{}, &fakeInstallerBinder{})
			return err
		}, "queries"},
		{"missing tx", func() error {
			_, err := NewRegistrationService(RegistrationServiceConfig{}, client, api, &db.Queries{}, nil, &InstallationService{}, &fakeInstallerBinder{})
			return err
		}, "TxStarter"},
		{"missing installs", func() error {
			_, err := NewRegistrationService(RegistrationServiceConfig{}, client, api, &db.Queries{}, fakeTxStarter{}, nil, &fakeInstallerBinder{})
			return err
		}, "InstallationService"},
		{"missing binder", func() error {
			_, err := NewRegistrationService(RegistrationServiceConfig{}, client, api, &db.Queries{}, fakeTxStarter{}, &InstallationService{}, nil)
			return err
		}, "InstallerBinder"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			if err == nil || !strings.Contains(err.Error(), tc.needle) {
				t.Errorf("want error mentioning %q, got %v", tc.needle, err)
			}
		})
	}
}

// TestBotNamePreset pins the bot-name pre-fill format that rides on the
// QR URL: "<agent> - Multica", with a blank agent name degrading to
// plain "Multica" rather than a dangling " - Multica".
func TestBotNamePreset(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Ada", "Ada - Multica"},
		{"  Ada  ", "Ada - Multica"},
		{"产品助手", "产品助手 - Multica"},
		{"", "Multica"},
		{"   ", "Multica"},
	}
	for _, tc := range cases {
		if got := botNamePreset(tc.in); got != tc.want {
			t.Errorf("botNamePreset(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestRegistrationGetSessionNotFound pins both halves of the
// not-found path: unknown session id, and (the security-critical one)
// known session id but from a different workspace. Both must surface
// the same ErrRegistrationSessionNotFound — leaking "exists but wrong
// workspace" would let an attacker enumerate session ids across
// workspaces.
func TestRegistrationGetSessionNotFound(t *testing.T) {
	s := newRegistrationServiceForTest(t)
	ws := uuidFromStringSvc(t, "11111111-1111-1111-1111-111111111111")
	otherWs := uuidFromStringSvc(t, "22222222-2222-2222-2222-222222222222")

	if _, err := s.GetSession(ws, "nope"); !errors.Is(err, ErrRegistrationSessionNotFound) {
		t.Errorf("unknown session: want ErrRegistrationSessionNotFound, got %v", err)
	}

	// Plant a session by hand for the cross-workspace test (BeginInstall
	// requires a live DB; we are only exercising the lookup boundary).
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

// TestRegistrationGetSessionGCsExpiredEntries pins that a session
// whose gcAfter is in the past is dropped on the next lookup, so the
// in-memory map cannot grow unbounded across restarts of long-lived
// servers.
func TestRegistrationGetSessionGCsExpiredEntries(t *testing.T) {
	clock := &fakeClockSvc{now: time.Unix(1_700_000_000, 0)}
	s := newRegistrationServiceForTest(t)
	s.cfg.Now = clock.Now
	ws := uuidFromStringSvc(t, "11111111-1111-1111-1111-111111111111")

	s.mu.Lock()
	s.sessions["expired"] = &registrationSession{
		id:          "expired",
		workspaceID: ws,
		status:      RegistrationStatusError,
		gcAfter:     clock.Now().Add(-1 * time.Minute),
	}
	s.sessions["live"] = &registrationSession{
		id:          "live",
		workspaceID: ws,
		status:      RegistrationStatusSuccess,
		gcAfter:     clock.Now().Add(10 * time.Minute),
	}
	s.mu.Unlock()

	// Lookup of any id triggers gcExpiredLocked — the expired one
	// disappears, the live one stays.
	if _, err := s.GetSession(ws, "live"); err != nil {
		t.Errorf("live session lookup: %v", err)
	}
	if _, err := s.GetSession(ws, "expired"); !errors.Is(err, ErrRegistrationSessionNotFound) {
		t.Errorf("expired session lookup: want not-found, got %v", err)
	}
	s.mu.Lock()
	_, expiredExists := s.sessions["expired"]
	s.mu.Unlock()
	if expiredExists {
		t.Errorf("GC should have dropped the expired session from the map")
	}
}

// TestRegistrationSessionMarkErrorIsIdempotent guards against a
// double-fire race between the expiry timer and a Poll-driven terminal
// error: whichever fires first wins, and the second mark must NOT
// clobber the first reason (the user already saw it).
func TestRegistrationSessionMarkErrorIsIdempotent(t *testing.T) {
	sess := &registrationSession{
		id:     "x",
		status: RegistrationStatusPending,
	}
	deadline := time.Unix(1_700_001_000, 0)
	sess.markError(RegistrationReasonAccessDenied, "user denied", deadline)
	sess.markError(RegistrationReasonExpired, "qr expired", deadline) // second mark — should no-op
	st := sess.snapshot()
	if st.ErrorReason != RegistrationReasonAccessDenied {
		t.Errorf("first reason should win; got %q", st.ErrorReason)
	}
}

// TestRegistrationSessionStateSnapshotIsValueCopy pins that the
// snapshot does not return a pointer alias of the internal session —
// a leaked alias would let the handler's serializer race the polling
// goroutine on field reads. The snapshot is value-copied so the
// caller can read it without holding the session mutex.
func TestRegistrationSessionStateSnapshotIsValueCopy(t *testing.T) {
	sess := &registrationSession{
		id:     "x",
		status: RegistrationStatusPending,
	}
	s1 := sess.snapshot()
	deadline := time.Unix(1_700_001_000, 0)
	sess.markSuccess(uuidFromStringSvc(t, "33333333-3333-3333-3333-333333333333"), deadline)
	if s1.Status != RegistrationStatusPending {
		t.Errorf("snapshot must be a value copy; got mutated to %q", s1.Status)
	}
	s2 := sess.snapshot()
	if s2.Status != RegistrationStatusSuccess {
		t.Errorf("second snapshot should reflect new state; got %q", s2.Status)
	}
}

// TestRandomSessionIDUnique pins the in-process collision risk: 1024
// rounds with no duplicate is enough headroom for the 24-byte input
// (~10^57 keyspace) and matches the bar applied to randomToken.
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

// TestRegistrationServicePublishInstalledEmitsCreatedEvent pins the
// MUL-3059 fix: a completed install must publish lark_installation:created
// at the row-write point so every workspace client refreshes its
// connection badge without a page reload. The bug was that this event only
// fired from the HTTP status-poll handler, so any surface that wasn't the
// polling install dialog stayed stale until a manual refresh. The exact
// shape (type, workspace, system actor, installation_id payload) is what
// the SubscribeAll fanout and the frontend lark_installation-prefix
// invalidation depend on.
func TestRegistrationServicePublishInstalledEmitsCreatedEvent(t *testing.T) {
	bus := events.New()
	var caught []events.Event
	bus.Subscribe(protocol.EventLarkInstallationCreated, func(e events.Event) {
		caught = append(caught, e)
	})

	svc := newRegistrationServiceForTest(t)
	svc.SetEventBus(bus)

	ws := uuidFromStringSvc(t, "11111111-1111-1111-1111-111111111111")
	inst := uuidFromStringSvc(t, "22222222-2222-2222-2222-222222222222")
	svc.publishInstalled(ws, inst)

	// Exactly one — guards against a future re-introduction of the
	// now-removed second emit in the status-poll handler.
	if len(caught) != 1 {
		t.Fatalf("expected exactly 1 lark_installation:created event, got %d", len(caught))
	}
	got := caught[0]
	if got.Type != protocol.EventLarkInstallationCreated {
		t.Errorf("type = %q, want %q", got.Type, protocol.EventLarkInstallationCreated)
	}
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

// TestRegistrationServicePublishInstalledNilBusIsNoOp pins that an install
// still completes when no bus is wired — the bus is optional (SetEventBus
// is never called in self-host builds that disable realtime), so the
// publish must be a silent no-op rather than a nil-deref panic.
func TestRegistrationServicePublishInstalledNilBusIsNoOp(t *testing.T) {
	svc := newRegistrationServiceForTest(t) // no SetEventBus
	svc.publishInstalled(
		uuidFromStringSvc(t, "33333333-3333-3333-3333-333333333333"),
		uuidFromStringSvc(t, "44444444-4444-4444-4444-444444444444"),
	)
}

// fakeInstallerBinder records BindInstallerTx calls for tests that
// need to assert the bind happened.
type fakeInstallerBinder struct {
	mu    sync.Mutex
	calls []InstallerBindParams
	err   error
}

func (f *fakeInstallerBinder) BindInstallerTx(_ context.Context, _ *ChannelStore, p InstallerBindParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, p)
	return f.err
}

// fakeTxStarter is a TxStarter stub for constructor tests — never
// actually called.
type fakeTxStarter struct{}

func (fakeTxStarter) Begin(_ context.Context) (pgx.Tx, error) {
	return nil, errors.New("fakeTxStarter Begin not implemented")
}

// newRegistrationServiceForTest constructs a service with all
// dependencies mocked / nil — the GetSession boundary does not exercise
// the polling goroutine, so the unused deps stay zero.
func newRegistrationServiceForTest(t *testing.T) *RegistrationService {
	t.Helper()
	return &RegistrationService{
		cfg:      RegistrationServiceConfig{}.withDefaults(),
		sessions: make(map[string]*registrationSession),
	}
}

func uuidFromStringSvc(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("scan uuid: %v", err)
	}
	return u
}

type fakeClockSvc struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClockSvc) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
