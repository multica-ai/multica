package lark

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	inboundDeliveryConcurrency  = 2
	inboundDeliveryMaxAttempts  = 3
	inboundDeliveryPollInterval = time.Second
	inboundDeliveryTimeout      = 5 * time.Minute
)

type InboundDeliveryAcceptor interface {
	Enqueue(ctx context.Context, inst Installation, msg InboundMessage) error
}

// InboundDeliveryWorker moves every network and agent operation out of Lark's
// acknowledgement path. Postgres is both the restart journal and replica lease;
// the notification channel only removes local polling latency.
type InboundDeliveryWorker struct {
	queries       *db.Queries
	installations *InstallationService
	enricher      Enricher
	handler       channel.InboundHandler
	logger        *slog.Logger
	notify        chan struct{}
	done          chan struct{}
}

func NewInboundDeliveryWorker(queries *db.Queries, installations *InstallationService, enricher Enricher, handler channel.InboundHandler, logger *slog.Logger) *InboundDeliveryWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &InboundDeliveryWorker{
		queries: queries, installations: installations, enricher: enricher, handler: handler, logger: logger,
		notify: make(chan struct{}, inboundDeliveryConcurrency), done: make(chan struct{}),
	}
}

func (w *InboundDeliveryWorker) Enqueue(ctx context.Context, inst Installation, msg InboundMessage) error {
	if w == nil || w.queries == nil {
		return errors.New("lark inbound delivery queue is unavailable")
	}
	if !inst.ID.Valid || msg.MessageID == "" {
		return errors.New("lark inbound delivery is missing identity")
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("encode lark inbound delivery: %w", err)
	}
	_, err = w.queries.EnqueueChannelInboundDelivery(ctx, db.EnqueueChannelInboundDeliveryParams{
		InstallationID: inst.ID,
		MessageID:      msg.MessageID,
		SequenceKey:    inboundSequenceKey(inst.ID, msg),
		Payload:        payload,
	})
	if err != nil {
		return fmt.Errorf("persist lark inbound delivery: %w", err)
	}
	select {
	case w.notify <- struct{}{}:
	default:
	}
	return nil
}

func inboundSequenceKey(installationID pgtype.UUID, msg InboundMessage) string {
	h := sha256.New()
	_, _ = h.Write(installationID.Bytes[:])
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(msg.ChatID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(msg.ThreadID))
	return hex.EncodeToString(h.Sum(nil))
}

func (w *InboundDeliveryWorker) Run(ctx context.Context) {
	if w == nil {
		return
	}
	defer close(w.done)
	if w.queries == nil || w.installations == nil || w.handler == nil {
		return
	}
	var workers sync.WaitGroup
	workers.Add(inboundDeliveryConcurrency)
	for range inboundDeliveryConcurrency {
		go func() {
			defer workers.Done()
			w.runLoop(ctx)
		}()
	}
	workers.Wait()
}

func (w *InboundDeliveryWorker) runLoop(ctx context.Context) {
	ticker := time.NewTicker(inboundDeliveryPollInterval)
	defer ticker.Stop()
	for {
		worked, err := w.ProcessNext(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			w.logger.Error("lark inbound worker: process delivery", "error", err)
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-w.notify:
		case <-ticker.C:
		}
	}
}

func (w *InboundDeliveryWorker) WaitWithTimeout(timeout time.Duration) bool {
	if w == nil {
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-w.done:
		return true
	case <-timer.C:
		return false
	}
}

