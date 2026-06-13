package lark

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeHubQueries is the unit-test seam for HubQueries. The lease state
// is held in memory so a single fake can play both "we hold the lease"
// and "another replica holds the lease" scenarios across one test.
type fakeHubQueries struct {
	mu             sync.Mutex
	installations  []db.LarkInstallation
	listErr        error
	leaseOwner     map[string]string    // installation_id -> ws_lease_token
	leaseExpiresAt map[string]time.Time // installation_id -> expiry
	acquireErr     error
	releaseErr     error
	now            func() time.Time
	acquireCount   int32

	// releaseBlock, if non-nil, makes ReleaseLarkWSLease block until
	// either the channel is closed/sent on OR the caller's ctx fires.
	// Used to simulate a frozen DB pool so the bounded-release timeout
	// can be exercised without standing up real infrastructure.
	releaseBlock chan struct{}
	// releaseObservedCtxErr captures the ctx error (typically
	// context.DeadlineExceeded) the blocked release call observed
	// when its bounded ctx fired. Inspected by tests to prove the
	// bound actually fired instead of the test happening to win the
	// race naturally.
	releaseObservedCtxErr error
}

func newFakeHubQueries() *fakeHubQueries {
	return &fakeHubQueries{
		leaseOwner:     make(map[string]string),
		leaseExpiresAt: make(map[string]time.Time),
		now:            time.Now,
	}
}

func (f *fakeHubQueries) ListActiveLarkInstallations(ctx context.Context) ([]db.LarkInstallation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]db.LarkInstallation, len(f.installations))
	copy(out, f.installations)
	return out, nil
}

func (f *fakeHubQueries) AcquireLarkWSLease(ctx context.Context, arg db.AcquireLarkWSLeaseParams) (db.LarkInstallation, error) {
	atomic.AddInt32(&f.acquireCount, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.acquireErr != nil {
		return db.LarkInstallation{}, f.acquireErr
	}
	id := uuidString(arg.ID)
	owner, hasOwner := f.leaseOwner[id]
	exp := f.leaseExpiresAt[id]
	now := f.now()
	// CAS: accept when no holder, holder expired, or holder is us.
	if !hasOwner || exp.Before(now) || owner == arg.NewToken.String {
		f.leaseOwner[id] = arg.NewToken.String
		f.leaseExpiresAt[id] = arg.NewExpiresAt.Time
		// Return the (synthetic) row — the supervise loop only checks
		// the error, not the row contents.
		return db.LarkInstallation{ID: arg.ID}, nil
	}
	// Live lease held by someone else.
	return db.LarkInstallation{}, errPgxNoRows
}

func (f *fakeHubQueries) ReleaseLarkWSLease(ctx context.Context, arg db.ReleaseLarkWSLeaseParams) error {
	f.mu.Lock()
	block := f.releaseBlock
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
			// Released by the test — fall through to the normal path.
		case <-ctx.Done():
			f.mu.Lock()
			f.releaseObservedCtxErr = ctx.Err()
			f.mu.Unlock()
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.releaseErr != nil {
		return f.releaseErr
	}
	id := uuidString(arg.ID)
	if f.leaseOwner[id] == arg.CurrentToken.String {
		delete(f.leaseOwner, id)
		delete(f.leaseExpiresAt, id)
	}
	return nil
}

// presetLease forcibly assigns a lease to a holder other than the hub
// under test. Used to verify "another replica owns it" branches.
func (f *fakeHubQueries) presetLease(id pgtype.UUID, token string, expires time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leaseOwner[uuidString(id)] = token
	f.leaseExpiresAt[uuidString(id)] = expires
}

// fakeConnector counts how many times Run was invoked and behaves
// according to the script provided per-call. The default behavior
// (script nil) blocks on ctx.Done — useful for the "owns lease, stays
// connected" test.
type fakeConnector struct {
	mu     sync.Mutex
	runs   int
	script []func(ctx context.Context, emit EventEmitter) error
	emit   EventEmitter
}

func (f *fakeConnector) Run(ctx context.Context, _ db.LarkInstallation, emit EventEmitter) error {
	f.mu.Lock()
	idx := f.runs
	f.runs++
	if idx < len(f.script) {
		fn := f.script[idx]
		f.mu.Unlock()
		return fn(ctx, emit)
	}
	f.mu.Unlock()
	// Default: hold until cancelled.
	f.emit = emit
	<-ctx.Done()
	return nil
}

func (f *fakeConnector) Runs() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runs
}

