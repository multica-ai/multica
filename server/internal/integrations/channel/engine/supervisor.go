package engine

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	mathrand "math/rand/v2"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
)

// Installation is the channel-agnostic view of one channel_installation
// row the Supervisor needs to drive a connection. It is intentionally
// minimal: the engine never reads platform credentials directly — it
// hands Config to the registry factory, which decodes what it needs — and
// it never branches on ChannelType beyond using it to pick the factory.
type Installation struct {
	// ID is the channel_installation primary key. It is the lease key and
	// the supervisors-map key (one supervisor goroutine per ID).
	ID pgtype.UUID

	// ChannelType selects the registry Factory that builds this row's
	// Channel ("feishu", "slack", …).
	ChannelType channel.Type

	// Fingerprint condenses the credential-bearing config into an opaque
	// string. Two rows with equal fingerprints are interchangeable to the
	// supervisor; any change between sweeps tears the running connection
	// down and rebuilds it so a re-installed channel (fresh app_id /
	// secret / region) is picked up instead of running indefinitely
	// against stale credentials. The store computes it; the engine treats
	// it as opaque.
	Fingerprint string

	// Config is the platform credential/config blob, passed verbatim to
	// the registry Factory as channel.Config.Raw. The engine never reads
	// inside it.
	Config json.RawMessage
}

// AcquireLeaseParams fences the WS supervisor lease for an installation.
// The store performs the CAS: grant when the row is unleased, expired, or
// already held by Token (renewal); otherwise report it held elsewhere via
// ErrLeaseNotAcquired.
type AcquireLeaseParams struct {
	ID        pgtype.UUID
	Token     string
	ExpiresAt time.Time
}

// ReleaseLeaseParams releases a WS supervisor lease the caller still
// holds. The store must fence on Token so a stale release from a rotation
// predecessor cannot clobber a successor's freshly acquired lease.
type ReleaseLeaseParams struct {
	ID    pgtype.UUID
	Token string
}

// ErrLeaseNotAcquired is the sentinel a store returns from AcquireWSLease
// when the CAS predicate did not match — i.e. another replica (or an
// in-process predecessor mid-rotation) holds a live lease. The Supervisor
// treats it as "not ours yet, retry later", distinct from a transport
// error. Stores wrap their backend's no-rows signal into this.
var ErrLeaseNotAcquired = errors.New("engine: ws lease held elsewhere")

// InstallationStore is the narrow data seam the Supervisor needs:
// enumerate active installations across every channel type and manage the
// per-installation WS lease. The application backs it with the generalized
// channel_* tables; tests substitute a fake.
type InstallationStore interface {
	// ListActiveInstallations returns every active installation across ALL
	// channel types. There is no per-platform filter here — that hard-coded
	// "feishu" was the whole limitation MUL-3620 removes.
	ListActiveInstallations(ctx context.Context) ([]Installation, error)

	// AcquireWSLease grants or renews the lease, or returns
	// ErrLeaseNotAcquired when it is held elsewhere.
	AcquireWSLease(ctx context.Context, arg AcquireLeaseParams) error

	// ReleaseWSLease releases a lease the caller holds (token-fenced).
	ReleaseWSLease(ctx context.Context, arg ReleaseLeaseParams) error
}

// Config tunes the Supervisor's lifecycle loops. All fields have sensible
// production defaults via withDefaults; tests typically set Now and Logger
// to inject determinism.
type Config struct {
	// LeaseTTL is how long a successful AcquireWSLease grant is valid
	// before another replica may steal it. Renewals happen on the tighter
	// LeaseRenewInterval; the gap absorbs transient DB blips.
	LeaseTTL time.Duration

	// LeaseRenewInterval is the cadence at which the Supervisor re-acquires
	// leases it already owns. MUST be substantially less than LeaseTTL so a
	// single missed renewal does not yield the lease.
	LeaseRenewInterval time.Duration

	// PollInterval is how often the Supervisor scans for installations to
	// take over (new ones, or ones whose lease expired on another replica).
	PollInterval time.Duration

	// MinBackoff / MaxBackoff bound the per-installation reconnect
	// schedule: start at MinBackoff, double after each consecutive failure
	// (capped at MaxBackoff), reset on any connection that lived at least
	// ResetBackoffAfter.
	MinBackoff        time.Duration
	MaxBackoff        time.Duration
	ResetBackoffAfter time.Duration

	// LeaseReleaseTimeout caps a single ReleaseWSLease call. The release
	// runs on a fresh context (the parent ctx is already cancelled by the
	// time we release on shutdown), so without a deadline a frozen pool
	// could hang shutdown indefinitely. On timeout the lease falls back to
	// natural TTL expiry on the next replica.
	LeaseReleaseTimeout time.Duration

	// DisconnectTimeout caps a single Channel.Disconnect call made after a
	// connection ends, for the same reason as LeaseReleaseTimeout.
	DisconnectTimeout time.Duration

	// ShutdownTimeout bounds how long Wait blocks for supervisor goroutines
	// after their parent ctx is cancelled. Wait itself does not enforce it;
	// callers pass it to WaitWithTimeout. Exposed so boot and tests share a
	// default.
	ShutdownTimeout time.Duration

	// Now returns the current time. Injected for tests; production uses
	// time.Now.
	Now func() time.Time

	// Logger optional; defaults to slog.Default.
	Logger *slog.Logger
}

