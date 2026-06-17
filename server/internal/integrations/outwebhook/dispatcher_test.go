package outwebhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/internal/webhooksign"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeStore returns a fixed subscription list, ignoring the workspace filter
// (the dispatcher does the workspace-level vs project-level filtering itself).
// It also captures recorded delivery rows so tests can assert the dispatcher
// persists the terminal outcome.
type fakeStore struct {
	subs []db.WebhookSubscription
	err  error

	mu        sync.Mutex
	delivered []db.CreateOutboundWebhookDeliveryParams
}

func (f *fakeStore) ListEnabledWebhookSubscriptionsForDispatch(_ context.Context, _ pgtype.UUID) ([]db.WebhookSubscription, error) {
	return f.subs, f.err
}

func (f *fakeStore) CreateOutboundWebhookDelivery(_ context.Context, arg db.CreateOutboundWebhookDeliveryParams) (db.OutboundWebhookDelivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delivered = append(f.delivered, arg)
	return db.OutboundWebhookDelivery{}, nil
}

// records returns a copy of the captured delivery rows.
func (f *fakeStore) records() []db.CreateOutboundWebhookDeliveryParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]db.CreateOutboundWebhookDeliveryParams(nil), f.delivered...)
}

// newTestDispatcher builds a dispatcher with a fast retry backoff and registers
// Close on cleanup so worker goroutines never leak across tests.
func newTestDispatcher(t *testing.T, store Store, client *http.Client) *Dispatcher {
	t.Helper()
	d := newWithClient(store, client)
	d.retryBackoff = []time.Duration{5 * time.Millisecond, 5 * time.Millisecond}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = d.Close(ctx)
	})
	return d
}

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}

const (
	wsID   = "11111111-1111-1111-1111-111111111111"
	projA  = "22222222-2222-2222-2222-222222222222"
	projB  = "33333333-3333-3333-3333-333333333333"
	subID1 = "44444444-4444-4444-4444-444444444444"
)

func sub(t *testing.T, id, project string, events []string, url string) db.WebhookSubscription {
	t.Helper()
	ev, _ := json.Marshal(events)
	s := db.WebhookSubscription{
		ID:          mustUUID(t, id),
		WorkspaceID: mustUUID(t, wsID),
		Url:         url,
		Secret:      "whsec_test",
		Events:      ev,
		Enabled:     true,
	}
	if project != "" {
		s.ProjectID = mustUUID(t, project)
	}
	return s
}

func TestSubscriptionMatches(t *testing.T) {
	wsLevel := sub(t, subID1, "", []string{EventIssueStatusChanged}, "http://x")
	projLevel := sub(t, subID1, projA, []string{EventIssueStatusChanged}, "http://x")

	if !subscriptionMatches(wsLevel, projA) {
		t.Error("workspace-level subscription should match any project")
	}
	if !subscriptionMatches(wsLevel, "") {
		t.Error("workspace-level subscription should match issues with no project")
	}
	if !subscriptionMatches(projLevel, projA) {
		t.Error("project-level subscription should match its own project")
	}
	if subscriptionMatches(projLevel, projB) {
		t.Error("project-level subscription must NOT match a different project")
	}
	if subscriptionMatches(projLevel, "") {
		t.Error("project-level subscription must NOT match issues with no project")
	}
}

func TestSubscribedToEvent(t *testing.T) {
	s := sub(t, subID1, "", []string{"issue.status_changed"}, "http://x")
	if !subscribedToEvent(s, EventIssueStatusChanged) {
		t.Error("expected subscription to match its declared event")
	}
	if subscribedToEvent(s, "issue.created") {
		t.Error("expected subscription not to match an undeclared event")
	}

	// Malformed events column is treated as "no events", never a panic.
	bad := s
	bad.Events = []byte("{not json")
	if subscribedToEvent(bad, EventIssueStatusChanged) {
		t.Error("malformed events column should match nothing")
	}
}

// collector is a test webhook receiver that records request bodies + headers.
type collector struct {
	mu       sync.Mutex
	bodies   [][]byte
	sigs     []string
	events   []string
	wg       *sync.WaitGroup
	failNext atomic.Int32 // respond 500 this many times before succeeding
}