func (w *InboundDeliveryWorker) ProcessNext(ctx context.Context) (bool, error) {
	delivery, err := w.queries.ClaimChannelInboundDelivery(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim inbound delivery: %w", err)
	}
	jobCtx, cancel := context.WithTimeout(ctx, inboundDeliveryTimeout)
	defer cancel()

	processErr := w.process(jobCtx, delivery)
	if processErr == nil {
		return true, w.complete(ctx, delivery, "completed", "")
	}
	if IsRetryableResourceError(processErr) && delivery.Attempts+1 >= inboundDeliveryMaxAttempts {
		if err := w.processWithoutAttachments(jobCtx, delivery); err == nil {
			return true, w.complete(ctx, delivery, "completed", "attachments_unavailable")
		} else {
			processErr = err
		}
	}
	if delivery.Attempts+1 >= inboundDeliveryMaxAttempts {
		return true, w.complete(ctx, delivery, "failed", deliveryErrorCategory(processErr))
	}
	backoff := time.Second * time.Duration(1<<delivery.Attempts)
	_, err = w.queries.RetryChannelInboundDelivery(ctx, db.RetryChannelInboundDeliveryParams{
		ID: delivery.ID, LeaseToken: delivery.LeaseToken,
		AvailableAt: pgtype.Timestamptz{Time: time.Now().Add(backoff), Valid: true},
		LastError:   pgtype.Text{String: deliveryErrorCategory(processErr), Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	return true, err
}

func (w *InboundDeliveryWorker) process(ctx context.Context, delivery db.ChannelInboundDelivery) error {
	var msg InboundMessage
	if err := json.Unmarshal(delivery.Payload, &msg); err != nil {
		return errors.New("decode stored inbound delivery")
	}
	inst, creds, err := w.loadInstallation(ctx, delivery.InstallationID)
	if err != nil {
		return err
	}
	if msg.AppID == "" || msg.AppID != inst.AppID {
		return errors.New("stored inbound delivery installation mismatch")
	}
	if w.enricher != nil {
		msg = w.enricher.Enrich(ctx, msg, creds)
	}
	return w.handler(ctx, channelMessageFromLark(msg))
}

func (w *InboundDeliveryWorker) processWithoutAttachments(ctx context.Context, delivery db.ChannelInboundDelivery) error {
	var msg InboundMessage
	if err := json.Unmarshal(delivery.Payload, &msg); err != nil {
		return errors.New("decode stored inbound delivery")
	}
	inst, creds, err := w.loadInstallation(ctx, delivery.InstallationID)
	if err != nil {
		return err
	}
	if msg.AppID == "" || msg.AppID != inst.AppID {
		return errors.New("stored inbound delivery installation mismatch")
	}
	msg.Resources = nil
	msg.Body = appendAttachmentWarning(msg.Body, 1)
	if w.enricher != nil {
		msg = w.enricher.Enrich(ctx, msg, creds)
	}
	return w.handler(ctx, channelMessageFromLark(msg))
}

func (w *InboundDeliveryWorker) loadInstallation(ctx context.Context, id pgtype.UUID) (Installation, InstallationCredentials, error) {
	row, err := w.queries.GetChannelInstallation(ctx, db.GetChannelInstallationParams{ID: id, ChannelType: channelTypeFeishu})
	if err != nil {
		return Installation{}, InstallationCredentials{}, fmt.Errorf("load inbound installation: %w", err)
	}
	inst, err := installationFromRow(row)
	if err != nil {
		return Installation{}, InstallationCredentials{}, err
	}
	secret, err := w.installations.DecryptAppSecret(inst)
	if err != nil {
		return Installation{}, InstallationCredentials{}, err
	}
	creds := InstallationCredentials{AppID: inst.AppID, AppSecret: secret, Region: RegionOrDefault(inst.Region)}
	if inst.TenantKey.Valid {
		creds.TenantKey = inst.TenantKey.String
	}
	return inst, creds, nil
}

func (w *InboundDeliveryWorker) complete(ctx context.Context, delivery db.ChannelInboundDelivery, status, category string) error {
	params := db.CompleteChannelInboundDeliveryParams{ID: delivery.ID, LeaseToken: delivery.LeaseToken, Status: status}
	if category != "" {
		params.LastError = pgtype.Text{String: category, Valid: true}
	}
	_, err := w.queries.CompleteChannelInboundDelivery(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

func deliveryErrorCategory(err error) string {
	if IsRetryableResourceError(err) {
		return "attachment_unavailable"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "processing_timeout"
	}
	return "processing_failed"
}

var _ InboundDeliveryAcceptor = (*InboundDeliveryWorker)(nil)
