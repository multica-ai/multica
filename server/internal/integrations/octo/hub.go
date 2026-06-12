package octo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/octo/transport"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Hub tuning. A WS lease lets only one server replica hold the long connection
// for a given installation; the lease is renewed well within its TTL.
const (
	defaultLeaseTTL         = 90 * time.Second
	defaultLeaseRenew       = 30 * time.Second
	defaultSweepInterval    = 30 * time.Second
	defaultSuperviseBackoff = 5 * time.Second
	defaultShutdownTimeout  = 10 * time.Second
)

// HubQueries is the subset of generated queries the Hub needs.
type HubQueries interface {
	ListActiveOctoInstallations(ctx context.Context) ([]db.OctoInstallation, error)
	AcquireOctoWSLease(ctx context.Context, arg db.AcquireOctoWSLeaseParams) (db.OctoInstallation, error)
	ReleaseOctoWSLease(ctx context.Context, arg db.ReleaseOctoWSLeaseParams) error
}

// InboundHandler is what the Hub calls for each decoded message — satisfied by
// *Dispatcher.Handle.
type InboundHandler interface {
	Handle(ctx context.Context, msg InboundMessage) (DispatchResult, error)
}

// Connector is the per-installation transport. Run blocks until ctx is
// cancelled (lease lost / shutdown) or the connection terminally fails. The
// production connector wraps a transport.Socket; tests provide a fake.
type Connector interface {
	Run(ctx context.Context, inst db.OctoInstallation, onMessage func(transport.BotMessage)) error
}

// ConnectorFactory builds a Connector for an installation. The factory needs
// the decrypted bot token, so it composes the InstallationService.
type ConnectorFactory func(inst db.OctoInstallation) (Connector, error)

// HubConfig tunes lease and sweep timing; zero values fall back to defaults.
type HubConfig struct {
	LeaseTTL           time.Duration
	LeaseRenewInterval time.Duration
	SweepInterval      time.Duration
	// ShutdownTimeout bounds how long WaitWithTimeout blocks for supervisors to
	// exit (and release their leases) during graceful shutdown. Zero falls back
	// to defaultShutdownTimeout.
	ShutdownTimeout time.Duration
}

func (c HubConfig) withDefaults() HubConfig {
	if c.LeaseTTL == 0 {
		c.LeaseTTL = defaultLeaseTTL
	}
	if c.LeaseRenewInterval == 0 {
		c.LeaseRenewInterval = defaultLeaseRenew
	}
	if c.SweepInterval == 0 {
		c.SweepInterval = defaultSweepInterval
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = defaultShutdownTimeout
	}
	return c
}

// Hub owns the per-installation WS connections. It sweeps active installations,
// claims a WS lease for each, and runs a Connector that bridges inbound
// messages to the Dispatcher. Reconnect within a held lease is the Connector's
// concern (transport.Socket reconnects internally); the Hub handles lease lifecycle
// and starting/stopping supervisors as installations come and go.
type Hub struct {
	queries  HubQueries
	factory  ConnectorFactory
	dispatch InboundHandler
	replier  OutcomeReplier
	cfg      HubConfig
	nodeID   string
	logger   *slog.Logger

	mu          sync.Mutex
	supervisors map[string]*supervisorHandle // installation id -> handle
	wg          sync.WaitGroup
}

// supervisorHandle tracks a running per-installation supervisor: its cancel
// func and the installation's updated_at at the time it was started. The hub
// compares updatedAt on each sweep to detect a reconfigure (token rotation,
// re-register) and restart the supervisor so the connector picks up the new
// config — a running connector holds an in-memory snapshot of the installation
// row, so an in-place DB update alone never reaches the live connection.
type supervisorHandle struct {
	cancel    context.CancelFunc
	updatedAt time.Time
}