func (c *collector) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	c.bodies = append(c.bodies, body)
	c.sigs = append(c.sigs, r.Header.Get("X-Multica-Signature-256"))
	c.events = append(c.events, r.Header.Get("X-Multica-Event"))
	c.mu.Unlock()
	if c.failNext.Load() > 0 {
		c.failNext.Add(-1)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	if c.wg != nil {
		c.wg.Done()
	}
}

func TestDispatchDeliversToMatchingSubscriptions(t *testing.T) {
	c := &collector{wg: &sync.WaitGroup{}}
	srv := httptest.NewServer(http.HandlerFunc(c.handler))
	defer srv.Close()

	// Two subs: a workspace-level one and a project-A one. An issue in project A
	// should hit both. A project-B sub should NOT fire.
	store := &fakeStore{subs: []db.WebhookSubscription{
		sub(t, subID1, "", []string{EventIssueStatusChanged}, srv.URL),
		sub(t, projA, projA, []string{EventIssueStatusChanged}, srv.URL),
		sub(t, projB, projB, []string{EventIssueStatusChanged}, srv.URL),
	}}
	d := newTestDispatcher(t, store, &http.Client{Timeout: deliveryTimeout})

	c.wg.Add(2) // expect exactly 2 successful deliveries
	d.DispatchIssueStatusChanged(IssueStatusChanged{
		WorkspaceID:    wsID,
		ProjectID:      projA,
		ActorType:      "member",
		ActorID:        "actor-1",
		PreviousStatus: "in_progress",
		Issue:          map[string]any{"id": "issue-1", "status": "done"},
	})

	waitTimeout(t, c.wg, 5*time.Second)

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.bodies) != 2 {
		t.Fatalf("expected 2 deliveries, got %d", len(c.bodies))
	}

	// Verify signature + payload shape on the first delivery.
	var payload struct {
		Event          string `json:"event"`
		WorkspaceID    string `json:"workspace_id"`
		PreviousStatus string `json:"previous_status"`
		Actor          struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"actor"`
	}
	if err := json.Unmarshal(c.bodies[0], &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Event != EventIssueStatusChanged {
		t.Errorf("event = %q, want %q", payload.Event, EventIssueStatusChanged)
	}
	if payload.WorkspaceID != wsID {
		t.Errorf("workspace_id = %q, want %q", payload.WorkspaceID, wsID)
	}
	if payload.PreviousStatus != "in_progress" {
		t.Errorf("previous_status = %q, want in_progress", payload.PreviousStatus)
	}
	if payload.Actor.Type != "member" || payload.Actor.ID != "actor-1" {
		t.Errorf("actor = %+v, want member/actor-1", payload.Actor)
	}
	if c.events[0] != EventIssueStatusChanged {
		t.Errorf("X-Multica-Event = %q", c.events[0])
	}
	if !webhooksign.Verify("whsec_test", c.sigs[0], c.bodies[0]) {
		t.Errorf("signature did not verify: %q", c.sigs[0])
	}
}

func TestDispatchSkipsUnsubscribedEvent(t *testing.T) {
	c := &collector{}
	srv := httptest.NewServer(http.HandlerFunc(c.handler))
	defer srv.Close()

	store := &fakeStore{subs: []db.WebhookSubscription{
		sub(t, subID1, "", []string{"issue.created"}, srv.URL),
	}}
	d := newTestDispatcher(t, store, &http.Client{Timeout: deliveryTimeout})
	d.DispatchIssueStatusChanged(IssueStatusChanged{
		WorkspaceID: wsID,
		Issue:       map[string]any{"id": "issue-1"},
	})

	// No goroutine should have been spawned; give any stray one a moment.
	time.Sleep(200 * time.Millisecond)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.bodies) != 0 {
		t.Fatalf("expected 0 deliveries for unsubscribed event, got %d", len(c.bodies))
	}
}

// TestProductionClientBlocksInternalDelivery proves the default constructor
// wires the SSRF-restricted client: a delivery to a loopback endpoint (what
// httptest binds) must be refused at dial time, so the catcher records nothing.
func TestProductionClientBlocksInternalDelivery(t *testing.T) {
	c := &collector{}
	srv := httptest.NewServer(http.HandlerFunc(c.handler))
	defer srv.Close()

	store := &fakeStore{subs: []db.WebhookSubscription{
		sub(t, subID1, "", []string{EventIssueStatusChanged}, srv.URL),
	}}
	// Use the production constructor to prove New() wires the SSRF-restricted
	// client. Short backoff + Close-on-cleanup keep it fast and leak-free.
	d := New(store)
	d.retryBackoff = []time.Duration{5 * time.Millisecond, 5 * time.Millisecond}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = d.Close(ctx)
	})
	d.DispatchIssueStatusChanged(IssueStatusChanged{
		WorkspaceID: wsID,
		Issue:       map[string]any{"id": "issue-1"},
	})

	// Allow the (doomed) attempts to run; they fail fast at dial.
	time.Sleep(300 * time.Millisecond)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.bodies) != 0 {
		t.Fatalf("SSRF guard should have blocked loopback delivery, got %d deliveries", len(c.bodies))
	}
}

func TestDeliverRetriesOn5xx(t *testing.T) {
	c := &collector{wg: &sync.WaitGroup{}}
	c.failNext.Store(2) // fail twice, succeed on the 3rd attempt
	srv := httptest.NewServer(http.HandlerFunc(c.handler))
	defer srv.Close()

	store := &fakeStore{subs: []db.WebhookSubscription{
		sub(t, subID1, "", []string{EventIssueStatusChanged}, srv.URL),
	}}
	d := newTestDispatcher(t, store, &http.Client{Timeout: deliveryTimeout})
	// Shorten backoff so the test is fast (per-instance — no shared global).
	d.retryBackoff = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}

	c.wg.Add(1) // one eventual success
	d.DispatchIssueStatusChanged(IssueStatusChanged{
		WorkspaceID: wsID,
		Issue:       map[string]any{"id": "issue-1"},
	})

	waitTimeout(t, c.wg, 5*time.Second)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.bodies) != 3 {
		t.Fatalf("expected 3 attempts (2 failures + 1 success), got %d", len(c.bodies))
	}
}

func waitTimeout(t *testing.T, wg *sync.WaitGroup, d time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatal("timed out waiting for deliveries")
	}
}

func TestRedactErr(t *testing.T) {
	// A *url.Error embeds the full URL (with tokens) in its Error(); redactErr
	// must drop the URL and keep only the operation + underlying cause.
	ue := &url.Error{
		Op:  "Post",
		URL: "https://hooks.example.com/in?token=SUPERSECRET",
		Err: fmt.Errorf("context deadline exceeded"),
	}
	got := redactErr(ue)
	if got != "Post: context deadline exceeded" {
		t.Fatalf("redactErr = %q, want %q", got, "Post: context deadline exceeded")
	}
	if got == "" || containsSecret(got) {
		t.Fatalf("redactErr leaked URL/token: %q", got)
	}
	if redactErr(nil) != "" {
		t.Errorf("redactErr(nil) should be empty")
	}
	// Non-url errors pass through.
	if redactErr(fmt.Errorf("plain")) != "plain" {
		t.Errorf("redactErr(plain) mismatch")
	}
}

func containsSecret(s string) bool {
	for i := 0; i+11 <= len(s); i++ {
		if s[i:i+11] == "SUPERSECRET" {
			return true
		}
	}
	return false
}

func TestDeliverRetriesOn429(t *testing.T) {
	c := &collector{wg: &sync.WaitGroup{}}
	c.failNext.Store(1) // fail once, then succeed
	// Make the failure a 429 rather than 500.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.bodies = append(c.bodies, body)
		c.mu.Unlock()
		if c.failNext.Load() > 0 {
			c.failNext.Add(-1)
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		c.wg.Done()
	}))
	defer srv.Close()

	store := &fakeStore{subs: []db.WebhookSubscription{
		sub(t, subID1, "", []string{EventIssueStatusChanged}, srv.URL),
	}}
	d := newTestDispatcher(t, store, &http.Client{Timeout: deliveryTimeout})
	d.retryBackoff = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}
	c.wg.Add(1)
	d.DispatchIssueStatusChanged(IssueStatusChanged{WorkspaceID: wsID, Issue: map[string]any{"id": "i"}})

	waitTimeout(t, c.wg, 5*time.Second)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.bodies) != 2 {
		t.Fatalf("429 should be retried: expected 2 attempts, got %d", len(c.bodies))
	}
}

func TestClose_DrainsInFlightAndStopsAccepting(t *testing.T) {
	c := &collector{wg: &sync.WaitGroup{}}
	srv := httptest.NewServer(http.HandlerFunc(c.handler))
	defer srv.Close()

	store := &fakeStore{subs: []db.WebhookSubscription{
		sub(t, subID1, "", []string{EventIssueStatusChanged}, srv.URL),
	}}
	d := newTestDispatcher(t, store, &http.Client{Timeout: deliveryTimeout})

	c.wg.Add(1)
	d.DispatchIssueStatusChanged(IssueStatusChanged{WorkspaceID: wsID, Issue: map[string]any{"id": "i"}})

	// Close drains the in-flight delivery before returning.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	waitTimeout(t, c.wg, time.Second) // delivery already happened during drain

	c.mu.Lock()
	delivered := len(c.bodies)
	c.mu.Unlock()
	if delivered != 1 {
		t.Fatalf("expected 1 delivery drained on close, got %d", delivered)
	}

	// After Close, new events are dropped (no panic, no delivery).
	d.DispatchIssueStatusChanged(IssueStatusChanged{WorkspaceID: wsID, Issue: map[string]any{"id": "j"}})
	time.Sleep(100 * time.Millisecond)
	c.mu.Lock()
	after := len(c.bodies)
	c.mu.Unlock()
	if after != 1 {
		t.Fatalf("post-close dispatch should be dropped, got %d deliveries", after)
	}

	// Idempotent.
	if err := d.Close(ctx); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// waitForRecords polls the fakeStore until it has captured at least n recorded
// deliveries, or fails after the timeout. Recording happens after the HTTP
// response, on the delivery worker, so tests can't rely on the collector wg.
func waitForRecords(t *testing.T, store *fakeStore, n int, d time.Duration) []db.CreateOutboundWebhookDeliveryParams {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		recs := store.records()
		if len(recs) >= n {
			return recs
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d recorded deliveries, got %d", n, len(recs))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDeliveryRecordedOnSuccess(t *testing.T) {
	c := &collector{wg: &sync.WaitGroup{}}
	srv := httptest.NewServer(http.HandlerFunc(c.handler))
	defer srv.Close()

	store := &fakeStore{subs: []db.WebhookSubscription{
		sub(t, subID1, "", []string{EventIssueStatusChanged}, srv.URL),
	}}
	d := newTestDispatcher(t, store, &http.Client{Timeout: deliveryTimeout})

	c.wg.Add(1)
	d.DispatchIssueStatusChanged(IssueStatusChanged{
		WorkspaceID: wsID,
		Issue:       map[string]any{"id": "issue-1", "status": "done"},
	})
	waitTimeout(t, c.wg, 5*time.Second)

	recs := waitForRecords(t, store, 1, 2*time.Second)
	r := recs[0]
	if r.Status != "delivered" {
		t.Errorf("status = %q, want delivered", r.Status)
	}
	if r.AttemptCount != 1 {
		t.Errorf("attempt_count = %d, want 1", r.AttemptCount)
	}
	if !r.ResponseStatus.Valid || r.ResponseStatus.Int32 != http.StatusOK {
		t.Errorf("response_status = %+v, want 200", r.ResponseStatus)
	}
	if len(r.RequestBody) == 0 {
		t.Errorf("request_body should be recorded for redelivery")
	}
	if uuidStr(r.SubscriptionID) != subID1 {
		t.Errorf("subscription_id = %q, want %q", uuidStr(r.SubscriptionID), subID1)
	}
	if r.Event != EventIssueStatusChanged {
		t.Errorf("event = %q", r.Event)
	}
	if r.RedeliveredFromID.Valid {
		t.Errorf("a normal delivery must not have redelivered_from_id")
	}
}

func TestDeliveryRecordedAsFailedOn4xx(t *testing.T) {
	// A 4xx is non-retryable; deliver() records one failed row and stops.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	store := &fakeStore{subs: []db.WebhookSubscription{
		sub(t, subID1, "", []string{EventIssueStatusChanged}, srv.URL),
	}}
	d := newTestDispatcher(t, store, &http.Client{Timeout: deliveryTimeout})
	d.DispatchIssueStatusChanged(IssueStatusChanged{WorkspaceID: wsID, Issue: map[string]any{"id": "i"}})

	recs := waitForRecords(t, store, 1, 3*time.Second)
	r := recs[0]
	if r.Status != "failed" {
		t.Errorf("status = %q, want failed", r.Status)
	}
	if r.AttemptCount != 1 {
		t.Errorf("attempt_count = %d, want 1 (4xx not retried)", r.AttemptCount)
	}
	if !r.ResponseStatus.Valid || r.ResponseStatus.Int32 != http.StatusBadRequest {
		t.Errorf("response_status = %+v, want 400", r.ResponseStatus)
	}
}

func TestRedeliverEnqueuesAndRecordsLineage(t *testing.T) {
	c := &collector{wg: &sync.WaitGroup{}}
	srv := httptest.NewServer(http.HandlerFunc(c.handler))
	defer srv.Close()

	store := &fakeStore{}
	d := newTestDispatcher(t, store, &http.Client{Timeout: deliveryTimeout})

	s := sub(t, subID1, "", []string{EventIssueStatusChanged}, srv.URL)
	fromID := mustUUID(t, "55555555-5555-5555-5555-555555555555")

	c.wg.Add(1)
	if !d.Redeliver(s, EventIssueStatusChanged, []byte(`{"event":"issue.status_changed"}`), fromID) {
		t.Fatal("Redeliver returned false (queue full?)")
	}
	waitTimeout(t, c.wg, 5*time.Second)

	recs := waitForRecords(t, store, 1, 2*time.Second)
	r := recs[0]
	if r.Status != "delivered" {
		t.Errorf("status = %q, want delivered", r.Status)
	}
	if !r.RedeliveredFromID.Valid || uuidStr(r.RedeliveredFromID) != uuidStr(fromID) {
		t.Errorf("redelivered_from_id = %+v, want %q", r.RedeliveredFromID, uuidStr(fromID))
	}
}

func uuidStr(u pgtype.UUID) string { return util.UUIDToString(u) }

// TestDeliveryRecordsSanitizedResponseBody proves that a subscriber response
// containing non-UTF-8 bytes is coerced to valid UTF-8 before being recorded,
// so the INSERT into the TEXT response_body column can't fail (which would
// silently drop the whole delivery-history row).
func TestDeliveryRecordsSanitizedResponseBody(t *testing.T) {
	c := &collector{wg: &sync.WaitGroup{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Invalid UTF-8 bytes (a lone continuation byte + a truncated 3-byte rune).
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte{0xff, 0xfe, 0xe2, 0x80})
		c.wg.Done()
	}))
	defer srv.Close()

	store := &fakeStore{subs: []db.WebhookSubscription{
		sub(t, subID1, "", []string{EventIssueStatusChanged}, srv.URL),
	}}
	d := newTestDispatcher(t, store, &http.Client{Timeout: deliveryTimeout})

	c.wg.Add(1)
	d.DispatchIssueStatusChanged(IssueStatusChanged{WorkspaceID: wsID, Issue: map[string]any{"id": "i"}})
	waitTimeout(t, c.wg, 5*time.Second)

	recs := waitForRecords(t, store, 1, 2*time.Second)
	rb := recs[0].ResponseBody
	if !rb.Valid {
		t.Fatalf("response_body should be recorded")
	}
	if !utf8.ValidString(rb.String) {
		t.Fatalf("response_body was not sanitized to valid UTF-8: %q", rb.String)
	}
}
