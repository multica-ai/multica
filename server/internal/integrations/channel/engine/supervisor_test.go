package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// fakeStore is the unit-test seam for InstallationStore. Lease state is
// held in memory so a single fake can play both "we hold the lease" and
// "another replica holds the lease" within one test.
type fakeStore struct {
	mu                   sync.Mutex
	installations        []Installation
	listErr              error
	leaseOwner           map[string]string    // installation_id -> lease token
	leaseExpiresAt       map[string]time.Time // installation_id -> expiry
	acquireErr           error
	acquireAfterWriteErr error
	renewErr             error
	listHeldErr          error
	releaseErr           error
	now                  func() time.Time
	acquireCount         int32
	renewCount           int32

	// releaseBlock, if non-nil, makes ReleaseWSLease block until it is
	// closed/sent on OR the caller's ctx fires. Simulates a frozen pool so
	// the bounded-release timeout can be exercised without real infra.
	releaseBlock          chan struct{}
	releaseObservedCtxErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		leaseOwner:     make(map[string]string),
		leaseExpiresAt: make(map[string]time.Time),
		now:            time.Now,
	}
}

func (f *fakeStore) ListActiveInstallations(ctx context.Context) ([]Installation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]Installation, len(f.installations))
	copy(out, f.installations)
	return out, nil
}

func (f *fakeStore) ListHeldWSLeases(_ context.Context, ids []pgtype.UUID) (map[string]struct{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listHeldErr != nil {
		return nil, f.listHeldErr
	}
	held := make(map[string]struct{}, len(ids))
	for _, instID := range ids {
		id := uuidString(instID)
		if _, ok := f.leaseOwner[id]; ok && f.leaseExpiresAt[id].After(f.now()) {
			held[id] = struct{}{}
		}
	}
	return held, nil
}

func (f *fakeStore) TryAcquireWSLease(ctx context.Context, arg AcquireLeaseParams) error {
	atomic.AddInt32(&f.acquireCount, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.acquireErr != nil {
		return f.acquireErr
	}
	id := uuidString(arg.ID)
	owner, hasOwner := f.leaseOwner[id]
	exp := f.leaseExpiresAt[id]
	now := f.now()
	// CAS: accept when no holder, holder expired, or holder is us.
	if !hasOwner || exp.Before(now) || owner == arg.Token {
		f.leaseOwner[id] = arg.Token
		f.leaseExpiresAt[id] = arg.ExpiresAt
		if f.acquireAfterWriteErr != nil {
			err := f.acquireAfterWriteErr
			f.acquireAfterWriteErr = nil
			return err
		}
		return nil
	}
	return ErrLeaseNotAcquired
}

func (f *fakeStore) RenewWSLease(_ context.Context, arg AcquireLeaseParams) error {
	atomic.AddInt32(&f.renewCount, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.renewErr != nil {
		return f.renewErr
	}
	id := uuidString(arg.ID)
	if f.leaseOwner[id] != arg.Token || !f.leaseExpiresAt[id].After(f.now()) {
		return ErrLeaseNotAcquired
	}
	f.leaseExpiresAt[id] = arg.ExpiresAt
	return nil
}

func (f *fakeStore) ReleaseWSLease(ctx context.Context, arg ReleaseLeaseParams) error {
	f.mu.Lock()
	block := f.releaseBlock
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
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
	if f.leaseOwner[id] == arg.Token {
		delete(f.leaseOwner, id)
		delete(f.leaseExpiresAt, id)
	}
	return nil
}

func (f *fakeStore) presetLease(id pgtype.UUID, token string, expires time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.leaseOwner[uuidString(id)] = token
	f.leaseExpiresAt[uuidString(id)] = expires
}

func (f *fakeStore) leaseHolder(id pgtype.UUID) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	owner, ok := f.leaseOwner[uuidString(id)]
	return owner, ok
}

// fakeChannel is a channel.Channel whose Connect behaves per a script
// (default: block until ctx is cancelled). It records connect/disconnect
// counts and captures the injected handler.
type fakeChannel struct {
	mu          sync.Mutex
	typ         channel.Type
	connects    int
	disconnects int
	script      []func(ctx context.Context) error
	handler     channel.InboundHandler
}

func (f *fakeChannel) Type() channel.Type { return f.typ }

func (f *fakeChannel) Connect(ctx context.Context) error {
	f.mu.Lock()
	idx := f.connects
	f.connects++
	var fn func(ctx context.Context) error
	if idx < len(f.script) {
		fn = f.script[idx]
	}
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	<-ctx.Done()
	return nil
}

func (f *fakeChannel) Disconnect(ctx context.Context) error {
	f.mu.Lock()
	f.disconnects++
	f.mu.Unlock()
	return nil
}

func (f *fakeChannel) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	return channel.SendResult{}, nil
}

func (f *fakeChannel) Capabilities() channel.Capability { return channel.CapText }

func (f *fakeChannel) Connects() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connects
}

