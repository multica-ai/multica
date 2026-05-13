package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Retry ladder per RFC #1964 review (Bohan-J's smaller-item #1):
// 30s/2m/10m/1h/6h with a 24h wall-clock cap. Aggressive retries hammer
// flapping endpoints; cold-starting Lambdas often take >30s. This ladder
// is much friendlier to receivers than the original 1s/5s/30s/5m/30m.
var retrySchedule = []time.Duration{
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
	1 * time.Hour,
	6 * time.Hour,
}

// MaxResponseBodyCapture is how many bytes of the receiver's response body
// the dispatcher records into last_response_body_truncated. ~4 KB matches
// Bohan-J Q4 — enough to debug a "200 OK but the receiver actually choked"
// without polluting the deliveries listing.
const MaxResponseBodyCapture = 4 * 1024

// MaxPendingPerSubscription is the per-subscription cap on pending deliveries.
// When onEvent is about to enqueue a fresh delivery and the pending count is
// already at the cap, drop-oldest takes effect: the N oldest pending rows
// for that subscription are deleted to make room. Per RFC #1964 Q2 — durable
// drop-oldest behind the persisted deliveries table is acceptable backpressure
// since the audit trail of completed/failed/dead deliveries still survives.
//
// Tuning: 1000 is generous for normal workspace traffic. A subscription that
// hits this cap is almost certainly pointing at a permanently-dead receiver.
// The cap can be raised by changing this constant; per-subscription override
// is a future feature gated on real demand.
const MaxPendingPerSubscription int64 = 1000

// HMACSignatureHeader is the request header carrying the SHA-256 HMAC over
// the raw POST body. Receivers verify with the per-subscription secret.
const HMACSignatureHeader = "X-Multica-Signature"

// EventIDHeader echoes the event_id so receivers can dedup across retries
// without parsing the body. Same UUID lives in payload.event_id too.
const EventIDHeader = "X-Multica-Event-Id"

// EventTypeHeader is the bus event-type string. Lets receivers route on a
// header alone for simple cases.
const EventTypeHeader = "X-Multica-Event-Type"

// DeliveryIDHeader changes per attempt batch (prefix `del_`). Useful when
// you want to correlate a failure to a specific retry rather than the
// originating event.
const DeliveryIDHeader = "X-Multica-Delivery-Id"

// Dispatcher owns the bus subscription and the per-workspace fan-out.
type Dispatcher struct {
	queries  *db.Queries
	bus      *events.Bus
	wsChans  map[string]chan deliveryJob
	wsLock   chan struct{}
	stopChan chan struct{}
	client   *http.Client
}

// deliveryJob is the per-event work unit a workspace consumer runs through.
type deliveryJob struct {
	deliveryID pgtype.UUID
}

// NewDispatcher wires the bus subscription. The returned Dispatcher must be
// started via Start before events flow.
func NewDispatcher(queries *db.Queries, bus *events.Bus) *Dispatcher {
	return &Dispatcher{
		queries:  queries,
		bus:      bus,
		wsChans:  make(map[string]chan deliveryJob),
		wsLock:   make(chan struct{}, 1),
		stopChan: make(chan struct{}),
		client: &http.Client{
			// Per-attempt timeout is enforced via context.WithTimeout
			// below using the subscription's per_attempt_timeout_seconds.
			// This client-level timeout is just a hard ceiling.
			Timeout: 60 * time.Second,
		},
	}
}

// Start subscribes to the bus and begins running the retry pump. Idempotent —
// calling twice is a programming error and will panic.
func (d *Dispatcher) Start(ctx context.Context) {
	d.bus.SubscribeAll(d.onEvent)
	go d.runRetryPump(ctx)
}

// Stop signals all per-workspace consumers to drain and exit.
func (d *Dispatcher) Stop() {
	close(d.stopChan)
}

