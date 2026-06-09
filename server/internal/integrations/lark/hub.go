package lark

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	mathrand "math/rand/v2"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// HubQueries is the narrow subset of *db.Queries the Hub needs for
// installation enumeration and lease management. *db.Queries satisfies
// it directly; tests substitute a fake.
type HubQueries interface {
	ListActiveLarkInstallations(ctx context.Context) ([]db.LarkInstallation, error)
	AcquireLarkWSLease(ctx context.Context, arg db.AcquireLarkWSLeaseParams) (db.LarkInstallation, error)
	ReleaseLarkWSLease(ctx context.Context, arg db.ReleaseLarkWSLeaseParams) error
}

// EventEmitter is the per-message callback the Hub hands to an
// EventConnector. Calling it dispatches the normalized inbound
// message and returns the typed outcome plus any infrastructure
// error from the Dispatcher.
//
// Connectors react to the return value to decide what to do on the
// Lark side:
//
//   - A non-nil error is a real infrastructure failure (DB down,
//     etc.) — the connector should reconnect (the Hub will retry
//     under backoff) and surface the error to ops, NOT swallow it.
//   - A nil error with OutcomeNeedsBinding tells the connector to
//     send the binding-prompt card to the sender's open_id.
//   - OutcomeAgentOffline / OutcomeAgentArchived tell the connector
//     to send the respective copy as a Lark card; the chat_message
//     row is already persisted, so the agent will pick the message
//     up on resume.
//   - OutcomeIngested means the message landed and (optionally) a
//     task was enqueued; the connector emits a "thinking…" card and
//     lets the outbound Patcher take over from there.
//   - OutcomeDropped is informational only (the message was filtered
//     for a legitimate reason); typical connectors do nothing.
//
// The Dispatcher's invariants (identity check, dedup, audit) are NOT
// the connector's concern — the connector only sees the verdict.
type EventEmitter func(ctx context.Context, msg InboundMessage) (DispatchResult, error)

// EventConnector is the per-installation transport. The Hub owns the
// lifecycle (when to start, when to stop, when to back off), and the
// connector owns the actual wire protocol — opening the Lark long
// connection, decoding events, normalizing them into InboundMessage.
//
// Run MUST block until either:
//   - the ctx is cancelled (graceful shutdown / lease loss / revoke),
//     in which case it returns nil; or
//   - the connection ends and cannot be recovered locally, in which
//     case it returns an error describing why. The Hub treats a
//     non-nil return as "this attempt failed" and schedules a retry
//     under exponential backoff.
//
// Implementations MUST be tolerant of repeated Run calls on different
// contexts — the Hub may call Run, return, and call Run again after
// backoff. Allocating per-call state is fine; persistent state lives in
// the connector struct.
//
// emit returns the Dispatcher's verdict + any infra error so the
// connector can post the corresponding Lark-side reply (binding card,
// offline card, etc.) and / or decide to disconnect on a hard failure.
// The connector MUST NOT bypass the Dispatcher by writing to the DB
// directly; emit is the only ingress path.
type EventConnector interface {
	Run(ctx context.Context, inst db.LarkInstallation, emit EventEmitter) error
}

// ConnectorFactory builds an EventConnector for a specific installation
// row. The factory exists so the Hub doesn't need to know about Lark
// SDK construction (auth, app credentials decryption) — call sites
// inject a factory configured with their APIClient + secretbox box.
type ConnectorFactory func(inst db.LarkInstallation) (EventConnector, error)

