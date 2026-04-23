package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func cleanupNotificationDispatchData(t *testing.T) {
	t.Helper()

	if _, err := testPool.Exec(context.Background(), `
		DELETE FROM notification_delivery
		WHERE notification_event_id IN (
			SELECT id FROM notification_event WHERE workspace_id = $1
		)
	`, testWorkspaceID); err != nil {
		t.Fatalf("delete notification_delivery: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		DELETE FROM notification_event WHERE workspace_id = $1
	`, testWorkspaceID); err != nil {
		t.Fatalf("delete notification_event: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		DELETE FROM notification_channel_preference WHERE user_id = $1
	`, testUserID); err != nil {
		t.Fatalf("delete notification_channel_preference: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		DELETE FROM external_account_binding WHERE user_id = $1
	`, testUserID); err != nil {
		t.Fatalf("delete external_account_binding: %v", err)
	}
}

func seedPendingDingTalkDelivery(t *testing.T, corpID, unionID string) (string, string) {
	t.Helper()

	queries := db.New(testPool)
	metadata, err := json.Marshal(map[string]string{
		"corp_id":  corpID,
		"union_id": unionID,
		"open_id":  "open-" + unionID,
	})
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}

	binding, err := queries.UpsertExternalAccountBinding(context.Background(), db.UpsertExternalAccountBindingParams{
		UserID:                util.ParseUUID(testUserID),
		Provider:              "dingtalk",
		ExternalUserID:        unionID,
		DisplayName:           util.StrToText("Bound DingTalk User"),
		AccessTokenEncrypted:  pgtype.Text{},
		RefreshTokenEncrypted: pgtype.Text{},
		TokenExpiresAt:        pgtype.Timestamptz{},
		Status:                "active",
		Metadata:              metadata,
	})
	if err != nil {
		t.Fatalf("UpsertExternalAccountBinding: %v", err)
	}

	event, err := queries.CreateNotificationEvent(context.Background(), db.CreateNotificationEventParams{
		WorkspaceID:     util.ParseUUID(testWorkspaceID),
		RecipientUserID: util.ParseUUID(testUserID),
		Type:            "mentioned",
		Severity:        "info",
		IssueID:         pgtype.UUID{},
		CommentID:       pgtype.UUID{},
		ActorType:       util.StrToText("member"),
		ActorID:         util.ParseUUID(testUserID),
		Title:           "dispatcher issue",
		Body:            util.StrToText("please review this change"),
		Link:            util.StrToText("https://app.multica.test/test/issues/123"),
		Details:         []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("CreateNotificationEvent: %v", err)
	}

	payloadSnapshot, err := json.Marshal(map[string]any{
		"binding_id":       util.UUIDToString(binding.ID),
		"provider":         "dingtalk",
		"external_user_id": unionID,
		"notification_event": map[string]any{
			"type":  "mentioned",
			"title": "dispatcher issue",
			"body":  "please review this change",
			"link":  "https://app.multica.test/test/issues/123",
		},
	})
	if err != nil {
		t.Fatalf("marshal delivery payload: %v", err)
	}

	delivery, err := queries.CreateNotificationDelivery(context.Background(), db.CreateNotificationDeliveryParams{
		NotificationEventID: event.ID,
		Channel:             "dingtalk",
		Status:              "pending",
		AttemptCount:        0,
		LastError:           pgtype.Text{},
		PayloadSnapshot:     payloadSnapshot,
		SentAt:              pgtype.Timestamptz{},
	})
	if err != nil {
		t.Fatalf("CreateNotificationDelivery: %v", err)
	}

	return util.UUIDToString(event.ID), util.UUIDToString(delivery.ID)
}

func loadNotificationDeliveryByEvent(t *testing.T, eventID string) db.NotificationDelivery {
	t.Helper()

	queries := db.New(testPool)
	deliveries, err := queries.ListNotificationDeliveriesByEvent(context.Background(), util.ParseUUID(eventID))
	if err != nil {
		t.Fatalf("ListNotificationDeliveriesByEvent: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("expected 1 delivery for event %s, got %d", eventID, len(deliveries))
	}
	return deliveries[0]
}

func TestDispatchPendingDingTalkDeliveries_MarksSent(t *testing.T) {
	cleanupNotificationDispatchData(t)
	t.Cleanup(func() { cleanupNotificationDispatchData(t) })

	var tokenCalls int
	var messageCalls int
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/corp/") && strings.HasSuffix(r.URL.Path, "/token"):
			tokenCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"app-token","expires_in":7200}`))
		case r.URL.Path == "/message":
			messageCalls++
			if got := r.Header.Get("Authorization"); got != "Bearer app-token" {
				t.Fatalf("expected bearer app token, got %q", got)
			}

			var body struct {
				RobotCode string   `json:"robotCode"`
				UserIDs   []string `json:"userIds"`
				MsgKey    string   `json:"msgKey"`
				MsgParam  string   `json:"msgParam"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode message body: %v", err)
			}
			if body.RobotCode != "ding-robot-code" {
				t.Fatalf("expected robotCode %q, got %q", "ding-robot-code", body.RobotCode)
			}
			if len(body.UserIDs) != 1 || body.UserIDs[0] != "union-success" {
				t.Fatalf("expected userIds [union-success], got %#v", body.UserIDs)
			}
			if body.MsgKey != "sampleText" {
				t.Fatalf("expected msgKey %q, got %q", "sampleText", body.MsgKey)
			}
			if !strings.Contains(body.MsgParam, "dispatcher issue") {
				t.Fatalf("expected msgParam to include notification title, got %q", body.MsgParam)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"processQueryKey":"query-123"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(apiServer.Close)

	t.Setenv("DINGTALK_CLIENT_ID", "ding-client-id")
	t.Setenv("DINGTALK_CLIENT_SECRET", "ding-client-secret")
	t.Setenv("DINGTALK_ROBOT_CODE", "ding-robot-code")
	t.Setenv("DINGTALK_APP_TOKEN_URL", apiServer.URL+"/corp/{corpId}/token")
	t.Setenv("DINGTALK_MESSAGE_URL", apiServer.URL+"/message")

	eventID, _ := seedPendingDingTalkDelivery(t, "corp-success", "union-success")

	dispatchPendingNotificationDeliveries(context.Background(), db.New(testPool))

	delivery := loadNotificationDeliveryByEvent(t, eventID)
	if delivery.Status != "sent" {
		t.Fatalf("expected delivery status sent, got %q", delivery.Status)
	}
	if delivery.AttemptCount != 1 {
		t.Fatalf("expected attempt_count 1, got %d", delivery.AttemptCount)
	}
	if !delivery.SentAt.Valid {
		t.Fatal("expected sent_at to be populated")
	}
	if tokenCalls != 1 {
		t.Fatalf("expected 1 app token call, got %d", tokenCalls)
	}
	if messageCalls != 1 {
		t.Fatalf("expected 1 message call, got %d", messageCalls)
	}
}

func TestDispatchPendingDingTalkDeliveries_RequeuesThenFails(t *testing.T) {
	cleanupNotificationDispatchData(t)
	t.Cleanup(func() { cleanupNotificationDispatchData(t) })

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/corp/") && strings.HasSuffix(r.URL.Path, "/token"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"app-token","expires_in":7200}`))
		case r.URL.Path == "/message":
			http.Error(w, `{"code":"send.failed","message":"mock send failure"}`, http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(apiServer.Close)

	t.Setenv("DINGTALK_CLIENT_ID", "ding-client-id")
	t.Setenv("DINGTALK_CLIENT_SECRET", "ding-client-secret")
	t.Setenv("DINGTALK_ROBOT_CODE", "ding-robot-code")
	t.Setenv("DINGTALK_APP_TOKEN_URL", apiServer.URL+"/corp/{corpId}/token")
	t.Setenv("DINGTALK_MESSAGE_URL", apiServer.URL+"/message")

	eventID, _ := seedPendingDingTalkDelivery(t, "corp-fail", "union-fail")

	queries := db.New(testPool)
	dispatchPendingNotificationDeliveries(context.Background(), queries)
	delivery := loadNotificationDeliveryByEvent(t, eventID)
	if delivery.Status != "pending" {
		t.Fatalf("expected first failed attempt to requeue as pending, got %q", delivery.Status)
	}
	if delivery.AttemptCount != 1 {
		t.Fatalf("expected attempt_count 1 after first failure, got %d", delivery.AttemptCount)
	}
	if !delivery.LastError.Valid || !strings.Contains(delivery.LastError.String, "dingtalk send failed") {
		t.Fatalf("expected last_error to contain send failure, got %#v", delivery.LastError)
	}

	dispatchPendingNotificationDeliveries(context.Background(), queries)
	dispatchPendingNotificationDeliveries(context.Background(), queries)
	delivery = loadNotificationDeliveryByEvent(t, eventID)
	if delivery.Status != "failed" {
		t.Fatalf("expected delivery status failed after max attempts, got %q", delivery.Status)
	}
	if delivery.AttemptCount != dingTalkDeliveryMaxAttempts {
		t.Fatalf("expected attempt_count %d, got %d", dingTalkDeliveryMaxAttempts, delivery.AttemptCount)
	}
	if delivery.SentAt.Valid {
		t.Fatal("expected sent_at to remain empty after failures")
	}
}
