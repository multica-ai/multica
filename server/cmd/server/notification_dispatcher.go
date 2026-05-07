package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	notifyutil "github.com/multica-ai/multica/server/internal/notify"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	notificationDispatchInterval  = 10 * time.Second
	notificationDispatchBatchSize = 20
	dingTalkDeliveryMaxAttempts   = 3
	emailDeliveryMaxAttempts      = 3
	webhookDeliveryMaxAttempts    = 3
)

var dingtalkMentionLinkPattern = regexp.MustCompile(`\[@([^\]]+)\]\(mention://[^)]+\)`)
var customWebhookSender = notifyutil.WebhookSender{}

type dingtalkDeliveryPayload struct {
	BindingID         string          `json:"binding_id"`
	Provider          string          `json:"provider"`
	ExternalUserID    string          `json:"external_user_id"`
	NotificationEvent json.RawMessage `json:"notification_event"`
}

type notificationEventPayload struct {
	Type            string `json:"type"`
	Title           string `json:"title"`
	Body            string `json:"body"`
	Link            string `json:"link"`
	IssueIdentifier string `json:"issue_identifier"`
	ActorName       string `json:"actor_name,omitempty"`
}

type dingTalkBindingMetadata struct {
	CorpID  string `json:"corp_id"`
	UserID  string `json:"user_id"`
	UnionID string `json:"union_id"`
	OpenID  string `json:"open_id"`
	Mobile  string `json:"mobile"`
}

type emailDeliveryPayload struct {
	BindingID         string          `json:"binding_id"`
	Provider          string          `json:"provider"`
	ExternalUserID    string          `json:"external_user_id"`
	NotificationEvent json.RawMessage `json:"notification_event"`
}

type customWebhookDeliveryPayload struct {
	WebhookEndpointID string          `json:"webhook_endpoint_id"`
	NotificationEvent json.RawMessage `json:"notification_event"`
}

func runNotificationDeliveryDispatcher(ctx context.Context, queries *db.Queries, emailSvc *service.EmailService) {
	ticker := time.NewTicker(notificationDispatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dispatchPendingNotificationDeliveries(ctx, queries, emailSvc)
		}
	}
}

