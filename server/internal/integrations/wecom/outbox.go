package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	outboundLeaseTTL   = 30 * time.Second
	outboundPollEvery  = 2 * time.Second
	outboundClaimLease = 30 * time.Second
)

type bindingMinter interface {
	Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, wecomUserID string) (BindingToken, error)
}

type rateGate interface {
	Reserve(ctx context.Context, row db.ChannelOutboundQueue) (deferUntil time.Time, ok bool, err error)
}

type sendRequester interface {
	SendRequest(ctx context.Context, cmd string, body any) (Response, error)
}

type outboxStore interface {
	ClaimChannelOutbound(ctx context.Context, arg db.ClaimChannelOutboundParams) (db.ChannelOutboundQueue, error)
	DeferClaimedChannelOutbound(ctx context.Context, arg db.DeferClaimedChannelOutboundParams) (db.ChannelOutboundQueue, error)
	RetryClaimedChannelOutbound(ctx context.Context, arg db.RetryClaimedChannelOutboundParams) (db.ChannelOutboundQueue, error)
	CompleteClaimedChannelOutbound(ctx context.Context, arg db.CompleteClaimedChannelOutboundParams) (db.ChannelOutboundQueue, error)
	FailClaimedChannelOutbound(ctx context.Context, arg db.FailClaimedChannelOutboundParams) (db.ChannelOutboundQueue, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChatSession(ctx context.Context, id pgtype.UUID) (db.ChatSession, error)
}

// OutboxConsumer drains channel_outbound_queue for one installation on the
// WS lease holder (spec §5.3).
type OutboxConsumer struct {
	installationID string
	instUUID       pgtype.UUID
	locale         string
	q              outboxStore
	binding        bindingMinter
	rate           rateGate
	conn           sendRequester
	wake           <-chan struct{}
	appURL         string
	logger         *slog.Logger
	metrics        WecomMetrics
	now            func() time.Time
}

// OutboxConsumerConfig wires one installation consumer.
type OutboxConsumerConfig struct {
	InstallationID string
	Locale         string
	Queries        outboxStore
	Binding        bindingMinter
	Rate           rateGate
	Conn           sendRequester
	Wake           <-chan struct{}
	AppURL         string
	Logger         *slog.Logger
	Metrics        WecomMetrics
	Now            func() time.Time
}

// NewOutboxConsumer builds a consumer for Connect's lifetime.
func NewOutboxConsumer(cfg OutboxConsumerConfig) (*OutboxConsumer, error) {
	instUUID, err := util.ParseUUID(cfg.InstallationID)
	if err != nil || !instUUID.Valid {
		return nil, errors.New("wecom outbox: invalid installation id")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	metrics := cfg.Metrics
	if metrics == nil {
		metrics = NoopMetrics()
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &OutboxConsumer{
		installationID: cfg.InstallationID,
		instUUID:       instUUID,
		locale:         cfg.Locale,
		q:              cfg.Queries,
		binding:        cfg.Binding,
		rate:           cfg.Rate,
		conn:           cfg.Conn,
		wake:           cfg.Wake,
		appURL:         cfg.AppURL,
		logger:         logger,
		metrics:        metrics,
		now:            now,
	}, nil
}

// Run processes queued rows until ctx is cancelled.
func (c *OutboxConsumer) Run(ctx context.Context) {
	if c.q == nil || c.conn == nil {
		return
	}
	poll := time.NewTicker(outboundPollEvery)
	defer poll.Stop()
	for {
		worked, err := c.processOne(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			c.logger.WarnContext(ctx, "wecom outbox: process failed",
				"installation_id", c.installationID,
				"error", err,
			)
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		case <-poll.C:
		}
	}
}

func (c *OutboxConsumer) processOne(ctx context.Context) (bool, error) {
	row, err := c.q.ClaimChannelOutbound(ctx, db.ClaimChannelOutboundParams{
		InstallationID: c.instUUID,
		LeaseExpiresAt: pgtype.Timestamptz{Time: c.now().Add(outboundClaimLease), Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if !row.LeaseToken.Valid {
		return true, errors.New("wecom outbox: claimed row missing lease token")
	}
	lease := row.LeaseToken.String

	if err := c.deliverClaimed(ctx, row, lease); err != nil && !errors.Is(err, context.Canceled) {
		c.logger.WarnContext(ctx, "wecom outbox: deliver failed",
			"queue_id", util.UUIDToString(row.ID),
			"source_kind", row.SourceKind,
			"error", err,
		)
	}
	return true, nil
}

func (c *OutboxConsumer) deliverClaimed(ctx context.Context, row db.ChannelOutboundQueue, lease string) error {
	inst, err := c.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          row.InstallationID,
		ChannelType: string(TypeWecom),
	})
	// A failed read says nothing about whether the installation is still
	// active, so only a genuinely missing row is terminal; anything else is
	// retried rather than permanently dropping a user-visible reply.
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return c.retryOrFail(ctx, row, lease, fmt.Errorf("load installation: %w", err))
		}
		return c.terminate(ctx, row, lease, deliveryOutcomeFenced, "installation not found")
	}
	if inst.Status != "active" {
		return c.terminate(ctx, row, lease, deliveryOutcomeFenced, "installation inactive")
	}

	if row.ChatSessionID.Valid {
		reason, err := c.checkChatSessionDeliverable(ctx, row)
		if err != nil {
			return c.retryOrFail(ctx, row, lease, err)
		}
		if reason != "" {
			return c.terminate(ctx, row, lease, deliveryOutcomeFenced, reason)
		}
	}

	if row.PayloadVersion != 1 && row.PayloadVersion != 0 {
		return c.terminate(ctx, row, lease, deliveryOutcomeFailed, "unsupported payload version")
	}

	deferAt, allowed, err := c.rate.Reserve(ctx, row)
	if err != nil {
		return err
	}
	if !allowed {
		_, err := c.q.DeferClaimedChannelOutbound(ctx, db.DeferClaimedChannelOutboundParams{
			ID:            row.ID,
			LeaseToken:    pgtype.Text{String: lease, Valid: true},
			NextAttemptAt: pgtype.Timestamptz{Time: deferAt, Valid: true},
		})
		if err == nil {
			c.metrics.RecordOutboundDelivery(deliveryOutcomeDeferred)
		}
		return err
	}

	bindingToken := ""
	var payload outboundPayload
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		return c.terminate(ctx, row, lease, deliveryOutcomeFailed, "invalid payload")
	}
	if payload.Template == templateBindingPrompt && row.TargetChatType == TargetChatTypeP2P && c.binding != nil {
		tok, err := c.binding.Mint(ctx, row.WorkspaceID, row.InstallationID, row.TargetChatID)
		if err != nil {
			return c.retryOrFail(ctx, row, lease, fmt.Errorf("mint binding token: %w", err))
		}
		bindingToken = tok.Raw
	}

	workspaceSlug := ""
	if ws, err := c.q.GetWorkspace(ctx, row.WorkspaceID); err == nil {
		workspaceSlug = ws.Slug
	}
	sessionID := ""
	if row.ChatSessionID.Valid {
		sessionID = util.UUIDToString(row.ChatSessionID)
	}
	body, err := RenderOutbound(row.Payload, RenderInput{
		Locale:          c.locale,
		AppURL:          c.appURL,
		WorkspaceSlug:   workspaceSlug,
		ChatSessionID:   sessionID,
		BindingTokenRaw: bindingToken,
	})
	if err != nil {
		return c.terminate(ctx, row, lease, deliveryOutcomeFailed, err.Error())
	}

	sendBody := SendMsgBody{
		ChatID:   row.TargetChatID,
		ChatType: int(row.TargetChatType),
		MsgType:  "markdown",
		Markdown: &MarkdownBody{Content: body},
	}
	resp, sendErr := c.conn.SendRequest(ctx, CmdSendMsg, sendBody)
	if sendErr != nil {
		if payload.Template == templateBindingPrompt && isAmbiguousSendError(sendErr) {
			return c.complete(ctx, row, lease)
		}
		if classifySendRetryable(sendErr, resp.ErrCode) {
			return c.retryOrFail(ctx, row, lease, sendErr)
		}
		return c.terminate(ctx, row, lease, deliveryOutcomeFailed, sendErr.Error())
	}
	if resp.ErrCode != 0 {
		sendErr = fmt.Errorf("wecom send errcode=%d errmsg=%q", resp.ErrCode, resp.ErrMsg)
		if classifySendRetryable(sendErr, resp.ErrCode) {
			return c.retryOrFail(ctx, row, lease, sendErr)
		}
		return c.terminate(ctx, row, lease, deliveryOutcomeFailed, sendErr.Error())
	}
	return c.complete(ctx, row, lease)
}

// terminate moves a claimed row to `failed` with a fixed reason and records
// the outcome once. deliveryOutcomeFenced means the target stopped being
// deliverable after enqueue (revoked installation, unbound or archived
// session); deliveryOutcomeFailed means we tried and could not deliver.
func (c *OutboxConsumer) terminate(ctx context.Context, row db.ChannelOutboundQueue, lease, outcome, reason string) error {
	_, err := c.q.FailClaimedChannelOutbound(ctx, db.FailClaimedChannelOutboundParams{
		ID:         row.ID,
		LeaseToken: pgtype.Text{String: lease, Valid: true},
		LastError:  pgtype.Text{String: sanitizeLastError(reason), Valid: true},
	})
	if err == nil {
		c.metrics.RecordOutboundDelivery(outcome)
	}
	return err
}

func (c *OutboxConsumer) complete(ctx context.Context, row db.ChannelOutboundQueue, lease string) error {
	_, err := c.q.CompleteClaimedChannelOutbound(ctx, db.CompleteClaimedChannelOutboundParams{
		ID:         row.ID,
		LeaseToken: pgtype.Text{String: lease, Valid: true},
	})
	if err == nil {
		c.metrics.RecordOutboundDelivery(deliveryOutcomeSent)
	}
	return err
}

// checkChatSessionDeliverable re-verifies the claimed row's chat session is
// still bound to this installation and still active, immediately before
// send. The installation is claimed and the row enqueued asynchronously, so
// an unbind/rebind or session archive between enqueue and this delivery
// attempt must fence the send rather than deliver into a session the target
// no longer owns (spec §5.3.3).
//
// A non-empty reason fences the row terminally; a non-nil error means the check
// itself could not be completed and the row must be retried instead.
func (c *OutboxConsumer) checkChatSessionDeliverable(ctx context.Context, row db.ChannelOutboundQueue) (string, error) {
	binding, err := c.q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: row.ChatSessionID,
		ChannelType:   string(TypeWecom),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "chat session binding not found", nil
		}
		return "", fmt.Errorf("load chat session binding: %w", err)
	}
	if binding.InstallationID != row.InstallationID {
		return "chat session bound to a different installation", nil
	}
	session, err := c.q.GetChatSession(ctx, row.ChatSessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "chat session not found", nil
		}
		return "", fmt.Errorf("load chat session: %w", err)
	}
	if session.Status != "active" {
		return "chat session inactive", nil
	}
	return "", nil
}

