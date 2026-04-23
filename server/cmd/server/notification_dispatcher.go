package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	notifyutil "github.com/multica-ai/multica/server/internal/notify"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	notificationDispatchInterval  = 10 * time.Second
	notificationDispatchBatchSize = 20
	dingTalkDeliveryMaxAttempts   = 3
)

type dingtalkDeliveryPayload struct {
	BindingID         string          `json:"binding_id"`
	Provider          string          `json:"provider"`
	ExternalUserID    string          `json:"external_user_id"`
	NotificationEvent json.RawMessage `json:"notification_event"`
}

type notificationEventPayload struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Link  string `json:"link"`
}

type dingTalkBindingMetadata struct {
	CorpID  string `json:"corp_id"`
	UnionID string `json:"union_id"`
	OpenID  string `json:"open_id"`
}

func runNotificationDeliveryDispatcher(ctx context.Context, queries *db.Queries) {
	ticker := time.NewTicker(notificationDispatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dispatchPendingNotificationDeliveries(ctx, queries)
		}
	}
}

func dispatchPendingNotificationDeliveries(ctx context.Context, queries *db.Queries) {
	cfg, err := notifyutil.LoadDingTalkConfig()
	if err != nil {
		if errors.Is(err, notifyutil.ErrDingTalkNotConfigured) {
			return
		}
		slog.Warn("notification dispatcher: failed to load dingtalk config", "error", err)
		return
	}
	if err := cfg.ValidateDeliveryConfig(); err != nil {
		if errors.Is(err, notifyutil.ErrDingTalkDeliveryNotConfigured) {
			return
		}
		slog.Warn("notification dispatcher: invalid dingtalk delivery config", "error", err)
		return
	}

	deliveries, err := queries.ListNotificationDeliveriesByStatus(ctx, "pending")
	if err != nil {
		slog.Warn("notification dispatcher: failed to list pending deliveries", "error", err)
		return
	}

	dispatched := 0
	for _, delivery := range deliveries {
		if delivery.Channel != "dingtalk" {
			continue
		}
		if dispatched >= notificationDispatchBatchSize {
			break
		}
		dispatched++
		processDingTalkDelivery(ctx, queries, cfg, delivery)
	}
}

func processDingTalkDelivery(ctx context.Context, queries *db.Queries, cfg notifyutil.DingTalkConfig, delivery db.NotificationDelivery) {
	claimed, err := queries.ClaimNotificationDelivery(ctx, db.ClaimNotificationDeliveryParams{
		ID:       delivery.ID,
		Status:   "pending",
		Status_2: "pending",
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return
		}
		slog.Warn("notification dispatcher: failed to claim delivery",
			"delivery_id", util.UUIDToString(delivery.ID),
			"error", err,
		)
		return
	}

	payload, eventPayload, binding, err := loadDingTalkDispatchContext(ctx, queries, claimed)
	if err != nil {
		finalizeFailedDelivery(ctx, queries, claimed, err)
		return
	}

	var metadata dingTalkBindingMetadata
	if len(binding.Metadata) > 0 {
		if err := json.Unmarshal(binding.Metadata, &metadata); err != nil {
			finalizeFailedDelivery(ctx, queries, claimed, errors.New("invalid dingtalk binding metadata"))
			return
		}
	}

	corpID := strings.TrimSpace(metadata.CorpID)
	unionID := firstValue(strings.TrimSpace(metadata.UnionID), strings.TrimSpace(payload.ExternalUserID), strings.TrimSpace(binding.ExternalUserID), strings.TrimSpace(metadata.OpenID))
	if corpID == "" || unionID == "" {
		finalizeFailedDelivery(ctx, queries, claimed, errors.New("dingtalk delivery is missing corp_id or union_id"))
		return
	}

	if _, err := cfg.SendTextMessage(ctx, corpID, unionID, buildDingTalkDeliveryText(eventPayload)); err != nil {
		finalizeFailedDelivery(ctx, queries, claimed, err)
		return
	}

	if _, err := queries.CompleteNotificationDelivery(ctx, db.CompleteNotificationDeliveryParams{
		ID:        claimed.ID,
		Status:    "sent",
		LastError: pgtype.Text{},
		SentAt:    pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}); err != nil {
		slog.Warn("notification dispatcher: failed to mark delivery sent",
			"delivery_id", util.UUIDToString(claimed.ID),
			"error", err,
		)
	}
}