// HubConfig tunes the Hub's lifecycle loops. All fields have sensible
// production defaults via withDefaults; tests typically set Now and
// Logger to inject determinism.
type HubConfig struct {
	// LeaseTTL is how long a successful AcquireLarkWSLease grant is
	// valid before another server replica may steal it. Renewals
	// happen on a tighter interval (LeaseRenewInterval); the gap
	// between renew and TTL absorbs transient DB blips.
	LeaseTTL time.Duration

	// LeaseRenewInterval is the cadence at which the Hub re-acquires
	// the lease on connections it already owns. MUST be substantially
	// less than LeaseTTL so a single missed renewal does not yield
	// the lease.
	LeaseRenewInterval time.Duration

	// PollInterval is how often the Hub scans for new installations
	// (or ones whose lease has expired on another replica) to take
	// over.
	PollInterval time.Duration

	// MinBackoff / MaxBackoff bound the per-installation reconnect
	// schedule. The actual delay starts at MinBackoff, doubles after
	// each consecutive failure (capped at MaxBackoff), and resets on
	// any successful Run that lives at least ResetBackoffAfter.
	MinBackoff        time.Duration
	MaxBackoff        time.Duration
	ResetBackoffAfter time.Duration

	// LeaseReleaseTimeout caps how long a single ReleaseLarkWSLease
	// call may block. The release runs on a fresh context (not the
	// parent supervisor ctx, which is already cancelled by the time
	// we release on shutdown), so without an explicit deadline a
	// stalled DB pool could hang shutdown indefinitely. A bounded
	// timeout means a hung release falls back to the natural lease
	// TTL expiry on the next replica — slower than a clean release,
	// but still bounded.
	LeaseReleaseTimeout time.Duration

	// ShutdownTimeout bounds how long Hub.Wait blocks waiting for
	// supervisor goroutines to exit after their parent ctx is
	// cancelled. Hub.Wait itself does not enforce this — callers
	// pass it to WaitWithTimeout. Exposed on the config so main.go
	// and tests share the same default.
	ShutdownTimeout time.Duration

	// ReplyTimeout caps an OutcomeReplier.Reply call. The replier
	// runs in a detached goroutine off the ACK critical path —
	// dispatch returns the verdict to the connector immediately so
	// the ACK can be written, then the reply (a NeedsBinding card,
	// an offline notice, etc.) is best-effort under this deadline.
	// Lark's long-conn server expects a frame ACK within 3 seconds,
	// so this MUST stay strictly under 3s to keep the connector's
	// own ACK path comfortably bounded even if the dispatch itself
	// takes a few hundred ms (DB roundtrips, identity checks, etc.).
	// Zero defaults to 2.5s.
	ReplyTimeout time.Duration

	// Now returns the current time. Injected for tests; production
	// uses time.Now.
	Now func() time.Time

	// Logger optional; defaults to slog.Default.
	Logger *slog.Logger
}

