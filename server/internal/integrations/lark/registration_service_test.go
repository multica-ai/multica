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

// The service's own MemoryInstallSessionStore stands in for Redis here —
// it implements the same InstallSessionStore contract, so these tests
// exercise the real production type rather than a bespoke fake. The
// behaviour that only Redis can prove (cross-client sharing, TTL, SetNX
// first-writer-wins) is covered in install_session_store_test.go against
// a real server.

// plantSession registers a pending session and returns the in-memory
// handle the polling goroutine would have held.
func plantSession(t *testing.T, s *RegistrationService, id string, ws pgtype.UUID, expiresAt time.Time) *registrationSession {
	t.Helper()
	if err := s.sessionStore.Create(context.Background(), InstallSessionState{
		ID:          id,
		WorkspaceID: ws,
		InitiatorID: ws,
		Status:      RegistrationStatusPending,
		ExpiresAt:   expiresAt,
	}, time.Hour); err != nil {
		t.Fatalf("plant session: %v", err)
	}
	sess := &registrationSession{id: id, workspaceID: ws, expiresAt: expiresAt}
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()
	return sess
}

// TestRegistrationGetSessionNotFound pins both halves of the
// not-found path: unknown session id, and (the security-critical one)
// known session id but from a different workspace. Both must surface
// the same ErrRegistrationSessionNotFound — leaking "exists but wrong
// workspace" would let an attacker enumerate session ids across
// workspaces.
func TestRegistrationGetSessionNotFound(t *testing.T) {
	s := newRegistrationServiceForTest(t)
	ctx := context.Background()
	ws := uuidFromStringSvc(t, "11111111-1111-1111-1111-111111111111")
	otherWs := uuidFromStringSvc(t, "22222222-2222-2222-2222-222222222222")

	if _, err := s.GetSession(ctx, ws, "nope"); !errors.Is(err, ErrRegistrationSessionNotFound) {
		t.Errorf("unknown session: want ErrRegistrationSessionNotFound, got %v", err)
	}

	plantSession(t, s, "plant-1", ws, s.cfg.Now().Add(time.Hour))

	if _, err := s.GetSession(ctx, otherWs, "plant-1"); !errors.Is(err, ErrRegistrationSessionNotFound) {
		t.Errorf("cross-workspace lookup: want ErrRegistrationSessionNotFound, got %v", err)
	}

	state, err := s.GetSession(ctx, ws, "plant-1")
	if err != nil {
		t.Fatalf("same-workspace lookup: %v", err)
	}
	if state.Status != RegistrationStatusPending {
		t.Errorf("Status: got %q want pending", state.Status)
	}
}

// TestRegistrationGetSessionServesSessionOwnedByAnotherProcess is the
// MUL-7340 regression at the unit level: the status read must resolve
// entirely from the shared store, with NOTHING in this process's session
// map. When the map was the source of truth, a poll that landed on any
// other replica 404'd and the dialog showed "安装会话已失效或丢失" about
// 5s after the QR rendered.
func TestRegistrationGetSessionServesSessionOwnedByAnotherProcess(t *testing.T) {
	shared := NewMemoryInstallSessionStore()

	// Two services sharing one store — the stand-in for two replicas
	// pointed at the same Redis.
	instanceA := newRegistrationServiceForTest(t)
	instanceA.sessionStore = shared
	instanceB := newRegistrationServiceForTest(t)
	instanceB.sessionStore = shared

	ctx := context.Background()
	ws := uuidFromStringSvc(t, "11111111-1111-1111-1111-111111111111")

	// A serves begin; B has never seen this session.
	plantSession(t, instanceA, "owned-elsewhere", ws, instanceA.cfg.Now().Add(time.Hour))

	instanceB.mu.Lock()
	mapped := len(instanceB.sessions)
	instanceB.mu.Unlock()
	if mapped != 0 {
		t.Fatalf("precondition: instance B must hold no in-process session, got %d", mapped)
	}

	state, err := instanceB.GetSession(ctx, ws, "owned-elsewhere")
	if err != nil {
		t.Fatalf("status read on the instance that did not serve begin: %v", err)
	}
	if state.Status != RegistrationStatusPending {
		t.Errorf("Status: got %q want pending", state.Status)
	}
}

