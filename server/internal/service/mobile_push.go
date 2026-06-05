package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const expoPushURL = "https://exp.host/--/api/v2/push/send"

type MobilePushConfig struct {
	AccessToken string
	Endpoint    string
	DryRun      bool
	Timeout     time.Duration
}

type MobilePushService struct {
	queries *db.Queries
	client  *http.Client
	config  MobilePushConfig
}

func NewMobilePushServiceFromEnv(queries *db.Queries) *MobilePushService {
	timeout := 10 * time.Second
	cfg := MobilePushConfig{
		AccessToken: strings.TrimSpace(os.Getenv("EXPO_ACCESS_TOKEN")),
		Endpoint:    strings.TrimSpace(os.Getenv("MULTICA_EXPO_PUSH_URL")),
		DryRun:      os.Getenv("MULTICA_PUSH_DRY_RUN") == "true",
		Timeout:     timeout,
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = expoPushURL
	}
	if cfg.AccessToken == "" {
		cfg.DryRun = true
	}
	return NewMobilePushService(queries, cfg, nil)
}

func NewMobilePushService(queries *db.Queries, cfg MobilePushConfig, client *http.Client) *MobilePushService {
	if cfg.Endpoint == "" {
		cfg.Endpoint = expoPushURL
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	return &MobilePushService{queries: queries, client: client, config: cfg}
}

func (s *MobilePushService) Register(bus *events.Bus) {
	if s == nil || bus == nil {
		return
	}
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		go s.handleInboxNew(context.Background(), e)
	})
}

func (s *MobilePushService) handleInboxNew(ctx context.Context, e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	rawItem, ok := payload["item"].(map[string]any)
	if !ok {
		return
	}
	item, ok := pushInboxItemFromPayload(rawItem)
	if !ok {
		return
	}
	if item.RecipientType != "member" || item.RecipientID == "" || item.WorkspaceID == "" {
		return
	}
	if !isMobilePushInboxType(item.Type) {
		return
	}

	tokens, err := s.queries.ListEnabledMobilePushTokensForInboxItem(ctx, db.ListEnabledMobilePushTokensForInboxItemParams{
		WorkspaceID: util.MustParseUUID(item.WorkspaceID),
		UserID:      util.MustParseUUID(item.RecipientID),
	})
	if err != nil {
		slog.Warn("mobile push: list tokens failed", "error", err, "inbox_item_id", item.ID)
		return
	}
	for _, token := range tokens {
		status, providerID, sendErr := s.send(ctx, token, item)
		errText := ""
		if sendErr != nil {
			errText = sendErr.Error()
		}
		_, err := s.queries.CreateMobilePushDelivery(ctx, db.CreateMobilePushDeliveryParams{
			InboxItemID:       util.MustParseUUID(item.ID),
			DeviceTokenID:     token.ID,
			Provider:          "expo",
			Status:            status,
			ProviderMessageID: util.StrToText(providerID),
			Error:             util.StrToText(errText),
		})
		if err != nil {
			slog.Warn("mobile push: create delivery failed", "error", err, "inbox_item_id", item.ID)
		}
		if errors.Is(sendErr, errExpoDeviceNotRegistered) {
			if err := s.queries.DisableMobilePushDeviceTokenByID(ctx, token.ID); err != nil {
				slog.Warn("mobile push: disable invalid token failed", "error", err, "token_id", util.UUIDToString(token.ID))
			}
		}
	}
}

type pushInboxItem struct {
	ID            string
	WorkspaceID   string
	RecipientType string
	RecipientID   string
	Type          string
	Severity      string
	IssueID       string
	Title         string
	Body          string
	Details       map[string]string
}

