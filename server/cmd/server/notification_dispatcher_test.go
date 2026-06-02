package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

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
	if _, err := testPool.Exec(context.Background(), `
		DELETE FROM mobile_push_registration WHERE user_id = $1
	`, testUserID); err != nil {
		t.Fatalf("delete mobile_push_registration: %v", err)
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
		UserID:                util.MustParseUUID(testUserID),
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
		WorkspaceID:     util.MustParseUUID(testWorkspaceID),
		RecipientUserID: util.MustParseUUID(testUserID),
		Type:            "mentioned",
		Severity:        "info",
		IssueID:         pgtype.UUID{},
		CommentID:       pgtype.UUID{},
		ActorType:       util.StrToText("member"),
		ActorID:         util.MustParseUUID(testUserID),
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
	deliveries, err := queries.ListNotificationDeliveriesByEvent(context.Background(), util.MustParseUUID(eventID))
	if err != nil {
		t.Fatalf("ListNotificationDeliveriesByEvent: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("expected 1 delivery for event %s, got %d", eventID, len(deliveries))
	}
	return deliveries[0]
}

func seedPendingMobilePushDelivery(t *testing.T, cid string) (string, string) {
	t.Helper()

	queries := db.New(testPool)
	registration, err := queries.UpsertMobilePushRegistration(context.Background(), db.UpsertMobilePushRegistrationParams{
		UserID:           util.MustParseUUID(testUserID),
		InstallationID:   "dispatch-install-" + cid,
		Platform:         "android",
		Provider:         "getui",
		ProviderClientID: cid,
		AppVersion:       pgtype.Text{},
	})
	if err != nil {
		t.Fatalf("UpsertMobilePushRegistration: %v", err)
	}

	eventPayload := map[string]any{
		"type":             "mentioned",
		"title":            "dispatcher issue",
		"summary":          "please review this change",
		"body":             "please review this change",
		"link":             "https://app.multica.test/test/issues/123",
		"issue_identifier": "OPE-20",
	}
	payloadSnapshot, err := json.Marshal(map[string]any{
		"registration_id":    util.UUIDToString(registration.ID),
		"provider":           "getui",
		"provider_client_id": cid,
		"notification_event": eventPayload,
	})
	if err != nil {
		t.Fatalf("marshal mobile push payload: %v", err)
	}

	event, err := queries.CreateNotificationEvent(context.Background(), db.CreateNotificationEventParams{
		WorkspaceID:     util.MustParseUUID(testWorkspaceID),
		RecipientUserID: util.MustParseUUID(testUserID),
		Type:            "mentioned",
		Severity:        "info",
		IssueID:         pgtype.UUID{},
		CommentID:       pgtype.UUID{},
		ActorType:       util.StrToText("member"),
		ActorID:         util.MustParseUUID(testUserID),
		Title:           "dispatcher issue",
		Body:            util.StrToText("please review this change"),
		Link:            util.StrToText("https://app.multica.test/test/issues/123"),
		Details:         []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("CreateNotificationEvent: %v", err)
	}

	delivery, err := queries.CreateTargetedNotificationDelivery(context.Background(), db.CreateTargetedNotificationDeliveryParams{
		NotificationEventID: event.ID,
		Channel:             "mobile_push",
		TargetType:          "mobile_push_registration",
		TargetID:            registration.ID,
		Status:              "pending",
		AttemptCount:        0,
		LastError:           pgtype.Text{},
		PayloadSnapshot:     payloadSnapshot,
		SentAt:              pgtype.Timestamptz{},
	})
	if err != nil {
		t.Fatalf("CreateTargetedNotificationDelivery: %v", err)
	}

	return util.UUIDToString(event.ID), util.UUIDToString(delivery.ID)
}

func getuiDispatchFutureMillis() string {
	return strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
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

func TestBuildDingTalkDeliveryMarkdown_TruncatesBodyWithoutClippingActionLink(t *testing.T) {
	link := "https://multica.wujieai.com/openharness/issues/OPE-293"
	body := "Review " + link + "\n\n" + strings.Repeat(
		"The generated dashboards should include latency, request rate, and alerts for the multica-bot namespace.\n",
		80,
	)

	card := buildDingTalkDeliveryMarkdown(notificationEventPayload{
		Title:           "Build Observability Dashboards and Alerts for multica-bot Namespace",
		IssueIdentifier: "OPE-293",
		ActorName:       "Alice",
		Body:            body,
		Link:            link,
	})

	actionLink := "[Open In Multica](" + dingtalkExternalBrowserURL(link) + ")"
	if !strings.Contains(card.Text, actionLink) {
		t.Fatalf("expected full action link to be preserved, got %q", card.Text)
	}
	if !strings.HasSuffix(card.Text, actionLink) {
		t.Fatalf("expected truncation marker before the action link, got %q", card.Text)
	}
	if count := strings.Count(card.Text, "Open In Multica"); count != 1 {
		t.Fatalf("expected exactly one Open In Multica link, got %d in %q", count, card.Text)
	}
	if got := dingTalkRuneLen(card.Text); got > dingTalkMarkdownTextLimit {
		t.Fatalf("expected markdown text <= %d runes, got %d", dingTalkMarkdownTextLimit, got)
	}
	if !strings.Contains(card.Text, "\n...") {
		t.Fatalf("expected long body to be truncated on a separate line, got %q", card.Text)
	}
	if strings.Contains(card.Text, link+"...") || strings.Contains(card.Text, link+"…") {
		t.Fatalf("body truncation marker should not be appended to a URL, got %q", card.Text)
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

	dispatchPendingNotificationDeliveries(context.Background(), db.New(testPool), nil, nil)

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

func TestDispatchPendingMobilePushDeliveries_MarksSent(t *testing.T) {
	cleanupNotificationDispatchData(t)
	t.Cleanup(func() { cleanupNotificationDispatchData(t) })

	var authCalls int
	var pushCalls int
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/getui-dispatch-app/auth":
			authCalls++
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode getui auth body: %v", err)
			}
			if body["appkey"] != "getui-dispatch-key" || body["timestamp"] == "" || body["sign"] == "" {
				t.Fatalf("unexpected getui auth body: %#v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"expire_time":"` + getuiDispatchFutureMillis() + `","token":"dispatch-token"}}`))
		case "/v2/getui-dispatch-app/push/single/cid":
			pushCalls++
			if got := r.Header.Get("token"); got != "dispatch-token" {
				t.Fatalf("expected getui token %q, got %q", "dispatch-token", got)
			}
			var body struct {
				RequestID string `json:"request_id"`
				Settings  struct {
					TTL int64 `json:"ttl"`
				} `json:"settings"`
				Audience struct {
					CID []string `json:"cid"`
				} `json:"audience"`
				PushMessage struct {
					Notification struct {
						Title     string `json:"title"`
						Body      string `json:"body"`
						ClickType string `json:"click_type"`
					} `json:"notification"`
				} `json:"push_message"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode getui push body: %v", err)
			}
			if len(body.Audience.CID) != 1 || body.Audience.CID[0] != "cid-dispatch" {
				t.Fatalf("expected cid-dispatch audience, got %#v", body.Audience.CID)
			}
			if body.Settings.TTL != int64(getuiPushTTL/time.Millisecond) {
				t.Fatalf("expected ttl %d, got %d", int64(getuiPushTTL/time.Millisecond), body.Settings.TTL)
			}
			if body.PushMessage.Notification.Title != "OPE-20 · dispatcher issue" {
				t.Fatalf("unexpected title: %q", body.PushMessage.Notification.Title)
			}
			if body.PushMessage.Notification.Body != "please review this change" {
				t.Fatalf("unexpected body: %q", body.PushMessage.Notification.Body)
			}
			if body.PushMessage.Notification.ClickType != "none" {
				t.Fatalf("unexpected click_type: %q", body.PushMessage.Notification.ClickType)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"msg":"","data":{"task-dispatch":{"cid-dispatch":"successed_online"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(apiServer.Close)

	t.Setenv("GETUI_APP_ID", "getui-dispatch-app")
	t.Setenv("GETUI_APP_KEY", "getui-dispatch-key")
	t.Setenv("GETUI_MASTER_SECRET", "getui-dispatch-secret")
	t.Setenv("GETUI_BASE_URL", apiServer.URL+"/v2")

	eventID, _ := seedPendingMobilePushDelivery(t, "cid-dispatch")

	dispatchPendingNotificationDeliveries(context.Background(), db.New(testPool), nil, nil)

	delivery := loadNotificationDeliveryByEvent(t, eventID)
	if delivery.Status != "sent" {
		t.Fatalf("expected mobile push delivery status sent, got %q", delivery.Status)
	}
	if delivery.AttemptCount != 1 {
		t.Fatalf("expected attempt_count 1, got %d", delivery.AttemptCount)
	}
	if !delivery.SentAt.Valid {
		t.Fatal("expected sent_at to be populated")
	}
	if authCalls != 1 || pushCalls != 1 {
		t.Fatalf("expected one auth and one push, got auth=%d push=%d", authCalls, pushCalls)
	}
}

func TestDispatchPendingMobilePushDeliveries_CancelsDisabledRegistration(t *testing.T) {
	cleanupNotificationDispatchData(t)
	t.Cleanup(func() { cleanupNotificationDispatchData(t) })

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("getui API should not be called for disabled registration: %s", r.URL.Path)
	}))
	t.Cleanup(apiServer.Close)

	t.Setenv("GETUI_APP_ID", "getui-disabled-app")
	t.Setenv("GETUI_APP_KEY", "getui-disabled-key")
	t.Setenv("GETUI_MASTER_SECRET", "getui-disabled-secret")
	t.Setenv("GETUI_BASE_URL", apiServer.URL+"/v2")

	eventID, _ := seedPendingMobilePushDelivery(t, "cid-disabled")
	if _, err := testPool.Exec(context.Background(), `
		UPDATE mobile_push_registration
		SET enabled = false
		WHERE user_id = $1
	`, testUserID); err != nil {
		t.Fatalf("disable mobile_push_registration: %v", err)
	}

	dispatchPendingNotificationDeliveries(context.Background(), db.New(testPool), nil, nil)

	delivery := loadNotificationDeliveryByEvent(t, eventID)
	if delivery.Status != "cancelled" {
		t.Fatalf("expected mobile push delivery status cancelled, got %q", delivery.Status)
	}
	if delivery.AttemptCount != 1 {
		t.Fatalf("expected attempt_count 1 after claim, got %d", delivery.AttemptCount)
	}
	if !delivery.LastError.Valid || !strings.Contains(delivery.LastError.String, "disabled") {
		t.Fatalf("expected disabled last_error, got %#v", delivery.LastError)
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
	dispatchPendingNotificationDeliveries(context.Background(), queries, nil, nil)
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

	dispatchPendingNotificationDeliveries(context.Background(), queries, nil, nil)
	dispatchPendingNotificationDeliveries(context.Background(), queries, nil, nil)
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

	dispatchPendingNotificationDeliveries(context.Background(), db.New(testPool), nil, nil)

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

	binding, err := db.New(testPool).GetExternalAccountBinding(context.Background(), util.MustParseUUID(bindingID))
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