// TestRegistrationGetSessionReportsExpiryFromTimestamp pins the other
// half of cross-process correctness: if the process that owned the
// polling goroutine died, nobody ever records the terminal outcome. The
// status read must derive expiry from ExpiresAt rather than reporting
// 'pending' forever.
func TestRegistrationGetSessionReportsExpiryFromTimestamp(t *testing.T) {
	clock := &fakeClockSvc{now: time.Unix(1_700_000_000, 0)}
	s := newRegistrationServiceForTest(t)
	s.cfg.Now = clock.Now
	store := NewMemoryInstallSessionStore()
	store.now = clock.Now
	s.sessionStore = store
	ctx := context.Background()
	ws := uuidFromStringSvc(t, "11111111-1111-1111-1111-111111111111")

	plantSession(t, s, "stranded", ws, clock.Now().Add(-time.Minute))

	state, err := s.GetSession(ctx, ws, "stranded")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if state.Status != RegistrationStatusError {
		t.Fatalf("Status: got %q want error", state.Status)
	}
	if state.ErrorReason != RegistrationReasonExpired {
		t.Errorf("ErrorReason: got %q want %q", state.ErrorReason, RegistrationReasonExpired)
	}
}

// TestRegistrationMarkErrorIsIdempotent guards against a double-fire
// race between the expiry timer and a Poll-driven terminal error:
// whichever fires first wins, and the second mark must NOT clobber the
// first reason (the user already saw it). The guard now lives in the
// UPDATE's `status = 'pending'` predicate rather than a mutex.
func TestRegistrationMarkErrorIsIdempotent(t *testing.T) {
	s := newRegistrationServiceForTest(t)
	ctx := context.Background()
	ws := uuidFromStringSvc(t, "11111111-1111-1111-1111-111111111111")

	sess := plantSession(t, s, "x", ws, s.cfg.Now().Add(time.Hour))
	s.markError(sess, RegistrationReasonAccessDenied, "user denied")
	s.markError(sess, RegistrationReasonExpired, "qr expired") // second mark — must no-op

	st, err := s.GetSession(ctx, ws, "x")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if st.ErrorReason != RegistrationReasonAccessDenied {
		t.Errorf("first reason should win; got %q", st.ErrorReason)
	}
}

// TestRegistrationMarkDropsInProcessSession pins that a terminated
// session releases its in-memory working state (which holds the
// device_code) while the durable row survives for the dialog to read.
func TestRegistrationMarkDropsInProcessSession(t *testing.T) {
	s := newRegistrationServiceForTest(t)
	ctx := context.Background()
	ws := uuidFromStringSvc(t, "11111111-1111-1111-1111-111111111111")

	sess := plantSession(t, s, "done", ws, s.cfg.Now().Add(time.Hour))
	s.markSuccess(sess, uuidFromStringSvc(t, "33333333-3333-3333-3333-333333333333"))

	s.mu.Lock()
	_, stillMapped := s.sessions["done"]
	s.mu.Unlock()
	if stillMapped {
		t.Errorf("terminated session must be dropped from the in-process map")
	}

	st, err := s.GetSession(ctx, ws, "done")
	if err != nil {
		t.Fatalf("durable row must outlive the goroutine: %v", err)
	}
	if st.Status != RegistrationStatusSuccess {
		t.Errorf("Status: got %q want success", st.Status)
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
		cfg: RegistrationServiceConfig{}.withDefaults(),
		// Same default the constructor installs — every service needs a
		// session store, so building one by struct literal must not skip it.
		sessionStore: NewMemoryInstallSessionStore(),
		sessions:     make(map[string]*registrationSession),
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

// rotatingCredsAPIClient records the order of the two calls
// finishSuccess makes before it touches the database, so the test can
// assert the cache is dropped BEFORE a token is minted with the new
// credentials.
type rotatingCredsAPIClient struct {
	APIClient // unused methods; the test only drives the two below

	mu    sync.Mutex
	calls []string
	creds InstallationCredentials
}

func (c *rotatingCredsAPIClient) InvalidateTokenCache(appID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, "invalidate:"+appID)
}

func (c *rotatingCredsAPIClient) GetBotInfo(_ context.Context, creds InstallationCredentials) (BotInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, "botinfo:"+creds.AppID)
	c.creds = creds
	// Fail so finishSuccess returns before the database work; the
	// ordering assertion below is what this test is about.
	return BotInfo{}, errors.New("bot info unavailable")
}