func (c Config) withDefaults() Config {
	if c.LeaseTTL == 0 {
		c.LeaseTTL = 90 * time.Second
	}
	if c.LeaseRenewInterval == 0 {
		c.LeaseRenewInterval = 30 * time.Second
	}
	if c.PollInterval == 0 {
		c.PollInterval = 30 * time.Second
	}
	if c.MinBackoff == 0 {
		c.MinBackoff = 2 * time.Second
	}
	if c.MaxBackoff == 0 {
		c.MaxBackoff = 60 * time.Second
	}
	if c.ResetBackoffAfter == 0 {
		c.ResetBackoffAfter = 60 * time.Second
	}
	if c.LeaseReleaseTimeout == 0 {
		c.LeaseReleaseTimeout = 5 * time.Second
	}
	if c.DisconnectTimeout == 0 {
		c.DisconnectTimeout = 5 * time.Second
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = 15 * time.Second
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// Supervisor owns the per-installation supervisor goroutines that keep a
// long-running connection per active installation, across every channel
// type. It enforces the multi-replica safety rule via the WS lease CAS —
// at most one Supervisor globally holds the lease for any installation, so
// duplicate event consumption across replicas is impossible.
//
// It is the channel-agnostic generalization of lark.Hub: where the Hub
// drove a Feishu-only EventConnector built by a ConnectorFactory, the
// Supervisor drives any channel.Channel built by the channel.Registry, and
// enumerates installations of every channel type rather than just feishu.
//
// Lifecycle:
//
//	sup := NewSupervisor(store, registry, handler, engine.Config{})
//	go sup.Run(ctx)             // returns when ctx is cancelled
//	... ctx cancellation triggers ...
//	sup.Wait()                  // joins on every per-installation goroutine
type Supervisor struct {
	store    InstallationStore
	registry *channel.Registry
	handler  channel.InboundHandler
	cfg      Config

	// nodeID is the per-process lease ownership token. AcquireWSLease
	// treats matching tokens as "this is us, renew", so a stable nodeID
	// keeps renewals from ping-ponging between replicas.
	nodeID string

	mu sync.Mutex
	// supervisors keys each in-flight supervisor goroutine by
	// installation_id, alongside the credentials fingerprint the channel
	// was built with. When the row's fingerprint drifts (re-install), the
	// next sweep tears the connection down and rebuilds it with fresh
	// credentials.
	supervisors map[string]supervisorEntry
	// supervisorGen is the source of the monotonic gen counter stored on
	// each entry. Bumped under mu when a new entry is minted (initial start
	// or rotation restart).
	supervisorGen uint64
	wg            sync.WaitGroup
	stopped       bool
	stopChan      chan struct{}
}

// supervisorEntry is the per-installation state the Supervisor holds on
// each running goroutine. cancel terminates the goroutine (cascading into
// channel teardown + lease release); fingerprint is the credentials
// snapshot the channel was built with, used by sweep to detect mid-life
// rotation; gen is a monotonic counter so the goroutine's deferred cleanup
// can tell its own entry apart from a successor entry that the rotation
// path already swapped in.
type supervisorEntry struct {
	cancel      context.CancelFunc
	fingerprint string
	gen         uint64
}

// NewSupervisor constructs a Supervisor bound to the supplied store,
// channel registry, and shared inbound handler. The handler is injected
// into every Channel the Supervisor builds (via channel.Config.Handler) so
// the inbound pipeline is written once and shared across platforms. The
// Supervisor starts no goroutines until Run is called. A nil registry or
// store is a programming error and will panic at Run.
func NewSupervisor(store InstallationStore, registry *channel.Registry, handler channel.InboundHandler, cfg Config) *Supervisor {
	cfg = cfg.withDefaults()
	return &Supervisor{
		store:       store,
		registry:    registry,
		handler:     handler,
		cfg:         cfg,
		nodeID:      newNodeID(),
		supervisors: make(map[string]supervisorEntry),
		stopChan:    make(chan struct{}),
	}
}

// NodeID exposes the per-process lease token, for tests and observability
// (so operators can correlate DB lease rows to a running replica).
func (s *Supervisor) NodeID() string { return s.nodeID }

// Run is the Supervisor's main loop. It scans installations every
// PollInterval, attempts to lease any not currently supervised by this
// process, and reaps supervisors for installations that were revoked or
// whose lease was lost. Returns when ctx is cancelled; the caller MUST then
// call Wait to join all supervisor goroutines before exiting.
func (s *Supervisor) Run(ctx context.Context) {
	defer close(s.stopChan)

	// First sweep immediately so a freshly-restarted server doesn't wait a
	// full PollInterval before picking up its installations.
	s.sweep(ctx)

	t := time.NewTicker(s.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.cancelAll()
			return
		case <-t.C:
			s.sweep(ctx)
		}
	}
}

// Wait blocks until every supervisor goroutine the Supervisor started has
// exited. Call this AFTER cancelling Run's context.
//
// It first waits for Run to return (stopChan closed) and only then joins the
// supervisor WaitGroup. This ordering is load-bearing: Run is the sole caller
// of startSupervisor, which does s.wg.Add(1), and calling WaitGroup.Add
// concurrently with WaitGroup.Wait is a data race (and undefined per the
// WaitGroup contract). Once Run has returned no further Add can happen, so the
// wg.Wait below is race-free. (Run always closes stopChan via defer, even on
// panic; callers always pair Wait with a started Run + cancelled ctx.)
//
// Prefer WaitWithTimeout in shutdown paths so a stuck supervisor (typically
// a hung lease release on a frozen DB pool) cannot block process exit
// indefinitely.
func (s *Supervisor) Wait() {
	<-s.stopChan
	s.wg.Wait()
}

// WaitWithTimeout is the bounded variant of Wait. Returns true if all
// supervisor goroutines exited within the deadline, false on timeout. On
// timeout the orphaned goroutines are reclaimed by the OS and any
// unreleased leases expire naturally after LeaseTTL on the next replica. A
// timeout <= 0 falls back to unbounded Wait.
func (s *Supervisor) WaitWithTimeout(timeout time.Duration) bool {
	if timeout <= 0 {
		s.Wait()
		return true
	}
	done := make(chan struct{})
	go func() {
		s.Wait()
		close(done)
	}()
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-done:
		return true
	case <-t.C:
		return false
	}
}

