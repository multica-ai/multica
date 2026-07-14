package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	webhookWorkerPollInterval = time.Second
	webhookWorkerMaxAttempts  = 5
)

// WebhookDeliveryWorker owns queued webhook delivery dispatch. Its queue and
// lease both live in Postgres, so a process restart or replica failover simply
// reclaims expired rows; the in-memory notification is only a latency hint.
type WebhookDeliveryWorker struct {
	h      *Handler
	notify chan struct{}
	done   chan struct{}
}

func NewWebhookDeliveryWorker(h *Handler) *WebhookDeliveryWorker {
	return &WebhookDeliveryWorker{h: h, notify: make(chan struct{}, 1), done: make(chan struct{})}
}

func (w *WebhookDeliveryWorker) Notify() {
	if w == nil {
		return
	}
	select {
	case w.notify <- struct{}{}:
	default:
	}
}

// Run polls as a recovery sweeper and also wakes immediately after local
// ingress. ProcessNext is public to the package so handler tests can drive the
// durable queue synchronously without starting a goroutine.
func (w *WebhookDeliveryWorker) Run(ctx context.Context) {
	if w == nil || w.h == nil || w.h.Queries == nil {
		return
	}
	defer close(w.done)
	ticker := time.NewTicker(webhookWorkerPollInterval)
	defer ticker.Stop()
	for {
		worked, err := w.ProcessNext(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("webhook worker: process delivery", "error", err)
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

func (w *WebhookDeliveryWorker) WaitWithTimeout(timeout time.Duration) bool {
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

func (w *WebhookDeliveryWorker) ProcessNext(ctx context.Context) (bool, error) {
	delivery, err := w.h.Queries.ClaimQueuedWebhookDelivery(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim queued delivery: %w", err)
	}

	if w.h.WebhookRateLimiter != nil {
		key := uuidToString(delivery.TriggerID)
		if !w.h.WebhookRateLimiter.Allow(ctx, key) {
			w.h.Metrics.RecordWebhookRateLimited("worker_trigger")
			retryAfter := webhookLimiterRetryAfter(ctx, w.h.WebhookRateLimiter, key)
			if retryAfter <= 0 {
				retryAfter = time.Second
			}
			_, err := w.h.Queries.DeferClaimedWebhookDelivery(ctx, db.DeferClaimedWebhookDeliveryParams{
				ID:          delivery.ID,
				LeaseToken:  delivery.LeaseToken,
				AvailableAt: pgtype.Timestamptz{Time: time.Now().Add(retryAfter), Valid: true},
			})
			return true, err
		}
	}

	trigger, err := w.h.Queries.GetAutopilotTrigger(ctx, delivery.TriggerID)
	if err != nil {
		return true, w.retryOrFail(ctx, delivery, fmt.Errorf("load trigger: %w", err))
	}
	autopilot, err := w.h.Queries.GetAutopilot(ctx, delivery.AutopilotID)
	if err != nil {
		return true, w.retryOrFail(ctx, delivery, fmt.Errorf("load autopilot: %w", err))
	}
	if uuidToString(trigger.AutopilotID) != uuidToString(delivery.AutopilotID) ||
		uuidToString(autopilot.WorkspaceID) != uuidToString(delivery.WorkspaceID) {
		return true, w.complete(ctx, delivery, deliveryStatusFailed, pgtype.UUID{}, "delivery ownership mismatch")
	}

	headers := headersFromSelected(delivery.SelectedHeaders)
	if delivery.ContentType.Valid {
		headers.Set("Content-Type", delivery.ContentType.String)
	}
	envelope, err := normalizeWebhookPayload(delivery.RawBody, headers)
	if err != nil {
		return true, w.complete(ctx, delivery, deliveryStatusFailed, pgtype.UUID{}, "normalize stored body: "+err.Error())
	}
	if delivery.ReceivedAt.Valid {
		envelope.Request.ReceivedAt = delivery.ReceivedAt.Time.UTC().Format(time.RFC3339)
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return true, w.retryOrFail(ctx, delivery, fmt.Errorf("encode envelope: %w", err))
	}

	switch {
	case !trigger.Enabled:
		return true, w.complete(ctx, delivery, deliveryStatusIgnored, pgtype.UUID{}, "trigger_disabled")
	case autopilot.Status == "archived":
		return true, w.complete(ctx, delivery, deliveryStatusIgnored, pgtype.UUID{}, "autopilot_archived")
	case autopilot.Status != "active":
		return true, w.complete(ctx, delivery, deliveryStatusIgnored, pgtype.UUID{}, "autopilot_paused")
	case !webhookEventAllowedByTriggerScope(trigger.EventFilters, envelope):
		return true, w.complete(ctx, delivery, deliveryStatusIgnored, pgtype.UUID{}, "event_filtered")
	}

	run, dispatchErr := w.h.AutopilotService.DispatchAutopilotForWebhookDelivery(
		ctx,
		autopilot,
		trigger.ID,
		payload,
		delivery.ID,
	)
	if dispatchErr != nil {
		if run != nil {
			return true, w.complete(ctx, delivery, deliveryStatusFailed, run.ID, dispatchErr.Error())
		}
		return true, w.retryOrFail(ctx, delivery, dispatchErr)
	}
	if run.Status == "failed" {
		reason := "autopilot run failed"
		if run.FailureReason.Valid {
			reason = run.FailureReason.String
		}
		return true, w.complete(ctx, delivery, deliveryStatusFailed, run.ID, reason)
	}

	if err := w.h.Queries.TouchAutopilotTriggerFiredAt(ctx, trigger.ID); err != nil {
		slog.Warn("webhook worker: touch last_fired_at",
			"delivery_id", uuidToString(delivery.ID),
			"trigger_id", uuidToString(trigger.ID),
			"error", err,
		)
	}
	return true, w.complete(ctx, delivery, deliveryStatusDispatched, run.ID, "")
}

func (w *WebhookDeliveryWorker) complete(
	ctx context.Context,
	delivery db.WebhookDelivery,
	status string,
	runID pgtype.UUID,
	reason string,
) error {
	params := db.CompleteClaimedWebhookDeliveryParams{
		ID:             delivery.ID,
		LeaseToken:     delivery.LeaseToken,
		Status:         status,
		AutopilotRunID: runID,
	}
	if reason != "" {
		params.Error = pgtype.Text{String: reason, Valid: true}
	}
	if _, err := w.h.Queries.CompleteClaimedWebhookDelivery(ctx, params); err != nil {
		return fmt.Errorf("complete claimed delivery: %w", err)
	}
	w.h.Metrics.RecordWebhookDelivery(delivery.Provider, status)
	return nil
}

func (w *WebhookDeliveryWorker) retryOrFail(ctx context.Context, delivery db.WebhookDelivery, cause error) error {
	if delivery.DispatchAttempts+1 >= webhookWorkerMaxAttempts {
		return w.complete(ctx, delivery, deliveryStatusFailed, pgtype.UUID{}, cause.Error())
	}
	backoff := time.Second * time.Duration(1<<min(delivery.DispatchAttempts, 6))
	_, err := w.h.Queries.RetryClaimedWebhookDelivery(ctx, db.RetryClaimedWebhookDeliveryParams{
		ID:          delivery.ID,
		LeaseToken:  delivery.LeaseToken,
		AvailableAt: pgtype.Timestamptz{Time: time.Now().Add(backoff), Valid: true},
		Error:       pgtype.Text{String: cause.Error(), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("retry claimed delivery after %v: %w", cause, err)
	}
	slog.Warn("webhook worker: delivery deferred",
		"delivery_id", uuidToString(delivery.ID),
		"attempt", delivery.DispatchAttempts+1,
		"backoff", backoff,
		"error", cause,
	)
	return nil
}