// fakeRegistry wires a single fakeChannel under channel.TypeFeishu and
// counts how many times the factory built a channel (i.e. supervise-loop
// rebuilds). buildErr, when set, makes the factory fail.
func fakeRegistry(fc *fakeChannel, builds *int32, buildErr error) *channel.Registry {
	reg := channel.NewRegistry()
	reg.Register(channel.TypeFeishu, func(cfg channel.Config) (channel.Channel, error) {
		atomic.AddInt32(builds, 1)
		if buildErr != nil {
			return nil, buildErr
		}
		fc.mu.Lock()
		fc.handler = cfg.Handler
		fc.mu.Unlock()
		return fc, nil
	})
	return reg
}

func uuidFromString(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("scan uuid %q: %v", s, err)
	}
	return u
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// syncBuffer is a mutex-guarded sink for capturing slog output. The
// supervisor logs from several goroutines, so an unguarded bytes.Buffer
// would race under -race.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func fastConfig() Config {
	return Config{
		LeaseTTL:           500 * time.Millisecond,
		LeaseRenewInterval: 20 * time.Millisecond,
		PollInterval:       10 * time.Millisecond,
		MinBackoff:         5 * time.Millisecond,
		MaxBackoff:         50 * time.Millisecond,
		ResetBackoffAfter:  1 * time.Second,
		Logger:             discardLogger(),
	}
}

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

func activeInst(id pgtype.UUID, fingerprint string) Installation {
	return Installation{ID: id, ChannelType: channel.TypeFeishu, Fingerprint: fingerprint, Config: []byte(`{}`)}
}

func TestSupervisorAcquiresLeaseAndConnects(t *testing.T) {
	q := newFakeStore()
	instID := uuidFromString(t, "11111111-1111-1111-1111-111111111111")
	q.installations = []Installation{activeInst(instID, "fp1")}

	fc := &fakeChannel{typ: channel.TypeFeishu}
	var builds int32
	reg := fakeRegistry(fc, &builds, nil)

	sup := NewSupervisor(q, q, reg, nil, fastConfig())
	ctx, cancel := context.WithCancel(context.Background())
	go sup.Run(ctx)

	if !waitFor(300*time.Millisecond, func() bool { return fc.Connects() >= 1 }) {
		t.Fatalf("expected channel to connect; connects=%d", fc.Connects())
	}

	cancel()
	sup.Wait()

	// Lease released after shutdown so another replica can take over.
	if owner, ok := q.leaseHolder(instID); ok {
		t.Fatalf("lease should be released after shutdown, got owner %q", owner)
	}
	// The injected handler is threaded into the built channel.
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if fc.disconnects == 0 {
		t.Fatalf("expected Disconnect to be called after Connect returned")
	}
}