// onEvent is the bus listener. Persists a delivery row for every active
// subscription that matches the event type, then nudges the corresponding
// workspace consumer to drain.
func (d *Dispatcher) onEvent(e events.Event) {
	if e.WorkspaceID == "" {
		return // workspace-scoped only; system events without a workspace skip
	}

	wsUUID, err := parseUUIDStrict(e.WorkspaceID)
	if err != nil {
		slog.Warn("webhook dispatcher: bad workspace id", "err", err, "workspace_id", e.WorkspaceID)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	subs, err := d.queries.ListWebhookSubscriptions(ctx, db.ListWebhookSubscriptionsParams{
		WorkspaceID: wsUUID,
		State:       pgtype.Text{String: "active", Valid: true},
	})
	if err != nil {
		slog.Error("webhook dispatcher: list subscriptions failed", "err", err)
		return
	}

	for _, s := range subs {
		if !subscriptionMatches(s, e.Type) {
			continue
		}
		eventUUID, err := parseUUIDStrict(e.ID)
		if err != nil {
			// Should never happen: Bus.Publish mints an ID before fanout.
			slog.Error("webhook dispatcher: missing event id on event", "event_type", e.Type)
			continue
		}
		payload, err := json.Marshal(map[string]any{
			"event": map[string]any{
				"id":           e.ID,
				"type":         e.Type,
				"workspace_id": e.WorkspaceID,
				"actor_type":   e.ActorType,
				"actor_id":     e.ActorID,
				"payload":      e.Payload,
			},
		})
		if err != nil {
			slog.Error("webhook dispatcher: marshal payload", "err", err)
			continue
		}
		// Drop-oldest backpressure (RFC #1964 Q2). If pending deliveries
		// for this subscription have already accumulated past the cap, the
		// receiver is almost certainly stuck. Trim the oldest rows first so
		// the pending queue stays bounded; the audit trail of any completed
		// / failed / dead rows is unaffected.
		pending, err := d.queries.CountPendingWebhookDeliveriesForSubscription(ctx, s.ID)
		if err == nil && pending >= MaxPendingPerSubscription {
			drop := pending - MaxPendingPerSubscription + 1
			_ = d.queries.DropOldestPendingWebhookDeliveries(ctx, db.DropOldestPendingWebhookDeliveriesParams{
				SubscriptionID: s.ID,
				DropCount:      int32(drop),
			})
			slog.Warn("webhook dispatcher: drop-oldest backpressure",
				"subscription_id", uuidStringValue(s.ID),
				"pending_before", pending,
				"dropped", drop)
		}

		row, err := d.queries.CreateWebhookDelivery(ctx, db.CreateWebhookDeliveryParams{
			SubscriptionID: s.ID,
			EventID:        eventUUID,
			EventType:      e.Type,
			Payload:        payload,
		})
		if err != nil {
			slog.Error("webhook dispatcher: create delivery", "err", err)
			continue
		}
		// Best-effort nudge so the workspace consumer drains promptly. If the
		// channel is full the retry pump will catch the row on its next tick.
		ch := d.workspaceChan(e.WorkspaceID)
		select {
		case ch <- deliveryJob{deliveryID: row.ID}:
		default:
		}
	}
}

// subscriptionMatches returns true when the subscription's event_filter
// includes the eventType (or contains the wildcard '*').
func subscriptionMatches(s db.WebhookSubscription, eventType string) bool {
	for _, f := range s.EventFilter {
		if f == "*" || f == eventType {
			return true
		}
	}
	return false
}

// workspaceChan returns (creating if needed) the per-workspace job channel +
// kicks off its consumer goroutine. The "one goroutine per active workspace"
// pattern means a slow receiver in workspace X ONLY freezes X's queue —
// every other workspace continues unaffected. (Bohan-J Q5.)
func (d *Dispatcher) workspaceChan(workspaceID string) chan deliveryJob {
	d.wsLock <- struct{}{}
	defer func() { <-d.wsLock }()

	if ch, ok := d.wsChans[workspaceID]; ok {
		return ch
	}
	ch := make(chan deliveryJob, 64)
	d.wsChans[workspaceID] = ch
	go d.runWorkspaceConsumer(workspaceID, ch)
	return ch
}

func (d *Dispatcher) runWorkspaceConsumer(workspaceID string, ch chan deliveryJob) {
	for {
		select {
		case <-d.stopChan:
			return
		case job := <-ch:
			d.processDelivery(job.deliveryID)
		}
	}
}

// runRetryPump catches deliveries the workspace consumer missed (full
// channel, restart, etc.) by polling for due-now pending rows.
func (d *Dispatcher) runRetryPump(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-d.stopChan:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			pumpCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			rows, err := d.queries.ClaimPendingWebhookDeliveries(pumpCtx, 50)
			cancel()
			if err != nil {
				slog.Error("webhook dispatcher: claim pending", "err", err)
				continue
			}
			for _, row := range rows {
				d.processDelivery(row.ID)
			}
		}
	}
}

