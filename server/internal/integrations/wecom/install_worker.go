package wecom

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// InstallWorker drives the install state machine independently of any HTTP
// request. It is always constructed. When the service is not fully wired
// (missing secret key / source id / provider) it runs in maintenance mode:
// existing creating/pending rows are marked error/integration_unconfigured
// so no session hangs forever, GC still runs, but no upstream WeCom call
// is issued.
//
// The worker is safe to run across replicas — every mutation matches on
// the current lease token so a stale worker cannot clobber a new owner
// (spec §3.3, §7.1.1).
type InstallWorker struct {
	svc           *InstallService
	supervisor    Notifier
	bus           *events.Bus
	pollInterval  time.Duration
	purgeInterval time.Duration
	done          chan struct{}
	wake          chan struct{}
}

// Notifier is the narrow surface the worker needs on engine.Supervisor —
// exactly Supervisor.Notify. Kept as an interface so tests don't need to
// spin up a real Supervisor.
type Notifier interface {
	Notify()
}

// InstallWorkerConfig configures the worker's cadence hooks. Zero values
// use pragmatic defaults (spec §5.3 / §3.3).
type InstallWorkerConfig struct {
	PollInterval  time.Duration
	PurgeInterval time.Duration
	Bus           *events.Bus
	Supervisor    Notifier
}

// NewInstallWorker binds the worker to a service. Both maintenance mode
// (svc.Configured() == false) and fully-wired mode use the same worker; the
// service's Configured flag gates upstream calls.
func NewInstallWorker(svc *InstallService, cfg InstallWorkerConfig) *InstallWorker {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = time.Second
	}
	if cfg.PurgeInterval == 0 {
		cfg.PurgeInterval = time.Minute
	}
	return &InstallWorker{
		svc:           svc,
		supervisor:    cfg.Supervisor,
		bus:           cfg.Bus,
		pollInterval:  cfg.PollInterval,
		purgeInterval: cfg.PurgeInterval,
		done:          make(chan struct{}),
		wake:          make(chan struct{}, 1),
	}
}

