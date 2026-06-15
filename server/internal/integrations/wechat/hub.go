package wechat

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

type HubQueries interface {
	ListActiveWechatInstallations(ctx context.Context) ([]db.WechatInstallation, error)
	AcquireWechatWSLease(ctx context.Context, arg db.AcquireWechatWSLeaseParams) (db.WechatInstallation, error)
	ReleaseWechatWSLease(ctx context.Context, arg db.ReleaseWechatWSLeaseParams) error
}

type EventEmitter func(ctx context.Context, msg InboundMessage) (DispatchResult, error)

type EventConnector interface {
	Run(ctx context.Context, inst db.WechatInstallation, emit EventEmitter) error
}

type ConnectorFactory func(inst db.WechatInstallation) (EventConnector, error)

type HubConfig struct {
	LeaseTTL            time.Duration
	LeaseRenewInterval  time.Duration
	PollInterval        time.Duration
	MinBackoff          time.Duration
	MaxBackoff          time.Duration
	ResetBackoffAfter   time.Duration
	LeaseReleaseTimeout time.Duration
	ShutdownTimeout     time.Duration
	Now                 func() time.Time
	Logger              *slog.Logger
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
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

type Hub struct {
	queries    HubQueries
	factory    ConnectorFactory
	dispatcher *Dispatcher
	cfg        HubConfig
	nodeID     string

	mu          sync.Mutex
	supervisors map[string]supervisorEntry
	supGen      uint64
	wg          sync.WaitGroup
	stopped     bool
	stopChan    chan struct{}
}

type supervisorEntry struct {
	cancel      context.CancelFunc
	fingerprint string
	gen         uint64
}

func NewHub(queries HubQueries, factory ConnectorFactory, dispatcher *Dispatcher, cfg HubConfig) *Hub {
	cfg = cfg.withDefaults()
	return &Hub{
		queries:     queries,
		factory:     factory,
		dispatcher:  dispatcher,
		cfg:         cfg,
		nodeID:      newNodeID(),
		supervisors: make(map[string]supervisorEntry),
		stopChan:    make(chan struct{}),
	}
}

func (h *Hub) Run(ctx context.Context) {
	defer close(h.stopChan)
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

func (h *Hub) Wait() {
	h.wg.Wait()
}

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

func (h *Hub) ShutdownTimeout() time.Duration { return h.cfg.ShutdownTimeout }

func (h *Hub) sweep(ctx context.Context) {
	rows, err := h.queries.ListActiveWechatInstallations(ctx)
	if err != nil {
		h.cfg.Logger.Warn("wechat hub: list active installations failed", "error", err)
		return
	}
	active := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		id := uuidString(row.ID)
		active[id] = struct{}{}
		h.maybeRestartOnRotation(id, row)
		h.startSupervisor(ctx, row)
	}
	h.mu.Lock()
	for id, entry := range h.supervisors {
		if _, stillActive := active[id]; !stillActive {
			entry.cancel()
			delete(h.supervisors, id)
		}
	}
	h.mu.Unlock()
}

func (h *Hub) maybeRestartOnRotation(id string, row db.WechatInstallation) {
	want := installationFingerprint(row)
	h.mu.Lock()
	entry, ok := h.supervisors[id]
	if !ok || entry.fingerprint == want {
		h.mu.Unlock()
		return
	}
	h.cfg.Logger.Info("wechat hub: credentials rotated, restarting supervisor",
		"installation_id", id,
		"bot_id", row.BotID,
	)
	entry.cancel()
	delete(h.supervisors, id)
	h.mu.Unlock()
}

func (h *Hub) startSupervisor(parent context.Context, inst db.WechatInstallation) {
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
	h.supGen++
	gen := h.supGen
	h.supervisors[id] = supervisorEntry{
		cancel:      cancel,
		fingerprint: installationFingerprint(inst),
		gen:         gen,
	}
	h.wg.Add(1)
	h.mu.Unlock()
	go h.supervise(ctx, inst, id, gen)
}

func (h *Hub) supervise(ctx context.Context, inst db.WechatInstallation, id string, gen uint64) {
	defer h.wg.Done()
	defer func() {
		h.mu.Lock()
		if entry, ok := h.supervisors[id]; ok && entry.gen == gen {
			delete(h.supervisors, id)
		}
		h.mu.Unlock()
	}()

	leaseTok := leaseToken(h.nodeID, gen)
	log := h.cfg.Logger.With(
		"installation_id", id,
		"node_id", h.nodeID,
	)
	backoff := h.cfg.MinBackoff

	for {
		if ctx.Err() != nil {
			return
		}

		leased, err := h.acquireLease(ctx, inst.ID, leaseTok)
		if err != nil {
			log.Warn("wechat hub: acquire lease error", "error", err)
			if sleep(ctx, h.cfg.LeaseRenewInterval) {
				return
			}
			continue
		}
		if !leased {
			if sleep(ctx, h.cfg.LeaseRenewInterval) {
				return
			}
			continue
		}

		conn, err := h.factory(inst)
		if err != nil {
			log.Error("wechat hub: connector factory failed", "error", err)
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
			h.renewLeaseUntil(runCtx, runCancel, inst.ID, leaseTok)
		}()

		startedAt := h.cfg.Now()
		runErr := conn.Run(runCtx, inst, func(emitCtx context.Context, msg InboundMessage) (DispatchResult, error) {
			if h.dispatcher == nil {
				return DispatchResult{}, errors.New("wechat hub: dispatcher not configured")
			}
			return h.dispatcher.Handle(emitCtx, msg)
		})
		runCancel()
		<-renewDone
		h.releaseLease(inst.ID, leaseTok)

		if ctx.Err() != nil {
			return
		}

		uptime := h.cfg.Now().Sub(startedAt)
		if uptime >= h.cfg.ResetBackoffAfter {
			backoff = h.cfg.MinBackoff
		}
		if runErr != nil {
			log.Warn("wechat hub: connector exited with error", "error", runErr, "uptime", uptime.String())
		}
		if sleep(ctx, jitter(backoff)) {
			return
		}
		backoff = nextBackoff(backoff, h.cfg.MaxBackoff)
	}
}

func (h *Hub) acquireLease(ctx context.Context, instID pgtype.UUID, token string) (bool, error) {
	expires := h.cfg.Now().Add(h.cfg.LeaseTTL)
	_, err := h.queries.AcquireWechatWSLease(ctx, db.AcquireWechatWSLeaseParams{
		ID:           instID,
		NewToken:     pgtype.Text{String: token, Valid: true},
		NewExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true},
	})
	if err == nil {
		return true, nil
	}
	if isNoRowsErr(err) {
		return false, nil
	}
	return false, err
}

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
				h.cfg.Logger.Warn("wechat hub: lease renewal error",
					"installation_id", uuidString(instID),
					"error", err,
				)
				continue
			}
			if !leased {
				h.cfg.Logger.Warn("wechat hub: lease lost; tearing down connector",
					"installation_id", uuidString(instID),
				)
				cancelRun()
				return
			}
		}
	}
}