// ShutdownTimeout exposes the configured graceful-shutdown deadline so boot
// can pass the same value to WaitWithTimeout without re-deriving it.
func (s *Supervisor) ShutdownTimeout() time.Duration { return s.cfg.ShutdownTimeout }

// sweep enumerates currently-active installations and starts a supervisor
// for any this process does not yet supervise. Supervisors for revoked
// installations are cancelled. Supervisors whose installation row rotated
// credentials are cancelled and replaced inline so the new channel picks up
// the fresh row.
func (s *Supervisor) sweep(ctx context.Context) {
	rows, err := s.store.ListActiveInstallations(ctx)
	if err != nil {
		s.cfg.Logger.Warn("channel engine: list active installations failed", "error", err)
		return
	}
	active := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		id := uuidString(row.ID)
		active[id] = struct{}{}
		s.maybeRestartOnRotation(id, row)
		s.startSupervisor(ctx, row)
	}
	// Reap supervisors whose installation is no longer active (revoked
	// since the last sweep). The supervisor exits on the next boundary,
	// releases its lease, and the goroutine returns.
	s.mu.Lock()
	for id, entry := range s.supervisors {
		if _, stillActive := active[id]; !stillActive {
			entry.cancel()
			delete(s.supervisors, id)
		}
	}
	s.mu.Unlock()
}

// maybeRestartOnRotation cancels an existing supervisor when its
// fingerprint differs from the current row's. The replacement is started by
// the subsequent startSupervisor call in the same sweep iteration. The map
// entry is dropped inline so the startSupervisor "skip if already
// supervised" guard does not race the cancel.
func (s *Supervisor) maybeRestartOnRotation(id string, row Installation) {
	want := row.Fingerprint
	s.mu.Lock()
	entry, ok := s.supervisors[id]
	if !ok || entry.fingerprint == want {
		s.mu.Unlock()
		return
	}
	s.cfg.Logger.Info("channel engine: credentials rotated, restarting supervisor",
		"installation_id", id,
		"channel_type", string(row.ChannelType),
	)
	entry.cancel()
	delete(s.supervisors, id)
	s.mu.Unlock()
}