func (c *OutboxConsumer) retryOrFail(ctx context.Context, row db.ChannelOutboundQueue, lease string, cause error) error {
	if row.Attempts+1 >= maxOutboundAttempts {
		return c.terminate(ctx, row, lease, deliveryOutcomeFailed, cause.Error())
	}
	next := c.now().Add(outboundBackoff(row.Attempts + 1))
	_, err := c.q.RetryClaimedChannelOutbound(ctx, db.RetryClaimedChannelOutboundParams{
		ID:            row.ID,
		LeaseToken:    pgtype.Text{String: lease, Valid: true},
		NextAttemptAt: pgtype.Timestamptz{Time: next, Valid: true},
		LastError:     pgtype.Text{String: sanitizeLastError(cause.Error()), Valid: true},
	})
	if err == nil {
		c.metrics.RecordOutboundDelivery(deliveryOutcomeRetried)
	}
	return err
}

func isAmbiguousSendError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrConnectionClosed) || errors.Is(err, ErrNotConnected) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "closed")
}

func isRetryableErrcode(code int) bool {
	switch code {
	case 45009, 45033, -1:
		return true
	default:
		return false
	}
}

func classifySendRetryable(err error, errcode int) bool {
	if isRetryableErrcode(errcode) {
		return true
	}
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrConnectionClosed) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "temporary")
}
