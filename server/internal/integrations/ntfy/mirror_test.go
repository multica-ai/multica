package ntfy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	testRecipientID = "11111111-1111-4111-8111-111111111111"
	testWorkspaceID = "22222222-2222-4222-8222-222222222222"
	testIssueID     = "33333333-3333-4333-8333-333333333333"
)

type fakeQueries struct {
	subscribed bool
	subErr     error
	prefs      []byte
	prefErr    error
}

func (f *fakeQueries) IsIssueSubscriber(context.Context, db.IsIssueSubscriberParams) (bool, error) {
	return f.subscribed, f.subErr
}

func (f *fakeQueries) GetNotificationPreference(context.Context, db.GetNotificationPreferenceParams) (db.NotificationPreference, error) {
	if f.prefErr != nil {
		return db.NotificationPreference{}, f.prefErr
	}
	if f.prefs == nil {
		return db.NotificationPreference{}, pgx.ErrNoRows
	}
	return db.NotificationPreference{Preferences: f.prefs}, nil
}

type capturedRequest struct {
	payload publishPayload
	raw     string
	auth    string
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func testMirrorConfig(baseURL string) Config {
	return Config{
		BaseURL:       baseURL,
		Topic:         strings.Repeat("random", 8),
		Token:         "publisher-token",
		RecipientID:   testRecipientID,
		AppURL:        "https://multica.example",
		Timeout:       500 * time.Millisecond,
		QueueCapacity: 8,
	}
}

func inboxEvent(notificationType, status, recipientID string) events.Event {
	return events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: testWorkspaceID,
		Payload: map[string]any{"item": map[string]any{
			"recipient_type": "member",
			"recipient_id":   recipientID,
			"workspace_id":   testWorkspaceID,
			"issue_id":       testIssueID,
			"issue_status":   status,
			"type":           notificationType,
			"title":          "private issue title",
			"body":           "private full comment body",
		}},
	}
}

func taskEvent(eventType string, retryPending bool) events.Event {
	return events.Event{
		Type:        eventType,
		WorkspaceID: testWorkspaceID,
		Payload: map[string]any{
			"issue_id":      testIssueID,
			"retry_pending": retryPending,
			"error":         "provider error containing a credential",
		},
	}
}

func stopMirror(t *testing.T, mirror *Mirror) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if !mirror.Stop(ctx) {
		t.Fatal("mirror did not stop")
	}
}

func TestMirrorMapsRequiredEventsWithoutUserContent(t *testing.T) {
	requests := make(chan capturedRequest, 8)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var payload publishPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- capturedRequest{payload: payload, raw: string(raw), auth: r.Header.Get("Authorization")}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := testMirrorConfig(server.URL)
	mirror := New(config, &fakeQueries{subscribed: true}, slog.New(slog.NewTextHandler(io.Discard, nil)), server.Client())
	defer stopMirror(t, mirror)
	bus := events.New()
	mirror.Register(bus)

	tests := []struct {
		event events.Event
		title string
		prio  int
	}{
		{inboxEvent("mentioned", "in_progress", testRecipientID), "Multica: Mention", 3},
		{inboxEvent("issue_assigned", "todo", testRecipientID), "Multica: Assignment", 3},
		{inboxEvent("status_changed", "blocked", testRecipientID), "Multica: Issue blocked", 4},
		{taskEvent(protocol.EventTaskCompleted, false), "Multica: Agent run completed", 3},
		{taskEvent(protocol.EventTaskFailed, false), "Multica: Agent run failed", 4},
	}

	for _, test := range tests {
		bus.Publish(test.event)
		select {
		case request := <-requests:
			if request.payload.Title != test.title || request.payload.Priority != test.prio {
				t.Errorf("payload title/priority = %q/%d, want %q/%d", request.payload.Title, request.payload.Priority, test.title, test.prio)
			}
			if request.payload.Topic != config.Topic {
				t.Errorf("topic was not sent to ntfy")
			}
			if request.auth != "Bearer publisher-token" {
				t.Errorf("Authorization = %q", request.auth)
			}
			if request.payload.Click != "https://multica.example/"+testWorkspaceID+"/inbox?issue="+testIssueID {
				t.Errorf("click = %q", request.payload.Click)
			}
			for _, secret := range []string{"private issue title", "private full comment body", "provider error containing a credential", "publisher-token"} {
				if strings.Contains(request.raw, secret) {
					t.Errorf("request body leaked %q: %s", secret, request.raw)
				}
			}
		case <-time.After(time.Second):
			t.Fatalf("no request for %s", test.title)
		}
	}
}