func loadDingTalkDispatchContext(ctx context.Context, queries *db.Queries, delivery db.NotificationDelivery) (dingtalkDeliveryPayload, notificationEventPayload, db.ExternalAccountBinding, error) {
	var payload dingtalkDeliveryPayload
	if err := json.Unmarshal(delivery.PayloadSnapshot, &payload); err != nil {
		return dingtalkDeliveryPayload{}, notificationEventPayload{}, db.ExternalAccountBinding{}, errors.New("invalid dingtalk delivery payload")
	}
	if strings.TrimSpace(payload.BindingID) == "" {
		return dingtalkDeliveryPayload{}, notificationEventPayload{}, db.ExternalAccountBinding{}, errors.New("missing dingtalk binding id")
	}

	var eventPayload notificationEventPayload
	if len(payload.NotificationEvent) > 0 {
		if err := json.Unmarshal(payload.NotificationEvent, &eventPayload); err != nil {
			return dingtalkDeliveryPayload{}, notificationEventPayload{}, db.ExternalAccountBinding{}, errors.New("invalid nested notification payload")
		}
	}

	binding, err := queries.GetExternalAccountBinding(ctx, parseUUID(payload.BindingID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return dingtalkDeliveryPayload{}, notificationEventPayload{}, db.ExternalAccountBinding{}, errors.New("dingtalk binding not found")
		}
		return dingtalkDeliveryPayload{}, notificationEventPayload{}, db.ExternalAccountBinding{}, err
	}
	if binding.Provider != "dingtalk" || binding.Status != "active" {
		return dingtalkDeliveryPayload{}, notificationEventPayload{}, db.ExternalAccountBinding{}, errors.New("dingtalk binding is not active")
	}

	return payload, eventPayload, binding, nil
}

func finalizeFailedDelivery(ctx context.Context, queries *db.Queries, delivery db.NotificationDelivery, dispatchErr error) {
	nextStatus := "pending"
	if delivery.AttemptCount >= dingTalkDeliveryMaxAttempts {
		nextStatus = "failed"
	}

	if _, err := queries.CompleteNotificationDelivery(ctx, db.CompleteNotificationDeliveryParams{
		ID:        delivery.ID,
		Status:    nextStatus,
		LastError: util.StrToText(truncateError(dispatchErr)),
		SentAt:    pgtype.Timestamptz{},
	}); err != nil {
		slog.Warn("notification dispatcher: failed to mark delivery failure",
			"delivery_id", util.UUIDToString(delivery.ID),
			"error", err,
		)
		return
	}

	slog.Warn("notification dispatcher: delivery failed",
		"delivery_id", util.UUIDToString(delivery.ID),
		"status", nextStatus,
		"attempt_count", delivery.AttemptCount,
		"error", dispatchErr,
	)
}

func buildDingTalkDeliveryText(event notificationEventPayload) string {
	parts := []string{"Multica Notification"}
	if title := strings.TrimSpace(event.Title); title != "" {
		parts = append(parts, title)
	}
	if body := strings.TrimSpace(event.Body); body != "" {
		parts = append(parts, body)
	}
	if link := strings.TrimSpace(event.Link); link != "" {
		parts = append(parts, "Link: "+link)
	}
	return strings.Join(parts, "\n\n")
}

func truncateError(err error) string {
	if err == nil {
		return ""
	}
	raw := strings.TrimSpace(err.Error())
	if len(raw) <= 500 {
		return raw
	}
	return raw[:500]
}

func firstValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