func dispatchPendingNotificationDeliveries(ctx context.Context, queries *db.Queries, emailSvc *service.EmailService) {
	cfg, err := notifyutil.LoadDingTalkConfig()
	dingtalkReady := err == nil
	if err != nil && !errors.Is(err, notifyutil.ErrDingTalkNotConfigured) {
		slog.Warn("notification dispatcher: failed to load dingtalk config", "error", err)
	}
	if dingtalkReady {
		if err := cfg.ValidateDeliveryConfig(); err != nil {
			if !errors.Is(err, notifyutil.ErrDingTalkDeliveryNotConfigured) {
				slog.Warn("notification dispatcher: invalid dingtalk delivery config", "error", err)
			}
			dingtalkReady = false
		}
	}

	deliveries, err := queries.ListNotificationDeliveriesByStatus(ctx, "pending")
	if err != nil {
		slog.Warn("notification dispatcher: failed to list pending deliveries", "error", err)
		return
	}

	dispatched := 0
	for _, delivery := range deliveries {
		if dispatched >= notificationDispatchBatchSize {
			break
		}
		switch delivery.Channel {
		case "dingtalk":
			if !dingtalkReady {
				continue
			}
			dispatched++
			processDingTalkDelivery(ctx, queries, cfg, delivery)
		case "email":
			dispatched++
			processEmailDelivery(ctx, queries, emailSvc, delivery)
		case "custom_webhook":
			dispatched++
			processCustomWebhookDelivery(ctx, queries, delivery)
		}
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

	_, eventPayload, binding, err := loadDingTalkDispatchContext(ctx, queries, claimed)
	if err != nil {
		finalizeFailedDelivery(ctx, queries, claimed, err)
		return
	}
	eventPayload = hydrateNotificationActorName(ctx, queries, claimed, eventPayload)

	var metadata dingTalkBindingMetadata
	if len(binding.Metadata) > 0 {
		if err := json.Unmarshal(binding.Metadata, &metadata); err != nil {
			finalizeFailedDelivery(ctx, queries, claimed, errors.New("invalid dingtalk binding metadata"))
			return
		}
	}

	corpID := strings.TrimSpace(metadata.CorpID)
	if corpID == "" {
		finalizeFailedDelivery(ctx, queries, claimed, errors.New("dingtalk delivery is missing corp_id"))
		return
	}

	targetUserID := strings.TrimSpace(metadata.UserID)
	if targetUserID == "" {
		targetUserID, err = backfillDingTalkBindingUserID(ctx, queries, cfg, binding, metadata)
		if err != nil {
			finalizeFailedDelivery(ctx, queries, claimed, err)
			return
		}
	}

	if _, err := cfg.SendMarkdownMessage(ctx, corpID, targetUserID, buildDingTalkDeliveryMarkdown(eventPayload)); err != nil {
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

func processEmailDelivery(ctx context.Context, queries *db.Queries, emailSvc *service.EmailService, delivery db.NotificationDelivery) {
	claimed, err := queries.ClaimNotificationDelivery(ctx, db.ClaimNotificationDeliveryParams{
		ID:       delivery.ID,
		Status:   "pending",
		Status_2: "pending",
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return
		}
		slog.Warn("notification dispatcher: failed to claim email delivery",
			"delivery_id", util.UUIDToString(delivery.ID),
			"error", err,
		)
		return
	}

	var payload emailDeliveryPayload
	if err := json.Unmarshal(delivery.PayloadSnapshot, &payload); err != nil {
		finalizeFailedEmailDelivery(ctx, queries, claimed, errors.New("invalid email delivery payload"))
		return
	}

	recipientEmail := strings.TrimSpace(payload.ExternalUserID)
	if recipientEmail == "" {
		finalizeFailedEmailDelivery(ctx, queries, claimed, errors.New("missing recipient email"))
		return
	}

	var eventPayload notificationEventPayload
	if len(payload.NotificationEvent) > 0 {
		if err := json.Unmarshal(payload.NotificationEvent, &eventPayload); err != nil {
			finalizeFailedEmailDelivery(ctx, queries, claimed, errors.New("invalid nested notification payload"))
			return
		}
	}
	eventPayload = hydrateNotificationActorName(ctx, queries, claimed, eventPayload)

	title := strings.TrimSpace(eventPayload.Title)
	if title == "" {
		title = "Multica Notification"
	}
	body := strings.TrimSpace(eventPayload.Body)
	link := strings.TrimSpace(eventPayload.Link)

	if err := emailSvc.SendNotificationEmail(recipientEmail, title, body, link, eventPayload.ActorName); err != nil {
		finalizeFailedEmailDelivery(ctx, queries, claimed, err)
		return
	}

	if _, err := queries.CompleteNotificationDelivery(ctx, db.CompleteNotificationDeliveryParams{
		ID:        claimed.ID,
		Status:    "sent",
		LastError: pgtype.Text{},
		SentAt:    pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}); err != nil {
		slog.Warn("notification dispatcher: failed to mark email delivery sent",
			"delivery_id", util.UUIDToString(claimed.ID),
			"error", err,
		)
	}
}

func processCustomWebhookDelivery(ctx context.Context, queries *db.Queries, delivery db.NotificationDelivery) {
	claimed, err := queries.ClaimNotificationDelivery(ctx, db.ClaimNotificationDeliveryParams{
		ID:       delivery.ID,
		Status:   "pending",
		Status_2: "pending",
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return
		}
		slog.Warn("notification dispatcher: failed to claim custom webhook delivery",
			"delivery_id", util.UUIDToString(delivery.ID),
			"error", err,
		)
		return
	}

	var payload customWebhookDeliveryPayload
	if err := json.Unmarshal(claimed.PayloadSnapshot, &payload); err != nil {
		finalizeFailedCustomWebhookDelivery(ctx, queries, claimed, errors.New("invalid custom webhook delivery payload"))
		return
	}
	endpointID := strings.TrimSpace(payload.WebhookEndpointID)
	if endpointID == "" && claimed.TargetType == "webhook_endpoint" {
		endpointID = util.UUIDToString(claimed.TargetID)
	}
	if endpointID == "" {
		finalizeFailedCustomWebhookDelivery(ctx, queries, claimed, errors.New("missing custom webhook endpoint id"))
		return
	}

	endpoint, err := queries.GetNotificationWebhookEndpoint(ctx, util.ParseUUID(endpointID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			finalizeFailedCustomWebhookDelivery(ctx, queries, claimed, errors.New("custom webhook endpoint not found"))
			return
		}
		finalizeFailedCustomWebhookDelivery(ctx, queries, claimed, err)
		return
	}
	if !endpoint.Enabled {
		finalizeFailedCustomWebhookDelivery(ctx, queries, claimed, errors.New("custom webhook endpoint is disabled"))
		return
	}

	endpointURL, err := notifyutil.DecryptToken(endpoint.UrlEncrypted)
	if err != nil {
		finalizeFailedCustomWebhookDelivery(ctx, queries, claimed, errors.New("failed to decrypt custom webhook url"))
		return
	}
	secret := ""
	if endpoint.SecretEncrypted.Valid && strings.TrimSpace(endpoint.SecretEncrypted.String) != "" {
		secret, err = notifyutil.DecryptToken(endpoint.SecretEncrypted.String)
		if err != nil {
			finalizeFailedCustomWebhookDelivery(ctx, queries, claimed, errors.New("failed to decrypt custom webhook secret"))
			return
		}
	}

	var eventPayload notificationEventPayload
	if len(payload.NotificationEvent) > 0 {
		if err := json.Unmarshal(payload.NotificationEvent, &eventPayload); err != nil {
			finalizeFailedCustomWebhookDelivery(ctx, queries, claimed, errors.New("invalid nested notification payload"))
			return
		}
	}
	event, err := queries.GetNotificationEvent(ctx, claimed.NotificationEventID)
	if err != nil {
		finalizeFailedCustomWebhookDelivery(ctx, queries, claimed, err)
		return
	}

	outbound, err := buildCustomWebhookPayload(claimed, event, eventPayload)
	if err != nil {
		finalizeFailedCustomWebhookDelivery(ctx, queries, claimed, err)
		return
	}
	if err := customWebhookSender.SendJSON(ctx, endpointURL, secret, outbound); err != nil {
		finalizeFailedCustomWebhookDelivery(ctx, queries, claimed, err)
		return
	}

	if _, err := queries.CompleteNotificationDelivery(ctx, db.CompleteNotificationDeliveryParams{
		ID:        claimed.ID,
		Status:    "sent",
		LastError: pgtype.Text{},
		SentAt:    pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}); err != nil {
		slog.Warn("notification dispatcher: failed to mark custom webhook delivery sent",
			"delivery_id", util.UUIDToString(claimed.ID),
			"error", err,
		)
	}
}

func buildCustomWebhookPayload(delivery db.NotificationDelivery, event db.NotificationEvent, eventPayload notificationEventPayload) ([]byte, error) {
	eventID := util.UUIDToString(delivery.NotificationEventID)
	details := json.RawMessage(event.Details)
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}
	return json.Marshal(map[string]any{
		"event_id":          eventID,
		"delivery_id":       util.UUIDToString(delivery.ID),
		"event_type":        firstValue(event.Type, eventPayload.Type),
		"workspace_id":      util.UUIDToString(event.WorkspaceID),
		"recipient_user_id": util.UUIDToString(event.RecipientUserID),
		"title":             firstValue(event.Title, eventPayload.Title),
		"body":              firstValue(pgTextToString(event.Body), eventPayload.Body),
		"link":              firstValue(pgTextToString(event.Link), eventPayload.Link),
		"issue": map[string]any{
			"id":         util.UUIDToPtr(event.IssueID),
			"identifier": eventPayload.IssueIdentifier,
		},
		"comment": map[string]any{
			"id": util.UUIDToPtr(event.CommentID),
		},
		"actor": map[string]any{
			"type": util.TextToPtr(event.ActorType),
			"id":   util.UUIDToPtr(event.ActorID),
		},
		"details":     details,
		"occurred_at": delivery.CreatedAt.Time.UTC().Format(time.RFC3339),
	})
}

func finalizeFailedCustomWebhookDelivery(ctx context.Context, queries *db.Queries, delivery db.NotificationDelivery, dispatchErr error) {
	nextStatus := "pending"
	if delivery.AttemptCount >= webhookDeliveryMaxAttempts {
		nextStatus = "failed"
	}

	if _, err := queries.CompleteNotificationDelivery(ctx, db.CompleteNotificationDeliveryParams{
		ID:        delivery.ID,
		Status:    nextStatus,
		LastError: util.StrToText(truncateError(dispatchErr)),
		SentAt:    pgtype.Timestamptz{},
	}); err != nil {
		slog.Warn("notification dispatcher: failed to mark custom webhook delivery failure",
			"delivery_id", util.UUIDToString(delivery.ID),
			"error", err,
		)
		return
	}

	slog.Warn("notification dispatcher: custom webhook delivery failed",
		"delivery_id", util.UUIDToString(delivery.ID),
		"status", nextStatus,
		"attempt_count", delivery.AttemptCount,
		"error", dispatchErr,
	)
}

func finalizeFailedEmailDelivery(ctx context.Context, queries *db.Queries, delivery db.NotificationDelivery, dispatchErr error) {
	nextStatus := "pending"
	if delivery.AttemptCount >= emailDeliveryMaxAttempts {
		nextStatus = "failed"
	}

	if _, err := queries.CompleteNotificationDelivery(ctx, db.CompleteNotificationDeliveryParams{
		ID:        delivery.ID,
		Status:    nextStatus,
		LastError: util.StrToText(truncateError(dispatchErr)),
		SentAt:    pgtype.Timestamptz{},
	}); err != nil {
		slog.Warn("notification dispatcher: failed to mark email delivery failure",
			"delivery_id", util.UUIDToString(delivery.ID),
			"error", err,
		)
		return
	}

	slog.Warn("notification dispatcher: email delivery failed",
		"delivery_id", util.UUIDToString(delivery.ID),
		"status", nextStatus,
		"attempt_count", delivery.AttemptCount,
		"error", dispatchErr,
	)
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

func hydrateNotificationActorName(ctx context.Context, queries *db.Queries, delivery db.NotificationDelivery, event notificationEventPayload) notificationEventPayload {
	if strings.TrimSpace(event.ActorName) != "" {
		return event
	}

	notificationEvent, err := queries.GetNotificationEvent(ctx, delivery.NotificationEventID)
	if err != nil {
		return event
	}

	actorType := ""
	if notificationEvent.ActorType.Valid {
		actorType = notificationEvent.ActorType.String
	}
	event.ActorName = resolveNotificationActorName(ctx, queries, actorType, util.UUIDToString(notificationEvent.ActorID))
	return event
}

func backfillDingTalkBindingUserID(ctx context.Context, queries *db.Queries, cfg notifyutil.DingTalkConfig, binding db.ExternalAccountBinding, metadata dingTalkBindingMetadata) (string, error) {
	var profile notifyutil.DingTalkUserProfile
	var lookupErr error

	userID := ""
	if mobile := strings.TrimSpace(metadata.Mobile); mobile != "" {
		userID, lookupErr = cfg.UserIDByMobile(ctx, metadata.CorpID, mobile)
	}

	if userID == "" {
		if !binding.AccessTokenEncrypted.Valid || strings.TrimSpace(binding.AccessTokenEncrypted.String) == "" {
			return "", combineDingTalkLookupErrors(lookupErr, errors.New("dingtalk delivery is missing bound user_id"))
		}

		accessToken, err := notifyutil.DecryptToken(binding.AccessTokenEncrypted.String)
		if err != nil {
			return "", combineDingTalkLookupErrors(lookupErr, errors.New("failed to decrypt dingtalk access token"))
		}
		if strings.TrimSpace(accessToken) == "" {
			return "", combineDingTalkLookupErrors(lookupErr, errors.New("dingtalk delivery is missing bound user access token"))
		}

		profile, err = cfg.GetUserProfile(ctx, accessToken)
		if err != nil {
			return "", combineDingTalkLookupErrors(lookupErr, err)
		}
		userID = strings.TrimSpace(profile.UserID)
		if userID == "" {
			return "", combineDingTalkLookupErrors(lookupErr, errors.New("dingtalk user info missing user_id"))
		}
	}

	rawMetadata := map[string]any{}
	if len(binding.Metadata) > 0 {
		if err := json.Unmarshal(binding.Metadata, &rawMetadata); err != nil {
			return "", errors.New("invalid dingtalk binding metadata")
		}
	}
	rawMetadata["corp_id"] = firstValue(metadata.CorpID)
	rawMetadata["user_id"] = userID
	if unionID := firstValue(metadata.UnionID, profile.UnionID, binding.ExternalUserID); unionID != "" {
		rawMetadata["union_id"] = unionID
	}
	if openID := firstValue(metadata.OpenID, profile.OpenID); openID != "" {
		rawMetadata["open_id"] = openID
	}
	if mobile := firstValue(metadata.Mobile, profile.Mobile); mobile != "" {
		rawMetadata["mobile"] = mobile
	}

	metadataJSON, err := json.Marshal(rawMetadata)
	if err != nil {
		return "", errors.New("failed to encode dingtalk binding metadata")
	}

	if _, err := queries.UpsertExternalAccountBinding(ctx, db.UpsertExternalAccountBindingParams{
		UserID:                binding.UserID,
		Provider:              binding.Provider,
		ExternalUserID:        binding.ExternalUserID,
		DisplayName:           binding.DisplayName,
		AccessTokenEncrypted:  binding.AccessTokenEncrypted,
		RefreshTokenEncrypted: binding.RefreshTokenEncrypted,
		TokenExpiresAt:        binding.TokenExpiresAt,
		Status:                binding.Status,
		Metadata:              metadataJSON,
	}); err != nil {
		return "", err
	}

	return userID, nil
}

func combineDingTalkLookupErrors(primary, fallback error) error {
	if primary == nil {
		return fallback
	}
	if fallback == nil {
		return primary
	}
	return fmt.Errorf("dingtalk user_id lookup failed: mobile lookup: %v; user token fallback: %v", primary, fallback)
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
	if actorName := strings.TrimSpace(event.ActorName); actorName != "" {
		parts = append(parts, "From: "+actorName)
	}
	if body := strings.TrimSpace(event.Body); body != "" {
		parts = append(parts, body)
	}
	if link := strings.TrimSpace(event.Link); link != "" {
		parts = append(parts, "Link: "+link)
	}
	return strings.Join(parts, "\n\n")
}

func buildDingTalkDeliveryMarkdown(event notificationEventPayload) notifyutil.DingTalkMarkdownMessage {
	title := buildDingTalkIssueLabel(event)
	body := sanitizeDingTalkMessageText(event.Body)
	link := strings.TrimSpace(event.Link)
	parts := []string{"### Multica Notification"}
	if title != "" {
		parts = append(parts, "**Issue**\n"+title)
	}
	if actorName := strings.TrimSpace(event.ActorName); actorName != "" {
		parts = append(parts, "**From**\n"+actorName)
	}
	if body != "" {
		parts = append(parts, "**Message**\n"+body)
	}
	if link != "" {
		parts = append(parts, "[Open In Multica]("+dingtalkExternalBrowserURL(link)+")")
	}

	cardTitle := title
	if cardTitle == "" {
		cardTitle = "Multica Notification"
	}

	return notifyutil.DingTalkMarkdownMessage{
		Title: truncateDingTalkCardText(cardTitle, 80),
		Text:  truncateDingTalkCardText(strings.Join(parts, "\n\n"), 1800),
	}
}

func buildDingTalkIssueLabel(event notificationEventPayload) string {
	title := strings.TrimSpace(event.Title)
	identifier := strings.TrimSpace(event.IssueIdentifier)
	if title == "" {
		return identifier
	}
	if identifier == "" || title == identifier || strings.HasPrefix(title, identifier+" ") || strings.HasPrefix(title, identifier+" ·") {
		return title
	}
	return identifier + " · " + title
}

func dingtalkExternalBrowserURL(raw string) string {
	link := strings.TrimSpace(raw)
	if link == "" {
		return ""
	}
	return "dingtalk://dingtalkclient/page/link?url=" + url.QueryEscape(link) + "&pc_slide=false"
}

func sanitizeDingTalkMessageText(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	return strings.TrimSpace(dingtalkMentionLinkPattern.ReplaceAllString(text, "@$1"))
}

func truncateDingTalkCardText(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
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

func pgTextToString(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