func TestMirrorFiltersUnrelatedMutedAndRetryEvents(t *testing.T) {
	requests := make(chan struct{}, 8)
	doer := doerFunc(func(*http.Request) (*http.Response, error) {
		requests <- struct{}{}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
	})

	config := testMirrorConfig("https://ntfy.example")
	mirror := New(config, &fakeQueries{subscribed: false}, slog.New(slog.NewTextHandler(io.Discard, nil)), doer)
	bus := events.New()
	mirror.Register(bus)
	bus.Publish(inboxEvent("mentioned", "in_progress", "44444444-4444-4444-8444-444444444444"))
	bus.Publish(inboxEvent("new_comment", "in_progress", testRecipientID))
	bus.Publish(inboxEvent("status_changed", "in_review", testRecipientID))
	bus.Publish(taskEvent(protocol.EventTaskFailed, true))
	bus.Publish(taskEvent(protocol.EventTaskCompleted, false))

	select {
	case <-requests:
		t.Fatal("an unrelated, retrying, or unsubscribed event was delivered")
	case <-time.After(100 * time.Millisecond):
	}
	stopMirror(t, mirror)

	muted := New(config, &fakeQueries{subscribed: true, prefs: []byte(`{"agent_activity":"muted"}`)}, slog.New(slog.NewTextHandler(io.Discard, nil)), doer)
	mutedBus := events.New()
	muted.Register(mutedBus)
	mutedBus.Publish(taskEvent(protocol.EventTaskCompleted, false))
	select {
	case <-requests:
		t.Fatal("muted agent activity was delivered")
	case <-time.After(100 * time.Millisecond):
	}
	stopMirror(t, muted)
}

func TestMirrorDeliveryFailureNeverBlocksPublisherOrRetries(t *testing.T) {
	started := make(chan struct{}, 1)
	var calls atomic.Int64
	doer := doerFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		started <- struct{}{}
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	config := testMirrorConfig("https://ntfy.example")
	config.Timeout = 40 * time.Millisecond
	mirror := New(config, &fakeQueries{subscribed: true}, slog.New(slog.NewTextHandler(io.Discard, nil)), doer)
	defer stopMirror(t, mirror)
	bus := events.New()
	mirror.Register(bus)

	start := time.Now()
	bus.Publish(inboxEvent("mentioned", "in_progress", testRecipientID))
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Fatalf("event publish blocked for %s", elapsed)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("delivery did not start")
	}
	time.Sleep(120 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("delivery attempts = %d, want exactly 1 (no retries)", got)
	}
}

func TestMirrorQueueOverflowIsNonBlockingAndLogsNoSecrets(t *testing.T) {
	var logs bytes.Buffer
	started := make(chan struct{}, 1)
	doer := doerFunc(func(req *http.Request) (*http.Response, error) {
		started <- struct{}{}
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	config := testMirrorConfig("https://private-ntfy.example")
	config.Topic = strings.Repeat("secret-topic-", 4)
	config.Token = "secret-token"
	config.QueueCapacity = 1
	config.Timeout = 500 * time.Millisecond
	mirror := New(config, &fakeQueries{subscribed: true}, slog.New(slog.NewTextHandler(&logs, nil)), doer)
	bus := events.New()
	mirror.Register(bus)
	bus.Publish(inboxEvent("mentioned", "in_progress", testRecipientID))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first delivery did not start")
	}

	start := time.Now()
	bus.Publish(inboxEvent("mentioned", "in_progress", testRecipientID))
	bus.Publish(inboxEvent("mentioned", "in_progress", testRecipientID))
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Fatalf("full queue blocked publisher for %s", elapsed)
	}
	stopMirror(t, mirror)

	output := logs.String()
	if !strings.Contains(output, "queue full") {
		t.Fatalf("missing diagnostic queue-full log: %q", output)
	}
	for _, secret := range []string{config.Topic, config.Token, "private full comment body", "private-ntfy.example"} {
		if strings.Contains(output, secret) {
			t.Fatalf("logs leaked %q: %s", secret, output)
		}
	}
}

func TestMirrorRejectedDeliveryLogsStatusWithoutTopicOrBody(t *testing.T) {
	var logs bytes.Buffer
	called := make(chan struct{}, 1)
	doer := doerFunc(func(*http.Request) (*http.Response, error) {
		called <- struct{}{}
		return &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(strings.NewReader("server response may contain the topic"))}, nil
	})
	config := testMirrorConfig("https://private-ntfy.example")
	config.Topic = strings.Repeat("private", 6)
	config.Token = "secret-token"
	mirror := New(config, &fakeQueries{subscribed: true}, slog.New(slog.NewTextHandler(&logs, nil)), doer)
	bus := events.New()
	mirror.Register(bus)
	bus.Publish(inboxEvent("mentioned", "in_progress", testRecipientID))
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("delivery did not run")
	}
	stopMirror(t, mirror)

	output := logs.String()
	if !strings.Contains(output, "status=429") {
		t.Fatalf("missing safe status diagnostic: %q", output)
	}
	for _, secret := range []string{config.Topic, config.Token, "private full comment body", "private-ntfy.example", "server response may contain the topic"} {
		if strings.Contains(output, secret) {
			t.Fatalf("logs leaked %q: %s", secret, output)
		}
	}
}