func (c HubConfig) withDefaults() HubConfig {
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
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = 15 * time.Second
	}
	if c.ReplyTimeout == 0 {
		c.ReplyTimeout = 2500 * time.Millisecond
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// Hub owns the per-installation supervisor goroutines that keep a
// long-running Lark connection per active installation. It enforces
// the §4.4 multi-replica safety rule via the WS lease CAS — at most
// one Hub instance globally holds the lease for any installation, so
// duplicate event consumption across replicas is impossible.
//
// Lifecycle:
//
//	hub := NewHub(queries, factory, dispatcher, HubConfig{})
//	go hub.Run(ctx)             // returns when ctx is cancelled
//	... ctx cancellation triggers ...
//	hub.Wait()                  // joins on every per-installation goroutine
type Hub struct {
	queries    HubQueries
	factory    ConnectorFactory
	dispatcher *Dispatcher
	replier    OutcomeReplier
	cfg        HubConfig

	// nodeID is the per-process lease ownership token. The CAS
	// predicate on AcquireLarkWSLease treats matching tokens as
	// "this is us, renew" — so as long as nodeID is stable for the
	// Hub's lifetime, lease renewals don't ping-pong between replicas.
	nodeID string

	mu sync.Mutex
	// supervisors keys each in-flight supervisor goroutine by
	// installation_id, alongside a fingerprint of the credentials the
	// connector was started with. When the underlying lark_installation
	// row changes (re-scan creates a fresh bot → new app_id / new
	// encrypted secret), the next sweep detects the fingerprint drift
	// and forces a restart so the new connector picks up fresh
	// credentials. Without this, a stale connector keeps a WS open
	// against the OLD bot's app_id and the new bot silently goes dark.
	supervisors map[string]supervisorEntry
	// supervisorGen is the source of the monotonic gen counter stored
	// on each supervisorEntry. Bumped under mu when a new entry is
	// minted (initial start OR rotation restart).
	supervisorGen uint64
	wg            sync.WaitGroup
	stopped       bool
	stopChan      chan struct{}

	// replyWg tracks in-flight outbound reply goroutines (NeedsBinding
	// card, offline notice, etc.). The replier is detached from the
	// inbound ACK critical path — the connector ACKs as soon as
	// dispatch returns — so reply goroutines may still be running when
	// the supervisor goroutines have already exited. Hub.Wait joins on
	// these too, with each goroutine bounded by ReplyTimeout, so a
	// hung outbound Lark HTTP call cannot block shutdown beyond the
	// timeout.
	replyWg sync.WaitGroup
}

// supervisorEntry is the per-installation state the Hub holds on each
// running supervisor goroutine. cancel terminates the goroutine
// (cascading into connector teardown + lease release); fingerprint is
// the credentials snapshot the connector was launched with, used by
// sweep to detect mid-life rotation (re-scan replaces the bot's
// app_id / app_secret) and force a restart; gen is a monotonic
// counter so the goroutine's deferred cleanup can tell its own entry
// apart from a successor entry that the rotation path already swapped
// in. Without the gen check, the old goroutine's defer would race to
// delete the new entry it knows nothing about.
type supervisorEntry struct {
	cancel      context.CancelFunc
	fingerprint string
	gen         uint64
}

// NewHub constructs a Hub bound to the supplied queries, connector
// factory and dispatcher. The Hub does not start any goroutines until
// Run is called. The replier (OutcomeReplier) handles the outbound
// side of the EventEmitter contract — NeedsBinding / AgentOffline /
// AgentArchived cards — and is best-effort: failures are logged and
// do not interrupt inbound processing. A nil replier falls back to
// the noop replier so callers that have not wired outbound replies
// yet still get the inbound pipeline running.
func NewHub(queries HubQueries, factory ConnectorFactory, dispatcher *Dispatcher, cfg HubConfig) *Hub {
	cfg = cfg.withDefaults()
	return &Hub{
		queries:     queries,
		factory:     factory,
		dispatcher:  dispatcher,
		replier:     NewNoopOutcomeReplier(cfg.Logger),
		cfg:         cfg,
		nodeID:      newNodeID(),
		supervisors: make(map[string]supervisorEntry),
		stopChan:    make(chan struct{}),
	}
}

// SetOutcomeReplier installs the production replier on the Hub. Must
// be called BEFORE Run; setting it afterwards is a data race against
// the supervisor's emit goroutines. Nil resets back to the noop
// replier (useful for tests).
func (h *Hub) SetOutcomeReplier(r OutcomeReplier) {
	if r == nil {
		r = NewNoopOutcomeReplier(h.cfg.Logger)
	}
	h.replier = r
}

// NodeID exposes the per-process lease token. Useful for tests and
// for observability (so operators can correlate DB lease rows to a
// running replica).
func (h *Hub) NodeID() string { return h.nodeID }

// Run is the Hub's main loop. It scans installations every
// PollInterval, attempts to lease any that are not currently being
// supervised by this process, and reaps supervisors for installations
// that have been revoked or whose lease was lost. Returns when ctx is
// cancelled; the caller MUST then call Wait to join all supervisor
// goroutines before exiting.
func (h *Hub) Run(ctx context.Context) {
	defer close(h.stopChan)

	// First sweep immediately so a freshly-restarted server doesn't
	// wait a full PollInterval before picking up its installations.
	h.sweep(ctx)

	t := time.NewTicker(h.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			h.cancelAll()
			return
		case <-t.C:
			h.sweep(ctx)
		}
	}
}

// Wait blocks until every supervisor goroutine AND every detached
// reply goroutine the Hub started has exited. Call this AFTER
// cancelling Run's context; calling it before returns immediately if
// no goroutines are active.
//
// Prefer WaitWithTimeout in shutdown paths so a stuck supervisor
// (typically a hung lease release on a frozen DB pool) cannot block
// process exit indefinitely. Reply goroutines are independently
// bounded by ReplyTimeout, so even Wait() (unbounded) eventually
// returns once those deadlines elapse.
func (h *Hub) Wait() {
	h.wg.Wait()
	// Supervisors (and thus inbound delivery) have stopped, so no new
	// run triggers can be scheduled. Drain the debounced pending triggers
	// before joining replies: the flush may itself emit an offline/archived
	// notice, and FlushPendingRuns blocks until those finish.
	if h.dispatcher != nil {
		h.dispatcher.FlushPendingRuns()
	}
	h.replyWg.Wait()
}