func pushInboxItemFromPayload(raw map[string]any) (pushInboxItem, bool) {
	item := pushInboxItem{
		ID:            stringFromAny(raw["id"]),
		WorkspaceID:   stringFromAny(raw["workspace_id"]),
		RecipientType: stringFromAny(raw["recipient_type"]),
		RecipientID:   stringFromAny(raw["recipient_id"]),
		Type:          stringFromAny(raw["type"]),
		Severity:      stringFromAny(raw["severity"]),
		IssueID:       stringFromAny(raw["issue_id"]),
		Title:         stringFromAny(raw["title"]),
		Body:          stringFromAny(raw["body"]),
		Details:       map[string]string{},
	}
	if details, ok := raw["details"].(map[string]any); ok {
		for k, v := range details {
			if s := stringFromAny(v); s != "" {
				item.Details[k] = s
			}
		}
	} else if details, ok := raw["details"].(json.RawMessage); ok && len(details) > 0 {
		var parsed map[string]string
		if err := json.Unmarshal(details, &parsed); err == nil {
			item.Details = parsed
		}
	}
	return item, item.ID != ""
}

func stringFromAny(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case *string:
		if value == nil {
			return ""
		}
		return *value
	default:
		return ""
	}
}

func isMobilePushInboxType(t string) bool {
	switch t {
	case "issue_assigned", "assignee_changed", "mentioned", "new_comment", "task_completed", "task_failed", "agent_blocked", "agent_completed":
		return true
	default:
		return false
	}
}

func (s *MobilePushService) send(ctx context.Context, token db.MobilePushDeviceToken, item pushInboxItem) (string, string, error) {
	if s.config.DryRun {
		slog.Info("mobile push dry-run",
			"inbox_item_id", item.ID,
			"device_token_id", util.UUIDToString(token.ID),
			"type", item.Type)
		return "skipped", "", nil
	}

	body := expoPushRequest{
		To:       token.Token,
		Sound:    "default",
		Title:    pushTitle(item),
		Body:     pushBody(item),
		Priority: "high",
		Data: map[string]string{
			"workspace_id": item.WorkspaceID,
			"issue_id":     item.IssueID,
			"inbox_item_id": item.ID,
			"type":         item.Type,
		},
	}
	if commentID := item.Details["comment_id"]; commentID != "" {
		body.Data["comment_id"] = commentID
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "failed", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return "failed", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.config.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.config.AccessToken)
	}
	res, err := s.client.Do(req)
	if err != nil {
		return "failed", "", err
	}
	defer res.Body.Close()
	resBody, _ := io.ReadAll(io.LimitReader(res.Body, 16*1024))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "failed", "", fmt.Errorf("expo push returned %d", res.StatusCode)
	}

	var parsed expoPushResponse
	if err := json.Unmarshal(resBody, &parsed); err != nil {
		return "failed", "", err
	}
	if parsed.Data.Status == "ok" {
		return "sent", parsed.Data.ID, nil
	}
	errMsg := parsed.Data.Message
	if errMsg == "" {
		errMsg = parsed.Data.Details.Error
	}
	if parsed.Data.Details.Error == "DeviceNotRegistered" {
		return "failed", parsed.Data.ID, errExpoDeviceNotRegistered
	}
	if errMsg == "" {
		errMsg = "expo push failed"
	}
	return "failed", parsed.Data.ID, errors.New(errMsg)
}

type expoPushRequest struct {
	To       string            `json:"to"`
	Sound    string            `json:"sound"`
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	Priority string            `json:"priority"`
	Data     map[string]string `json:"data"`
}

type expoPushResponse struct {
	Data struct {
		Status  string `json:"status"`
		ID      string `json:"id"`
		Message string `json:"message"`
		Details struct {
			Error string `json:"error"`
		} `json:"details"`
	} `json:"data"`
}

var errExpoDeviceNotRegistered = errors.New("expo device not registered")

func pushTitle(item pushInboxItem) string {
	switch item.Type {
	case "issue_assigned":
		return "Assigned to you"
	case "mentioned":
		return "Mentioned you"
	case "new_comment":
		return "New comment"
	case "task_failed":
		return "Agent needs attention"
	case "task_completed", "agent_completed":
		return "Agent finished"
	case "agent_blocked":
		return "Agent is blocked"
	default:
		return "Multica"
	}
}

func pushBody(item pushInboxItem) string {
	body := strings.TrimSpace(item.Title)
	if body == "" {
		body = "Open Multica to view the update."
	}
	if len(body) > 160 {
		body = body[:157] + "..."
	}
	return body
}