// NewHub constructs a Hub over the supplied queries, connector factory, and
// inbound dispatcher. The outbound replier defaults to noop; call
// SetOutcomeReplier to install the production one.
func NewHub(queries HubQueries, factory ConnectorFactory, dispatch InboundHandler, cfg HubConfig, logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		queries:     queries,
		factory:     factory,
		dispatch:    dispatch,
		replier:     NewNoopOutcomeReplier(logger),
		cfg:         cfg.withDefaults(),
		nodeID:      newNodeID(),
		logger:      logger,
		supervisors: make(map[string]*supervisorHandle),
	}
}

// SetOutcomeReplier installs the production replier on the Hub. Must be called
// before Run so it is visible to the supervisor goroutines. A nil replier
// resets back to the noop replier (useful for tests).
func (h *Hub) SetOutcomeReplier(r OutcomeReplier) {
	if r == nil {
		r = NewNoopOutcomeReplier(h.logger)
	}
	h.replier = r
}

// Run sweeps for active installations until ctx is cancelled, starting a
// supervisor for each newly seen installation. On ctx cancellation it stops all
// supervisors and waits for them to exit.
func (h *Hub) Run(ctx context.Context) {
	ticker := time.NewTicker(h.cfg.SweepInterval)
	defer ticker.Stop()

	h.sweep(ctx)
	for {
		select {
		case <-ctx.Done():
			h.shutdown()
			return
		case <-ticker.C:
			h.sweep(ctx)
		}
	}
}

// sweep lists active installations and ensures a supervisor runs for each. It
// does not stop supervisors for installations that vanished — those exit on
// their own when the connection drops and the lease can't be renewed (revoked
// rows stop being returned here, so their next renewal fails and they unwind).
// When an installation's config changed since its supervisor started (updated_at
// advanced — a token rotation or re-register), the supervisor is cancelled so a
// subsequent sweep restarts it with the fresh config.
func (h *Hub) sweep(ctx context.Context) {
	insts, err := h.queries.ListActiveOctoInstallations(ctx)
	if err != nil {
		h.logger.Error("octo hub: list active installations failed", "err", err.Error())
		return
	}
	active := make(map[string]struct{}, len(insts))
	for _, inst := range insts {
		id := uuidString(inst.ID)
		active[id] = struct{}{}
		h.mu.Lock()
		handle, running := h.supervisors[id]
		h.mu.Unlock()
		if running {
			// Reconfigured in place: cancel the stale supervisor (it releases
			// its lease and removes itself on exit); the next sweep restarts it
			// with the new config. Skip starting now to avoid racing the
			// in-flight removal.
			if inst.UpdatedAt.Valid && !inst.UpdatedAt.Time.Equal(handle.updatedAt) {
				h.logger.Info("octo hub: installation reconfigured, restarting supervisor", "installation", id)
				handle.cancel()
			}
			continue
		}
		h.startSupervisor(ctx, inst)
	}
}

// startSupervisor launches the per-installation goroutine that holds the lease
// and runs the connector.
func (h *Hub) startSupervisor(parent context.Context, inst db.OctoInstallation) {
	id := uuidString(inst.ID)
	ctx, cancel := context.WithCancel(parent)

	h.mu.Lock()
	if _, exists := h.supervisors[id]; exists {
		h.mu.Unlock()
		cancel()
		return
	}
	h.supervisors[id] = &supervisorHandle{cancel: cancel, updatedAt: inst.UpdatedAt.Time}
	h.mu.Unlock()

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		defer func() {
			h.mu.Lock()
			delete(h.supervisors, id)
			h.mu.Unlock()
		}()
		h.supervise(ctx, inst, id)
	}()
}