// WaitWithTimeout is the bounded variant of Wait. Returns true if all
// supervisor AND reply goroutines exited within the deadline, false if
// the timeout fired first. On timeout, the process owner should log
// the fact and proceed with exit; the orphaned goroutines will be
// reclaimed by the OS, any unreleased leases expire naturally after
// LeaseTTL on the next replica, and in-flight replies abort at their
// own ReplyTimeout (already strictly under 3s, so well under any
// reasonable ShutdownTimeout).
//
// A timeout <= 0 falls back to unbounded Wait, matching the legacy
// behavior.
func (h *Hub) WaitWithTimeout(timeout time.Duration) bool {
	if timeout <= 0 {
		h.Wait()
		return true
	}
	done := make(chan struct{})
	go func() {
		h.Wait()
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

// ShutdownTimeout exposes the configured graceful-shutdown deadline so
// main.go can use the same value WaitWithTimeout is checked against
// without re-deriving it.
func (h *Hub) ShutdownTimeout() time.Duration { return h.cfg.ShutdownTimeout }

// sweep enumerates currently-active installations and starts a
// supervisor for any that this Hub does not yet supervise. Supervisors
// for revoked installations are cancelled. Supervisors whose
// underlying installation row has rotated credentials (re-scan in the
// device flow creates a fresh app_id / app_secret) are cancelled and
// replaced inline so the new connector picks up the fresh row instead
// of running indefinitely against stale credentials.
func (h *Hub) sweep(ctx context.Context) {
	rows, err := h.queries.ListActiveLarkInstallations(ctx)
	if err != nil {
		h.cfg.Logger.Warn("lark hub: list active installations failed", "error", err)
		return
	}
	active := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		id := uuidString(row.ID)
		active[id] = struct{}{}
		h.maybeRestartOnRotation(id, row)
		h.startSupervisor(ctx, row)
	}
	// Reap supervisors whose installation is no longer active (revoked
	// since the last sweep). The supervisor will exit on the next
	// boundary, release its lease, and the goroutine returns.
	h.mu.Lock()
	for id, entry := range h.supervisors {
		if _, stillActive := active[id]; !stillActive {
			entry.cancel()
			delete(h.supervisors, id)
		}
	}
	h.mu.Unlock()
}

// maybeRestartOnRotation cancels an existing supervisor when its
// credentials fingerprint differs from the current row's. The new
// supervisor is started by the subsequent startSupervisor call within
// the same sweep iteration. We drop the map entry inline so the
// startSupervisor "skip if already supervised" guard does not race the
// cancel — the deleted entry's goroutine releases its lease as part of
// teardown, and the new supervisor's acquireLease takes over on its
// own clock.
func (h *Hub) maybeRestartOnRotation(id string, row db.LarkInstallation) {
	want := installationFingerprint(row)
	h.mu.Lock()
	entry, ok := h.supervisors[id]
	if !ok || entry.fingerprint == want {
		h.mu.Unlock()
		return
	}
	h.cfg.Logger.Info("lark hub: credentials rotated, restarting supervisor",
		"installation_id", id,
		"app_id", row.AppID,
	)
	entry.cancel()
	delete(h.supervisors, id)
	h.mu.Unlock()
}

func (h *Hub) startSupervisor(parent context.Context, inst db.LarkInstallation) {
	id := uuidString(inst.ID)
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return
	}
	if _, exists := h.supervisors[id]; exists {
		h.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	h.supervisorGen++
	gen := h.supervisorGen
	h.supervisors[id] = supervisorEntry{
		cancel:      cancel,
		fingerprint: installationFingerprint(inst),
		gen:         gen,
	}
	h.wg.Add(1)
	h.mu.Unlock()
	go h.supervise(ctx, inst, id, gen)
}

// leaseToken composes the per-supervisor lease token. We pair the
// Hub-wide nodeID (so multi-replica observability still works — an
// operator inspecting lark_installation.ws_lease_token can map it
// back to a process) with the supervisor's gen so two supervisors
// inside the SAME Hub running back-to-back for the same installation
// (the rotation path) carry different tokens. That distinction is
// what stops an old supervisor's post-cancel releaseLease from
// CAS-matching and DELETE-ing the lease the successor just acquired.
//
// We use strconv.FormatUint rather than fmt.Sprintf because this is on
// the supervise hot path (called on every renew tick when its result
// would be ineligible to be cached) and we'd like to keep allocation
// pressure minimal.
func leaseToken(nodeID string, gen uint64) string {
	return nodeID + "-g" + strconv.FormatUint(gen, 10)
}

// installationFingerprint condenses the credentials-bearing columns of
// the installation row into an opaque string. Two rows with the same
// fingerprint are interchangeable as far as the connector is concerned;
// any byte difference here means the next sweep will tear the running
// supervisor down and start it again with the fresh credentials. We
// include app_id (which Lark rotates on every device-flow registration),
// bot_open_id (sanity — protects against an app_id reuse with a
// different bot identity), and a SHA-256 of the encrypted app_secret
// blob so a re-encrypt at rest also triggers a restart. The plaintext
// secret is never extracted; the encrypted ciphertext is fine to hash.
func installationFingerprint(inst db.LarkInstallation) string {
	sum := sha256.Sum256(inst.AppSecretEncrypted)
	// region is part of the fingerprint: if a re-install corrects the
	// cloud (e.g. a row mis-detected as feishu is re-scanned as lark),
	// the WS bootstrap host changes, so the running supervisor must be
	// torn down and restarted against the new host.
	return inst.AppID + "|" + inst.BotOpenID + "|" + inst.Region + "|" + hex.EncodeToString(sum[:])
}

// supervise owns one installation's connection lifecycle. It loops:
// acquire lease → spin up connector → renew lease while connector is
// running → on connector exit, back off → repeat. Returns (and the
// goroutine ends) when ctx is cancelled.
func (h *Hub) supervise(ctx context.Context, inst db.LarkInstallation, id string, gen uint64) {
	defer h.wg.Done()
	defer func() {
		// Only clear the supervisors map entry if it still belongs to
		// us — gen disambiguates "this entry is mine" from "the
		// rotation path already replaced me with a fresh supervisor."
		// Without the gen check, the old goroutine's defer would race
		// to delete the new entry it knows nothing about.
		h.mu.Lock()
		if entry, ok := h.supervisors[id]; ok && entry.gen == gen {
			delete(h.supervisors, id)
		}
		h.mu.Unlock()
	}()

	// Per-supervisor lease token: scopes the DB-level token fence to
	// THIS goroutine so a stale post-cancel release from a rotation
	// predecessor can't clobber a successor's just-acquired lease.
	// Embeds the Hub's nodeID for cross-replica observability and the
	// supervisor gen so the same Hub running multiple sequential
	// supervisors for the same installation (rotation) gets distinct
	// tokens for each.
	leaseTok := leaseToken(h.nodeID, gen)
	log := h.cfg.Logger.With(
		"installation_id", id,
		"node_id", h.nodeID,
		"lease_token", leaseTok,
	)
	backoff := h.cfg.MinBackoff

	for {
		if ctx.Err() != nil {
			return
		}

		// Try to claim the WS lease for this installation. If another
		// replica already owns a live lease, sleep until either the
		// lease expires or our context is cancelled.
		leased, err := h.acquireLease(ctx, inst.ID, leaseTok)
		if err != nil {
			log.Warn("lark hub: acquire lease error", "error", err)
			if sleep(ctx, h.cfg.LeaseRenewInterval) {
				return
			}
			continue
		}
		if !leased {
			// Another replica owns the lease. Wait LeaseRenewInterval
			// (less than LeaseTTL) and re-check; if they die, we'll
			// pick it up on the next iteration.
			if sleep(ctx, h.cfg.LeaseRenewInterval) {
				return
			}
			continue
		}

		// Lease acquired. Build a connector, run it under a child
		// context, and start the lease renewer in parallel. The
		// connector returns when its connection dies or our ctx is
		// cancelled; we always release the lease afterwards.
		conn, err := h.factory(inst)
		if err != nil {
			log.Error("lark hub: connector factory failed", "error", err)
			h.releaseLease(inst.ID, leaseTok)
			if sleep(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff, h.cfg.MaxBackoff)
			continue
		}

		runCtx, runCancel := context.WithCancel(ctx)
		renewDone := make(chan struct{})
		go func() {
			defer close(renewDone)
			// renewLeaseUntil cancels runCtx itself on lease loss so the
			// connector exits even if its wire I/O is currently blocked.
			// This is what makes the "at most one active WS per
			// installation across replicas" invariant hold under lease
			// theft: A's renewer fails CAS the moment B steals the
			// lease, A's runCtx flips done, A's connector returns, and
			// only B is consuming events.
			h.renewLeaseUntil(runCtx, runCancel, inst.ID, leaseTok)
		}()

		startedAt := h.cfg.Now()
		runErr := conn.Run(runCtx, inst, func(emitCtx context.Context, msg InboundMessage) (DispatchResult, error) {
			return h.handleEvent(emitCtx, inst, log, msg)
		})
		runCancel()
		<-renewDone
		h.releaseLease(inst.ID, leaseTok)

		if ctx.Err() != nil {
			return
		}

		// If the connection lived long enough that we believe it was
		// "stable", reset the backoff so a single late failure does
		// not start us at the cap. Otherwise step up the backoff.
		uptime := h.cfg.Now().Sub(startedAt)
		if uptime >= h.cfg.ResetBackoffAfter {
			backoff = h.cfg.MinBackoff
		}
		if runErr != nil {
			log.Warn("lark hub: connector exited with error", "error", runErr, "uptime", uptime.String())
		} else {
			log.Info("lark hub: connector exited cleanly", "uptime", uptime.String())
		}
		if sleep(ctx, jitter(backoff)) {
			return
		}
		backoff = nextBackoff(backoff, h.cfg.MaxBackoff)
	}
}

// acquireLease tries to claim or renew the WS lease for an
// installation. Returns (true, nil) when the lease is owned by this
// supervisor after the call; (false, nil) when another replica or
// in-process predecessor holds a live lease; or (false, err) for
// transport / DB failures.
//
// token is a per-supervisor identifier (see leaseToken) — NOT the
// hub-wide nodeID. This is the load-bearing change for rotation
// safety: an old supervisor and its successor inside the same Hub
// each carry a distinct token, so the old supervisor's stale release
// can no longer match (and DELETE) the successor's just-acquired
// lease row.
func (h *Hub) acquireLease(ctx context.Context, instID pgtype.UUID, token string) (bool, error) {
	expires := h.cfg.Now().Add(h.cfg.LeaseTTL)
	_, err := h.queries.AcquireLarkWSLease(ctx, db.AcquireLarkWSLeaseParams{
		ID:           instID,
		NewToken:     pgtype.Text{String: token, Valid: true},
		NewExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true},
	})
	if err == nil {
		return true, nil
	}
	if isNoRowsErr(err) {
		// CAS predicate didn't match — someone else (another replica
		// OR a sibling supervisor mid-rotation) holds the lease. We
		// back off and let the renewer / supervise loop re-poll on its
		// own clock.
		return false, nil
	}
	return false, err
}

