package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	notifyutil "github.com/multica-ai/multica/server/internal/notify"
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

type dingTalkDeliverySeed struct {
	CorpID      string
	UnionID     string
	UserID      string
	Mobile      string
	AccessToken string
}

func seedPendingDingTalkDelivery(t *testing.T, seed dingTalkDeliverySeed) (string, string, string) {
	t.Helper()

	queries := db.New(testPool)
	metadataMap := map[string]string{
		"corp_id": seed.CorpID,
	}
	if unionID := strings.TrimSpace(seed.UnionID); unionID != "" {
		metadataMap["union_id"] = unionID
		metadataMap["open_id"] = "open-" + unionID
	}
	if userID := strings.TrimSpace(seed.UserID); userID != "" {
		metadataMap["user_id"] = userID
	}
	if mobile := strings.TrimSpace(seed.Mobile); mobile != "" {
		metadataMap["mobile"] = mobile
	}
	metadata, err := json.Marshal(metadataMap)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}

	accessTokenEncrypted := pgtype.Text{}
	if accessToken := strings.TrimSpace(seed.AccessToken); accessToken != "" {
		encrypted, err := notifyutil.EncryptToken(accessToken)
		if err != nil {
			t.Fatalf("EncryptToken: %v", err)
		}
		accessTokenEncrypted = util.StrToText(encrypted)
	}
	externalUserID := firstValue(seed.UnionID, seed.UserID)

	binding, err := queries.UpsertExternalAccountBinding(context.Background(), db.UpsertExternalAccountBindingParams{
		UserID:                util.ParseUUID(testUserID),
		Provider:              "dingtalk",
		ExternalUserID:        externalUserID,
		DisplayName:           util.StrToText("Bound DingTalk User"),
		AccessTokenEncrypted:  accessTokenEncrypted,
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
		"external_user_id": externalUserID,
		"notification_event": map[string]any{
			"type":             "mentioned",
			"title":            "dispatcher issue",
			"body":             "please review this change",
			"link":             "https://app.multica.test/test/issues/123",
			"issue_identifier": "OPE-20",
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

	return util.UUIDToString(binding.ID), util.UUIDToString(event.ID), util.UUIDToString(delivery.ID)
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

func TestBuildDingTalkDeliveryMarkdown_SanitizesMentionLinks(t *testing.T) {
	card := buildDingTalkDeliveryMarkdown(notificationEventPayload{
		Title:           "1. Install a runtime (Desktop app or CLI)",
		IssueIdentifier: "OPE-20",
		ActorName:       "Alice",
		Body:            "[@guodage003](mention://member/04e19961-c5c1-4757-a114-1355a1049ea4) hello",
		Link:            "http://localhost:3000/guodage/issues/a77996be-cab2-4bc1-95bc-fbf4a33d5188?comment=5b0050c5-0575-4c5d-9ffb-9803c43af196",
	})

	if card.Title != "OPE-20 · 1. Install a runtime (Desktop app or CLI)" {
		t.Fatalf("unexpected title: %q", card.Title)
	}
	if !strings.Contains(card.Text, "@guodage003 hello") {
		t.Fatalf("expected sanitized mention in card text, got %q", card.Text)
	}
	if strings.Contains(card.Text, "mention://") {
		t.Fatalf("card text should not expose internal mention links: %q", card.Text)
	}
	if !strings.Contains(card.Text, "**From**\nAlice") {
		t.Fatalf("expected sender in card text, got %q", card.Text)
	}
	if !strings.Contains(card.Text, "**Issue**\nOPE-20 · 1. Install a runtime (Desktop app or CLI)") {
		t.Fatalf("expected issue identifier in card text, got %q", card.Text)
	}
	if count := strings.Count(card.Text, "Open In Multica"); count != 1 {
		t.Fatalf("expected exactly one Open In Multica link, got %d in %q", count, card.Text)
	}
	if !strings.Contains(card.Text, "dingtalk://dingtalkclient/page/link?url=http%3A%2F%2Flocalhost%3A3000%2Fguodage%2Fissues%2Fa77996be-cab2-4bc1-95bc-fbf4a33d5188%3Fcomment%3D5b0050c5-0575-4c5d-9ffb-9803c43af196&pc_slide=false") {
		t.Fatalf("expected dingtalk external browser URL in card text, got %q", card.Text)
	}
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
			if got := r.Header.Get("x-acs-dingtalk-access-token"); got != "app-token" {
				t.Fatalf("expected x-acs-dingtalk-access-token %q, got %q", "app-token", got)
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
			if len(body.UserIDs) != 1 || body.UserIDs[0] != "staff-success" {
				t.Fatalf("expected userIds [staff-success], got %#v", body.UserIDs)
			}
			if body.MsgKey != "sampleMarkdown" {
				t.Fatalf("expected msgKey %q, got %q", "sampleMarkdown", body.MsgKey)
			}
			var msgParam struct {
				Title string `json:"title"`
				Text  string `json:"text"`
			}
			if err := json.Unmarshal([]byte(body.MsgParam), &msgParam); err != nil {
				t.Fatalf("unmarshal msgParam: %v", err)
			}
			if msgParam.Title != "OPE-20 · dispatcher issue" {
				t.Fatalf("unexpected markdown title: %q", msgParam.Title)
			}
			if !strings.Contains(msgParam.Text, "**Issue**\nOPE-20 · dispatcher issue") {
				t.Fatalf("expected msgParam to include issue identifier, got %q", body.MsgParam)
			}
			if !strings.Contains(msgParam.Text, "**From**\nIntegration Tester") {
				t.Fatalf("expected msgParam to include sender, got %q", body.MsgParam)
			}
			if count := strings.Count(msgParam.Text, "Open In Multica"); count != 1 {
				t.Fatalf("expected exactly one Open In Multica link, got %d in %q", count, msgParam.Text)
			}
			if strings.Contains(body.MsgParam, "singleTitle") || strings.Contains(body.MsgParam, "singleURL") {
				t.Fatalf("markdown msgParam should not include action card fields, got %q", body.MsgParam)
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

	_, eventID, _ := seedPendingDingTalkDelivery(t, dingTalkDeliverySeed{
		CorpID:  "corp-success",
		UnionID: "union-success",
		UserID:  "staff-success",
	})

	dispatchPendingNotificationDeliveries(context.Background(), db.New(testPool), nil)

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

	_, eventID, _ := seedPendingDingTalkDelivery(t, dingTalkDeliverySeed{
		CorpID:  "corp-fail",
		UnionID: "union-fail",
		UserID:  "staff-fail",
	})

	queries := db.New(testPool)
	dispatchPendingNotificationDeliveries(context.Background(), queries, nil)
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

	dispatchPendingNotificationDeliveries(context.Background(), queries, nil)
	dispatchPendingNotificationDeliveries(context.Background(), queries, nil)
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

func TestDispatchPendingDingTalkDeliveries_BackfillsMissingUserID(t *testing.T) {
	cleanupNotificationDispatchData(t)
	t.Cleanup(func() { cleanupNotificationDispatchData(t) })

	var tokenCalls int
	var userByMobileCalls int
	var messageCalls int
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/corp/") && strings.HasSuffix(r.URL.Path, "/token"):
			tokenCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"app-token","expires_in":7200}`))
		case r.URL.Path == "/user-by-mobile":
			userByMobileCalls++
			if got := r.URL.Query().Get("access_token"); got != "app-token" {
				t.Fatalf("expected app access token query %q, got %q", "app-token", got)
			}
			var body struct {
				Mobile string `json:"mobile"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode user-by-mobile body: %v", err)
			}
			if body.Mobile != "13800000000" {
				t.Fatalf("expected mobile %q, got %q", "13800000000", body.Mobile)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"errcode":0,
				"errmsg":"ok",
				"result":{"userid":"staff-backfill"}
			}`))
		case r.URL.Path == "/message":
			messageCalls++
			var body struct {
				UserIDs []string `json:"userIds"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode message body: %v", err)
			}
			if len(body.UserIDs) != 1 || body.UserIDs[0] != "staff-backfill" {
				t.Fatalf("expected backfilled userIds [staff-backfill], got %#v", body.UserIDs)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"processQueryKey":"query-backfill"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(apiServer.Close)

	t.Setenv("DINGTALK_CLIENT_ID", "ding-client-id")
	t.Setenv("DINGTALK_CLIENT_SECRET", "ding-client-secret")
	t.Setenv("DINGTALK_ROBOT_CODE", "ding-robot-code")
	t.Setenv("DINGTALK_APP_TOKEN_URL", apiServer.URL+"/corp/{corpId}/token")
	t.Setenv("DINGTALK_USER_BY_MOBILE_URL", apiServer.URL+"/user-by-mobile?access_token={accessToken}")
	t.Setenv("DINGTALK_MESSAGE_URL", apiServer.URL+"/message")

	bindingID, eventID, _ := seedPendingDingTalkDelivery(t, dingTalkDeliverySeed{
		CorpID:  "corp-backfill",
		UnionID: "union-backfill",
		Mobile:  "13800000000",
	})

	dispatchPendingNotificationDeliveries(context.Background(), db.New(testPool), nil)

	delivery := loadNotificationDeliveryByEvent(t, eventID)
	if delivery.Status != "sent" {
		t.Fatalf("expected delivery status sent, got %q", delivery.Status)
	}
	if tokenCalls != 1 {
		t.Fatalf("expected 1 app token call, got %d", tokenCalls)
	}
	if userByMobileCalls != 1 {
		t.Fatalf("expected 1 user-by-mobile call, got %d", userByMobileCalls)
	}
	if messageCalls != 1 {
		t.Fatalf("expected 1 message call, got %d", messageCalls)
	}

	binding, err := db.New(testPool).GetExternalAccountBinding(context.Background(), util.ParseUUID(bindingID))
	if err != nil {
		t.Fatalf("GetExternalAccountBinding: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(binding.Metadata, &metadata); err != nil {
		t.Fatalf("unmarshal binding metadata: %v", err)
	}
	if metadata["user_id"] != "staff-backfill" {
		t.Fatalf("expected persisted user_id %q, got %#v", "staff-backfill", metadata["user_id"])
	}
}
