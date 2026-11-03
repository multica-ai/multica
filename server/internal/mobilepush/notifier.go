// Package mobilepush delivers committed Inbox items through Expo to APNs.
package mobilepush

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var tokenPattern = regexp.MustCompile(`^(ExpoPushToken|ExponentPushToken)\[[A-Za-z0-9_-]{1,200}\]$`)

func ValidToken(token string) bool { return tokenPattern.MatchString(token) }

type Store interface {
	ListMobilePushTargets(context.Context, pgtype.UUID) ([]db.ListMobilePushTargetsRow, error)
	CreateMobilePushReceipt(context.Context, db.CreateMobilePushReceiptParams) error
	ListMobilePushReceipts(context.Context) ([]db.MobilePushReceipt, error)
	DeleteMobilePushReceipt(context.Context, string) error
	InvalidateMobilePushSubscription(context.Context, db.InvalidateMobilePushSubscriptionParams) error
	PruneMobilePushSubscriptions(context.Context) error
}

type Notifier struct {
	store       Store
	client      *http.Client
	endpoint    string
	accessToken string
	queue       chan pgtype.UUID
}

func New(store Store, accessToken string) *Notifier {
	return &Notifier{store: store, client: &http.Client{Timeout: 10 * time.Second}, endpoint: "https://exp.host/--/api/v2/push", accessToken: accessToken, queue: make(chan pgtype.UUID, 256)}
}

// Register never does network or database work on the synchronous event bus.
// The queue is bounded: on overflow Inbox remains the durable source of truth.
func (n *Notifier) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		item, ok := e.Payload.(map[string]any)
		if !ok || item["recipient_type"] != "member" {
			return
		}
		id, ok := item["id"].(string)
		if !ok {
			return
		}
		uuid, err := util.ParseUUID(id)
		if err != nil {
			return
		}
		select {
		case n.queue <- uuid:
		default:
			slog.Warn("mobile push queue full")
		}
	})
}

func (n *Notifier) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-n.queue:
			n.deliver(ctx, id)
		case <-ticker.C:
			n.receipts(ctx)
		}
	}
}

type result struct {
	Status  string `json:"status"`
	ID      string `json:"id"`
	Details struct {
		Error string `json:"error"`
	} `json:"details"`
}

func (n *Notifier) post(ctx context.Context, path string, input, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if n.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+n.accessToken)
	}
	res, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("push transport unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("push HTTP status %d", res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(output); err != nil {
		return fmt.Errorf("invalid push response")
	}
	return nil
}

func (n *Notifier) deliver(ctx context.Context, id pgtype.UUID) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	targets, err := n.store.ListMobilePushTargets(ctx, id)
	if err != nil {
		slog.Warn("mobile push target lookup failed")
		return
	}
	for _, target := range targets {
		// No issue titles, comment bodies, account identifiers or arbitrary URLs
		// leave Multica. Opening the inbox still requires an authenticated session.
		message := map[string]any{"to": target.Token, "title": "Multica", "body": "You have new inbox activity.", "sound": "default", "data": map[string]string{"workspace": target.Slug}}
		var response struct {
			Data []result `json:"data"`
		}
		if err := n.post(ctx, "/send", []any{message}, &response); err != nil {
			slog.Warn("mobile push send failed", "error", err)
			continue
		}
		if len(response.Data) != 1 {
			slog.Warn("mobile push invalid ticket response")
			continue
		}
		ticket := response.Data[0]
		if ticket.Status == "error" {
			if ticket.Details.Error == "DeviceNotRegistered" {
				n.invalidate(ctx, target.Token, target.UpdatedAt)
			}
			slog.Warn("mobile push ticket rejected")
		} else if ticket.Status == "ok" && ticket.ID != "" {
			if err := n.store.CreateMobilePushReceipt(ctx, db.CreateMobilePushReceiptParams{ID: ticket.ID, Token: target.Token, SubscriptionUpdatedAt: target.UpdatedAt}); err != nil {
				slog.Warn("mobile push receipt persistence failed")
			}
		} else {
			slog.Warn("mobile push invalid ticket")
		}
	}
}

func (n *Notifier) invalidate(ctx context.Context, token string, updated pgtype.Timestamptz) {
	if err := n.store.InvalidateMobilePushSubscription(ctx, db.InvalidateMobilePushSubscriptionParams{Token: token, UpdatedAt: updated}); err != nil {
		slog.Warn("mobile push token cleanup failed")
	}
}

func (n *Notifier) receipts(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := n.store.PruneMobilePushSubscriptions(ctx); err != nil {
		slog.Warn("mobile push expiry cleanup failed")
	}
	pending, err := n.store.ListMobilePushReceipts(ctx)
	if err != nil {
		slog.Warn("mobile push receipt lookup failed")
		return
	}
	if len(pending) == 0 {
		return
	}
	ids := make([]string, 0, len(pending))
	for _, receipt := range pending {
		ids = append(ids, receipt.ID)
	}
	var response struct {
		Data map[string]result `json:"data"`
	}
	if err := n.post(ctx, "/getReceipts", map[string]any{"ids": ids}, &response); err != nil {
		slog.Warn("mobile push receipt request failed", "error", err)
		return
	}
	for _, receipt := range pending {
		result, found := response.Data[receipt.ID]
		if !found && time.Since(receipt.CreatedAt.Time) < 24*time.Hour {
			continue
		}
		if found && result.Status != "ok" && result.Status != "error" {
			continue
		}
		if result.Details.Error == "DeviceNotRegistered" {
			n.invalidate(ctx, receipt.Token, receipt.SubscriptionUpdatedAt)
		}
		if found && result.Status == "error" {
			slog.Warn("mobile push receipt rejected")
		}
		if err := n.store.DeleteMobilePushReceipt(ctx, receipt.ID); err != nil {
			slog.Warn("mobile push receipt cleanup failed")
		}
	}
}
