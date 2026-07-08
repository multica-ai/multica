package engine

import (
	"context"
	"errors"
	"io"
	"log/slog"
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
	mu             sync.Mutex
	installations  []Installation
	listErr        error
	leaseOwner     map[string]string    // installation_id -> lease token
	leaseExpiresAt map[string]time.Time // installation_id -> expiry
	acquireErr     error
	releaseErr     error
	now            func() time.Time
	acquireCount   int32

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

func (f *fakeStore) AcquireWSLease(ctx context.Context, arg AcquireLeaseParams) error {
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
		return nil
	}
	return ErrLeaseNotAcquired
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

	sup := NewSupervisor(q, reg, nil, fastConfig())
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

	sup := NewSupervisor(q, reg, nil, fastConfig())
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
	sup := NewSupervisor(q, reg, handler, fastConfig())
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

	sup := NewSupervisor(q, reg, nil, fastConfig())
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

	cancel()
	sup.Wait()
}

func TestSupervisorReclaimsLeaseAfterExpiry(t *testing.T) {
	q := newFakeStore()
	instID := uuidFromString(t, "33333333-3333-3333-3333-333333333333")
	q.installations = []Installation{activeInst(instID, "fp1")}
	q.presetLease(instID, "other-replica", time.Now().Add(80*time.Millisecond))

	fc := &fakeChannel{typ: channel.TypeFeishu}
	var builds int32
	reg := fakeRegistry(fc, &builds, nil)

	sup := NewSupervisor(q, reg, nil, fastConfig())
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

	sup := NewSupervisor(q, reg, nil, fastConfig())
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

	sup := NewSupervisor(q, reg, nil, fastConfig())
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

func TestSupervisorDoesNotRestartOnUnchangedRow(t *testing.T) {
	q := newFakeStore()
	instID := uuidFromString(t, "66666666-6666-6666-6666-666666666666")
	q.installations = []Installation{activeInst(instID, "stable")}

	fc := &fakeChannel{typ: channel.TypeFeishu}
	var builds int32
	reg := fakeRegistry(fc, &builds, nil)

	sup := NewSupervisor(q, reg, nil, fastConfig())
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

	sup := NewSupervisor(q, reg, nil, fastConfig())
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

	sup := NewSupervisor(q, reg, nil, fastConfig())
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
	sup := NewSupervisor(q, reg, nil, cfg)
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
	sup := NewSupervisor(q, reg, nil, cfg)
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

	sup := NewSupervisor(q, reg, nil, fastConfig())
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
	sup := NewSupervisor(newFakeStore(), channel.NewRegistry(), nil, Config{})
	if sup.cfg.LeaseTTL <= 0 || sup.cfg.LeaseRenewInterval <= 0 || sup.cfg.PollInterval <= 0 {
		t.Fatalf("lifecycle intervals must default to positive values")
	}
	if sup.cfg.LeaseRenewInterval >= sup.cfg.LeaseTTL {
		t.Fatalf("renew interval must be well under the TTL")
	}
	if sup.ShutdownTimeout() <= 0 {
		t.Fatalf("shutdown timeout must default to a positive value")
	}
	if sup.cfg.Now == nil || sup.cfg.Logger == nil {
		t.Fatalf("Now and Logger must be defaulted")
	}
	if sup.NodeID() == "" {
		t.Fatalf("node id must be assigned")
	}
}