// renewLeaseUntil re-acquires the lease on a tight cadence so a single
// missed renewal does not yield it. Exits when ctx is cancelled.
//
// Lease loss (acquireLease returns leased=false) MUST cancel the
// connector's run context — otherwise the supervise loop would
// release the lease but the connector goroutine could still be
// blocked on its wire I/O and continue consuming Lark events until
// its TCP read finally errored out. That window is exactly the
// "two replicas processing the same installation" failure mode the
// §4.4 ownership invariant rules out. Calling cancelRun here forces
// the connector's ctx to flip done immediately, so conn.Run returns
// in bounded time even when the underlying socket is silent.
//
// token MUST be the same per-supervisor token threaded through the
// initial acquireLease call. Otherwise the very first renewal would
// CAS-fail against the supervisor's own previous renew and tear the
// connector down on every renew interval.
func (h *Hub) renewLeaseUntil(ctx context.Context, cancelRun context.CancelFunc, instID pgtype.UUID, token string) {
	t := time.NewTicker(h.cfg.LeaseRenewInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			leased, err := h.acquireLease(ctx, instID, token)
			if err != nil {
				h.cfg.Logger.Warn("lark hub: lease renewal error",
					"installation_id", uuidString(instID),
					"error", err,
				)
				continue
			}
			if !leased {
				h.cfg.Logger.Warn("lark hub: lease lost; tearing down connector",
					"installation_id", uuidString(instID),
				)
				cancelRun()
				return
			}
		}
	}
}