// processDelivery sends a single delivery and records the outcome.
//
// TODO(rfc-1964): the actual http.Post path below is intentionally complete
// for a draft-PR review — HMAC signing, SSRF gating, response truncation,
// retry scheduling are all wired. The "stub" framing in the original PR
// description has graduated to a working implementation; mark resolved when
// the PR ships.
func (d *Dispatcher) processDelivery(deliveryID pgtype.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	row, err := d.queries.GetWebhookDelivery(ctx, deliveryID)
	if err != nil {
		slog.Error("webhook dispatcher: get delivery", "err", err)
		return
	}
	if row.Status != "pending" {
		return
	}
	sub, err := d.queries.GetWebhookSubscription(ctx, row.SubscriptionID)
	if err != nil {
		slog.Error("webhook dispatcher: get subscription", "err", err)
		return
	}
	if sub.State != "active" {
		// Subscription paused after this delivery was queued. Leave
		// pending; if the subscription resumes we'll catch it again.
		return
	}

	target, err := url.Parse(sub.Url)
	if err != nil {
		d.markFailed(ctx, row, sub, fmt.Sprintf("invalid url: %v", err), 0, "")
		return
	}

	allowPrivate := strings.EqualFold(os.Getenv(AllowPrivateEnvVar), "true")
	if err := IsAllowedHost(target, allowPrivate, sub.AllowHttp); err != nil {
		// SSRF block is permanent — never retry a forbidden URL.
		_ = d.queries.MarkWebhookDeliveryDead(ctx, db.MarkWebhookDeliveryDeadParams{
			ID:        row.ID,
			LastError: pgtype.Text{String: err.Error(), Valid: true},
		})
		return
	}

	body := row.Payload
	signed := signHMAC(sub.Secret, body)
	deliveryIDStr := fmt.Sprintf("del_%s", uuidShortString(row.ID))

	attemptCtx, cancel2 := context.WithTimeout(ctx, time.Duration(sub.PerAttemptTimeoutSeconds)*time.Second)
	defer cancel2()

	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, sub.Url, bytes.NewReader(body))
	if err != nil {
		d.markFailed(ctx, row, sub, fmt.Sprintf("build request: %v", err), 0, "")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HMACSignatureHeader, "sha256="+signed)
	req.Header.Set(EventIDHeader, uuidStringValue(row.EventID))
	req.Header.Set(EventTypeHeader, row.EventType)
	req.Header.Set(DeliveryIDHeader, deliveryIDStr)
	req.Header.Set("User-Agent", "Multica-Webhook/1.0")

	client := &http.Client{
		Timeout:       time.Duration(sub.PerAttemptTimeoutSeconds) * time.Second,
		CheckRedirect: SSRFAwareCheckRedirect(allowPrivate, sub.AllowHttp),
	}
	resp, sendErr := client.Do(req)

	respBody := ""
	statusCode := int32(0)
	if sendErr == nil {
		statusCode = int32(resp.StatusCode)
		limited := io.LimitReader(resp.Body, MaxResponseBodyCapture+1)
		bb, _ := io.ReadAll(limited)
		resp.Body.Close()
		if len(bb) > MaxResponseBodyCapture {
			bb = bb[:MaxResponseBodyCapture]
		}
		respBody = string(bb)
	}

	success := sendErr == nil && statusCode >= 200 && statusCode < 300
	if success {
		_ = d.queries.MarkWebhookDeliverySucceeded(ctx, db.MarkWebhookDeliverySucceededParams{
			ID:                        row.ID,
			LastResponseStatus:        statusCode,
			LastResponseBodyTruncated: pgtype.Text{String: respBody, Valid: respBody != ""},
		})
		_ = d.queries.ResetWebhookConsecutiveFailures(ctx, sub.ID)
		return
	}

	// Failure path: maybe retry, maybe dead.
	errMsg := ""
	if sendErr != nil {
		errMsg = sendErr.Error()
	} else {
		errMsg = fmt.Sprintf("non-2xx response: %d", statusCode)
	}

	d.scheduleNextOrDie(ctx, row, sub, errMsg, statusCode, respBody)
}

