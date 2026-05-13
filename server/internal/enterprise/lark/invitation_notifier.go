package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const defaultContactBatchGetIDURL = "https://open.feishu.cn/open-apis/contact/v3/users/batch_get_id"

type InvitationConfig struct {
	Enabled              bool
	AppID                string
	AppSecret            string
	TenantAccessTokenURL string
	MessageURL           string
	ContactBatchGetIDURL string
	TenantKey            string
	AppURL               string
	AllowedEmailDomains  []string
	PollInterval         time.Duration
	BatchSize            int32
	MaxAttempts          int32
	InitialBackoff       time.Duration
	MaxBackoff           time.Duration
}

type InvitationNotifier struct {
	queries *db.Queries
	cfg     InvitationConfig
	channel *NotificationChannel
}

func InvitationConfigFromEnv() InvitationConfig {
	return InvitationConfig{
		Enabled:              envBool("LARK_INVITE_NOTIFY_ENABLED"),
		AppID:                strings.TrimSpace(os.Getenv("LARK_APP_ID")),
		AppSecret:            firstNonEmpty(os.Getenv("LARK_APP_SECRET"), os.Getenv("LARK_APP_SECRET_REF")),
		TenantAccessTokenURL: firstNonEmpty(os.Getenv("LARK_TENANT_ACCESS_TOKEN_URL"), defaultTenantAccessTokenURL),
		MessageURL:           firstNonEmpty(os.Getenv("LARK_MESSAGE_URL"), defaultMessageURL),
		ContactBatchGetIDURL: firstNonEmpty(os.Getenv("LARK_CONTACT_BATCH_GET_ID_URL"), defaultContactBatchGetIDURL),
		TenantKey:            tenantKeyFromEnv(),
		AppURL:               appURLFromEnv(),
		AllowedEmailDomains:  inviteEmailDomainsFromEnv(),
		PollInterval:         envDuration("LARK_INVITE_DELIVERY_POLL_INTERVAL", 5*time.Second),
		BatchSize:            envInt32("LARK_INVITE_DELIVERY_BATCH_SIZE", 20),
		MaxAttempts:          envInt32("LARK_INVITE_DELIVERY_MAX_ATTEMPTS", 5),
		InitialBackoff:       envDuration("LARK_INVITE_DELIVERY_INITIAL_BACKOFF", 30*time.Second),
		MaxBackoff:           envDuration("LARK_INVITE_DELIVERY_MAX_BACKOFF", 30*time.Minute),
	}
}

func NewInvitationNotifierFromEnv(queries *db.Queries) *InvitationNotifier {
	return NewInvitationNotifier(queries, InvitationConfigFromEnv(), nil)
}

func NewInvitationNotifier(queries *db.Queries, cfg InvitationConfig, httpClient *http.Client) *InvitationNotifier {
	if cfg.TenantAccessTokenURL == "" {
		cfg.TenantAccessTokenURL = defaultTenantAccessTokenURL
	}
	if cfg.MessageURL == "" {
		cfg.MessageURL = defaultMessageURL
	}
	if cfg.ContactBatchGetIDURL == "" {
		cfg.ContactBatchGetIDURL = defaultContactBatchGetIDURL
	}
	if cfg.AppURL == "" {
		cfg.AppURL = "http://localhost:3000"
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 20
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = 30 * time.Second
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 30 * time.Minute
	}
	channel := NewNotificationChannel(NotificationConfig{
		Enabled:              cfg.Enabled,
		AppID:                cfg.AppID,
		AppSecret:            cfg.AppSecret,
		TenantAccessTokenURL: cfg.TenantAccessTokenURL,
		MessageURL:           cfg.MessageURL,
	}, httpClient)
	return &InvitationNotifier{queries: queries, cfg: cfg, channel: channel}
}

func (n *InvitationNotifier) Enabled() bool {
	return n != nil && n.cfg.Enabled && n.queries != nil && n.cfg.AppID != "" && n.cfg.AppSecret != "" && n.cfg.TenantKey != ""
}

func (n *InvitationNotifier) EnqueueInvitation(ctx context.Context, invitationID, workspaceID pgtype.UUID, inviteeEmail string) error {
	if !n.Enabled() {
		return nil
	}
	email := normalizeEmail(inviteeEmail)
	if email == "" {
		return errors.New("invitee email is required")
	}
	if !n.emailDomainAllowed(email) {
		return nil
	}
	return n.queries.CreateLarkInvitationDelivery(ctx, db.CreateLarkInvitationDeliveryParams{
		InvitationID: invitationID,
		WorkspaceID:  workspaceID,
		TenantKey:    n.cfg.TenantKey,
		InviteeEmail: email,
		DedupeKey:    "lark-invite:" + util.UUIDToString(invitationID),
	})
}