// releaseLease writes a token-fenced DELETE to lark_installation's
// lease columns so the next supervisor (this process or another
// replica) can pick up the installation without waiting for LeaseTTL
// to expire.
//
// The supervisor calls this from two places — factory-error retry and
// post-Run cleanup — and at shutdown the parent ctx is already done,
// so passing it through would short-circuit the DB call before it ever
// reached the pool. Instead we derive a fresh background ctx with a
// bounded LeaseReleaseTimeout: a healthy pool finishes in milliseconds,
// and a frozen pool can no longer hang shutdown indefinitely. The
// fallback when the bound trips is the same as a crash — the lease row
// stays put until its TTL elapses on the next replica.
//
// token MUST be the same per-supervisor token used to acquire. If a
// rotation has already replaced this supervisor and the successor has
// taken the lease with its own token, the DELETE's WHERE clause won't
// match this supervisor's stale token and the call no-ops instead of
// clobbering the successor's lease. That fence is the whole point of
// per-supervisor tokens.
func (h *Hub) releaseLease(instID pgtype.UUID, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), h.cfg.LeaseReleaseTimeout)
	defer cancel()
	if err := h.queries.ReleaseLarkWSLease(ctx, db.ReleaseLarkWSLeaseParams{
		ID:           instID,
		CurrentToken: pgtype.Text{String: token, Valid: true},
	}); err != nil {
		h.cfg.Logger.Warn("lark hub: release lease failed",
			"installation_id", uuidString(instID),
			"error", err,
		)
	}
}