func (s *Supervisor) startSupervisor(parent context.Context, inst Installation) {
	id := uuidString(inst.ID)
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	if _, exists := s.supervisors[id]; exists {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.supervisorGen++
	gen := s.supervisorGen
	s.supervisors[id] = supervisorEntry{
		cancel:      cancel,
		fingerprint: inst.Fingerprint,
		gen:         gen,
	}
	s.wg.Add(1)
	s.mu.Unlock()
	go s.supervise(ctx, inst, id, gen)
}

// leaseToken composes the per-supervisor lease token: the process-wide
// nodeID (for cross-replica observability) paired with the supervisor's gen
// so two supervisors inside the SAME process running back-to-back for the
// same installation (the rotation path) carry different tokens. That
// distinction stops an old supervisor's post-cancel release from
// CAS-matching and deleting the successor's just-acquired lease.
func leaseToken(nodeID string, gen uint64) string {
	return nodeID + "-g" + strconv.FormatUint(gen, 10)
}

// supervise owns one installation's connection lifecycle. It loops:
// acquire lease → build channel → run it (Connect blocks) → renew lease
// while it runs → on exit, release + back off → repeat. Returns when ctx is
// cancelled.
func (s *Supervisor) supervise(ctx context.Context, inst Installation, id string, gen uint64) {
	defer s.wg.Done()
	defer func() {
		// Only clear the map entry if it still belongs to us — gen
		// disambiguates "this entry is mine" from "the rotation path already
		// replaced me with a fresh supervisor."
		s.mu.Lock()
		if entry, ok := s.supervisors[id]; ok && entry.gen == gen {
			delete(s.supervisors, id)
		}
		s.mu.Unlock()
	}()

	leaseTok := leaseToken(s.nodeID, gen)
	log := s.cfg.Logger.With(
		"installation_id", id,
		"channel_type", string(inst.ChannelType),
		"node_id", s.nodeID,
		"lease_token", leaseTok,
	)
	backoff := s.cfg.MinBackoff

	for {
		if ctx.Err() != nil {
			return
		}

		// Claim the WS lease. If another replica owns a live lease, sleep
		// until it expires or our context is cancelled.
		leased, err := s.acquireLease(ctx, inst.ID, leaseTok)
		if err != nil {
			log.Warn("channel engine: acquire lease error", "error", err)
			if sleep(ctx, s.cfg.LeaseRenewInterval) {
				return
			}
			continue
		}
		if !leased {
			if sleep(ctx, s.cfg.LeaseRenewInterval) {
				return
			}
			continue
		}

		// Lease acquired. Build the platform channel via the registry,
		// run it under a child context, and renew the lease in parallel.
		ch, err := s.registry.Build(channel.Config{
			Type:    inst.ChannelType,
			Raw:     inst.Config,
			Handler: s.handler,
		})
		if err != nil {
			log.Error("channel engine: build channel failed", "error", err)
			s.releaseLease(inst.ID, leaseTok)
			if sleep(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff, s.cfg.MaxBackoff)
			continue
		}

		runCtx, runCancel := context.WithCancel(ctx)
		renewDone := make(chan struct{})
		go func() {
			defer close(renewDone)
			// renewLeaseUntil cancels runCtx itself on lease loss so the
			// channel exits even if its wire I/O is blocked. This is what
			// makes "at most one active connection per installation across
			// replicas" hold under lease theft.
			s.renewLeaseUntil(runCtx, runCancel, inst.ID, leaseTok)
		}()

		startedAt := s.cfg.Now()
		runErr := ch.Connect(runCtx)
		runCancel()
		<-renewDone
		s.disconnect(ch, id, log)
		s.releaseLease(inst.ID, leaseTok)

		if ctx.Err() != nil {
			return
		}

		// If the connection lived long enough to be "stable", reset the
		// backoff so a single late failure does not start us at the cap.
		uptime := s.cfg.Now().Sub(startedAt)
		if uptime >= s.cfg.ResetBackoffAfter {
			backoff = s.cfg.MinBackoff
		}
		if runErr != nil {
			log.Warn("channel engine: connection exited with error", "error", runErr, "uptime", uptime.String())
		} else {
			log.Info("channel engine: connection exited cleanly", "uptime", uptime.String())
		}
		if sleep(ctx, jitter(backoff)) {
			return
		}
		backoff = nextBackoff(backoff, s.cfg.MaxBackoff)
	}
}

// acquireLease tries to claim or renew the WS lease. Returns (true, nil)
// when owned after the call; (false, nil) when held elsewhere; (false, err)
// for transport / DB failures. token is the per-supervisor token (see
// leaseToken), NOT the process-wide nodeID.
func (s *Supervisor) acquireLease(ctx context.Context, instID pgtype.UUID, token string) (bool, error) {
	expires := s.cfg.Now().Add(s.cfg.LeaseTTL)
	err := s.store.AcquireWSLease(ctx, AcquireLeaseParams{
		ID:        instID,
		Token:     token,
		ExpiresAt: expires,
	})
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrLeaseNotAcquired) {
		return false, nil
	}
	return false, err
}