// supervise acquires the WS lease, runs the connector while renewing the lease,
// and retries with backoff on failure until ctx is cancelled.
func (h *Hub) supervise(ctx context.Context, inst db.OctoInstallation, id string) {
	token := h.nodeID + ":" + id
	for {
		if ctx.Err() != nil {
			return
		}
		ok, err := h.acquireLease(ctx, inst.ID, token)
		if err != nil {
			h.logger.Error("octo hub: acquire lease failed", "installation", id, "err", err.Error())
		}
		if !ok {
			// Another replica holds the lease; back off and retry.
			if sleepCtx(ctx, h.cfg.LeaseRenewInterval) {
				return
			}
			continue
		}

		runCtx, cancelRun := context.WithCancel(ctx)
		var renewWG sync.WaitGroup
		renewWG.Add(1)
		go func() {
			defer renewWG.Done()
			h.renewLeaseUntil(runCtx, cancelRun, inst.ID, token)
		}()

		conn, err := h.factory(inst)
		if err != nil {
			h.logger.Error("octo hub: build connector failed", "installation", id, "err", err.Error())
		} else {
			runErr := conn.Run(runCtx, inst, func(m transport.BotMessage) {
				h.onMessage(runCtx, inst, m)
			})
			if runErr != nil && runCtx.Err() == nil {
				h.logger.Warn("octo hub: connector ended", "installation", id, "err", runErr.Error())
			}
		}

		cancelRun()
		renewWG.Wait()
		h.releaseLease(inst.ID, token)

		if sleepCtx(ctx, defaultSuperviseBackoff) {
			return
		}
	}
}

// onMessage bridges a transport message to the dispatcher. The Connector has
// already populated routing fields; here we just hand it off.
func (h *Hub) onMessage(ctx context.Context, inst db.OctoInstallation, m transport.BotMessage) {
	// Ignore traffic that isn't a real inbound user message, BEFORE any dedup,
	// dispatch, or audit work:
	//   1. The bot's own messages echoed back to its socket (from_uid == robot
	//      id). Without this, every outbound reply loops back in and is treated
	//      as a new unbound-user message, triggering a bogus binding prompt that
	//      the bot then tries to DM to itself.
	//   2. Non-conversation channels. Octo emits system/command channels (e.g.
	//      channel_type 8 "systemcmdonline" on connect) that aren't DM/group/
	//      topic; they otherwise slip past the group-mention gate and get an
	//      unsolicited "please bind" reply with an empty sender uid.
	// Both are dropped silently (no audit row, no dedup churn) — they are not
	// user traffic. Mirrors the reference channel's processMessage guards.
	if m.FromUID == inst.RobotID {
		return
	}
	if !isConversationChannel(m.ChannelType) {
		return
	}
	msg := InboundMessage{
		RobotID:        inst.RobotID,
		MessageID:      m.MessageID,
		SenderUID:      UID(m.FromUID),
		ChannelID:      ChannelID(m.ChannelID),
		ChannelType:    ChannelType(m.ChannelType),
		Body:           m.Payload.Content,
		AddressedToBot: addressedToBot(inst.RobotID, m),
	}
	res, err := h.dispatch.Handle(ctx, msg)
	if err != nil {
		h.logger.Error("octo hub: dispatch failed", "installation", uuidString(inst.ID), "err", err.Error())
		return
	}
	// Outbound side effects for the synchronous outcomes (binding prompt,
	// agent-unavailable notice). Best-effort: the replier swallows its own
	// errors so a Octo outage never blocks inbound processing.
	h.replier.Reply(ctx, inst, msg, res)
}

// isConversationChannel reports whether a channel type is a real user
// conversation (DM, group, or community topic). Octo also emits system/command
// channels (e.g. channel_type 8 "systemcmdonline" on connect) that must not be
// dispatched as user messages.
func isConversationChannel(t transport.ChannelType) bool {
	switch t {
	case transport.ChannelDM, transport.ChannelGroup, transport.ChannelTopic:
		return true
	default:
		return false
	}
}

