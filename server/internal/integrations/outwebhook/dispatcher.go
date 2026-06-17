// Package outwebhook delivers outbound webhooks to external HTTP endpoints when
// subscribed Multica events occur. It is the reverse direction of the inbound
// autopilot webhook ingress (server/internal/handler/autopilot_webhook.go):
// there, external systems POST to Multica; here, Multica POSTs to external URLs.
//
// v1 emits a single event type, issue.status_changed, to webhook_subscription
// rows. Delivery is async fire-and-forget: a fixed pool of workers drains a
// bounded queue, each delivery does a couple of immediate retries on network
// error / 5xx, and outcomes are logged (no delivery-history persistence in v1).
package outwebhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/netguard"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/webhooksign"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EventIssueStatusChanged is the only event type emitted in v1. Subscriptions
// opt into it via their `events` JSONB array.
const EventIssueStatusChanged = "issue.status_changed"

// SupportedEventTypes is the single source of truth for the event types this
// dispatcher can actually deliver. The handler allow-list (which events a
// subscription may declare) is derived from it. Adding an entry here requires a
// matching bus subscription in cmd/server/webhook_listeners.go — otherwise
// subscriptions for the new type would be accepted by the API but never fire.
var SupportedEventTypes = []string{EventIssueStatusChanged}

const (
	deliveryTimeout = 30 * time.Second
	maxAttempts     = 3 // 1 initial + 2 retries
	// listTimeout bounds the subscription lookup. It runs on a detached
	// goroutine (not the request path), so a slow query never blocks an issue
	// update — the timeout just stops a stuck query from leaking a goroutine.
	listTimeout = 10 * time.Second
	// numWorkers is the fixed number of delivery goroutines. They are the only
	// goroutines that perform outbound POSTs, so concurrent in-flight deliveries
	// can never exceed this regardless of event volume.
	numWorkers = 16
	// queueCapacity bounds buffered deliveries. Enqueue is non-blocking: when
	// the queue is full a delivery is dropped + logged rather than letting
	// dispatch goroutines pile up waiting for a slot (fire-and-forget v1).
	queueCapacity = 1024
	// numDispatchWorkers bounds the goroutines that run the per-event
	// subscription lookup + filter + marshal. Without this, a status-change
	// storm (e.g. a large batch update) would spawn one goroutine per event,
	// each holding a DB query for up to listTimeout — unbounded query pressure.
	numDispatchWorkers = 4
	// eventQueueCapacity bounds buffered events awaiting dispatch. Enqueue is
	// non-blocking (drop + log on full), so the event bus is never blocked.
	eventQueueCapacity = 1024
)

// defaultRetryBackoff is the wait before attempt N+1 (index 0 = wait before the
// first retry). Short and fixed — fire-and-forget delivery shouldn't hold a
// worker for long. Copied into each Dispatcher so tests can override per
// instance without mutating shared state (which would race the worker pools).
var defaultRetryBackoff = []time.Duration{1 * time.Second, 4 * time.Second}

// Store is the subset of *db.Queries the dispatcher needs. An interface keeps
// the selection/filtering/signing logic testable without a database.
type Store interface {
	ListEnabledWebhookSubscriptionsForDispatch(ctx context.Context, workspaceID pgtype.UUID) ([]db.WebhookSubscription, error)
	CreateOutboundWebhookDelivery(ctx context.Context, arg db.CreateOutboundWebhookDeliveryParams) (db.OutboundWebhookDelivery, error)
}

// recordTimeout bounds the best-effort delivery-history write so a slow DB can
// never wedge a delivery worker.
const recordTimeout = 5 * time.Second

// maxRecordedResponseBody caps the response body kept per delivery record. The
// post() reader already limits what it reads from the wire; this is the storage
// cap (matches that 4 KiB limit).
const maxRecordedResponseBody = 4096

// deliverJob is one queued delivery: a subscription + the marshaled body shared
// (read-only) across retries. event identifies the payload's event type.
// redeliveredFromID is Valid only when this job is a manual redelivery of an
// earlier recorded delivery; deliver() persists it as the lineage link.
type deliverJob struct {
	sub               db.WebhookSubscription
	event             string
	body              []byte
	redeliveredFromID pgtype.UUID
}

// Dispatcher fans an event out to matching subscriptions via bounded worker
// pools — one stage for the per-event subscription lookup, one for delivery.
type Dispatcher struct {
	store        Store
	client       *http.Client
	events       chan IssueStatusChanged
	jobs         chan deliverJob
	retryBackoff []time.Duration

	// Lifecycle. stopDispatch is closed first (stop accepting events + drain the
	// dispatch stage); stopDeliver is closed after the dispatch stage has fully
	// drained into jobs, so no delivery is lost mid-shutdown. The channels are
	// never closed, so concurrent sends in DispatchIssueStatusChanged / dispatch
	// can never panic.
	stopDispatch chan struct{}
	stopDeliver  chan struct{}
	dispatchWG   sync.WaitGroup
	deliverWG    sync.WaitGroup
	closeOnce    sync.Once
	closeDone    chan struct{}
}