func (n *InvitationNotifier) RunWorker(ctx context.Context) {
	if !n.Enabled() {
		return
	}
	ticker := time.NewTicker(n.cfg.PollInterval)
	defer ticker.Stop()

	n.processBatch(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.processBatch(ctx)
		}
	}
}

func (n *InvitationNotifier) processBatch(ctx context.Context) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	rows, err := n.queries.ClaimPendingLarkInvitationDeliveries(queryCtx, db.ClaimPendingLarkInvitationDeliveriesParams{
		MaxAttempts: n.cfg.MaxAttempts,
		Limit:       n.cfg.BatchSize,
	})
	cancel()
	if err != nil {
		slog.Warn("failed to claim Lark invitation deliveries", "error", err)
		return
	}
	for _, delivery := range rows {
		n.processDelivery(ctx, delivery)
	}
}

func (n *InvitationNotifier) processDelivery(ctx context.Context, delivery db.LarkInvitationDelivery) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	row, err := n.queries.GetLarkInvitationDeliveryMessage(queryCtx, db.GetLarkInvitationDeliveryMessageParams{
		Provider: ProviderName,
		ID:       delivery.ID,
	})
	cancel()
	if err != nil {
		n.retryOrFail(ctx, delivery, err)
		return
	}
	if row.InvitationStatus != "pending" {
		n.markSkipped(ctx, delivery.ID, "invitation is no longer pending")
		return
	}
	if row.ExpiresAt.Valid && time.Now().After(row.ExpiresAt.Time) {
		n.markSkipped(ctx, delivery.ID, "invitation has expired")
		return
	}

	openID := firstNonEmpty(textValue(row.LarkOpenID), textValue(row.IdentityOpenID))
	if openID == "" {
		resolved, err := n.resolveOpenIDByEmail(ctx, row.InviteeEmail)
		if err != nil {
			if errors.Is(err, errLarkUserNotFound) {
				n.markSkipped(ctx, delivery.ID, err.Error())
			} else {
				n.retryOrFail(ctx, delivery, err)
			}
			return
		}
		openID = resolved
		updateCtx, updateCancel := context.WithTimeout(ctx, 5*time.Second)
		if err := n.queries.SetLarkInvitationDeliveryOpenID(updateCtx, db.SetLarkInvitationDeliveryOpenIDParams{
			ID:         delivery.ID,
			LarkOpenID: openID,
		}); err != nil {
			slog.Warn("failed to store Lark invitation open_id", "delivery_id", util.UUIDToString(delivery.ID), "error", err)
		}
		updateCancel()
	}

	sendCtx, sendCancel := context.WithTimeout(ctx, 10*time.Second)
	messageID, err := n.sendInvitationCard(sendCtx, openID, invitationCard{
		InvitationID:  util.UUIDToString(row.InvitationID),
		DedupeKey:     row.DedupeKey,
		WorkspaceName: row.WorkspaceName,
		InviterName:   firstNonEmpty(row.InviterName, row.InviterEmail),
		Role:          row.Role,
		InviteURL:     n.inviteURL(row.InvitationID),
		ExpiresAt:     row.ExpiresAt,
	})
	sendCancel()
	if err != nil {
		n.retryOrFail(ctx, delivery, err)
		return
	}

	updateCtx, updateCancel := context.WithTimeout(ctx, 5*time.Second)
	if err := n.queries.MarkLarkInvitationDeliverySent(updateCtx, db.MarkLarkInvitationDeliverySentParams{
		ID:            delivery.ID,
		SentMessageID: textParam(messageID),
	}); err != nil {
		slog.Warn("failed to mark Lark invitation delivery sent", "delivery_id", util.UUIDToString(delivery.ID), "error", err)
	}
	updateCancel()
}

var errLarkUserNotFound = errors.New("no Lark user found for invitee email")

