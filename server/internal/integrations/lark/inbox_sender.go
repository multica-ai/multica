package lark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channelnotify"
	"github.com/multica-ai/multica/server/internal/util"
)

type inboxInstallationStore interface {
	GetLarkInstallation(context.Context, pgtype.UUID) (Installation, error)
}

// InboxSender delivers one already-created Inbox item through the exact
// Feishu Bot installation selected by channelnotify. It never searches for a
// different Bot or binding.
type InboxSender struct {
	store       inboxInstallationStore
	credentials CredentialsResolver
	client      APIClient
	appURL      string
	logger      *slog.Logger
}

func NewInboxSender(store inboxInstallationStore, credentials CredentialsResolver, client APIClient, appURL string, logger *slog.Logger) *InboxSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &InboxSender{
		store:       store,
		credentials: credentials,
		client:      client,
		appURL:      strings.TrimRight(strings.TrimSpace(appURL), "/"),
		logger:      logger,
	}
}

func (s *InboxSender) SendInbox(ctx context.Context, target channelnotify.Target, notification channelnotify.Notification) error {
	if s == nil || s.store == nil {
		return errors.New("lark inbox sender: installation store missing")
	}
	if s.client == nil {
		return errors.New("lark inbox sender: API client missing")
	}
	if target.ChannelType != channel.TypeFeishu {
		return fmt.Errorf("lark inbox sender: unsupported channel type %q", target.ChannelType)
	}
	if target.ChannelUserID == "" {
		return errors.New("lark inbox sender: channel user id missing")
	}

	installation, err := s.store.GetLarkInstallation(ctx, target.InstallationID)
	if err != nil {
		return fmt.Errorf("lark inbox sender: load installation: %w", err)
	}
	if installation.ID != target.InstallationID {
		return errors.New("lark inbox sender: installation id mismatch")
	}
	if installation.Status != "active" {
		return errors.New("lark inbox sender: installation is not active")
	}
	if installation.AgentID != target.AgentID {
		return errors.New("lark inbox sender: installation agent mismatch")
	}

	creds, err := installationCredentialsFor(installation, s.credentials)
	if err != nil {
		return fmt.Errorf("lark inbox sender: credentials: %w", err)
	}
	cardJSON, err := s.renderCard(target, notification)
	if err != nil {
		return fmt.Errorf("lark inbox sender: render card: %w", err)
	}
	if err := s.client.SendDMCard(ctx, SendDMCardParams{
		InstallationID: creds,
		OpenID:         OpenID(target.ChannelUserID),
		CardJSON:       cardJSON,
	}); err != nil {
		return fmt.Errorf("lark inbox sender: send DM card: %w", err)
	}
	s.logger.Debug("sent Feishu Inbox notification",
		"installation_id", uuidString(target.InstallationID),
		"inbox_item_id", uuidString(notification.InboxItemID))
	return nil
}

func (s *InboxSender) renderCard(target channelnotify.Target, notification channelnotify.Notification) (string, error) {
	elements := []any{
		map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":     "plain_text",
				"content": notification.Body,
			},
		},
	}
	if deepLink := s.deepLink(target, notification); deepLink != "" {
		elements = append(elements, map[string]any{
			"tag": "action",
			"actions": []any{
				map[string]any{
					"tag":  "button",
					"text": map[string]any{"tag": "plain_text", "content": "Open in Multica"},
					"type": "primary",
					"url":  deepLink,
				},
			},
		})
	}
	doc := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"template": "blue",
			"title": map[string]any{
				"tag":     "plain_text",
				"content": notification.Title,
			},
		},
		"elements": elements,
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (s *InboxSender) deepLink(target channelnotify.Target, notification channelnotify.Notification) string {
	if s.appURL == "" || target.WorkspaceSlug == "" || !notification.IssueID.Valid {
		return ""
	}
	return s.appURL + "/" + url.PathEscape(target.WorkspaceSlug) +
		"/inbox?issue=" + url.QueryEscape(util.UUIDToString(notification.IssueID))
}
