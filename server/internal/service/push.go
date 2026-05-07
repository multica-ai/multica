package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"sync"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// PushService delivers Web Push notifications to subscribed devices.
//
// VAPID keys are read once from VAPID_PUBLIC_KEY / VAPID_PRIVATE_KEY env vars.
// Subject (mailto: or https URL identifying the app) comes from VAPID_SUBJECT,
// defaulting to mailto:noreply@multica.ai. If the keys aren't set, the service
// is disabled — Send is a no-op and the public key endpoint reports "not
// configured" so the frontend hides the subscribe UI.
type PushService struct {
	queries    *db.Queries
	publicKey  string
	privateKey string
	subject    string
	enabled    bool

	// Avoid hammering the same dead endpoint repeatedly when an issue gets a
	// burst of activity — once we've deleted a 404/410 endpoint we drop it
	// from the in-memory set after the request batch.
	deadOnce sync.Map // endpoint -> struct{}
}

func NewPushService(queries *db.Queries) *PushService {
	pub := os.Getenv("VAPID_PUBLIC_KEY")
	priv := os.Getenv("VAPID_PRIVATE_KEY")
	subj := os.Getenv("VAPID_SUBJECT")
	if subj == "" {
		subj = "mailto:noreply@multica.ai"
	}

	enabled := pub != "" && priv != ""
	if !enabled {
		slog.Info("push notifications disabled (VAPID keys not set)")
	}

	return &PushService{
		queries:    queries,
		publicKey:  pub,
		privateKey: priv,
		subject:    subj,
		enabled:    enabled,
	}
}

// PublicKey returns the VAPID application server key the browser needs when
// calling pushManager.subscribe. Empty string means push is disabled.
func (s *PushService) PublicKey() string {
	if !s.enabled {
		return ""
	}
	return s.publicKey
}

// Enabled reports whether the service can actually deliver pushes.
func (s *PushService) Enabled() bool {
	return s.enabled
}

// Payload is the JSON body shipped to the service worker. Keep field names
// stable — the SW reads them.
//
// UnreadCount is stamped by SendToUser before serialisation: it is the
// recipient's total unread inbox items across every workspace, used by the
// service worker to drive the PWA app badge (`navigator.setAppBadge`). The
// sender on a notify-* path leaves it zero; SendToUser overwrites it.
//
// Badge / Silent reflect the recipient's per-channel transport preferences
// for the mobile channel (preferences.notifications.channels.mobile.{badge,
// sound}). The caller fills them; SendToUser uses Badge to decide whether to
// stamp UnreadCount, and the SW reads Silent to skip sound on display.
type Payload struct {
	Title       string `json:"title"`
	Body        string `json:"body,omitempty"`
	URL         string `json:"url,omitempty"`
	Tag         string `json:"tag,omitempty"`
	IssueID     string `json:"issueId,omitempty"`
	Type        string `json:"type,omitempty"`
	UnreadCount int64  `json:"unreadCount"`
	Silent      bool   `json:"silent,omitempty"`
	// Badge is the caller's intent — true means stamp UnreadCount, false
	// means leave it at zero so the OS clears the icon badge. Not
	// serialised; it controls the count read.
	Badge bool `json:"-"`
}

// SendToUser fans the payload out to every device the user has subscribed.
// Errors per-device are logged and do not abort the broadcast. Endpoints that
// return 404 or 410 are pruned.
func (s *PushService) SendToUser(ctx context.Context, userID string, p Payload) {
	if !s.enabled {
		return
	}

	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		slog.Warn("push: invalid user id", "user_id", userID, "error", err)
		return
	}
	subs, err := s.queries.ListPushSubscriptionsByUser(ctx, userUUID)
	if err != nil {
		slog.Error("push: list subscriptions failed", "user_id", userID, "error", err)
		return
	}
	if len(subs) == 0 {
		return
	}

	// Stamp the OS app-badge counter on the payload. Counted across all of
	// the user's workspaces because the badge is single-icon and OS-level.
	// On error we leave the count at zero — the SW will simply clear the
	// badge, which is preferable to crashing the broadcast.
	//
	// When the recipient has opted out of the badge (Badge=false), keep
	// the count at zero so the SW clears the icon. The next push after
	// the user re-enables badge will reflect the true count.
	if p.Badge {
		count, err := s.queries.CountUnreadInboxForUserAllWorkspaces(ctx, userUUID)
		if err != nil {
			slog.Warn("push: unread count failed", "user_id", userID, "error", err)
		} else {
			p.UnreadCount = count
		}
	}

	body, err := json.Marshal(p)
	if err != nil {
		slog.Error("push: marshal payload failed", "error", err)
		return
	}

	for _, sub := range subs {
		s.sendOne(ctx, sub, body)
	}
}

func (s *PushService) sendOne(ctx context.Context, sub db.PushSubscription, body []byte) {
	wpSub := &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.P256dh,
			Auth:   sub.Auth,
		},
	}

	resp, err := webpush.SendNotificationWithContext(ctx, body, wpSub, &webpush.Options{
		Subscriber:      s.subject,
		VAPIDPublicKey:  s.publicKey,
		VAPIDPrivateKey: s.privateKey,
		TTL:             60 * 60 * 24, // 24h — drop if device is offline that long
		Urgency:         webpush.UrgencyNormal,
	})
	if err != nil {
		slog.Warn("push: send failed", "endpoint", endpointShort(sub.Endpoint), "error", err)
		return
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted:
		// success
		return
	case http.StatusNotFound, http.StatusGone:
		s.pruneDead(ctx, sub.Endpoint)
	default:
		slog.Warn("push: non-success", "status", resp.StatusCode, "endpoint", endpointShort(sub.Endpoint))
	}
}

func (s *PushService) pruneDead(ctx context.Context, endpoint string) {
	if _, loaded := s.deadOnce.LoadOrStore(endpoint, struct{}{}); loaded {
		return
	}
	if _, err := s.queries.DeletePushSubscriptionByEndpointAny(ctx, endpoint); err != nil {
		slog.Error("push: prune dead subscription failed", "error", err)
		return
	}
	slog.Info("push: pruned dead subscription", "endpoint", endpointShort(endpoint))
}

// GenerateVAPIDKeys is a one-shot helper for ops to mint VAPID keys without
// pulling in the webpush package elsewhere.
func GenerateVAPIDKeys() (privateKey, publicKey string, err error) {
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", "", err
	}
	if priv == "" || pub == "" {
		return "", "", errors.New("webpush returned empty key")
	}
	return priv, pub, nil
}

// endpointShort masks all but the host part of a push endpoint for logging.
// FCM/Mozilla URLs include opaque identifiers we don't want in logs.
func endpointShort(ep string) string {
	if len(ep) < 32 {
		return ep
	}
	return ep[:32] + "…"
}
