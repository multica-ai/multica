package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func seedAwaitingAckOpenclawDelivery(t *testing.T, recipientID string, attemptCount int32) (string, string) {
	t.Helper()

	queries := db.New(testPool)
	event, err := queries.CreateNotificationEvent(context.Background(), db.CreateNotificationEventParams{
		WorkspaceID:     util.MustParseUUID(testWorkspaceID),
		RecipientUserID: util.MustParseUUID(recipientID),
		Type:            "new_comment",
		Severity:        "info",
		IssueID:         pgtype.UUID{},
		CommentID:       pgtype.UUID{},
		ActorType:       util.StrToText("agent"),
		ActorID:         util.MustParseUUID("00000000-0000-0000-0000-000000000001"),
		Title:           "delivery result",
		Body:            util.StrToText("agent replied"),
		Link:            util.StrToText("https://multica.test/issues/OPE-2320"),
		Details:         []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("CreateNotificationEvent: %v", err)
	}
	delivery, err := queries.CreateNotificationDelivery(context.Background(), db.CreateNotificationDeliveryParams{
		NotificationEventID: event.ID,
		Channel:             "openclaw_weixin",
		Status:              "awaiting_ack",
		AttemptCount:        attemptCount,
		LastError:           pgtype.Text{},
		PayloadSnapshot:     []byte(`{}`),
		SentAt:              pgtype.Timestamptz{},
	})
	if err != nil {
		t.Fatalf("CreateNotificationDelivery: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM notification_delivery WHERE id = $1`, util.UUIDToString(delivery.ID))
		_, _ = testPool.Exec(context.Background(), `DELETE FROM notification_event WHERE id = $1`, util.UUIDToString(event.ID))
	})
	return util.UUIDToString(event.ID), util.UUIDToString(delivery.ID)
}

func deliveryForEvent(t *testing.T, eventID string) db.NotificationDelivery {
	t.Helper()
	deliveries, err := testHandler.Queries.ListNotificationDeliveriesByEvent(context.Background(), util.MustParseUUID(eventID))
	if err != nil {
		t.Fatalf("ListNotificationDeliveriesByEvent: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("expected one delivery, got %d", len(deliveries))
	}
	return deliveries[0]
}

func TestHandleNotificationDeliveryResultSuccessMarksSent(t *testing.T) {
	eventID, deliveryID := seedAwaitingAckOpenclawDelivery(t, testUserID, 1)

	err := testHandler.HandleNotificationDeliveryResult(context.Background(), daemonws.ClientIdentity{
		UserID:      testUserID,
		WorkspaceID: testWorkspaceID,
	}, protocol.NotificationDeliveryResultPayload{
		DeliveryID: deliveryID,
		Channel:    "openclaw_weixin",
		Success:    true,
	})
	if err != nil {
		t.Fatalf("HandleNotificationDeliveryResult: %v", err)
	}

	delivery := deliveryForEvent(t, eventID)
	if delivery.Status != "sent" || !delivery.SentAt.Valid || delivery.LastError.Valid {
		t.Fatalf("unexpected delivery after success ack: %#v", delivery)
	}
}

func TestHandleNotificationDeliveryResultFailureRetriesThenFails(t *testing.T) {
	eventID, deliveryID := seedAwaitingAckOpenclawDelivery(t, testUserID, 1)

	err := testHandler.HandleNotificationDeliveryResult(context.Background(), daemonws.ClientIdentity{
		UserID:      testUserID,
		WorkspaceID: testWorkspaceID,
	}, protocol.NotificationDeliveryResultPayload{
		DeliveryID: deliveryID,
		Channel:    "openclaw_weixin",
		Success:    false,
		Error:      "exit status 1",
		Output:     "uv_cwd",
	})
	if err != nil {
		t.Fatalf("HandleNotificationDeliveryResult: %v", err)
	}
	delivery := deliveryForEvent(t, eventID)
	if delivery.Status != "pending" {
		t.Fatalf("expected retry pending, got %q", delivery.Status)
	}
	if !delivery.LastError.Valid || !strings.Contains(delivery.LastError.String, "uv_cwd") {
		t.Fatalf("expected failure last_error, got %#v", delivery.LastError)
	}

	eventID, deliveryID = seedAwaitingAckOpenclawDelivery(t, testUserID, 3)
	err = testHandler.HandleNotificationDeliveryResult(context.Background(), daemonws.ClientIdentity{
		UserID:      testUserID,
		WorkspaceID: testWorkspaceID,
	}, protocol.NotificationDeliveryResultPayload{
		DeliveryID: deliveryID,
		Channel:    "openclaw_weixin",
		Success:    false,
		Error:      "exit status 1",
	})
	if err != nil {
		t.Fatalf("HandleNotificationDeliveryResult final: %v", err)
	}
	delivery = deliveryForEvent(t, eventID)
	if delivery.Status != "failed" {
		t.Fatalf("expected failed after max attempts, got %q", delivery.Status)
	}
}

func TestHandleNotificationDeliveryResultRejectsWrongUser(t *testing.T) {
	eventID, deliveryID := seedAwaitingAckOpenclawDelivery(t, testUserID, 1)

	err := testHandler.HandleNotificationDeliveryResult(context.Background(), daemonws.ClientIdentity{
		UserID:      "00000000-0000-0000-0000-000000000999",
		WorkspaceID: testWorkspaceID,
	}, protocol.NotificationDeliveryResultPayload{
		DeliveryID: deliveryID,
		Channel:    "openclaw_weixin",
		Success:    true,
	})
	if err == nil {
		t.Fatal("expected wrong user result to be ignored")
	}
	delivery := deliveryForEvent(t, eventID)
	if delivery.Status != "awaiting_ack" {
		t.Fatalf("expected delivery to remain awaiting_ack, got %q", delivery.Status)
	}
}