func (n *InvitationNotifier) resolveOpenIDByEmail(ctx context.Context, email string) (string, error) {
	token, err := n.channel.tenantAccessToken(ctx)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(n.cfg.ContactBatchGetIDURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("user_id_type", "open_id")
	u.RawQuery = q.Encode()

	body, _ := json.Marshal(map[string][]string{"emails": []string{email}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	var decoded map[string]any
	if err := n.channel.doJSON(req, &decoded); err != nil {
		return "", err
	}
	if code, ok := decoded["code"].(float64); ok && code != 0 {
		return "", fmt.Errorf("Lark contact lookup rejected: %s", stringField(decoded, "msg"))
	}
	openID := openIDFromBatchGetIDResponse(decoded, email)
	if openID == "" {
		return "", errLarkUserNotFound
	}
	return openID, nil
}

type invitationCard struct {
	InvitationID  string
	DedupeKey     string
	WorkspaceName string
	InviterName   string
	Role          string
	InviteURL     string
	ExpiresAt     pgtype.Timestamptz
}

func (n *InvitationNotifier) sendInvitationCard(ctx context.Context, openID string, card invitationCard) (string, error) {
	token, err := n.channel.tenantAccessToken(ctx)
	if err != nil {
		return "", err
	}
	contentBytes, _ := json.Marshal(buildInvitationCardContent(card))
	body, _ := json.Marshal(map[string]string{
		"receive_id": openID,
		"msg_type":   "interactive",
		"content":    string(contentBytes),
		"uuid":       card.DedupeKey,
	})
	u, err := url.Parse(n.cfg.MessageURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("receive_id_type", "open_id")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	var decoded map[string]any
	if err := n.channel.doJSON(req, &decoded); err != nil {
		return "", err
	}
	if code, ok := decoded["code"].(float64); ok && code != 0 {
		return "", fmt.Errorf("Lark invitation message rejected: %s", stringField(decoded, "msg"))
	}
	return messageIDFromResponse(decoded), nil
}

func buildInvitationCardContent(card invitationCard) map[string]any {
	role := map[string]string{"admin": "管理员", "member": "成员"}[card.Role]
	if role == "" {
		role = card.Role
	}
	expires := "邀请 7 天内有效"
	if card.ExpiresAt.Valid {
		expires = "邀请有效期至 " + card.ExpiresAt.Time.Format("2006-01-02 15:04 MST")
	}
	body := fmt.Sprintf("**%s** 邀请你以 **%s** 身份加入 **%s**。", card.InviterName, role, card.WorkspaceName)
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"template": "blue",
			"title":    map[string]any{"tag": "plain_text", "content": "你被邀请加入 Multica 工作区"},
		},
		"elements": []any{
			map[string]any{
				"tag": "div",
				"text": map[string]any{
					"tag":     "lark_md",
					"content": body + "\n\n" + expires,
				},
			},
			map[string]any{
				"tag": "action",
				"actions": []any{
					map[string]any{
						"tag":  "button",
						"text": map[string]any{"tag": "plain_text", "content": "打开 Multica"},
						"type": "primary",
						"url":  card.InviteURL,
					},
				},
			},
		},
	}
}

func (n *InvitationNotifier) retryOrFail(ctx context.Context, delivery db.LarkInvitationDelivery, err error) {
	if delivery.RetryCount+1 >= n.cfg.MaxAttempts {
		n.markFailed(ctx, delivery.ID, err.Error())
		return
	}
	delay := n.backoff(delivery.RetryCount)
	updateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if updateErr := n.queries.MarkLarkInvitationDeliveryPendingAfterFailure(updateCtx, db.MarkLarkInvitationDeliveryPendingAfterFailureParams{
		ID:           delivery.ID,
		LastError:    err.Error(),
		DelaySeconds: int32(delay.Seconds()),
	}); updateErr != nil {
		slog.Warn("failed to reschedule Lark invitation delivery", "delivery_id", util.UUIDToString(delivery.ID), "error", updateErr)
	}
	cancel()
}

func (n *InvitationNotifier) markFailed(ctx context.Context, id pgtype.UUID, msg string) {
	updateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if err := n.queries.MarkLarkInvitationDeliveryFailed(updateCtx, db.MarkLarkInvitationDeliveryFailedParams{
		ID:        id,
		LastError: msg,
	}); err != nil {
		slog.Warn("failed to mark Lark invitation delivery failed", "delivery_id", util.UUIDToString(id), "error", err)
	}
	cancel()
}

func (n *InvitationNotifier) markSkipped(ctx context.Context, id pgtype.UUID, msg string) {
	updateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if err := n.queries.MarkLarkInvitationDeliverySkipped(updateCtx, db.MarkLarkInvitationDeliverySkippedParams{
		ID:        id,
		LastError: msg,
	}); err != nil {
		slog.Warn("failed to mark Lark invitation delivery skipped", "delivery_id", util.UUIDToString(id), "error", err)
	}
	cancel()
}

func (n *InvitationNotifier) backoff(retryCount int32) time.Duration {
	delay := n.cfg.InitialBackoff
	for i := int32(0); i < retryCount; i++ {
		delay *= 2
		if delay >= n.cfg.MaxBackoff {
			return n.cfg.MaxBackoff
		}
	}
	if delay > n.cfg.MaxBackoff {
		return n.cfg.MaxBackoff
	}
	return delay
}

func (n *InvitationNotifier) inviteURL(invitationID pgtype.UUID) string {
	return strings.TrimRight(n.cfg.AppURL, "/") + "/invite/" + url.PathEscape(util.UUIDToString(invitationID))
}

func (n *InvitationNotifier) emailDomainAllowed(email string) bool {
	if len(n.cfg.AllowedEmailDomains) == 0 {
		return true
	}
	_, domain, ok := strings.Cut(email, "@")
	if !ok {
		return false
	}
	for _, allowed := range n.cfg.AllowedEmailDomains {
		if strings.EqualFold(domain, strings.TrimPrefix(allowed, "@")) {
			return true
		}
	}
	return false
}

func tenantKeyFromEnv() string {
	if v := strings.TrimSpace(os.Getenv("LARK_TENANT_KEY")); v != "" {
		return v
	}
	allowlist := splitAndTrim(os.Getenv("LARK_TENANT_ALLOWLIST"))
	if len(allowlist) == 1 {
		return allowlist[0]
	}
	return "default"
}

func inviteEmailDomainsFromEnv() []string {
	if v := strings.TrimSpace(os.Getenv("LARK_INVITE_ALLOWED_EMAIL_DOMAINS")); v != "" {
		return splitAndTrim(v)
	}
	return splitAndTrim(os.Getenv("ALLOWED_EMAIL_DOMAINS"))
}

func appURLFromEnv() string {
	for _, key := range []string{"MULTICA_APP_URL", "FRONTEND_ORIGIN"} {
		if v := strings.TrimRight(strings.TrimSpace(os.Getenv(key)), "/"); v != "" {
			return v
		}
	}
	return "http://localhost:3000"
}

func envInt32(key string, def int32) int32 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	var parsed int
	if _, err := fmt.Sscanf(raw, "%d", &parsed); err != nil || parsed <= 0 {
		return def
	}
	return int32(parsed)
}

func envDuration(key string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	v, err := time.ParseDuration(raw)
	if err != nil || v <= 0 {
		return def
	}
	return v
}

func normalizeEmail(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") {
		return ""
	}
	return email
}