// Notify wakes the worker for an immediate sweep. Safe to call any time —
// a pending wake coalesces.
func (w *InstallWorker) Notify() {
	if w == nil {
		return
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Run is the worker's main loop. Cancelling ctx stops it after the current
// iteration; call WaitWithTimeout to block until the goroutine exits.
func (w *InstallWorker) Run(ctx context.Context) {
	if w == nil {
		return
	}
	defer close(w.done)
	if w.svc == nil {
		return
	}
	pollT := time.NewTicker(w.pollInterval)
	defer pollT.Stop()
	purgeT := time.NewTicker(w.purgeInterval)
	defer purgeT.Stop()

	// Immediately drain on start.
	w.drain(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-purgeT.C:
			if err := w.purge(ctx); err != nil && !errors.Is(err, context.Canceled) {
				w.svc.cfg.Logger.Warn("wecom install worker: purge", "error", err)
			}
		case <-pollT.C:
			w.drain(ctx)
		case <-w.wake:
			w.drain(ctx)
		}
	}
}

// drain processes as many due rows as it can find, one at a time. Empty
// result stops the loop until the next tick / wake.
func (w *InstallWorker) drain(ctx context.Context) {
	for {
		worked, err := w.processOne(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			w.svc.cfg.Logger.Warn("wecom install worker: process", "error", err)
			return
		}
		if !worked {
			return
		}
	}
}

// WaitWithTimeout blocks until Run has returned or the deadline elapses.
// Returns true on clean exit, false on timeout — main.go logs the timeout
// but never blocks process exit indefinitely (spec §7.1.1).
func (w *InstallWorker) WaitWithTimeout(timeout time.Duration) bool {
	if w == nil {
		return true
	}
	if timeout <= 0 {
		<-w.done
		return true
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-w.done:
		return true
	case <-t.C:
		return false
	}
}

// processOne claims one due row and progresses it. Returns worked=true when
// a claim happened (successful or not); false when there was nothing to do.
func (w *InstallWorker) processOne(ctx context.Context) (bool, error) {
	leaseToken, err := mintLeaseToken()
	if err != nil {
		return false, fmt.Errorf("mint lease token: %w", err)
	}
	leaseExpires := w.svc.cfg.Now().Add(w.svc.cfg.LeaseTTL)
	row, err := w.svc.store.ClaimDueWecomInstallSession(ctx, db.ClaimDueWecomInstallSessionParams{
		LeaseToken:     pgtype.Text{String: leaseToken, Valid: true},
		LeaseExpiresAt: pgtype.Timestamptz{Time: leaseExpires, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("claim due session: %w", err)
	}
	// Maintenance mode: the whole integration is unconfigured. Mark the
	// row terminal so the frontend gets a clean error rather than a
	// forever-creating dialog, and clear the ciphertext columns.
	if !w.svc.Configured() {
		w.failSession(ctx, row.ID, leaseToken, InstallErrorIntegrationUnconfigured,
			"WeCom install is not enabled on this server")
		return true, nil
	}
	switch row.Status {
	case InstallStatusCreating:
		return true, w.handleCreating(ctx, row, leaseToken)
	case InstallStatusPending:
		return true, w.handlePending(ctx, row, leaseToken)
	default:
		return true, nil
	}
}

func (w *InstallWorker) handleCreating(ctx context.Context, row db.WecomInstallSession, leaseToken string) error {
	// Deadline guard: if the row has sat in creating past GenerateDeadline
	// without a QR, give up.
	now := w.svc.cfg.Now()
	if row.CreatedAt.Valid && now.Sub(row.CreatedAt.Time) > w.svc.cfg.GenerateDeadline {
		w.failSession(ctx, row.ID, leaseToken, InstallErrorGenerateFailed,
			"Failed to generate WeCom QR code in time")
		return nil
	}
	upstreamCtx, cancel := context.WithTimeout(ctx, w.svc.cfg.UpstreamTimeout)
	defer cancel()
	gen, err := w.svc.cfg.Provider.Generate(upstreamCtx)
	if err != nil {
		w.svc.cfg.Logger.Warn("wecom install: generate failed",
			"session_id", util.UUIDToString(row.ID), "err", err)
		// Retry until the deadline: defer with a short poll_after so the
		// next tick tries again. On the next attempt the deadline guard
		// above will terminate the session if we've run out of time.
		nextPoll := now.Add(w.svc.cfg.CreatingPollInterval)
		_, err := w.svc.store.DeferClaimedWecomInstallSession(ctx, db.DeferClaimedWecomInstallSessionParams{
			ID:         row.ID,
			LeaseToken: pgtype.Text{String: leaseToken, Valid: true},
			PollAfter:  pgtype.Timestamptz{Time: nextPoll, Valid: true},
			Status:     InstallStatusCreating,
		})
		return err
	}
	scodeEnc, err := sealAndEncode(w.svc.cfg.Box, []byte(gen.Scode))
	if err != nil {
		return fmt.Errorf("seal scode: %w", err)
	}
	urlEnc, err := sealAndEncode(w.svc.cfg.Box, []byte(gen.AuthURL))
	if err != nil {
		return fmt.Errorf("seal auth_url: %w", err)
	}
	expires := now.Add(w.svc.cfg.QRTTL)
	nextPoll := now.Add(w.svc.cfg.PendingPollInterval)
	if _, err := w.svc.store.DeferClaimedWecomInstallSession(ctx, db.DeferClaimedWecomInstallSessionParams{
		ID:                 row.ID,
		LeaseToken:         pgtype.Text{String: leaseToken, Valid: true},
		PollAfter:          pgtype.Timestamptz{Time: nextPoll, Valid: true},
		Status:             InstallStatusPending,
		ScodeEncrypted:     pgtype.Text{String: scodeEnc, Valid: true},
		QrCodeUrlEncrypted: pgtype.Text{String: urlEnc, Valid: true},
		ExpiresAt:          pgtype.Timestamptz{Time: expires, Valid: true},
	}); err != nil {
		return fmt.Errorf("defer to pending: %w", err)
	}
	return nil
}

func (w *InstallWorker) handlePending(ctx context.Context, row db.WecomInstallSession, leaseToken string) error {
	now := w.svc.cfg.Now()
	if row.ExpiresAt.Valid && !row.ExpiresAt.Time.After(now) {
		w.failSession(ctx, row.ID, leaseToken, InstallErrorExpired,
			"The WeCom QR code has expired")
		return nil
	}
	if !row.ScodeEncrypted.Valid || w.svc.cfg.Box == nil {
		w.failSession(ctx, row.ID, leaseToken, InstallErrorInternalError,
			"Install session is missing state")
		return nil
	}
	scode, err := decodeAndOpen(w.svc.cfg.Box, row.ScodeEncrypted.String)
	if err != nil {
		w.failSession(ctx, row.ID, leaseToken, InstallErrorInternalError,
			"Install session state could not be decrypted")
		return nil
	}
	upstreamCtx, cancel := context.WithTimeout(ctx, w.svc.cfg.UpstreamTimeout)
	defer cancel()
	res, err := w.svc.cfg.Provider.QueryResult(upstreamCtx, string(scode))
	if err != nil {
		w.svc.cfg.Logger.Warn("wecom install: query_result failed",
			"session_id", util.UUIDToString(row.ID), "err", err)
		nextPoll := now.Add(w.svc.cfg.PendingPollInterval)
		_, deferErr := w.svc.store.DeferClaimedWecomInstallSession(ctx, db.DeferClaimedWecomInstallSessionParams{
			ID:         row.ID,
			LeaseToken: pgtype.Text{String: leaseToken, Valid: true},
			PollAfter:  pgtype.Timestamptz{Time: nextPoll, Valid: true},
			Status:     InstallStatusPending,
		})
		return deferErr
	}
	switch res.Status {
	case QueryStatusInit, QueryStatusPending:
		// Post-response expiry check (spec §4.2 priority 3).
		if row.ExpiresAt.Valid && !row.ExpiresAt.Time.After(w.svc.cfg.Now()) {
			w.failSession(ctx, row.ID, leaseToken, InstallErrorExpired,
				"The WeCom QR code has expired")
			return nil
		}
		nextPoll := now.Add(w.svc.cfg.PendingPollInterval)
		_, err := w.svc.store.DeferClaimedWecomInstallSession(ctx, db.DeferClaimedWecomInstallSessionParams{
			ID:         row.ID,
			LeaseToken: pgtype.Text{String: leaseToken, Valid: true},
			PollAfter:  pgtype.Timestamptz{Time: nextPoll, Valid: true},
			Status:     InstallStatusPending,
		})
		return err
	case QueryStatusSuccess:
		return w.finalizeSuccess(ctx, row, leaseToken, res.BotInfo)
	}
	return nil
}

// finalizeSuccess binds the scanned bot and settles the session.
//
// The bind goes through InstallationService.Upsert — the same path the manual
// bot_id + secret form uses — so reclaim-then-upsert, the (channel_type, app_id)
// routing slot, and the owner-conflict message all behave identically however a
// bot arrived.
//
// The bind and the session completion are two writes, not one transaction, and
// that is safe because Upsert is idempotent on (workspace, agent, wecom): if the
// completion fails after a successful bind, the next poll re-upserts the same
// row and completes again. A single transaction would instead have to hold
// Upsert's own transaction open across this function.
func (w *InstallWorker) finalizeSuccess(ctx context.Context, row db.WecomInstallSession, leaseToken string, bot *BotInfo) error {
	if bot == nil || strings.TrimSpace(bot.BotID) == "" || strings.TrimSpace(bot.Secret) == "" {
		w.failSession(ctx, row.ID, leaseToken, InstallErrorWecomProtocolError,
			"WeCom returned success without bot credentials")
		return nil
	}

	inst, err := w.svc.installations.Upsert(ctx, InstallationParams{
		WorkspaceID:     row.WorkspaceID,
		AgentID:         row.AgentID,
		InstallerUserID: row.InitiatorUserID,
		BotID:           bot.BotID,
		Secret:          bot.Secret,
	})
	if err != nil {
		// A conflict here means the bot's routing slot is held elsewhere. The bot
		// exists on WeCom's side either way, so the session must end with an
		// actionable reason rather than retry into the same conflict.
		w.svc.cfg.Logger.WarnContext(ctx, "wecom install: bind failed",
			"session_id", util.UUIDToString(row.ID), "error", err)
		w.failSession(ctx, row.ID, leaseToken, InstallErrorInstallationConflict,
			"Could not finalize the WeCom install: "+err.Error())
		return nil
	}

	rows, err := w.svc.store.CompleteWecomInstallSession(ctx, db.CompleteWecomInstallSessionParams{
		InstallationID: inst.ID,
		ID:             row.ID,
		LeaseToken:     pgtype.Text{String: leaseToken, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("finalize: complete session: %w", err)
	}
	if rows == 0 {
		// Lost the lease mid-finalize: another worker owns this session and will
		// complete it. The Upsert above was idempotent, so nothing is left over.
		return nil
	}

	// Published only after the session is durably complete, so a failed
	// completion cannot fire a phantom installation-created event.
	if w.bus != nil {
		w.bus.Publish(events.Event{
			Type:        protocol.EventWecomInstallationCreated,
			WorkspaceID: util.UUIDToString(row.WorkspaceID),
			ActorType:   "system",
			ActorID:     util.UUIDToString(row.InitiatorUserID),
			Payload: map[string]any{
				"installation_id": util.UUIDToString(inst.ID),
				"agent_id":        util.UUIDToString(row.AgentID),
			},
		})
	}
	// Nudge the supervisor so the new bot's connection comes up now rather than
	// on the next installation scan.
	if w.supervisor != nil {
		w.supervisor.Notify()
	}
	w.svc.cfg.Logger.InfoContext(ctx, "wecom install: complete",
		"session_id", util.UUIDToString(row.ID),
		"installation_id", util.UUIDToString(inst.ID),
		"workspace_id", util.UUIDToString(row.WorkspaceID),
		"agent_id", util.UUIDToString(row.AgentID))
	return nil
}

// failSession is the idempotent terminal marker. Logs infra errors but never
// returns them — the worker's loop must move on regardless.
//
// Takes the lease it was claimed under, like every other mutation here: a worker
// whose lease has already expired must not be able to mark a session error out
// from under the replica that now owns it (see this file's header).
func (w *InstallWorker) failSession(ctx context.Context, id pgtype.UUID, leaseToken, reason, message string) {
	if _, err := w.svc.store.FailWecomInstallSession(ctx, db.FailWecomInstallSessionParams{
		ID:           id,
		LeaseToken:   pgtype.Text{String: leaseToken, Valid: leaseToken != ""},
		ErrorReason:  pgtype.Text{String: strings.TrimSpace(reason), Valid: reason != ""},
		ErrorMessage: pgtype.Text{String: message, Valid: message != ""},
	}); err != nil && !errors.Is(err, context.Canceled) {
		w.svc.cfg.Logger.Warn("wecom install: fail session",
			"session_id", util.UUIDToString(id), "err", err)
	}
}

func (w *InstallWorker) purge(ctx context.Context) error {
	now := w.svc.cfg.Now()
	terminalCutoff := now.Add(-w.svc.cfg.TerminalRetention)
	// Sessions that never got claimed should not live forever either: the
	// generate deadline plus a little slack is the reasonable ceiling.
	creatingCutoff := now.Add(-2 * w.svc.cfg.GenerateDeadline)
	_, err := w.svc.store.PurgeTerminalWecomInstallSessions(ctx, db.PurgeTerminalWecomInstallSessionsParams{
		TerminalCutoff: pgtype.Timestamptz{Time: terminalCutoff, Valid: true},
		CreatingCutoff: pgtype.Timestamptz{Time: creatingCutoff, Valid: true},
	})
	return err
}