// handleEvent is the seam between the connector (which emits normalized
// InboundMessage) and the inbound Dispatcher + outbound OutcomeReplier.
// We deliberately do not retry here — the Dispatcher classifies errors
// itself (productizable outcomes vs. infra failures), and infra
// failures propagate up to the connector, which decides whether to
// reconnect.
//
// CRITICAL: handleEvent MUST return promptly because the connector
// writes the frame ACK right after emit returns, and Lark's long-conn
// server expects an ACK within 3 seconds. The dispatcher itself is
// fast (one DB roundtrip + identity check) but the outbound replier
// can do real Lark HTTP calls — token mint, card send — each with
// its own multi-second client timeout. Running the replier inline
// would couple ACK latency to outbound HTTP, and if that HTTP stalls
// for >3s Lark treats the event as un-ACKed and re-pushes it; by then
// Dispatcher has already marked the dedup row terminal, so the retry
// gets dropped and the user never receives the binding prompt /
// offline notice.
//
// Fix: dispatch is synchronous (in the connector's read loop), the
// reply is detached. Each reply runs in its own goroutine under a
// fresh context bounded by ReplyTimeout (strictly under 3s). Hub.Wait
// joins on replyWg so shutdown still drains in-flight replies.
func (h *Hub) handleEvent(ctx context.Context, inst db.LarkInstallation, log *slog.Logger, msg InboundMessage) (DispatchResult, error) {
	if h.dispatcher == nil {
		log.Warn("lark hub: dispatcher not configured; dropping event",
			"event_id", msg.EventID,
		)
		return DispatchResult{}, ErrDispatcherNotConfigured
	}
	res, err := h.dispatcher.Handle(ctx, msg)
	if err != nil {
		log.Error("lark hub: dispatcher error",
			"event_id", msg.EventID,
			"error", err,
		)
		return res, err
	}
	log.Debug("lark hub: dispatch outcome",
		"event_id", msg.EventID,
		"outcome", string(res.Outcome),
		"drop_reason", string(res.DropReason),
	)
	h.scheduleReply(inst, msg, res, log)
	return res, nil
}