// New builds a Dispatcher and starts its worker pools. The HTTP client is
// SSRF-restricted (rejects internal addresses at dial time, on every redirect
// hop) and carries a fixed per-request timeout; retries are handled per attempt.
func New(store Store) *Dispatcher {
	return newWithClient(store, netguard.NewRestrictedHTTPClient(deliveryTimeout))
}

// newWithClient is the shared constructor. Tests use it to inject a permissive
// client so they can exercise delivery against a loopback httptest server (the
// SSRF guard itself is covered by the netguard package tests).
func newWithClient(store Store, client *http.Client) *Dispatcher {
	d := &Dispatcher{
		store:        store,
		client:       client,
		events:       make(chan IssueStatusChanged, eventQueueCapacity),
		jobs:         make(chan deliverJob, queueCapacity),
		retryBackoff: defaultRetryBackoff,
		stopDispatch: make(chan struct{}),
		stopDeliver:  make(chan struct{}),
	}
	d.dispatchWG.Add(numDispatchWorkers)
	for i := 0; i < numDispatchWorkers; i++ {
		go d.dispatchWorker()
	}
	d.deliverWG.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go d.worker()
	}
	return d
}

// Close stops accepting new events and drains in-flight work: dispatch workers
// finish enqueuing their deliveries, then delivery workers drain the queue.
// Blocks until drained or ctx expires. Safe to call more than once.
func (d *Dispatcher) Close(ctx context.Context) error {
	d.closeOnce.Do(func() {
		d.closeDone = make(chan struct{})
		close(d.stopDispatch)
		go func() {
			d.dispatchWG.Wait() // dispatch stage drained → all jobs enqueued
			close(d.stopDeliver)
			d.deliverWG.Wait() // delivery stage drained
			close(d.closeDone)
		}()
	})
	select {
	case <-d.closeDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// dispatchWorker drains the event queue, running the subscription lookup +
// filter + marshal for each event. Bounded to numDispatchWorkers so concurrent
// DB queries can't grow with event volume. On Close it drains buffered events
// then exits.
func (d *Dispatcher) dispatchWorker() {
	defer d.dispatchWG.Done()
	for {
		select {
		case ev := <-d.events:
			d.dispatch(ev)
		case <-d.stopDispatch:
			for {
				select {
				case ev := <-d.events:
					d.dispatch(ev)
				default:
					return
				}
			}
		}
	}
}

// worker drains the delivery queue. A delivery panic is recovered so a single
// bad delivery can never kill a long-lived worker. On Close it drains buffered
// jobs then exits.
func (d *Dispatcher) worker() {
	defer d.deliverWG.Done()
	for {
		select {
		case job := <-d.jobs:
			d.safeDeliver(job)
		case <-d.stopDeliver:
			for {
				select {
				case job := <-d.jobs:
					d.safeDeliver(job)
				default:
					return
				}
			}
		}
	}
}

func (d *Dispatcher) safeDeliver(job deliverJob) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("outwebhook: delivery panic recovered",
				"subscription_id", util.UUIDToString(job.sub.ID), "recovered", r)
		}
	}()
	d.deliver(job)
}

// IssueStatusChanged describes a single issue status transition. The listener
// builds it from the issue:updated event payload. Issue is the JSON-serializable
// issue representation (handler.IssueResponse) embedded verbatim in the outbound
// body — the dispatcher does not depend on the handler package.
type IssueStatusChanged struct {
	WorkspaceID    string
	ProjectID      string // "" when the issue has no project
	ActorType      string
	ActorID        string
	PreviousStatus string
	Issue          any
}

// actorPayload mirrors the {type,id} shape used elsewhere for polymorphic actors.
type actorPayload struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// outboundPayload is the versioned JSON body POSTed to subscribers.
type outboundPayload struct {
	Event          string       `json:"event"`
	WorkspaceID    string       `json:"workspace_id"`
	Actor          actorPayload `json:"actor"`
	Issue          any          `json:"issue"`
	PreviousStatus string       `json:"previous_status"`
	DeliveredAt    string       `json:"delivered_at"`
}