func textValue(v pgtype.Text) string {
	if !v.Valid {
		return ""
	}
	return strings.TrimSpace(v.String)
}

func textParam(v string) pgtype.Text {
	v = strings.TrimSpace(v)
	return pgtype.Text{String: v, Valid: v != ""}
}

func openIDFromBatchGetIDResponse(root map[string]any, email string) string {
	data := root
	if nested, ok := root["data"].(map[string]any); ok {
		data = nested
	}
	for _, key := range []string{"user_list", "users"} {
		if openID := openIDFromUserList(data[key], email); openID != "" {
			return openID
		}
	}
	if usersByEmail, ok := data["email_users"].(map[string]any); ok {
		if openID := openIDFromUserList(usersByEmail[email], email); openID != "" {
			return openID
		}
	}
	return ""
}

func openIDFromUserList(raw any, email string) string {
	switch users := raw.(type) {
	case []any:
		for _, item := range users {
			user, ok := item.(map[string]any)
			if !ok {
				continue
			}
			itemEmail := strings.ToLower(strings.TrimSpace(stringField(user, "email")))
			if itemEmail != "" && itemEmail != email {
				continue
			}
			if openID := firstNonEmpty(stringField(user, "open_id"), stringField(user, "user_id")); openID != "" {
				return openID
			}
		}
	case map[string]any:
		return firstNonEmpty(stringField(users, "open_id"), stringField(users, "user_id"))
	}
	return ""
}

func messageIDFromResponse(root map[string]any) string {
	data := root
	if nested, ok := root["data"].(map[string]any); ok {
		data = nested
	}
	return firstNonEmpty(stringField(data, "message_id"), stringField(data, "messageId"))
}
