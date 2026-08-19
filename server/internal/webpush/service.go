package webpush

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	pushproto "github.com/SherClockHolmes/webpush-go"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const deliveryTimeout = 10 * time.Second

type Config struct {
	PublicKey  string
	PrivateKey string
	Subject    string
}

type Subscription struct {
	Endpoint string
	P256DH   string
	Auth     string
}

type Payload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Tag   string `json:"tag"`
	URL   string `json:"url"`
}

type Store interface {
	Upsert(ctx context.Context, userID string, subscription Subscription) error
	DeleteForUser(ctx context.Context, userID, endpoint string) error
	DeleteByEndpoint(ctx context.Context, endpoint string) error
	ListByUser(ctx context.Context, userID string) ([]Subscription, error)
	NotificationPreferences(ctx context.Context, workspaceID, userID string) (map[string]string, error)
	WorkspaceSlug(ctx context.Context, workspaceID string) (string, error)
}

type Sender interface {
	Send(ctx context.Context, subscription Subscription, payload Payload) (status int, err error)
}

type Service struct {
	config Config
	store  Store
	sender Sender
}

func normalizeConfig(config Config) Config {
	config.PublicKey = strings.TrimSpace(config.PublicKey)
	config.PrivateKey = strings.TrimSpace(config.PrivateKey)
	config.Subject = strings.TrimSpace(config.Subject)
	if config.Subject == "" {
		config.Subject = "mailto:notifications@multica.ai"
	}
	return config
}

func NewService(config Config, store Store, sender Sender) *Service {
	config = normalizeConfig(config)
	return &Service{config: config, store: store, sender: sender}
}

func NewProtocolSender(config Config) Sender {
	return &protocolSender{config: normalizeConfig(config)}
}

func (s *Service) Enabled() bool {
	return s != nil && s.store != nil && s.sender != nil && s.config.PublicKey != "" && s.config.PrivateKey != ""
}

func (s *Service) PublicKey() (string, bool) {
	if !s.Enabled() {
		return "", false
	}
	return s.config.PublicKey, true
}

func validateSubscription(subscription Subscription) error {
	parsed, err := url.Parse(subscription.Endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || len(subscription.Endpoint) > 4096 {
		return errors.New("invalid push endpoint")
	}
	if subscription.P256DH == "" || subscription.Auth == "" || len(subscription.P256DH) > 1024 || len(subscription.Auth) > 1024 {
		return errors.New("invalid push subscription keys")
	}
	return nil
}

func (s *Service) Upsert(ctx context.Context, userID, endpoint, p256dh, auth string) error {
	if !s.Enabled() {
		return errors.New("web push is not configured")
	}
	subscription := Subscription{Endpoint: endpoint, P256DH: p256dh, Auth: auth}
	if err := validateSubscription(subscription); err != nil {
		return err
	}
	return s.store.Upsert(ctx, userID, subscription)
}

func (s *Service) Delete(ctx context.Context, userID, endpoint string) error {
	if strings.TrimSpace(endpoint) == "" {
		return errors.New("push endpoint is required")
	}
	return s.store.DeleteForUser(ctx, userID, endpoint)
}

func (s *Service) Register(bus *events.Bus) {
	if !s.Enabled() || bus == nil {
		return
	}
	bus.Subscribe(protocol.EventInboxNew, func(event events.Event) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), deliveryTimeout)
			defer cancel()
			if err := s.Deliver(ctx, event); err != nil {
				slog.Warn("web push delivery failed", "workspace_id", event.WorkspaceID, "error", err)
			}
		}()
	})
}

type inboxEventPayload struct {
	ID            string  `json:"id"`
	RecipientType string  `json:"recipient_type"`
	RecipientID   string  `json:"recipient_id"`
	IssueID       *string `json:"issue_id"`
	Title         string  `json:"title"`
	Body          *string `json:"body"`
}

func decodeInboxEvent(value any) (inboxEventPayload, bool) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return inboxEventPayload{}, false
	}
	var envelope struct {
		Item inboxEventPayload `json:"item"`
	}
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		return inboxEventPayload{}, false
	}
	payload := envelope.Item
	if payload.ID == "" || payload.RecipientType != "member" || payload.RecipientID == "" || payload.Title == "" {
		return inboxEventPayload{}, false
	}
	return payload, true
}

func deliveryMuted(preferences map[string]string) bool {
	if value, ok := preferences["browser_push"]; ok {
		return value == "muted"
	}
	return preferences["system_notifications"] == "muted"
}

func (s *Service) Deliver(ctx context.Context, event events.Event) error {
	if !s.Enabled() || event.Type != protocol.EventInboxNew {
		return nil
	}
	payload, ok := decodeInboxEvent(event.Payload)
	if !ok {
		return nil
	}
	preferences, err := s.store.NotificationPreferences(ctx, event.WorkspaceID, payload.RecipientID)
	if err != nil {
		return fmt.Errorf("load notification preferences: %w", err)
	}
	if deliveryMuted(preferences) {
		return nil
	}
	slug, err := s.store.WorkspaceSlug(ctx, event.WorkspaceID)
	if err != nil {
		return fmt.Errorf("load source workspace slug: %w", err)
	}
	if slug == "" {
		return errors.New("source workspace slug is empty")
	}
	subscriptions, err := s.store.ListByUser(ctx, payload.RecipientID)
	if err != nil {
		return fmt.Errorf("list web push subscriptions: %w", err)
	}
	inboxURL := "/tag/" + url.PathEscape(slug) + "/inbox"
	if payload.IssueID != nil && *payload.IssueID != "" {
		inboxURL += "?issue=" + url.QueryEscape(*payload.IssueID)
	}
	body := payload.Title
	if payload.Body != nil && *payload.Body != "" {
		body = *payload.Body
	}
	pushPayload := Payload{
		Title: payload.Title,
		Body:  body,
		Tag:   "inbox:" + payload.ID,
		URL:   inboxURL,
	}

	for _, subscription := range subscriptions {
		status, sendErr := s.sender.Send(ctx, subscription, pushPayload)
		if status == http.StatusNotFound || status == http.StatusGone {
			if err := s.store.DeleteByEndpoint(ctx, subscription.Endpoint); err != nil {
				slog.Warn("failed to remove revoked web push subscription", "error", err)
			}
			continue
		}
		if sendErr != nil {
			slog.Warn("web push subscription delivery failed", "status", status, "error", sendErr)
		}
	}
	return nil
}

type protocolSender struct {
	config Config
}

func (s *protocolSender) Send(ctx context.Context, subscription Subscription, payload Payload) (int, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	response, err := pushproto.SendNotificationWithContext(ctx, encoded, &pushproto.Subscription{
		Endpoint: subscription.Endpoint,
		Keys:     pushproto.Keys{P256dh: subscription.P256DH, Auth: subscription.Auth},
	}, &pushproto.Options{
		Subscriber:      s.config.Subject,
		VAPIDPublicKey:  s.config.PublicKey,
		VAPIDPrivateKey: s.config.PrivateKey,
		TTL:             60,
	})
	if response == nil {
		return 0, err
	}
	defer response.Body.Close()
	return response.StatusCode, err
}