// addressedToBot reports whether a group message targets the bot (@mention).
// DMs are always addressed; for groups we check the mention uid list.
func addressedToBot(robotID string, m transport.BotMessage) bool {
	if transport.ChannelType(m.ChannelType) == transport.ChannelDM {
		return true
	}
	if m.Payload.Mention == nil {
		return false
	}
	for _, uid := range m.Payload.Mention.UIDs {
		if uid == robotID {
			return true
		}
	}
	return false
}

func (h *Hub) acquireLease(ctx context.Context, instID pgtype.UUID, token string) (bool, error) {
	_, err := h.queries.AcquireOctoWSLease(ctx, db.AcquireOctoWSLeaseParams{
		ID:           instID,
		NewToken:     pgtype.Text{String: token, Valid: true},
		NewExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(h.cfg.LeaseTTL), Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil // another live holder
		}
		return false, err
	}
	return true, nil
}

// renewLeaseUntil re-acquires the lease on an interval; if a renewal fails (lost
// the lease to another replica, or DB error), it cancels the run so the
// connector unwinds and the supervisor retries from scratch.
func (h *Hub) renewLeaseUntil(ctx context.Context, cancelRun context.CancelFunc, instID pgtype.UUID, token string) {
	ticker := time.NewTicker(h.cfg.LeaseRenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ok, err := h.acquireLease(ctx, instID, token)
			if err != nil {
				if isBenignRenewCancel(ctx, err) {
					// The run context was cancelled intentionally (reconfigure
					// restart or shutdown) and the ticker raced ahead of the
					// ctx.Done branch above. This is not a renewal failure —
					// return quietly without an ERROR log or a redundant
					// cancelRun (the context is already done).
					return
				}
				h.logger.Error("octo hub: renew lease failed", "installation", uuidString(instID), "err", err.Error())
				cancelRun()
				return
			}
			if !ok {
				h.logger.Warn("octo hub: lost lease", "installation", uuidString(instID))
				cancelRun()
				return
			}
		}
	}
}

// isBenignRenewCancel reports whether a lease-renewal error is just the run
// context being cancelled on purpose (reconfigure restart / shutdown) rather
// than a real DB failure. Such an error must not be logged at ERROR level —
// it's the expected outcome of stopping a supervisor.
func isBenignRenewCancel(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, context.Canceled)
}

func (h *Hub) releaseLease(instID pgtype.UUID, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.queries.ReleaseOctoWSLease(ctx, db.ReleaseOctoWSLeaseParams{
		ID:           instID,
		CurrentToken: pgtype.Text{String: token, Valid: true},
	}); err != nil {
		h.logger.Warn("octo hub: release lease failed", "installation", uuidString(instID), "err", err.Error())
	}
}

func (h *Hub) shutdown() {
	h.mu.Lock()
	for _, handle := range h.supervisors {
		handle.cancel()
	}
	h.mu.Unlock()
	h.wg.Wait()
}

// Wait blocks until every supervisor goroutine has exited. A supervisor only
// returns after releasing its WS lease, so once Wait returns this replica holds
// no leases and another replica can take over immediately (no LeaseTTL wait).
// Callers must cancel the context passed to Run first, otherwise Wait blocks
// forever. Prefer WaitWithTimeout in shutdown paths.
func (h *Hub) Wait() {
	h.wg.Wait()
}

// WaitWithTimeout is the bounded variant of Wait. Returns true if all
// supervisors exited (and released their leases) within the deadline, false if
// the timeout fired first. On timeout the process owner should log and proceed:
// orphaned goroutines are reclaimed by the OS and any unreleased lease expires
// naturally after LeaseTTL on the next replica. A timeout <= 0 falls back to
// unbounded Wait.
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

// ShutdownTimeout exposes the configured graceful-shutdown deadline so main.go
// can pass the same value to WaitWithTimeout without re-deriving it.
func (h *Hub) ShutdownTimeout() time.Duration { return h.cfg.ShutdownTimeout }

// sleepCtx sleeps for d or until ctx is cancelled; returns true if cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-t.C:
		return false
	}
}

func newNodeID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