// DispatchIssueStatusChanged hands the event to the bounded dispatch queue and
// returns immediately. The event bus invokes listeners synchronously on the
// issue-update HTTP request path, so NO work here may touch that path — not the
// subscription DB query, not JSON marshaling, not delivery. Enqueue is
// non-blocking: a full queue drops the event (fire-and-forget v1) rather than
// blocking the bus.
func (d *Dispatcher) DispatchIssueStatusChanged(ev IssueStatusChanged) {
	// Stop accepting once shutdown has begun (never send on a closed channel —
	// the queues are never closed, so this gate is the only stop signal).
	select {
	case <-d.stopDispatch:
		return
	default:
	}
	select {
	case d.events <- ev:
	case <-d.stopDispatch:
	default:
		slog.Warn("outwebhook: event queue full, dropping", "workspace_id", ev.WorkspaceID)
	}
}

// dispatch (off the request path, on a bounded dispatch worker) selects matching
// subscriptions and enqueues their deliveries.
func (d *Dispatcher) dispatch(ev IssueStatusChanged) {
	wsUUID, err := util.ParseUUID(ev.WorkspaceID)
	if err != nil {
		slog.Warn("outwebhook: invalid workspace id", "workspace_id", ev.WorkspaceID, "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), listTimeout)
	subs, err := d.store.ListEnabledWebhookSubscriptionsForDispatch(ctx, wsUUID)
	cancel()
	if err != nil {
		slog.Error("outwebhook: failed to list subscriptions", "workspace_id", ev.WorkspaceID, "error", err)
		return
	}

	matched := make([]db.WebhookSubscription, 0, len(subs))
	for _, s := range subs {
		if subscriptionMatches(s, ev.ProjectID) && subscribedToEvent(s, EventIssueStatusChanged) {
			matched = append(matched, s)
		}
	}
	if len(matched) == 0 {
		return
	}

	body, err := json.Marshal(outboundPayload{
		Event:          EventIssueStatusChanged,
		WorkspaceID:    ev.WorkspaceID,
		Actor:          actorPayload{Type: ev.ActorType, ID: ev.ActorID},
		Issue:          ev.Issue,
		PreviousStatus: ev.PreviousStatus,
		DeliveredAt:    time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		slog.Error("outwebhook: failed to marshal payload", "workspace_id", ev.WorkspaceID, "error", err)
		return
	}

	for _, s := range matched {
		// Non-blocking enqueue: a full queue drops the delivery (fire-and-forget
		// v1) rather than letting this dispatch goroutine block, which would let
		// dispatch goroutines accumulate under a status-change storm.
		select {
		case d.jobs <- deliverJob{sub: s, event: EventIssueStatusChanged, body: body}:
		default:
			slog.Warn("outwebhook: delivery queue full, dropping",
				"subscription_id", util.UUIDToString(s.ID), "host", hostOf(s.Url))
		}
	}
}

// Redeliver enqueues a manual redelivery of a previously recorded delivery: it
// re-POSTs the stored body to the subscription's CURRENT url/secret (a fresh
// signature is computed in deliver). Non-blocking, like dispatch — the new
// delivery is recorded with redeliveredFromID linking it to the original.
// Returns false if the queue is full (the caller surfaces that to the operator).
func (d *Dispatcher) Redeliver(sub db.WebhookSubscription, event string, body []byte, fromID pgtype.UUID) bool {
	select {
	case <-d.stopDispatch:
		return false
	default:
	}
	select {
	case d.jobs <- deliverJob{sub: sub, event: event, body: body, redeliveredFromID: fromID}:
		return true
	default:
		slog.Warn("outwebhook: delivery queue full, dropping redelivery",
			"subscription_id", util.UUIDToString(sub.ID), "host", hostOf(sub.Url))
		return false
	}
}

// deliver POSTs body to one subscription, retrying on network error / 5xx, then
// records a single delivery-history row for the terminal outcome.
func (d *Dispatcher) deliver(job deliverJob) {
	sub := job.sub
	deliveryID := uuid.NewString()
	signature := webhooksign.Sign(sub.Secret, job.body)
	subID := util.UUIDToString(sub.ID)
	// Log host only — subscriber URLs frequently carry tokens in path/query.
	host := hostOf(sub.Url)

	var (
		lastStatus int
		lastErr    error
		lastBody   []byte
		attempts   int
	)
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(d.retryBackoff[attempt-1])
		}
		attempts = attempt + 1

		status, respBody, err := d.post(sub.Url, job.event, deliveryID, signature, job.body)
		lastStatus, lastErr, lastBody = status, err, respBody
		if err == nil && status >= 200 && status < 300 {
			slog.Debug("outwebhook: delivered",
				"subscription_id", subID, "event", job.event, "status", status, "attempt", attempts)
			d.record(job, "delivered", attempts, status, lastBody, nil)
			return
		}

		// 429 is "retry later", not a permanent rejection.
		retryable := err != nil || status == http.StatusTooManyRequests || status >= 500
		slog.Warn("outwebhook: delivery attempt failed",
			"subscription_id", subID, "event", job.event, "host", host,
			"status", status, "attempt", attempts, "retryable", retryable, "error", redactErr(err))
		if !retryable {
			d.record(job, "failed", attempts, status, lastBody, err) // 4xx — endpoint rejected the payload.
			return
		}
	}
	slog.Error("outwebhook: delivery exhausted retries",
		"subscription_id", subID, "event", job.event, "host", host)
	d.record(job, "failed", attempts, lastStatus, lastBody, lastErr)
}

// record writes one delivery-history row for a terminal outcome. Best-effort: a
// failure to record is logged, never propagated (the delivery itself already
// happened or failed; losing the audit row must not crash a worker). status is
// "delivered" or "failed"; httpStatus is 0 when no response was received.
func (d *Dispatcher) record(job deliverJob, status string, attempts, httpStatus int, respBody []byte, deliverErr error) {
	params := db.CreateOutboundWebhookDeliveryParams{
		WorkspaceID:       job.sub.WorkspaceID,
		SubscriptionID:    job.sub.ID,
		Event:             job.event,
		Status:            status,
		AttemptCount:      int32(attempts),
		RequestBody:       job.body,
		RedeliveredFromID: job.redeliveredFromID,
	}
	if httpStatus > 0 {
		params.ResponseStatus = pgtype.Int4{Int32: int32(httpStatus), Valid: true}
	}
	if len(respBody) > 0 {
		// response_body is a TEXT column but respBody holds the subscriber's raw
		// response (which may be binary / non-UTF-8 / cut mid-rune by the read
		// cap). Coerce to valid UTF-8 so the INSERT can't fail with "invalid byte
		// sequence for encoding UTF8" and silently drop the whole history row.
		// (post() already caps the length via io.LimitReader.)
		params.ResponseBody = pgtype.Text{String: strings.ToValidUTF8(string(respBody), "�"), Valid: true}
	}
	if deliverErr != nil {
		// redactErr strips the URL (which carries subscriber tokens) from the
		// transport error before it is persisted.
		params.Error = pgtype.Text{String: redactErr(deliverErr), Valid: true}
	}

	ctx, cancel := context.WithTimeout(context.Background(), recordTimeout)
	defer cancel()
	if _, err := d.store.CreateOutboundWebhookDelivery(ctx, params); err != nil {
		slog.Error("outwebhook: failed to record delivery",
			"subscription_id", util.UUIDToString(job.sub.ID), "error", err)
	}
}

// redactErr renders a transport error for logging without leaking the request
// URL. http.Client.Do returns a *url.Error whose Error() embeds the full URL
// (path + query + userinfo), which for webhook subscribers routinely carries
// tokens — so we log only the underlying cause + operation, never the URL.
func redactErr(err error) string {
	if err == nil {
		return ""
	}
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Op + ": " + ue.Err.Error()
	}
	return err.Error()
}