// scheduleReply detaches the OutcomeReplier from the ACK critical path.
// The reply goroutine uses a fresh context.Background() with a
// ReplyTimeout deadline so it is independent of the inbound emit ctx
// (which the connector cancels as soon as Run exits). A nil or noop
// replier short-circuits — no goroutine, no wg tracking.
//
// Why a fresh background ctx instead of inheriting from the emit ctx:
// the emit ctx is cancelled when the connector's runCtx fires, which
// can happen mid-reply on a normal reconnect cycle. Inheriting would
// kill the outbound reply for no reason — the binding card / offline
// notice is still wanted. ReplyTimeout is the only guard we need.
func (h *Hub) scheduleReply(inst db.LarkInstallation, msg InboundMessage, res DispatchResult, log *slog.Logger) {
	r := h.replier
	if r == nil {
		return
	}
	// Fast path: noop replier doesn't do any IO, run it inline so we
	// don't pay goroutine + waitgroup cost for a no-op. The exposed
	// type is unexported but the test seam uses NewNoopOutcomeReplier
	// which returns *noopReplier, so the type-assert is safe.
	if _, isNoop := r.(*noopReplier); isNoop {
		r.Reply(context.Background(), inst, msg, res)
		return
	}
	h.replyWg.Add(1)
	go func() {
		defer h.replyWg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), h.cfg.ReplyTimeout)
		defer cancel()
		r.Reply(ctx, inst, msg, res)
		if ctx.Err() == context.DeadlineExceeded {
			log.Warn("lark hub: outbound reply timed out",
				"event_id", msg.EventID,
				"outcome", string(res.Outcome),
				"timeout", h.cfg.ReplyTimeout.String(),
			)
		}
	}()
}

// ErrDispatcherNotConfigured is surfaced to the connector when emit is
// called on a Hub that was constructed without a Dispatcher. Returning
// it (instead of silently dropping) lets the connector log and / or
// disconnect so the misconfiguration is visible in production.
var ErrDispatcherNotConfigured = errors.New("lark hub: dispatcher not configured")

func (h *Hub) cancelAll() {
	h.mu.Lock()
	h.stopped = true
	for id, entry := range h.supervisors {
		entry.cancel()
		delete(h.supervisors, id)
	}
	h.mu.Unlock()
}

// newNodeID returns a 16-byte hex random string unique to this process.
// The DB stores it in lark_installation.ws_lease_token; matching tokens
// on subsequent acquires are treated as renewals (same owner).
func newNodeID() string {
	buf := make([]byte, 16)
	if _, err := cryptorand.Read(buf); err != nil {
		// crypto/rand failure is catastrophic and rare; fall back to a
		// timestamp-derived token rather than panicking on boot.
		return fmt.Sprintf("nodeid-fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// nextBackoff doubles the current backoff up to max. Pure helper so
// the supervise loop reads top-to-bottom.
func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		return max
	}
	return next
}

// jitter spreads reconnect storms (e.g. after a Lark-side outage)
// across the [0.5d, 1.5d) window, so 100 installations don't all
// retry on the same timer edge.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	delta := d / 2
	return d - delta + time.Duration(mathrand.Int64N(int64(2*delta)+1))
}

// sleep is a ctx-aware time.Sleep. Returns true iff the ctx was
// cancelled before the sleep completed — callers use the boolean to
// short-circuit shutdown.
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

// isNoRowsErr is the local equivalent of errors.Is(err, pgx.ErrNoRows)
// without importing pgx into this file. The CAS predicate on
// AcquireLarkWSLease surfaces "lease held by someone else" as a
// no-rows return, not a structured error type.
func isNoRowsErr(err error) bool {
	if err == nil {
		return false
	}
	// pgx.ErrNoRows is the sentinel; matching by message is
	// sufficient and avoids importing pgx purely for this comparison.
	return errors.Is(err, errPgxNoRows) || err.Error() == "no rows in result set"
}

// errPgxNoRows is initialized in hub_pgx.go to pgx.ErrNoRows so the
// no-rows check above works under both the real pgx import path and
// the string-matched fallback (test fakes return that string directly).
var errPgxNoRows error

func uuidString(u pgtype.UUID) string { return util.UUIDToString(u) }