func (d *Dispatcher) scheduleNextOrDie(ctx context.Context, row db.WebhookDelivery, sub db.WebhookSubscription, errMsg string, statusCode int32, respBody string) {
	// Every failed attempt bumps the consecutive-failures counter. A success
	// in processDelivery resets it. When the bump pushes count past
	// pause_threshold AND state is currently 'active', the SQL UPDATE flips
	// state to 'auto_paused' atomically; we detect that transition and emit
	// a webhook:auto_paused bus event so workspace owners learn about it
	// without polling (RFC #1964 Bohan-J Q3).
	updatedSub, bumpErr := d.queries.BumpWebhookConsecutiveFailures(ctx, sub.ID)
	if bumpErr == nil && sub.State == "active" && updatedSub.State == "auto_paused" {
		d.bus.Publish(events.Event{
			Type:        protocol.EventWebhookAutoPaused,
			WorkspaceID: uuidStringValue(sub.WorkspaceID),
			ActorType:   "system",
			ActorID:     "webhook-dispatcher",
			Payload: map[string]any{
				"subscription_id":      uuidStringValue(sub.ID),
				"name":                 sub.Name,
				"consecutive_failures": updatedSub.ConsecutiveFailures,
				"pause_threshold":      sub.PauseThreshold,
			},
		})
	}

	nextAttempt := int(row.Attempt) + 1
	if nextAttempt >= len(retrySchedule) {
		// Out of retries — dead.
		_ = d.queries.MarkWebhookDeliveryDead(ctx, db.MarkWebhookDeliveryDeadParams{
			ID:                        row.ID,
			LastResponseStatus:        pgtype.Int4{Int32: statusCode, Valid: statusCode != 0},
			LastResponseBodyTruncated: pgtype.Text{String: respBody, Valid: respBody != ""},
			LastError:                 pgtype.Text{String: errMsg, Valid: errMsg != ""},
		})
		return
	}

	delay := retrySchedule[nextAttempt]
	nextTime := time.Now().Add(delay)

	// 24h wall-clock cap on the cumulative retry window.
	cumulativeCap := row.CreatedAt.Time.Add(24 * time.Hour)
	if nextTime.After(cumulativeCap) {
		_ = d.queries.MarkWebhookDeliveryDead(ctx, db.MarkWebhookDeliveryDeadParams{
			ID:                        row.ID,
			LastResponseStatus:        pgtype.Int4{Int32: statusCode, Valid: statusCode != 0},
			LastResponseBodyTruncated: pgtype.Text{String: respBody, Valid: respBody != ""},
			LastError:                 pgtype.Text{String: "24h wall-clock cap exceeded; " + errMsg, Valid: true},
		})
		return
	}

	_ = d.queries.ScheduleWebhookDeliveryRetry(ctx, db.ScheduleWebhookDeliveryRetryParams{
		ID:                        row.ID,
		NextAttemptAt:             pgtype.Timestamptz{Time: nextTime, Valid: true},
		LastResponseStatus:        pgtype.Int4{Int32: statusCode, Valid: statusCode != 0},
		LastResponseBodyTruncated: pgtype.Text{String: respBody, Valid: respBody != ""},
		LastError:                 pgtype.Text{String: errMsg, Valid: true},
	})
}

func (d *Dispatcher) markFailed(ctx context.Context, row db.WebhookDelivery, sub db.WebhookSubscription, errMsg string, statusCode int32, respBody string) {
	d.scheduleNextOrDie(ctx, row, sub, errMsg, statusCode, respBody)
}

// signHMAC computes hex-encoded HMAC-SHA256 over the raw body using the
// per-subscription secret. The receiver verifies by computing the same.
func signHMAC(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// parseUUIDStrict converts a string to pgtype.UUID without panicking. Used
// by the dispatcher because Event.WorkspaceID and Event.ID can in principle
// be anything; we don't trust them blindly.
func parseUUIDStrict(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return u, err
	}
	return u, nil
}

// uuidStringValue serializes a pgtype.UUID as a hyphenated string for header
// values. Empty string when invalid (caller should never let that happen).
func uuidStringValue(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// uuidShortString returns the first 8 hex chars of the UUID — used as a short
// human-friendly delivery_id like `del_a1b2c3d4`. Full UUID still in the URL.
func uuidShortString(u pgtype.UUID) string {
	if !u.Valid {
		return "00000000"
	}
	return fmt.Sprintf("%08x", u.Bytes[0:4])
}