// TestFinishSuccess_DropsCachedTokenBeforeMintingWithRotatedCreds covers
// credential rotation (#7611): a re-registration issues a new
// client_secret under the SAME client_id, so Lark revokes every token
// minted from the old one while the in-process cache — keyed by app_id —
// keeps looking valid. Nothing recovers on its own if the stale entry
// survives into the first call made with the new credentials.
func TestFinishSuccess_DropsCachedTokenBeforeMintingWithRotatedCreds(t *testing.T) {
	api := &rotatingCredsAPIClient{}
	svc := &RegistrationService{
		cfg: RegistrationServiceConfig{}.withDefaults(),
		api: api,
		// finishSuccess records a terminal status on the bot-info failure
		// this test forces, so the store seam has to be present.
		sessionStore: NewMemoryInstallSessionStore(),
		sessions:     make(map[string]*registrationSession),
	}
	sess := &registrationSession{id: "sess-rotate"}

	svc.finishSuccess(context.Background(), sess, &PollResult{
		ClientID:     "cli_rotated",
		ClientSecret: "secret_new",
	}, RegionFeishu)

	api.mu.Lock()
	defer api.mu.Unlock()
	want := []string{"invalidate:cli_rotated", "botinfo:cli_rotated"}
	if len(api.calls) != len(want) || api.calls[0] != want[0] || api.calls[1] != want[1] {
		t.Fatalf("call order = %v, want %v", api.calls, want)
	}
	if api.creds.AppSecret != "secret_new" {
		t.Errorf("bot info credentials carried app_secret %q, want the rotated one", api.creds.AppSecret)
	}
}

// flakyTerminalStore fails the first n MarkTerminal calls, then delegates.
type flakyTerminalStore struct {
	InstallSessionStore
	remainingFailures int
	calls             int
}

func (s *flakyTerminalStore) MarkTerminal(ctx context.Context, id string, outcome InstallSessionOutcome, ttl time.Duration) error {
	s.calls++
	if s.remainingFailures > 0 {
		s.remainingFailures--
		return errors.New("injected store outage")
	}
	return s.InstallSessionStore.MarkTerminal(ctx, id, outcome, ttl)
}

// TestRegistrationTerminalWriteSurvivesTransientStoreFailure covers the
// review finding on #8376: by the time markSuccess runs, the installation
// and the installer binding are already committed in Postgres. Logging the
// store failure and walking away discarded that completion permanently —
// the browser kept reading `pending` until the QR expired and then showed
// a failure, for a bind that had actually succeeded.
func TestRegistrationTerminalWriteSurvivesTransientStoreFailure(t *testing.T) {
	shared := NewMemoryInstallSessionStore()
	flaky := &flakyTerminalStore{InstallSessionStore: shared, remainingFailures: 2}

	// Instance A owns the polling goroutine and hits the outage; instance
	// B is the replica the browser happens to poll.
	instanceA := newRegistrationServiceForTest(t)
	instanceA.sessionStore = flaky
	instanceB := newRegistrationServiceForTest(t)
	instanceB.sessionStore = shared

	ctx := context.Background()
	ws := uuidFromStringSvc(t, "11111111-1111-1111-1111-111111111111")
	installationID := uuidFromStringSvc(t, "33333333-3333-3333-3333-333333333333")

	sess := plantSession(t, instanceA, "retry-1", ws, instanceA.cfg.Now().Add(time.Hour))
	instanceA.markSuccess(sess, installationID)

	if flaky.calls < 3 {
		t.Errorf("expected the write to be retried past the outage, got %d call(s)", flaky.calls)
	}

	state, err := instanceB.GetSession(ctx, ws, "retry-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if state.Status != RegistrationStatusSuccess {
		t.Fatalf("Status: got %q want success — a committed bind must not be lost to a transient store failure", state.Status)
	}
	if state.InstallationID != installationID {
		t.Errorf("InstallationID: got %v want %v", state.InstallationID, installationID)
	}
}

// A session that has already expired out of the store cannot be repaired
// by retrying, so the loop must stop on the first not-found rather than
// spending its whole budget sleeping.
func TestRegistrationTerminalWriteStopsWhenSessionIsGone(t *testing.T) {
	s := newRegistrationServiceForTest(t)
	counting := &flakyTerminalStore{InstallSessionStore: NewMemoryInstallSessionStore()}
	s.sessionStore = counting

	// Never planted, so the store reports it gone.
	s.markError(&registrationSession{id: "absent"}, RegistrationReasonExpired, "qr expired")

	if counting.calls != 1 {
		t.Errorf("want a single attempt for a session that no longer exists, got %d", counting.calls)
	}
}