func (h *Hub) releaseLease(instID pgtype.UUID, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), h.cfg.LeaseReleaseTimeout)
	defer cancel()
	if err := h.queries.ReleaseWechatWSLease(ctx, db.ReleaseWechatWSLeaseParams{
		ID:           instID,
		CurrentToken: pgtype.Text{String: token, Valid: true},
	}); err != nil {
		h.cfg.Logger.Warn("wechat hub: release lease failed",
			"installation_id", uuidString(instID),
			"error", err,
		)
	}
}

func (h *Hub) cancelAll() {
	h.mu.Lock()
	h.stopped = true
	for id, entry := range h.supervisors {
		entry.cancel()
		delete(h.supervisors, id)
	}
	h.mu.Unlock()
}

func installationFingerprint(inst db.WechatInstallation) string {
	sum := sha256.Sum256(inst.SecretEncrypted)
	return inst.BotID + "|" + hex.EncodeToString(sum[:])
}

func leaseToken(nodeID string, gen uint64) string {
	return nodeID + "-g" + strconv.FormatUint(gen, 10)
}

func newNodeID() string {
	buf := make([]byte, 16)
	if _, err := cryptorand.Read(buf); err != nil {
		return fmt.Sprintf("nodeid-fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		return max
	}
	return next
}

func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	delta := d / 2
	return d - delta + time.Duration(mathrand.Int64N(int64(2*delta)+1))
}

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

func isNoRowsErr(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == "no rows in result set"
}

func uuidString(u pgtype.UUID) string { return util.UUIDToString(u) }
