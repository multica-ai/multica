package handler

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/daemonws"
	notifyutil "github.com/multica-ai/multica/server/internal/notify"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var errNotificationDeliveryResultIgnored = errors.New("notification delivery result ignored")

func (h *Handler) HandleNotificationDeliveryResult(ctx context.Context, identity daemonws.ClientIdentity, payload protocol.NotificationDeliveryResultPayload) error {
	deliveryID := strings.TrimSpace(payload.DeliveryID)
	if deliveryID == "" {
		return errNotificationDeliveryResultIgnored
	}
	if strings.TrimSpace(payload.Channel) != "openclaw_weixin" {
		return errNotificationDeliveryResultIgnored
	}
	parsedDeliveryID, err := util.ParseUUID(deliveryID)
	if err != nil {
		return errNotificationDeliveryResultIgnored
	}

	row, err := h.Queries.GetNotificationDeliveryWithEvent(ctx, parsedDeliveryID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotificationDeliveryResultIgnored
		}
		return err
	}
	if row.Channel != "openclaw_weixin" || row.Status != "awaiting_ack" {
		return errNotificationDeliveryResultIgnored
	}
	if identity.UserID == "" || util.UUIDToString(row.RecipientUserID) != identity.UserID {
		return errNotificationDeliveryResultIgnored
	}
	if identity.WorkspaceID != "" && util.UUIDToString(row.WorkspaceID) != identity.WorkspaceID {
		return errNotificationDeliveryResultIgnored
	}

	if payload.Success {
		_, err = h.Queries.CompleteNotificationDeliveryIfStatus(ctx, db.CompleteNotificationDeliveryIfStatusParams{
			ID:        row.DeliveryID,
			Status:    "sent",
			LastError: pgtype.Text{},
			SentAt:    pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
			Status_2:  "awaiting_ack",
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return errNotificationDeliveryResultIgnored
		}
		return err
	}

	nextStatus := "pending"
	if row.AttemptCount >= notifyutil.OpenclawWeixinDeliveryMaxAttempts {
		nextStatus = "failed"
	}
	_, err = h.Queries.CompleteNotificationDeliveryIfStatus(ctx, db.CompleteNotificationDeliveryIfStatusParams{
		ID:        row.DeliveryID,
		Status:    nextStatus,
		LastError: util.StrToText(truncateNotificationDeliveryResultError(payload)),
		SentAt:    pgtype.Timestamptz{},
		Status_2:  "awaiting_ack",
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return errNotificationDeliveryResultIgnored
	}
	return err
}

func truncateNotificationDeliveryResultError(payload protocol.NotificationDeliveryResultPayload) string {
	parts := []string{}
	if errText := strings.TrimSpace(payload.Error); errText != "" {
		parts = append(parts, errText)
	}
	if output := strings.TrimSpace(payload.Output); output != "" {
		parts = append(parts, output)
	}
	if len(parts) == 0 {
		parts = append(parts, "daemon delivery failed")
	}
	raw := strings.Join(parts, ": ")
	if len(raw) <= 500 {
		return raw
	}
	return raw[:500]
}