// renewLeaseUntil re-acquires the lease on a tight cadence so a single
// missed renewal does not yield it. Exits when ctx is cancelled. Lease loss
// MUST cancel the channel's run context — otherwise the supervise loop would
// release the lease while the channel's receive loop kept consuming events
// until its wire I/O finally errored, exactly the "two replicas processing
// the same installation" failure mode. cancelRun forces the channel's ctx
// done immediately, so Connect returns in bounded time even on a silent
// socket. token MUST be the same per-supervisor token used to acquire.
func (s *Supervisor) renewLeaseUntil(ctx context.Context, cancelRun context.CancelFunc, instID pgtype.UUID, token string) {
	t := time.NewTicker(s.cfg.LeaseRenewInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			leased, err := s.acquireLease(ctx, instID, token)
			if err != nil {
				s.cfg.Logger.Warn("channel engine: lease renewal error",
					"installation_id", uuidString(instID),
					"error", err,
				)
				continue
			}
			if !leased {
				s.cfg.Logger.Warn("channel engine: lease lost; tearing down connection",
					"installation_id", uuidString(instID),
				)
				cancelRun()
				return
			}
		}
	}
}

// releaseLease writes a token-fenced release so the next supervisor (this
// process or another replica) can pick up the installation without waiting
// for LeaseTTL. It runs on a fresh, bounded context (the parent ctx is
// already cancelled by shutdown time). token MUST be the same per-supervisor
// token used to acquire — a rotation successor's lease carries a different
// token, so a stale release no-ops instead of clobbering it.
func (s *Supervisor) releaseLease(instID pgtype.UUID, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.LeaseReleaseTimeout)
	defer cancel()
	if err := s.store.ReleaseWSLease(ctx, ReleaseLeaseParams{
		ID:    instID,
		Token: token,
	}); err != nil {
		s.cfg.Logger.Warn("channel engine: release lease failed",
			"installation_id", uuidString(instID),
			"error", err,
		)
	}
}

// disconnect tears down a channel after its Connect returned, on a fresh
// bounded context so a wedged Disconnect cannot hang the supervise loop. By
// the time we get here the link is already down (Connect returned), so this
// is best-effort resource cleanup.
func (s *Supervisor) disconnect(ch channel.Channel, id string, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.DisconnectTimeout)
	defer cancel()
	if err := ch.Disconnect(ctx); err != nil {
		log.Warn("channel engine: disconnect failed", "installation_id", id, "error", err)
	}
}

func (s *Supervisor) cancelAll() {
	s.mu.Lock()
	s.stopped = true
	for id, entry := range s.supervisors {
		entry.cancel()
		delete(s.supervisors, id)
	}
	s.mu.Unlock()
}

// newNodeID returns a 16-byte hex random string unique to this process.
// Stored in channel_installation.ws_lease_token; matching tokens on a
// subsequent acquire are treated as renewals (same owner).
func newNodeID() string {
	buf := make([]byte, 16)
	if _, err := cryptorand.Read(buf); err != nil {
		// crypto/rand failure is catastrophic and rare; fall back to a
		// timestamp-derived token rather than panicking on boot.
		return fmt.Sprintf("nodeid-fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// nextBackoff doubles the current backoff up to max.
func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		return max
	}
	return next
}

// jitter spreads reconnect storms across the [0.5d, 1.5d) window so many
// installations do not all retry on the same timer edge.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	delta := d / 2
	return d - delta + time.Duration(mathrand.Int64N(int64(2*delta)+1))
}

// sleep is a ctx-aware time.Sleep. Returns true iff ctx was cancelled before
// the sleep completed, so callers can short-circuit shutdown.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() != nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-t.C:
		return false
	}
}

func uuidString(u pgtype.UUID) string { return util.UUIDToString(u) }