// TestSupervisorSkipsUnregisteredChannelType covers the B2 (MUL-3666) guard:
// an active installation whose channel_type has no registered Factory must be
// left alone — never leased, never Built — because it is driven outside the
// Supervisor (Slack's app-level connector owns one shared connection for all
// its installations). A registered type alongside it still connects normally.
func TestSupervisorSkipsUnregisteredChannelType(t *testing.T) {
	q := newFakeStore()
	feishuID := uuidFromString(t, "2a111111-1111-1111-1111-111111111111")
	slackID := uuidFromString(t, "2b222222-2222-2222-2222-222222222222")
	q.installations = []Installation{
		activeInst(feishuID, "fp1"),
		{ID: slackID, ChannelType: channel.Type("slack"), Fingerprint: "fp2", Config: []byte(`{}`)},
	}

	fc := &fakeChannel{typ: channel.TypeFeishu}
	var builds int32
	reg := fakeRegistry(fc, &builds, nil) // registers ONLY TypeFeishu

	sup := NewSupervisor(q, q, reg, nil, fastConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sup.Run(ctx)

	if !waitFor(300*time.Millisecond, func() bool { return fc.Connects() >= 1 }) {
		t.Fatalf("registered feishu installation should connect; connects=%d", fc.Connects())
	}
	// Give the supervisor a few sweep cycles to (not) act on the slack row.
	time.Sleep(50 * time.Millisecond)
	if owner, ok := q.leaseHolder(slackID); ok {
		t.Fatalf("unregistered channel type must never be leased, got owner %q", owner)
	}
	if got := atomic.LoadInt32(&builds); got != 1 {
		t.Fatalf("only the registered feishu channel should be built, builds=%d", got)
	}
}

func TestSupervisorInjectsHandler(t *testing.T) {
	q := newFakeStore()
	instID := uuidFromString(t, "1a111111-1111-1111-1111-111111111111")
	q.installations = []Installation{activeInst(instID, "fp1")}

	fc := &fakeChannel{typ: channel.TypeFeishu}
	var builds int32
	reg := fakeRegistry(fc, &builds, nil)

	var called atomic.Bool
	handler := func(ctx context.Context, msg channel.InboundMessage) error {
		called.Store(true)
		return nil
	}
	sup := NewSupervisor(q, q, reg, handler, fastConfig())
	ctx, cancel := context.WithCancel(context.Background())
	go sup.Run(ctx)

	if !waitFor(300*time.Millisecond, func() bool { return fc.Connects() >= 1 }) {
		t.Fatalf("channel never connected")
	}
	fc.mu.Lock()
	h := fc.handler
	fc.mu.Unlock()
	if h == nil {
		t.Fatalf("expected handler injected into channel.Config.Handler")
	}
	_ = h(context.Background(), channel.InboundMessage{})
	if !called.Load() {
		t.Fatalf("injected handler was not the supervisor's handler")
	}

	cancel()
	sup.Wait()
}

func TestSupervisorSkipsWhenAnotherReplicaHoldsLease(t *testing.T) {
	q := newFakeStore()
	instID := uuidFromString(t, "22222222-2222-2222-2222-222222222222")
	q.installations = []Installation{activeInst(instID, "fp1")}
	q.presetLease(instID, "other-replica", time.Now().Add(10*time.Second))

	fc := &fakeChannel{typ: channel.TypeFeishu}
	var builds int32
	reg := fakeRegistry(fc, &builds, nil)

	sup := NewSupervisor(q, q, reg, nil, fastConfig())
	ctx, cancel := context.WithCancel(context.Background())
	go sup.Run(ctx)

	// Give the supervisor time to try and fail to acquire.
	time.Sleep(120 * time.Millisecond)
	if fc.Connects() != 0 {
		t.Fatalf("channel should not connect while another replica holds lease; connects=%d", fc.Connects())
	}
	if owner, _ := q.leaseHolder(instID); owner != "other-replica" {
		t.Fatalf("foreign lease should be untouched, got %q", owner)
	}
	if got := atomic.LoadInt32(&q.acquireCount); got != 0 {
		t.Fatalf("loser should rely on batched held-key sweeps, acquire attempts=%d", got)
	}

	// Revoking an installation this node only observed as contended must prune
	// its takeover-timing state; otherwise repeated install/revoke cycles leak.
	q.mu.Lock()
	q.installations = nil
	q.mu.Unlock()
	if !waitFor(200*time.Millisecond, func() bool {
		sup.mu.Lock()
		defer sup.mu.Unlock()
		_, ok := sup.contendedSince[uuidString(instID)]
		return !ok
	}) {
		t.Fatalf("revoked contended installation was not pruned")
	}

	cancel()
	sup.Wait()
}

func TestSupervisorTwoNodesHaveExactlyOneOwner(t *testing.T) {
	q := newFakeStore()
	instID := uuidFromString(t, "23232323-2323-2323-2323-232323232323")
	q.installations = []Installation{activeInst(instID, "fp1")}

	first := &fakeChannel{typ: channel.TypeFeishu}
	second := &fakeChannel{typ: channel.TypeFeishu}
	var firstBuilds, secondBuilds int32
	supA := NewSupervisor(q, q, fakeRegistry(first, &firstBuilds, nil), nil, fastConfig())
	supB := NewSupervisor(q, q, fakeRegistry(second, &secondBuilds, nil), nil, fastConfig())
	ctx, cancel := context.WithCancel(context.Background())
	go supA.Run(ctx)
	go supB.Run(ctx)

	if !waitFor(300*time.Millisecond, func() bool { return first.Connects()+second.Connects() == 1 }) {
		t.Fatalf("expected one owner, connects A=%d B=%d", first.Connects(), second.Connects())
	}
	time.Sleep(100 * time.Millisecond)
	if got := first.Connects() + second.Connects(); got != 1 {
		t.Fatalf("both nodes connected to one installation: connects=%d", got)
	}

	cancel()
	supA.Wait()
	supB.Wait()
}

func TestSupervisorAcquireResponseLossWaitsForExpiryBeforeRetry(t *testing.T) {
	q := newFakeStore()
	q.acquireAfterWriteErr = errors.New("response lost")
	instID := uuidFromString(t, "24242424-2424-2424-2424-242424242424")
	q.installations = []Installation{activeInst(instID, "fp1")}
	fc := &fakeChannel{typ: channel.TypeFeishu}
	var builds int32
	sup := NewSupervisor(q, q, fakeRegistry(fc, &builds, nil), nil, fastConfig())
	ctx, cancel := context.WithCancel(context.Background())
	go sup.Run(ctx)
	defer func() {
		cancel()
		sup.Wait()
	}()

	time.Sleep(150 * time.Millisecond)
	if got := fc.Connects(); got != 0 {
		t.Fatalf("must not connect when acquire acknowledgement was lost; connects=%d", got)
	}
	if got := atomic.LoadInt32(&q.acquireCount); got != 1 {
		t.Fatalf("held key should suppress retries before expiry; attempts=%d", got)
	}
	if !waitFor(800*time.Millisecond, func() bool { return fc.Connects() == 1 }) {
		t.Fatalf("expected takeover after uncertain lease expired")
	}
}

func TestSupervisorReclaimsLeaseAfterExpiry(t *testing.T) {
	q := newFakeStore()
	instID := uuidFromString(t, "33333333-3333-3333-3333-333333333333")
	q.installations = []Installation{activeInst(instID, "fp1")}
	q.presetLease(instID, "other-replica", time.Now().Add(80*time.Millisecond))

	fc := &fakeChannel{typ: channel.TypeFeishu}
	var builds int32
	reg := fakeRegistry(fc, &builds, nil)

	sup := NewSupervisor(q, q, reg, nil, fastConfig())
	ctx, cancel := context.WithCancel(context.Background())
	go sup.Run(ctx)

	if !waitFor(600*time.Millisecond, func() bool { return fc.Connects() >= 1 }) {
		t.Fatalf("expected to reclaim lease after expiry; connects=%d", fc.Connects())
	}

	cancel()
	sup.Wait()
}

func TestSupervisorReapsSupervisorWhenRevoked(t *testing.T) {
	q := newFakeStore()
	instID := uuidFromString(t, "44444444-4444-4444-4444-444444444444")
	q.installations = []Installation{activeInst(instID, "fp1")}

	fc := &fakeChannel{typ: channel.TypeFeishu}
	var builds int32
	reg := fakeRegistry(fc, &builds, nil)

	sup := NewSupervisor(q, q, reg, nil, fastConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sup.Run(ctx)

	if !waitFor(300*time.Millisecond, func() bool { return fc.Connects() >= 1 }) {
		t.Fatalf("channel never connected")
	}

	// Revoke: drop from the active list.
	q.mu.Lock()
	q.installations = nil
	q.mu.Unlock()

	// The supervisor exits and releases its lease.
	if !waitFor(400*time.Millisecond, func() bool {
		_, held := q.leaseHolder(instID)
		return !held
	}) {
		t.Fatalf("expected lease released after revoke")
	}
}

func TestSupervisorRestartsOnCredentialsRotation(t *testing.T) {
	q := newFakeStore()
	instID := uuidFromString(t, "55555555-5555-5555-5555-555555555555")
	q.installations = []Installation{activeInst(instID, "fp-one")}

	fc := &fakeChannel{typ: channel.TypeFeishu}
	var builds int32
	reg := fakeRegistry(fc, &builds, nil)

	sup := NewSupervisor(q, q, reg, nil, fastConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sup.Run(ctx)

	if !waitFor(300*time.Millisecond, func() bool { return atomic.LoadInt32(&builds) >= 1 }) {
		t.Fatalf("channel never built")
	}
	buildsBefore := atomic.LoadInt32(&builds)

	// Rotate credentials: fingerprint changes -> supervisor restart -> rebuild.
	q.mu.Lock()
	q.installations[0].Fingerprint = "fp-two"
	q.mu.Unlock()

	if !waitFor(500*time.Millisecond, func() bool { return atomic.LoadInt32(&builds) > buildsBefore }) {
		t.Fatalf("expected rebuild after rotation; builds before=%d after=%d", buildsBefore, atomic.LoadInt32(&builds))
	}
}

func TestSupervisorRotationReplacesConnectionInSameSweep(t *testing.T) {
	q := newFakeStore()
	instID := uuidFromString(t, "56565656-5656-5656-5656-565656565656")
	q.installations = []Installation{activeInst(instID, "fp-one")}

	fc := &fakeChannel{typ: channel.TypeFeishu}
	var builds int32
	cfg := fastConfig()
	cfg.LeaseTTL = 6 * time.Second
	cfg.LeaseRenewInterval = 3 * time.Second
	cfg.PollInterval = 2 * time.Second
	cfg.LeaseErrorRetryInterval = 100 * time.Millisecond
	cfg.LeaseExpirySafetyMargin = 500 * time.Millisecond
	cfg.RotationWaitTimeout = 500 * time.Millisecond
	sup := NewSupervisor(q, q, fakeRegistry(fc, &builds, nil), nil, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	go sup.Run(ctx)
	defer func() {
		cancel()
		sup.Wait()
	}()
	if !waitFor(300*time.Millisecond, func() bool { return atomic.LoadInt32(&builds) == 1 }) {
		t.Fatalf("initial channel never built")
	}

	q.mu.Lock()
	q.installations[0].Fingerprint = "fp-two"
	q.mu.Unlock()
	started := time.Now()
	sup.sweep(ctx)
	if !waitFor(500*time.Millisecond, func() bool { return atomic.LoadInt32(&builds) == 2 }) {
		t.Fatalf("rotation waited for the next %s poll instead of restarting in the same sweep", cfg.PollInterval)
	}
	if elapsed := time.Since(started); elapsed >= cfg.PollInterval {
		t.Fatalf("rotation replacement took %s, poll interval is %s", elapsed, cfg.PollInterval)
	}
}

func TestSupervisorDoesNotRestartOnUnchangedRow(t *testing.T) {
	q := newFakeStore()
	instID := uuidFromString(t, "66666666-6666-6666-6666-666666666666")
	q.installations = []Installation{activeInst(instID, "stable")}

	fc := &fakeChannel{typ: channel.TypeFeishu}
	var builds int32
	reg := fakeRegistry(fc, &builds, nil)

	sup := NewSupervisor(q, q, reg, nil, fastConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sup.Run(ctx)

	if !waitFor(300*time.Millisecond, func() bool { return atomic.LoadInt32(&builds) >= 1 }) {
		t.Fatalf("channel never built")
	}
	// Several sweeps observe the same fingerprint; no rebuild should happen.
	time.Sleep(150 * time.Millisecond)
	if got := atomic.LoadInt32(&builds); got != 1 {
		t.Fatalf("expected exactly 1 build for an unchanged row, got %d", got)
	}
}

func TestSupervisorBacksOffOnBuildError(t *testing.T) {
	q := newFakeStore()
	instID := uuidFromString(t, "77777777-7777-7777-7777-777777777777")
	q.installations = []Installation{activeInst(instID, "fp1")}

	fc := &fakeChannel{typ: channel.TypeFeishu}
	var builds int32
	reg := fakeRegistry(fc, &builds, errors.New("boom"))

	sup := NewSupervisor(q, q, reg, nil, fastConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sup.Run(ctx)

	// It keeps retrying (building) under backoff, and releases the lease
	// between attempts so it never wedges holding the lease.
	if !waitFor(400*time.Millisecond, func() bool { return atomic.LoadInt32(&builds) >= 2 }) {
		t.Fatalf("expected repeated build attempts under backoff; builds=%d", atomic.LoadInt32(&builds))
	}
	if fc.Connects() != 0 {
		t.Fatalf("channel should never connect when build fails; connects=%d", fc.Connects())
	}
}

func TestSupervisorLeaseLossCancelsConnection(t *testing.T) {
	q := newFakeStore()
	instID := uuidFromString(t, "88888888-8888-8888-8888-888888888888")
	q.installations = []Installation{activeInst(instID, "fp1")}

	connectReturned := make(chan struct{}, 1)
	fc := &fakeChannel{
		typ: channel.TypeFeishu,
		script: []func(ctx context.Context) error{
			func(ctx context.Context) error {
				<-ctx.Done()
				connectReturned <- struct{}{}
				return ctx.Err()
			},
		},
	}
	var builds int32
	reg := fakeRegistry(fc, &builds, nil)

	sup := NewSupervisor(q, q, reg, nil, fastConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sup.Run(ctx)

	if !waitFor(300*time.Millisecond, func() bool { return fc.Connects() >= 1 }) {
		t.Fatalf("channel never connected")
	}

	// A thief steals the lease with a far-future expiry; the renewer's CAS
	// fails and must cancel the running Connect.
	q.presetLease(instID, "thief", time.Now().Add(10*time.Second))

	select {
	case <-connectReturned:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("lease loss did not cancel the running connection")
	}
}

func TestSupervisorClearedLeaseKeyCancelsOldOwner(t *testing.T) {
	q := newFakeStore()
	instID := uuidFromString(t, "8b8b8b8b-8b8b-8b8b-8b8b-8b8b8b8b8b8b")
	q.installations = []Installation{activeInst(instID, "fp1")}

	connectReturned := make(chan struct{}, 1)
	fc := &fakeChannel{
		typ: channel.TypeFeishu,
		script: []func(context.Context) error{func(ctx context.Context) error {
			<-ctx.Done()
			connectReturned <- struct{}{}
			return ctx.Err()
		}},
	}
	var builds int32
	sup := NewSupervisor(q, q, fakeRegistry(fc, &builds, nil), nil, fastConfig())
	ctx, cancel := context.WithCancel(context.Background())
	go sup.Run(ctx)
	defer func() {
		cancel()
		sup.Wait()
	}()
	if !waitFor(300*time.Millisecond, func() bool { return fc.Connects() == 1 }) {
		t.Fatalf("channel never connected")
	}

	// Simulate Redis losing/clearing the key. Strict renewal must treat absence
	// as lease loss instead of recreating the key from the old owner.
	q.mu.Lock()
	delete(q.leaseOwner, uuidString(instID))
	delete(q.leaseExpiresAt, uuidString(instID))
	q.mu.Unlock()
	select {
	case <-connectReturned:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("cleared lease key did not tear down the old owner")
	}
}

func TestSupervisorTransientRenewalErrorRecoversBeforeDeadline(t *testing.T) {
	q := newFakeStore()
	instID := uuidFromString(t, "89898989-8989-8989-8989-898989898989")
	q.installations = []Installation{activeInst(instID, "fp1")}
	fc := &fakeChannel{typ: channel.TypeFeishu}
	var builds int32
	sup := NewSupervisor(q, q, fakeRegistry(fc, &builds, nil), nil, fastConfig())
	ctx, cancel := context.WithCancel(context.Background())
	go sup.Run(ctx)
	defer func() {
		cancel()
		sup.Wait()
	}()
	if !waitFor(300*time.Millisecond, func() bool { return fc.Connects() == 1 }) {
		t.Fatalf("channel never connected")
	}

	q.mu.Lock()
	q.renewErr = errors.New("temporary redis timeout")
	q.mu.Unlock()
	if !waitFor(200*time.Millisecond, func() bool { return atomic.LoadInt32(&q.renewCount) >= 2 }) {
		t.Fatalf("renewal errors were not retried quickly")
	}
	q.mu.Lock()
	q.renewErr = nil
	q.mu.Unlock()
	before := atomic.LoadInt32(&q.renewCount)
	if !waitFor(200*time.Millisecond, func() bool { return atomic.LoadInt32(&q.renewCount) > before }) {
		t.Fatalf("renewal did not recover")
	}
	if got := fc.Connects(); got != 1 {
		t.Fatalf("transient error should not reconnect the channel; connects=%d", got)
	}
}

func TestSupervisorOneWayRedisPartitionDisconnectsBeforeConfirmedExpiry(t *testing.T) {
	q := newFakeStore()
	q.releaseBlock = make(chan struct{})
	instID := uuidFromString(t, "8a8a8a8a-8a8a-8a8a-8a8a-8a8a8a8a8a8a")
	q.installations = []Installation{activeInst(instID, "fp1")}
	connectReturned := make(chan struct{}, 1)
	fc := &fakeChannel{
		typ: channel.TypeFeishu,
		script: []func(context.Context) error{func(ctx context.Context) error {
			<-ctx.Done()
			connectReturned <- struct{}{}
			return ctx.Err()
		}},
	}
	var builds int32
	cfg := fastConfig()
	cfg.LeaseReleaseTimeout = 100 * time.Millisecond
	sup := NewSupervisor(q, q, fakeRegistry(fc, &builds, nil), nil, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	go sup.Run(ctx)
	if !waitFor(300*time.Millisecond, func() bool { return fc.Connects() == 1 }) {
		t.Fatalf("channel never connected")
	}

	q.mu.Lock()
	q.renewErr = errors.New("partitioned from redis")
	q.mu.Unlock()
	select {
	case <-connectReturned:
	case <-time.After(800 * time.Millisecond):
		t.Fatalf("partitioned owner stayed connected beyond confirmed lease")
	}
	q.mu.Lock()
	expiresAt := q.leaseExpiresAt[uuidString(instID)]
	q.mu.Unlock()
	if !time.Now().Before(expiresAt) {
		t.Fatalf("connection was not cancelled before backend lease expiry: expires_at=%s", expiresAt)
	}
	close(q.releaseBlock)
	cancel()
	sup.Wait()
}

func TestSupervisorReleaseLeaseBoundedByTimeout(t *testing.T) {
	q := newFakeStore()
	q.releaseBlock = make(chan struct{}) // never closed; release always hits ctx.Done
	instID := uuidFromString(t, "99999999-9999-9999-9999-999999999999")
	q.installations = []Installation{activeInst(instID, "fp1")}

	fc := &fakeChannel{typ: channel.TypeFeishu}
	var builds int32
	reg := fakeRegistry(fc, &builds, nil)

	cfg := fastConfig()
	cfg.LeaseReleaseTimeout = 40 * time.Millisecond
	sup := NewSupervisor(q, q, reg, nil, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	go sup.Run(ctx)

	if !waitFor(300*time.Millisecond, func() bool { return fc.Connects() >= 1 }) {
		t.Fatalf("channel never connected")
	}

	cancel()
	if !sup.WaitWithTimeout(2 * time.Second) {
		t.Fatalf("shutdown should complete despite a frozen release (bounded by timeout)")
	}
	q.mu.Lock()
	observed := q.releaseObservedCtxErr
	q.mu.Unlock()
	if !errors.Is(observed, context.DeadlineExceeded) {
		t.Fatalf("expected release to observe DeadlineExceeded, got %v", observed)
	}
}

func TestSupervisorWaitWithTimeoutReturnsFalseWhenStuck(t *testing.T) {
	q := newFakeStore()
	q.releaseBlock = make(chan struct{}) // never closed
	instID := uuidFromString(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	q.installations = []Installation{activeInst(instID, "fp1")}

	fc := &fakeChannel{typ: channel.TypeFeishu}
	var builds int32
	reg := fakeRegistry(fc, &builds, nil)

	cfg := fastConfig()
	cfg.LeaseReleaseTimeout = 10 * time.Second // longer than the WaitWithTimeout below
	sup := NewSupervisor(q, q, reg, nil, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	go sup.Run(ctx)

	if !waitFor(300*time.Millisecond, func() bool { return fc.Connects() >= 1 }) {
		t.Fatalf("channel never connected")
	}

	cancel()
	if sup.WaitWithTimeout(80 * time.Millisecond) {
		t.Fatalf("expected WaitWithTimeout to report timeout while release is wedged")
	}
	close(q.releaseBlock) // let the goroutine finish so the test cleans up
	sup.Wait()
}

// TestSupervisorRotationStaleReleaseDoesNotClearSuccessorLease proves the
// per-supervisor token fence: an old supervisor's post-cancel release must
// not delete the lease a rotation successor just acquired with a different
// token. We drive it through the public API by rotating credentials while
// the old connection is held open, then asserting a live lease remains.
func TestSupervisorRotationStaleReleaseDoesNotClearSuccessorLease(t *testing.T) {
	q := newFakeStore()
	instID := uuidFromString(t, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	q.installations = []Installation{activeInst(instID, "fp-one")}

	fc := &fakeChannel{typ: channel.TypeFeishu}
	var builds int32
	reg := fakeRegistry(fc, &builds, nil)

	sup := NewSupervisor(q, q, reg, nil, fastConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sup.Run(ctx)

	if !waitFor(300*time.Millisecond, func() bool { return atomic.LoadInt32(&builds) >= 1 }) {
		t.Fatalf("channel never built")
	}

	// Rotate: old supervisor cancelled, successor starts with a new token.
	q.mu.Lock()
	q.installations[0].Fingerprint = "fp-two"
	q.mu.Unlock()

	// After the dust settles, a live lease should remain held by the
	// successor (token ending in a higher gen) — not cleared by the
	// predecessor's stale release.
	if !waitFor(500*time.Millisecond, func() bool { return atomic.LoadInt32(&builds) >= 2 }) {
		t.Fatalf("successor never built after rotation")
	}
	if !waitFor(300*time.Millisecond, func() bool {
		owner, held := q.leaseHolder(instID)
		return held && owner != ""
	}) {
		t.Fatalf("successor lease was cleared by a stale predecessor release")
	}
}

func TestSupervisorConfigDefaults(t *testing.T) {
	store := newFakeStore()
	sup := NewSupervisor(store, store, channel.NewRegistry(), nil, Config{})
	if sup.cfg.LeaseTTL <= 0 || sup.cfg.LeaseRenewInterval <= 0 || sup.cfg.PollInterval <= 0 {
		t.Fatalf("lifecycle intervals must default to positive values")
	}
	if sup.cfg.LeaseRenewInterval >= sup.cfg.LeaseTTL {
		t.Fatalf("renew interval must be well under the TTL")
	}
	if sup.ShutdownTimeout() <= 0 {
		t.Fatalf("shutdown timeout must default to a positive value")
	}
	if sup.cfg.RotationWaitTimeout <= 0 {
		t.Fatalf("rotation wait timeout must default to a positive value")
	}
	if sup.cfg.Now == nil || sup.cfg.Logger == nil {
		t.Fatalf("Now and Logger must be defaulted")
	}
	if sup.NodeID() == "" {
		t.Fatalf("node id must be assigned")
	}
}

func TestSupervisorPartialTTLConfigDerivesSafeIntervals(t *testing.T) {
	store := newFakeStore()
	sup := NewSupervisor(store, store, channel.NewRegistry(), nil, Config{LeaseTTL: 30 * time.Second})
	if sup.cfg.LeaseRenewInterval != 10*time.Second {
		t.Fatalf("renew interval = %s, want TTL/3", sup.cfg.LeaseRenewInterval)
	}
	if sup.cfg.PollInterval != 5*time.Second {
		t.Fatalf("poll interval = %s, want renew/2", sup.cfg.PollInterval)
	}
}

func TestSupervisorRejectsUnsafeLeaseIntervals(t *testing.T) {
	store := newFakeStore()
	defer func() {
		if recover() == nil {
			t.Fatalf("expected invalid poll/renew/ttl relation to panic")
		}
	}()
	NewSupervisor(store, store, channel.NewRegistry(), nil, Config{
		LeaseTTL: 30 * time.Second, LeaseRenewInterval: 20 * time.Second, PollInterval: 25 * time.Second,
		LeaseErrorRetryInterval: time.Second, LeaseExpirySafetyMargin: time.Second,
	})
}

// The connection-exit log must not carry a field named lease_token. The
// lease token is an internal CAS marker rather than a credential, but a
// plaintext `lease_token=` in the log reads as a leaked channel credential
// to anyone tailing it — that misread is what GH #7132 reported. node_id
// and lease_gen are the same information under a name that does not invite
// the false positive, so this asserts the field NAME is gone and the
// diagnostic fields survived, not that anything is secret.
func TestSupervisorExitLogOmitsLeaseTokenField(t *testing.T) {
	q := newFakeStore()
	instID := uuidFromString(t, "cccccccc-cccc-cccc-cccc-cccccccccccc")
	q.installations = []Installation{activeInst(instID, "fp1")}

	// First Connect fails immediately, driving the exact "connection exited
	// with error" branch the report quoted; later attempts park on ctx.
	fc := &fakeChannel{
		typ: channel.TypeFeishu,
		script: []func(ctx context.Context) error{
			func(ctx context.Context) error { return errors.New("read message: websocket: close 1006") },
		},
	}
	var builds int32
	reg := fakeRegistry(fc, &builds, nil)

	var logs syncBuffer
	cfg := fastConfig()
	cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))

	sup := NewSupervisor(q, q, reg, nil, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sup.Run(ctx)

	if !waitFor(time.Second, func() bool {
		return strings.Contains(logs.String(), "connection exited with error")
	}) {
		t.Fatalf("expected the exit-with-error log; got %q", logs.String())
	}
	cancel()
	sup.Wait()

	out := logs.String()
	if strings.Contains(out, "lease_token") {
		t.Errorf("supervisor logs must not carry a lease_token field; got %q", out)
	}
	for _, field := range []string{"installation_id=", "node_id=", "lease_gen="} {
		if !strings.Contains(out, field) {
			t.Errorf("supervisor logs lost diagnostic field %s; got %q", field, out)
		}
	}
}