// hostOf returns the host of a webhook URL for logging, omitting any
// path/query that may carry secrets. Returns "invalid-url" if unparseable.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "invalid-url"
	}
	return u.Host
}

// post performs a single delivery attempt and returns the HTTP status code plus
// a truncated copy of the response body (read up to maxRecordedResponseBody so
// the connection can be reused and the body can be recorded for debugging).
func (d *Dispatcher) post(url, event, deliveryID, signature string, body []byte) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), deliveryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Multica-Webhook/1.0")
	req.Header.Set("X-Multica-Event", event)
	req.Header.Set("X-Multica-Delivery", deliveryID)
	req.Header.Set("X-Multica-Signature-256", signature)

	resp, err := d.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	// Read a bounded prefix so the connection can be reused and the body can be
	// recorded in the delivery history.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxRecordedResponseBody))
	return resp.StatusCode, respBody, nil
}

// subscriptionMatches reports whether a subscription applies to an issue with
// the given project. Workspace-level subscriptions (no project_id) match every
// issue; project-level subscriptions match only their own project.
func subscriptionMatches(sub db.WebhookSubscription, issueProjectID string) bool {
	if !sub.ProjectID.Valid {
		return true // workspace-level
	}
	return util.UUIDToString(sub.ProjectID) == issueProjectID
}

// subscribedToEvent reports whether the subscription's events array contains
// event. A malformed events column is treated as "no events" (skip), never a
// panic.
func subscribedToEvent(sub db.WebhookSubscription, event string) bool {
	var events []string
	if err := json.Unmarshal(sub.Events, &events); err != nil {
		slog.Warn("outwebhook: subscription has malformed events column",
			"subscription_id", util.UUIDToString(sub.ID), "error", err)
		return false
	}
	for _, e := range events {
		if e == event {
			return true
		}
	}
	return false
}