func uuidFromString(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("scan uuid %q: %v", s, err)
	}
	return u
}

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHubAcquiresLeaseAndStartsSupervisor(t *testing.T) {
	q := newFakeHubQueries()
	instID := uuidFromString(t, "11111111-1111-1111-1111-111111111111")
	q.installations = []db.LarkInstallation{{ID: instID, Status: "active"}}

	conn := &fakeConnector{}
	factory := func(_ db.LarkInstallation) (EventConnector, error) { return conn, nil }

	hub := NewHub(q, factory, nil, HubConfig{
		LeaseTTL:           500 * time.Millisecond,
		LeaseRenewInterval: 50 * time.Millisecond,
		PollInterval:       10 * time.Millisecond,
		MinBackoff:         5 * time.Millisecond,
		MaxBackoff:         50 * time.Millisecond,
		ResetBackoffAfter:  1 * time.Second,
		Logger:             newDiscardLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)

	// Wait until the supervisor has started the connector at least once.
	if !waitFor(200*time.Millisecond, func() bool { return conn.Runs() >= 1 }) {
		t.Fatalf("expected connector to start; runs=%d", conn.Runs())
	}

	cancel()
	hub.Wait()

	// After shutdown the lease should be released so another replica
	// can take over without waiting for the TTL to elapse.
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, ok := q.leaseOwner[uuidString(instID)]; ok {
		t.Fatalf("lease should be released after shutdown, got owner %q", q.leaseOwner[uuidString(instID)])
	}
}

func TestHubSkipsInstallationWhenAnotherReplicaHoldsLease(t *testing.T) {
	q := newFakeHubQueries()
	instID := uuidFromString(t, "22222222-2222-2222-2222-222222222222")
	q.installations = []db.LarkInstallation{{ID: instID, Status: "active"}}
	// Another replica already owns the lease for the next 10 seconds.
	q.presetLease(instID, "other-replica", time.Now().Add(10*time.Second))

	conn := &fakeConnector{}
	factory := func(_ db.LarkInstallation) (EventConnector, error) { return conn, nil }

	hub := NewHub(q, factory, nil, HubConfig{
		LeaseTTL:           500 * time.Millisecond,
		LeaseRenewInterval: 20 * time.Millisecond,
		PollInterval:       20 * time.Millisecond,
		MinBackoff:         5 * time.Millisecond,
		MaxBackoff:         20 * time.Millisecond,
		ResetBackoffAfter:  1 * time.Second,
		Logger:             newDiscardLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)

	// Give the hub plenty of opportunity to try to take over.
	time.Sleep(150 * time.Millisecond)

	if conn.Runs() != 0 {
		t.Fatalf("connector should not run while another replica owns lease; runs=%d", conn.Runs())
	}

	cancel()
	hub.Wait()
}

func TestHubReclaimsLeaseAfterAnotherReplicaExpires(t *testing.T) {
	q := newFakeHubQueries()
	instID := uuidFromString(t, "33333333-3333-3333-3333-333333333333")
	q.installations = []db.LarkInstallation{{ID: instID, Status: "active"}}
	// Set the other replica's lease to expire in 80ms so the hub
	// (which polls/renews on 20ms intervals) will pick it up.
	q.presetLease(instID, "other-replica", time.Now().Add(80*time.Millisecond))

	conn := &fakeConnector{}
	factory := func(_ db.LarkInstallation) (EventConnector, error) { return conn, nil }

	hub := NewHub(q, factory, nil, HubConfig{
		LeaseTTL:           500 * time.Millisecond,
		LeaseRenewInterval: 20 * time.Millisecond,
		PollInterval:       20 * time.Millisecond,
		MinBackoff:         5 * time.Millisecond,
		MaxBackoff:         20 * time.Millisecond,
		ResetBackoffAfter:  1 * time.Second,
		Logger:             newDiscardLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)

	if !waitFor(500*time.Millisecond, func() bool { return conn.Runs() >= 1 }) {
		t.Fatalf("expected connector to start after lease expiry; runs=%d", conn.Runs())
	}
	cancel()
	hub.Wait()
}

func TestHubReapsSupervisorWhenInstallationRevoked(t *testing.T) {
	q := newFakeHubQueries()
	instID := uuidFromString(t, "44444444-4444-4444-4444-444444444444")
	q.installations = []db.LarkInstallation{{ID: instID, Status: "active"}}

	conn := &fakeConnector{}
	factory := func(_ db.LarkInstallation) (EventConnector, error) { return conn, nil }

	hub := NewHub(q, factory, nil, HubConfig{
		LeaseTTL:           500 * time.Millisecond,
		LeaseRenewInterval: 20 * time.Millisecond,
		PollInterval:       20 * time.Millisecond,
		MinBackoff:         5 * time.Millisecond,
		MaxBackoff:         20 * time.Millisecond,
		ResetBackoffAfter:  1 * time.Second,
		Logger:             newDiscardLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)
	defer func() { cancel(); hub.Wait() }()

	if !waitFor(200*time.Millisecond, func() bool { return conn.Runs() >= 1 }) {
		t.Fatalf("expected connector to start; runs=%d", conn.Runs())
	}

	// Simulate revocation: the installation disappears from
	// ListActiveLarkInstallations. The Hub should cancel its
	// supervisor on the next sweep, which releases the lease.
	q.mu.Lock()
	q.installations = nil
	q.mu.Unlock()

	if !waitFor(500*time.Millisecond, func() bool {
		q.mu.Lock()
		defer q.mu.Unlock()
		_, stillHeld := q.leaseOwner[uuidString(instID)]
		return !stillHeld
	}) {
		t.Fatalf("expected lease to be released after revocation")
	}
}

// TestHubRestartsSupervisorOnCredentialsRotation pins the rotation
// invariant Bohan hit on the live env: re-scanning the same agent
// runs the device flow again, which mints a brand-new Lark bot with
// a fresh app_id / encrypted app_secret. The lark_installation row is
// updated in place (UNIQUE(agent_id)), but the running supervisor
// holds the OLD inst struct by value. Without a fingerprint-driven
// restart the connector keeps a WS open against the OLD bot's app_id
// and the new bot silently goes dark — exactly the "claude code 没反应"
// symptom Bohan reported.
//
// Repro: start the hub with installation A (app_id=app_one), wait for
// the connector factory to be called, then mutate the row to a new
// app_id (app_two). The next sweep MUST cancel the first supervisor
// and start a second one with the new credentials.
func TestHubRestartsSupervisorOnCredentialsRotation(t *testing.T) {
	q := newFakeHubQueries()
	instID := uuidFromString(t, "abcdabcd-abcd-abcd-abcd-abcdabcdabcd")
	q.installations = []db.LarkInstallation{{
		ID:                 instID,
		Status:             "active",
		AppID:              "app_one",
		BotOpenID:          "bot_open_id_one",
		AppSecretEncrypted: []byte("encrypted_one"),
	}}

	type seenInst struct{ AppID string }
	var mu sync.Mutex
	var seen []seenInst
	factory := func(inst db.LarkInstallation) (EventConnector, error) {
		mu.Lock()
		seen = append(seen, seenInst{AppID: inst.AppID})
		mu.Unlock()
		return &fakeConnector{}, nil
	}

	hub := NewHub(q, factory, nil, HubConfig{
		LeaseTTL:           500 * time.Millisecond,
		LeaseRenewInterval: 50 * time.Millisecond,
		PollInterval:       20 * time.Millisecond,
		MinBackoff:         5 * time.Millisecond,
		MaxBackoff:         20 * time.Millisecond,
		ResetBackoffAfter:  1 * time.Second,
		Logger:             newDiscardLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)
	defer func() { cancel(); hub.Wait() }()

	if !waitFor(300*time.Millisecond, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(seen) >= 1
	}) {
		t.Fatalf("expected initial connector start; got %d", len(seen))
	}

	// Rotate credentials in place — same installation_id, new app_id +
	// new encrypted secret. This is exactly what device-flow re-scan
	// produces.
	q.mu.Lock()
	q.installations[0].AppID = "app_two"
	q.installations[0].BotOpenID = "bot_open_id_two"
	q.installations[0].AppSecretEncrypted = []byte("encrypted_two")
	q.mu.Unlock()

	if !waitFor(500*time.Millisecond, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, s := range seen {
			if s.AppID == "app_two" {
				return true
			}
		}
		return false
	}) {
		mu.Lock()
		got := append([]seenInst(nil), seen...)
		mu.Unlock()
		t.Fatalf("expected a second connector start with rotated app_id; got %+v", got)
	}
}

// TestHubDoesNotRestartSupervisorOnUnchangedRow pins the negative case:
// a sweep that observes an installation row identical to the fingerprint
// the supervisor was started with MUST NOT restart. Without this guard
// the rotation logic would degrade into a busy-loop, tearing the
// connector down on every poll tick.
func TestHubDoesNotRestartSupervisorOnUnchangedRow(t *testing.T) {
	q := newFakeHubQueries()
	instID := uuidFromString(t, "bcdebcde-bcde-bcde-bcde-bcdebcdebcde")
	q.installations = []db.LarkInstallation{{
		ID:                 instID,
		Status:             "active",
		AppID:              "app_stable",
		BotOpenID:          "bot_stable",
		AppSecretEncrypted: []byte("stable"),
	}}

	starts := int32(0)
	factory := func(_ db.LarkInstallation) (EventConnector, error) {
		atomic.AddInt32(&starts, 1)
		return &fakeConnector{}, nil
	}

	hub := NewHub(q, factory, nil, HubConfig{
		LeaseTTL:           500 * time.Millisecond,
		LeaseRenewInterval: 50 * time.Millisecond,
		PollInterval:       10 * time.Millisecond,
		MinBackoff:         5 * time.Millisecond,
		MaxBackoff:         20 * time.Millisecond,
		ResetBackoffAfter:  1 * time.Second,
		Logger:             newDiscardLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)
	defer func() { cancel(); hub.Wait() }()

	// Wait until the supervisor has called the factory at least once.
	if !waitFor(200*time.Millisecond, func() bool { return atomic.LoadInt32(&starts) >= 1 }) {
		t.Fatalf("expected initial connector start; got %d", starts)
	}
	// Let several sweep ticks happen with an unchanged row.
	time.Sleep(120 * time.Millisecond)
	got := atomic.LoadInt32(&starts)
	if got > 2 {
		// Allow one extra start in case the fakeConnector returns
		// immediately and the supervise loop re-enters the connector
		// factory under backoff. More than that is the busy-loop bug.
		t.Fatalf("supervisor restarted unexpectedly on unchanged row: %d factory calls in 320ms", got)
	}
}

// TestHubRotationStaleReleaseDoesNotClearSuccessorLease pins the
// fix for Elon's review of HEAD be8d4cef. Before per-supervisor lease
// tokens, both old and new supervisors of the same Hub used the same
// hub-wide nodeID as their lease token. The rotation race went:
//
//   1. Old supervisor cancelled by maybeRestartOnRotation.
//   2. New supervisor started; acquireLease takes the lease.
//   3. Old supervisor finishes post-cancel unwind and calls
//      releaseLease(nodeID).
//   4. DB row's CurrentToken still equals nodeID (because new
//      supervisor wrote the SAME token). DELETE matches → DB row
//      cleared → new supervisor's lease silently lost.
//
// Per-supervisor tokens (nodeID + "-g" + gen) make step 3's
// CurrentToken belong to the OLD supervisor, and the DB row's actual
// CurrentToken belongs to the NEW supervisor — so the DELETE no-ops
// and the successor keeps its lease.
//
// This test drives the lease state machine directly through the
// fakeHubQueries, simulating the exact ordering: old acquires
// (token_A) → rotation → new acquires (token_B) → old's stale
// release(token_A). The lease must remain held by token_B.
func TestHubRotationStaleReleaseDoesNotClearSuccessorLease(t *testing.T) {
	q := newFakeHubQueries()
	instID := uuidFromString(t, "deadbeef-dead-beef-dead-beefdeadbeef")
	expires := time.Now().Add(time.Minute)

	tokenA := leaseToken("node_xyz", 1)
	tokenB := leaseToken("node_xyz", 2)
	if tokenA == tokenB {
		t.Fatalf("per-supervisor tokens must differ; got %q for both", tokenA)
	}

	// Old supervisor acquires.
	if _, err := q.AcquireLarkWSLease(context.Background(), db.AcquireLarkWSLeaseParams{
		ID:           instID,
		NewToken:     pgtype.Text{String: tokenA, Valid: true},
		NewExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true},
	}); err != nil {
		t.Fatalf("old acquire: %v", err)
	}
	q.mu.Lock()
	if got := q.leaseOwner[uuidString(instID)]; got != tokenA {
		t.Fatalf("after old acquire, owner = %q; want %q", got, tokenA)
	}
	q.mu.Unlock()

	// Rotation: new supervisor takes over. acquireLease's CAS in the
	// production query accepts "matching token" as a renewal — but
	// here we want to exercise the post-cancel succession where the
	// old lease is gone or being replaced. The fake's CAS also accepts
	// expired rows, so we simulate the old supervisor's lease having
	// been released cleanly (its renewal hasn't fired yet on rotation
	// boundary, but in production a rotation cancels the old loop and
	// the old supervisor's defer eventually calls releaseLease — let
	// the new supervisor acquire BEFORE that release lands).
	q.mu.Lock()
	delete(q.leaseOwner, uuidString(instID))
	delete(q.leaseExpiresAt, uuidString(instID))
	q.mu.Unlock()
	if _, err := q.AcquireLarkWSLease(context.Background(), db.AcquireLarkWSLeaseParams{
		ID:           instID,
		NewToken:     pgtype.Text{String: tokenB, Valid: true},
		NewExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true},
	}); err != nil {
		t.Fatalf("new acquire: %v", err)
	}
	q.mu.Lock()
	if got := q.leaseOwner[uuidString(instID)]; got != tokenB {
		t.Fatalf("after new acquire, owner = %q; want %q", got, tokenB)
	}
	q.mu.Unlock()

	// Old supervisor's defer / cleanup fires AFTER new acquired.
	// Without per-supervisor tokens this would clobber tokenB's lease.
	if err := q.ReleaseLarkWSLease(context.Background(), db.ReleaseLarkWSLeaseParams{
		ID:           instID,
		CurrentToken: pgtype.Text{String: tokenA, Valid: true},
	}); err != nil {
		t.Fatalf("old release: %v", err)
	}

	// Successor's lease must still be held by tokenB.
	q.mu.Lock()
	got, ok := q.leaseOwner[uuidString(instID)]
	q.mu.Unlock()
	if !ok {
		t.Fatalf("successor lease silently cleared by stale release — the rotation race is back")
	}
	if got != tokenB {
		t.Fatalf("after stale release, owner = %q; want %q (successor's token)", got, tokenB)
	}
}

// TestHubRotationEndToEndKeepsSuccessorLeased exercises the same
// rotation race through the live supervise loop — not just the lease
// state machine in isolation. Drives a hub through:
//
//   1. install row with credentials A → supervisor1 acquires lease(A)
//   2. credentials rotate to B → maybeRestartOnRotation cancels sup1
//   3. supervisor2 starts, acquires lease(B)
//   4. sup1's post-cancel releaseLease(A) runs; must NOT clear lease(B)
//
// Even with the timing being non-deterministic (real goroutines), the
// fake's lease map either ends up with sup2's token or empty — empty
// means the successor lost its lease and would never deliver events,
// which is exactly the bug Elon flagged. We assert the lease ends up
// held by sup2's token.
func TestHubRotationEndToEndKeepsSuccessorLeased(t *testing.T) {
	q := newFakeHubQueries()
	instID := uuidFromString(t, "feedfeed-feed-feed-feed-feedfeedfeed")
	q.installations = []db.LarkInstallation{{
		ID:                 instID,
		Status:             "active",
		AppID:              "app_one",
		BotOpenID:          "bot_one",
		AppSecretEncrypted: []byte("secret_one"),
	}}

	// Use a fakeConnector that blocks on ctx.Done so the renewer keeps
	// running and refreshes the lease at each tick, mirroring
	// production timing for the rotation handoff.
	factoryCalls := int32(0)
	factory := func(_ db.LarkInstallation) (EventConnector, error) {
		atomic.AddInt32(&factoryCalls, 1)
		return &fakeConnector{}, nil
	}

	hub := NewHub(q, factory, nil, HubConfig{
		LeaseTTL:           500 * time.Millisecond,
		LeaseRenewInterval: 30 * time.Millisecond,
		PollInterval:       15 * time.Millisecond,
		MinBackoff:         5 * time.Millisecond,
		MaxBackoff:         20 * time.Millisecond,
		ResetBackoffAfter:  1 * time.Second,
		Logger:             newDiscardLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)
	defer func() { cancel(); hub.Wait() }()

	if !waitFor(300*time.Millisecond, func() bool { return atomic.LoadInt32(&factoryCalls) >= 1 }) {
		t.Fatalf("expected sup1 to start; got %d factory calls", factoryCalls)
	}
	q.mu.Lock()
	sup1Token := q.leaseOwner[uuidString(instID)]
	q.mu.Unlock()
	if sup1Token == "" {
		t.Fatalf("sup1 did not write a lease token")
	}

	// Rotation.
	q.mu.Lock()
	q.installations[0].AppID = "app_two"
	q.installations[0].BotOpenID = "bot_two"
	q.installations[0].AppSecretEncrypted = []byte("secret_two")
	q.mu.Unlock()

	// Wait for sup2 to start AND its lease token to differ from sup1's.
	// The successor's token must end up owning the row regardless of
	// when sup1's stale release lands.
	if !waitFor(500*time.Millisecond, func() bool {
		if atomic.LoadInt32(&factoryCalls) < 2 {
			return false
		}
		q.mu.Lock()
		defer q.mu.Unlock()
		curr, ok := q.leaseOwner[uuidString(instID)]
		return ok && curr != sup1Token
	}) {
		q.mu.Lock()
		got, ok := q.leaseOwner[uuidString(instID)]
		q.mu.Unlock()
		t.Fatalf("successor lease never present; ok=%v owner=%q factoryCalls=%d",
			ok, got, atomic.LoadInt32(&factoryCalls))
	}

	// Give sup1's deferred release a chance to land, then re-check.
	time.Sleep(150 * time.Millisecond)
	q.mu.Lock()
	owner, ok := q.leaseOwner[uuidString(instID)]
	q.mu.Unlock()
	if !ok {
		t.Fatalf("successor lease cleared after sup1's stale release — rotation fencing broken")
	}
	if owner == sup1Token {
		t.Fatalf("lease still held by sup1 token %q; successor never took over", sup1Token)
	}
}

func TestHubBacksOffOnFactoryError(t *testing.T) {
	q := newFakeHubQueries()
	instID := uuidFromString(t, "55555555-5555-5555-5555-555555555555")
	q.installations = []db.LarkInstallation{{ID: instID, Status: "active"}}

	factoryCalls := int32(0)
	factory := func(_ db.LarkInstallation) (EventConnector, error) {
		atomic.AddInt32(&factoryCalls, 1)
		return nil, errors.New("boom")
	}

	hub := NewHub(q, factory, nil, HubConfig{
		LeaseTTL:           500 * time.Millisecond,
		LeaseRenewInterval: 20 * time.Millisecond,
		PollInterval:       20 * time.Millisecond,
		MinBackoff:         5 * time.Millisecond,
		MaxBackoff:         20 * time.Millisecond,
		ResetBackoffAfter:  1 * time.Second,
		Logger:             newDiscardLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)

	// Let the supervisor retry under backoff. We want > 1 call to
	// prove the loop is alive but the increasing delay should keep
	// the rate sane.
	if !waitFor(200*time.Millisecond, func() bool { return atomic.LoadInt32(&factoryCalls) >= 2 }) {
		t.Fatalf("expected factory retries under backoff; got %d", atomic.LoadInt32(&factoryCalls))
	}
	calls := atomic.LoadInt32(&factoryCalls)
	cancel()
	hub.Wait()
	if calls > 200 {
		t.Fatalf("backoff appears broken: %d factory calls in 200ms", calls)
	}
}

// TestHubLeaseLossCancelsConnector pins the §4.4 ownership invariant.
// When another replica steals the lease, the renewer must cancel the
// connector's run context so the connector exits even if its wire I/O
// is currently blocked. Without that cancel, replica A could keep
// reading Lark events for an unbounded window while replica B already
// believes it is the sole owner — duplicate consumption, exactly what
// the lease is supposed to prevent.
func TestHubLeaseLossCancelsConnector(t *testing.T) {
	q := newFakeHubQueries()
	instID := uuidFromString(t, "66666666-6666-6666-6666-666666666666")
	q.installations = []db.LarkInstallation{{ID: instID, Status: "active"}}

	// fakeConnector default behavior blocks on ctx.Done — perfect for
	// "simulate a socket that never returns until we explicitly cancel
	// it" scenarios. We capture the ctx the supervisor handed it so we
	// can wait on its done channel directly.
	connCtxCh := make(chan context.Context, 1)
	conn := &fakeConnector{
		script: []func(ctx context.Context, emit EventEmitter) error{
			func(ctx context.Context, _ EventEmitter) error {
				connCtxCh <- ctx
				<-ctx.Done()
				return ctx.Err()
			},
		},
	}
	factory := func(_ db.LarkInstallation) (EventConnector, error) { return conn, nil }

	hub := NewHub(q, factory, nil, HubConfig{
		LeaseTTL:           500 * time.Millisecond,
		LeaseRenewInterval: 20 * time.Millisecond,
		PollInterval:       1 * time.Hour, // disable sweep noise; we drive lease state by hand.
		MinBackoff:         5 * time.Millisecond,
		MaxBackoff:         20 * time.Millisecond,
		ResetBackoffAfter:  10 * time.Second,
		Logger:             newDiscardLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); hub.Wait() }()
	go hub.Run(ctx)

	// Wait for the supervisor to hand the connector a run context.
	var runCtx context.Context
	select {
	case runCtx = <-connCtxCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("connector never started")
	}

	// Simulate lease theft: rewrite the lease row to point at another
	// replica with a fresh expiry. The next renewal CAS will fail
	// because the token no longer matches our nodeID, the renewer
	// returns leased=false, and (with the fix) cancels the run ctx.
	q.presetLease(instID, "thief-replica", time.Now().Add(10*time.Second))

	select {
	case <-runCtx.Done():
		// Expected: renewer cancelled runCtx within a few renewal ticks.
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("renewer did not cancel run ctx after lease loss")
	}
}

// TestHubEmitReturnsDispatchResultAndError pins the connector-facing
// emit contract: the supervisor's emit shim wraps the Dispatcher and
// surfaces both the typed DispatchResult and any infra error so the
// real Lark connector can post the right outbound (binding card,
// offline card, etc.) and react to infra failures.
func TestHubEmitReturnsDispatchResultAndError(t *testing.T) {
	q := newFakeHubQueries()
	instID := uuidFromString(t, "77777777-7777-7777-7777-777777777777")
	q.installations = []db.LarkInstallation{{ID: instID, Status: "active"}}

	// Capture what emit returned on the first invocation so the
	// connector goroutine can stash it for the test.
	var (
		gotRes DispatchResult
		gotErr error
		gotMu  sync.Mutex
	)
	emitDone := make(chan struct{})

	conn := &fakeConnector{
		script: []func(ctx context.Context, emit EventEmitter) error{
			func(ctx context.Context, emit EventEmitter) error {
				res, err := emit(ctx, InboundMessage{
					EventID:   "evt-1",
					EventType: "im.message.receive_v1",
				})
				gotMu.Lock()
				gotRes = res
				gotErr = err
				gotMu.Unlock()
				close(emitDone)
				<-ctx.Done()
				return ctx.Err()
			},
		},
	}
	factory := func(_ db.LarkInstallation) (EventConnector, error) { return conn, nil }

	// No dispatcher wired -> emit must return ErrDispatcherNotConfigured.
	// The point is the error surfaces back to the connector instead of
	// being silently dropped at the Hub.
	hub := NewHub(q, factory, nil, HubConfig{
		LeaseTTL:           500 * time.Millisecond,
		LeaseRenewInterval: 20 * time.Millisecond,
		PollInterval:       1 * time.Hour,
		MinBackoff:         5 * time.Millisecond,
		MaxBackoff:         20 * time.Millisecond,
		ResetBackoffAfter:  10 * time.Second,
		Logger:             newDiscardLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); hub.Wait() }()
	go hub.Run(ctx)

	select {
	case <-emitDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("connector never invoked emit")
	}

	gotMu.Lock()
	defer gotMu.Unlock()
	if !errors.Is(gotErr, ErrDispatcherNotConfigured) {
		t.Fatalf("emit should propagate dispatcher errors; got %v", gotErr)
	}
	if gotRes.Outcome != "" {
		t.Fatalf("emit should not invent an outcome on dispatcher error; got %q", gotRes.Outcome)
	}
}

// TestHubReleaseLeaseBoundedByTimeout pins the shutdown-safety
// invariant: a frozen DB pool must NOT keep the supervisor blocked
// on releaseLease past the configured LeaseReleaseTimeout. Without
// the bound, ctx.Background()-rooted release calls could hang
// forever on a stalled pool, dragging out process shutdown well
// past the operator's expected drain budget.
func TestHubReleaseLeaseBoundedByTimeout(t *testing.T) {
	q := newFakeHubQueries()
	q.releaseBlock = make(chan struct{}) // never closed; release always sees ctx.Done
	instID := uuidFromString(t, "88888888-8888-8888-8888-888888888888")
	q.installations = []db.LarkInstallation{{ID: instID, Status: "active"}}

	conn := &fakeConnector{}
	factory := func(_ db.LarkInstallation) (EventConnector, error) { return conn, nil }

	releaseTimeout := 50 * time.Millisecond
	hub := NewHub(q, factory, nil, HubConfig{
		LeaseTTL:            500 * time.Millisecond,
		LeaseRenewInterval:  20 * time.Millisecond,
		PollInterval:        1 * time.Hour,
		MinBackoff:          5 * time.Millisecond,
		MaxBackoff:          20 * time.Millisecond,
		ResetBackoffAfter:   10 * time.Second,
		LeaseReleaseTimeout: releaseTimeout,
		ShutdownTimeout:     2 * time.Second, // generous; we want WaitWithTimeout to succeed
		Logger:              newDiscardLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)

	if !waitFor(500*time.Millisecond, func() bool { return conn.Runs() >= 1 }) {
		cancel()
		hub.Wait()
		t.Fatalf("expected connector to start; runs=%d", conn.Runs())
	}

	start := time.Now()
	cancel()
	// WaitWithTimeout MUST return true: the bound on releaseLease
	// has to let the supervisor unwind even though our fake release
	// never returns on its own.
	if !hub.WaitWithTimeout(2 * time.Second) {
		t.Fatalf("supervisor stuck despite bounded release; lease release timeout did not fire")
	}
	elapsed := time.Since(start)

	// Sanity bound: shutdown must complete in roughly the release
	// timeout plus a small jitter, NOT seconds. If the bound regressed
	// (e.g. someone reintroduced ctx.Background() without a deadline),
	// this assertion catches it.
	if elapsed > 500*time.Millisecond {
		t.Fatalf("shutdown took %s; expected ≈ %s + slack", elapsed, releaseTimeout)
	}

	q.mu.Lock()
	gotErr := q.releaseObservedCtxErr
	q.mu.Unlock()
	if !errors.Is(gotErr, context.DeadlineExceeded) {
		t.Fatalf("release should have observed DeadlineExceeded from its bounded ctx; got %v", gotErr)
	}
}

// TestHubWaitWithTimeoutReturnsTrueWhenSupervisorsExit covers the
// happy path: everything stops cleanly within the deadline, so the
// caller can proceed without logging a timeout warning.
func TestHubWaitWithTimeoutReturnsTrueWhenSupervisorsExit(t *testing.T) {
	q := newFakeHubQueries()
	instID := uuidFromString(t, "99999999-9999-9999-9999-999999999999")
	q.installations = []db.LarkInstallation{{ID: instID, Status: "active"}}

	conn := &fakeConnector{}
	factory := func(_ db.LarkInstallation) (EventConnector, error) { return conn, nil }

	hub := NewHub(q, factory, nil, HubConfig{
		LeaseTTL:           500 * time.Millisecond,
		LeaseRenewInterval: 20 * time.Millisecond,
		PollInterval:       1 * time.Hour,
		MinBackoff:         5 * time.Millisecond,
		MaxBackoff:         20 * time.Millisecond,
		ResetBackoffAfter:  10 * time.Second,
		Logger:             newDiscardLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)

	if !waitFor(500*time.Millisecond, func() bool { return conn.Runs() >= 1 }) {
		cancel()
		hub.Wait()
		t.Fatalf("expected connector to start; runs=%d", conn.Runs())
	}

	cancel()
	if !hub.WaitWithTimeout(1 * time.Second) {
		t.Fatalf("WaitWithTimeout returned false despite supervisor exiting promptly")
	}
}

// TestHubWaitWithTimeoutReturnsFalseWhenSupervisorStuck pins the
// bound on the join itself: if a (future real) connector or release
// path ignores ctx and refuses to exit, WaitWithTimeout MUST return
// false so main.go can log + proceed with shutdown rather than block
// the process forever.
func TestHubWaitWithTimeoutReturnsFalseWhenSupervisorStuck(t *testing.T) {
	q := newFakeHubQueries()
	q.releaseBlock = make(chan struct{}) // never closed
	instID := uuidFromString(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	q.installations = []db.LarkInstallation{{ID: instID, Status: "active"}}

	conn := &fakeConnector{}
	factory := func(_ db.LarkInstallation) (EventConnector, error) { return conn, nil }

	// LeaseReleaseTimeout > ShutdownTimeout so the release is still
	// blocked when the join deadline expires. This pins the "join
	// deadline trips before the supervisor unwinds" branch.
	hub := NewHub(q, factory, nil, HubConfig{
		LeaseTTL:            500 * time.Millisecond,
		LeaseRenewInterval:  20 * time.Millisecond,
		PollInterval:        1 * time.Hour,
		MinBackoff:          5 * time.Millisecond,
		MaxBackoff:          20 * time.Millisecond,
		ResetBackoffAfter:   10 * time.Second,
		LeaseReleaseTimeout: 5 * time.Second,
		Logger:              newDiscardLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)

	if !waitFor(500*time.Millisecond, func() bool { return conn.Runs() >= 1 }) {
		cancel()
		hub.Wait() // unbounded fallback; test will time out instead of hanging
		t.Fatalf("expected connector to start; runs=%d", conn.Runs())
	}

	cancel()
	if hub.WaitWithTimeout(50 * time.Millisecond) {
		t.Fatalf("WaitWithTimeout returned true while release was still blocked")
	}

	// Unblock the release so the supervisor can finally exit and the
	// test doesn't leak a goroutine.
	close(q.releaseBlock)
	hub.Wait()
}

// TestHubConfigDefaultsCoverShutdownKnobs documents that callers
// that omit the new shutdown knobs still get sensible defaults
// (matching the behavior router.go relies on by passing HubConfig{}).
// If the defaults regress to zero, releaseLease would derive a
// 0-deadline ctx that fails instantly — the real symptom would be
// "release lease failed: context deadline exceeded" warnings on
// every shutdown.
func TestHubConfigDefaultsCoverShutdownKnobs(t *testing.T) {
	c := HubConfig{}.withDefaults()
	if c.LeaseReleaseTimeout <= 0 {
		t.Fatalf("LeaseReleaseTimeout default must be > 0; got %s", c.LeaseReleaseTimeout)
	}
	if c.ShutdownTimeout <= 0 {
		t.Fatalf("ShutdownTimeout default must be > 0; got %s", c.ShutdownTimeout)
	}
}

// waitFor polls cond until it returns true or the deadline is reached.
// Returns true on success. Tests use this instead of time.Sleep so they
// remain robust on slow CI runners without slowing fast ones down.
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}

// slowReplier blocks Reply for the configured duration unless the ctx
// fires first. Used to prove the Hub's reply-off-critical-path
// invariant: a Reply that exceeds ReplyTimeout MUST get its ctx
// cancelled instead of running unbounded.
type slowReplier struct {
	delay       time.Duration
	startCh     chan struct{}
	finishCh    chan struct{}
	mu          sync.Mutex
	callCount   int
	lastCtxErr  error // ctx.Err() observed when Reply returned
}

func newSlowReplier(delay time.Duration) *slowReplier {
	return &slowReplier{
		delay:    delay,
		startCh:  make(chan struct{}, 16),
		finishCh: make(chan struct{}, 16),
	}
}

func (s *slowReplier) Reply(ctx context.Context, _ db.LarkInstallation, _ InboundMessage, _ DispatchResult) {
	s.mu.Lock()
	s.callCount++
	s.mu.Unlock()
	select {
	case s.startCh <- struct{}{}:
	default:
	}
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
	}
	s.mu.Lock()
	s.lastCtxErr = ctx.Err()
	s.mu.Unlock()
	select {
	case s.finishCh <- struct{}{}:
	default:
	}
}

func (s *slowReplier) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.callCount
}

func (s *slowReplier) ctxErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastCtxErr
}

// TestHubScheduleReplyReturnsImmediately verifies the core invariant
// behind the OutcomeReplier refactor: a slow replier MUST NOT block
// the dispatch caller. handleEvent is on the ACK critical path — the
// connector ACKs as soon as it returns — so coupling it to outbound
// Lark HTTP would let any slow card-send stall ACK past Lark's 3s
// deadline. This test puts a 10s sleep in the replier and asserts the
// scheduling call still returns in < 50ms.
func TestHubScheduleReplyReturnsImmediately(t *testing.T) {
	t.Parallel()
	rep := newSlowReplier(10 * time.Second)
	hub := NewHub(nil, nil, nil, HubConfig{
		ReplyTimeout: 100 * time.Millisecond,
		Logger:       newDiscardLogger(),
	})
	hub.SetOutcomeReplier(rep)

	start := time.Now()
	hub.scheduleReply(db.LarkInstallation{}, InboundMessage{EventID: "e1"},
		DispatchResult{Outcome: OutcomeNeedsBinding}, newDiscardLogger())
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Fatalf("scheduleReply took %s; ACK critical path would be blocked by outbound HTTP", elapsed)
	}

	// The replier should still be in flight — proves it really did
	// detach into a goroutine.
	select {
	case <-rep.startCh:
		// good — Reply was invoked async
	case <-time.After(500 * time.Millisecond):
		t.Fatal("detached replier never ran")
	}

	// Drain the timeout-bounded reply so the test doesn't leak goroutines.
	if !hub.WaitWithTimeout(1 * time.Second) {
		t.Fatal("reply goroutine did not exit after ReplyTimeout fired")
	}

	if !errors.Is(rep.ctxErr(), context.DeadlineExceeded) {
		t.Fatalf("replier ctx.Err() = %v; want DeadlineExceeded", rep.ctxErr())
	}
}

// TestHubReplyTimeoutCancelsHungReplier pins the bound on the detached
// reply: a replier that ignores ctx (or its outbound HTTP that hasn't
// noticed yet) MUST be cancelled at ReplyTimeout. The test gives the
// replier a deliberately long sleep and a short timeout, then asserts
// the reply goroutine exits within roughly the timeout — not the sleep.
func TestHubReplyTimeoutCancelsHungReplier(t *testing.T) {
	t.Parallel()
	timeout := 80 * time.Millisecond
	rep := newSlowReplier(10 * time.Second)
	hub := NewHub(nil, nil, nil, HubConfig{
		ReplyTimeout: timeout,
		Logger:       newDiscardLogger(),
	})
	hub.SetOutcomeReplier(rep)

	start := time.Now()
	hub.scheduleReply(db.LarkInstallation{}, InboundMessage{EventID: "e2"},
		DispatchResult{Outcome: OutcomeAgentOffline}, newDiscardLogger())

	select {
	case <-rep.finishCh:
	case <-time.After(1 * time.Second):
		t.Fatal("replier never exited; ReplyTimeout did not fire")
	}
	elapsed := time.Since(start)

	// Sanity bound: shutdown must complete in roughly timeout + jitter.
	if elapsed > 500*time.Millisecond {
		t.Fatalf("replier exit took %s; expected ≈ %s", elapsed, timeout)
	}
	if !errors.Is(rep.ctxErr(), context.DeadlineExceeded) {
		t.Fatalf("replier should observe DeadlineExceeded; got %v", rep.ctxErr())
	}
	hub.Wait()
}

// TestHubWaitDrainsInFlightReplies verifies that Hub.Wait (and
// WaitWithTimeout) joins on the replier goroutines, not just the
// supervisor goroutines. Without this, shutdown could close the
// process while a binding-card send is still in flight — the user
// gets no card, no log entry, and the binding token they were going
// to receive is orphaned with no observability.
func TestHubWaitDrainsInFlightReplies(t *testing.T) {
	t.Parallel()
	rep := newSlowReplier(30 * time.Millisecond) // shorter than ReplyTimeout
	hub := NewHub(nil, nil, nil, HubConfig{
		ReplyTimeout: 1 * time.Second,
		Logger:       newDiscardLogger(),
	})
	hub.SetOutcomeReplier(rep)

	hub.scheduleReply(db.LarkInstallation{}, InboundMessage{EventID: "e3"},
		DispatchResult{Outcome: OutcomeNeedsBinding}, newDiscardLogger())

	// Wait should block until the reply finishes its 30ms work.
	start := time.Now()
	hub.Wait()
	elapsed := time.Since(start)

	if elapsed < 20*time.Millisecond {
		t.Fatalf("Wait returned in %s; should have blocked until reply completed", elapsed)
	}
	if rep.calls() != 1 {
		t.Fatalf("reply call count = %d; want 1", rep.calls())
	}
	// Reply finished naturally (sleep returned), not via cancellation.
	if rep.ctxErr() != nil {
		t.Errorf("reply ctxErr = %v; want nil (slept normally)", rep.ctxErr())
	}
}

// TestHubNoopReplierInlineNoGoroutine verifies the optimisation: the
// noop replier (used when outbound APIClient isn't configured) runs
// inline, not under a goroutine. This avoids the cost of a goroutine
// per inbound event on a deployment that hasn't wired outbound replies
// yet. Indirectly proven by observing replyWg is not bumped (Wait
// returns immediately without any reply goroutine to drain).
func TestHubNoopReplierInlineNoGoroutine(t *testing.T) {
	t.Parallel()
	hub := NewHub(nil, nil, nil, HubConfig{
		Logger: newDiscardLogger(),
	})
	// hub.replier defaults to noop. Call scheduleReply many times — if
	// it spawned goroutines, Wait would wait at least until those
	// goroutines schedule, but with the fast-path it must return
	// instantly.
	for i := 0; i < 1000; i++ {
		hub.scheduleReply(db.LarkInstallation{}, InboundMessage{EventID: "e"},
			DispatchResult{Outcome: OutcomeNeedsBinding}, newDiscardLogger())
	}
	done := make(chan struct{})
	go func() { hub.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Wait should return immediately when no goroutine replies were scheduled")
	}
}

// TestHubReplyTimeoutDefaultIsUnder3s pins the value the production
// path uses — Lark requires ACK within 3 seconds, and the replier
// runs ON TOP of the dispatch latency, so it must complete strictly
// under 3s even if dispatch took 500ms. Defaulting to 2.5s leaves
// headroom both for outbound HTTP and for shutdown to drain replies
// without hitting the broader ShutdownTimeout.
func TestHubReplyTimeoutDefaultIsUnder3s(t *testing.T) {
	t.Parallel()
	c := HubConfig{}.withDefaults()
	if c.ReplyTimeout <= 0 {
		t.Fatalf("ReplyTimeout default must be > 0; got %s", c.ReplyTimeout)
	}
	if c.ReplyTimeout >= 3*time.Second {
		t.Fatalf("ReplyTimeout default %s is too close to Lark's 3s ACK deadline; outbound HTTP would race ACK", c.ReplyTimeout)
	}
}

// TestHubACKNotBlockedByOutboundReply proves the full integrated
// invariant: even when the OutcomeReplier hangs for far longer than
// the Lark ACK deadline, the connector's data-frame ACK still lands
// on the wire promptly. This is the end-to-end version of the unit
// test above, exercising connector -> Hub.handleEvent -> scheduleReply
// against a fakeWSConn so we can observe the actual binary ACK frame
// timing.
//
// Construct a dispatcher manually that returns OutcomeNeedsBinding
// (the outcome most prone to expensive outbound work — token mint +
// card send). Wire a slowReplier that sleeps 5s. Push one event.
// Assert: an ACK frame appears in conn.writes within 500ms (well
// under Lark's 3s budget).
func TestHubACKNotBlockedByOutboundReply(t *testing.T) {
	t.Parallel()

	conn := newFakeWSConn()
	decoder := FrameDecoderFunc(func(payload []byte, _ db.LarkInstallation) (InboundMessage, bool, error) {
		return InboundMessage{EventID: string(payload)}, true, nil
	})
	c := quietConnector(t, conn, decoder, time.Hour) // disable ping

	// Slow replier that would block ACK if the critical path coupling
	// regressed. 5s sleep, ReplyTimeout 2.5s — replier must be
	// cancelled at ~2.5s and the ACK must NOT have waited for it.
	rep := newSlowReplier(5 * time.Second)
	hub := NewHub(nil, nil, nil, HubConfig{
		ReplyTimeout: 2500 * time.Millisecond,
		Logger:       newDiscardLogger(),
	})
	hub.SetOutcomeReplier(rep)

	emit := func(ctx context.Context, msg InboundMessage) (DispatchResult, error) {
		// Simulate the dispatcher's "successful binding-needed" verdict
		// without standing up the full Dispatcher (concrete struct +
		// service deps). The async reply is the only behaviour under
		// test here, and scheduleReply is what the real handleEvent
		// hands to the replier.
		res := DispatchResult{
			Outcome:      OutcomeNeedsBinding,
			SenderOpenID: "ou_user_42",
		}
		hub.scheduleReply(db.LarkInstallation{}, msg, res, newDiscardLogger())
		return res, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- c.Run(ctx, db.LarkInstallation{AppID: "test_app"}, emit)
	}()

	start := time.Now()
	pushDataFrame(conn, []byte("evt-binding"), "om-binding")

	// The data-frame ACK MUST appear well under Lark's 3s budget.
	if !waitFor(500*time.Millisecond, func() bool {
		for _, w := range conn.snapshot() {
			f, err := UnmarshalFrame(w)
			if err != nil {
				continue
			}
			if f.Method == FrameMethodData && bytes.Contains(f.Payload, []byte(`"code":200`)) {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("data-frame ACK did not land within 500ms; outbound reply blocked the critical path (replier still running? %v)", rep.calls() == 1)
	}
	ackLatency := time.Since(start)
	if ackLatency >= 3*time.Second {
		t.Fatalf("ACK landed in %s, past Lark's 3s deadline", ackLatency)
	}

	// Verify the replier was indeed launched (proves we didn't accidentally
	// short-circuit it).
	select {
	case <-rep.startCh:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("replier never ran; the reply path is silently broken")
	}

	cancel()
	<-done
	hub.WaitWithTimeout(5 * time.Second)
}
