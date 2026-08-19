package webpush

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
)

type fakeStore struct {
	prefs         map[string]string
	slug          string
	subscriptions []Subscription
	deleted       []string
}

func (s *fakeStore) Upsert(context.Context, string, Subscription) error  { return nil }
func (s *fakeStore) DeleteForUser(context.Context, string, string) error { return nil }
func (s *fakeStore) DeleteByEndpoint(_ context.Context, endpoint string) error {
	s.deleted = append(s.deleted, endpoint)
	return nil
}
func (s *fakeStore) ListByUser(context.Context, string) ([]Subscription, error) {
	return s.subscriptions, nil
}
func (s *fakeStore) NotificationPreferences(context.Context, string, string) (map[string]string, error) {
	return s.prefs, nil
}
func (s *fakeStore) WorkspaceSlug(context.Context, string) (string, error) { return s.slug, nil }

type sentPush struct {
	subscription Subscription
	payload      Payload
}

type fakeSender struct {
	status int
	sent   []sentPush
}

func (s *fakeSender) Send(_ context.Context, subscription Subscription, payload Payload) (int, error) {
	s.sent = append(s.sent, sentPush{subscription: subscription, payload: payload})
	return s.status, nil
}

func inboxEvent() events.Event {
	return events.Event{
		Type:        "inbox:new",
		WorkspaceID: "workspace-1",
		Payload: map[string]any{
			"item": map[string]any{
				"id":             "item-1",
				"recipient_type": "member",
				"recipient_id":   "user-1",
				"issue_id":       "task-1",
				"title":          "Task blocked",
				"body":           "Needs your attention",
			},
		},
	}
}

func TestDeliverProjectsExistingInboxEventToCanonicalTagRoute(t *testing.T) {
	store := &fakeStore{
		slug:          "studio-a",
		subscriptions: []Subscription{{Endpoint: "https://push.test/one", P256DH: "key", Auth: "auth"}},
	}
	sender := &fakeSender{status: 201}
	service := NewService(Config{PublicKey: "public", PrivateKey: "private", Subject: "mailto:test@example.com"}, store, sender)

	if err := service.Deliver(context.Background(), inboxEvent()); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sent = %d, want 1", len(sender.sent))
	}
	payload := sender.sent[0].payload
	if payload.URL != "/tag/studio-a/inbox?issue=task-1" {
		t.Fatalf("URL = %q", payload.URL)
	}
	if payload.Tag != "inbox:item-1" {
		t.Fatalf("Tag = %q", payload.Tag)
	}
	if _, err := json.Marshal(payload); err != nil {
		t.Fatalf("payload is not JSON serializable: %v", err)
	}
}

func TestDeliverProjectsSystemInboxEventToBareCanonicalTagRoute(t *testing.T) {
	for _, issueID := range []any{nil, ""} {
		name := "missing_issue_id"
		if issueID != nil {
			name = "empty_issue_id"
		}
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{
				slug:          "studio-a",
				subscriptions: []Subscription{{Endpoint: "https://push.test/one", P256DH: "key", Auth: "auth"}},
			}
			sender := &fakeSender{status: 201}
			service := NewService(Config{PublicKey: "public", PrivateKey: "private", Subject: "mailto:test@example.com"}, store, sender)
			event := inboxEvent()
			item := event.Payload.(map[string]any)["item"].(map[string]any)
			if issueID == nil {
				delete(item, "issue_id")
			} else {
				item["issue_id"] = issueID
			}
			item["title"] = "Runtime disconnected"
			item["body"] = "Agent runtime needs attention"

			if err := service.Deliver(context.Background(), event); err != nil {
				t.Fatal(err)
			}
			if len(sender.sent) != 1 {
				t.Fatalf("sent = %d, want 1", len(sender.sent))
			}
			if got := sender.sent[0].payload.URL; got != "/tag/studio-a/inbox" {
				t.Fatalf("URL = %q, want bare canonical Inbox route", got)
			}
		})
	}
}

func TestDeliverHonorsCanonicalAndLegacyDeliveryChannel(t *testing.T) {
	for _, prefs := range []map[string]string{
		{"browser_push": "muted"},
		{"system_notifications": "muted"},
		{"browser_push": "muted", "system_notifications": "all"},
	} {
		store := &fakeStore{
			prefs:         prefs,
			slug:          "studio-a",
			subscriptions: []Subscription{{Endpoint: "https://push.test/one", P256DH: "key", Auth: "auth"}},
		}
		sender := &fakeSender{status: 201}
		service := NewService(Config{PublicKey: "public", PrivateKey: "private"}, store, sender)
		if err := service.Deliver(context.Background(), inboxEvent()); err != nil {
			t.Fatal(err)
		}
		if len(sender.sent) != 0 {
			t.Fatalf("prefs %v sent %d pushes, want 0", prefs, len(sender.sent))
		}
	}
}

func TestDeliverRemovesRevokedSubscription(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			store := &fakeStore{
				slug:          "studio-a",
				subscriptions: []Subscription{{Endpoint: "https://push.test/revoked", P256DH: "key", Auth: "auth"}},
			}
			sender := &fakeSender{status: status}
			service := NewService(Config{PublicKey: "public", PrivateKey: "private"}, store, sender)

			if err := service.Deliver(context.Background(), inboxEvent()); err != nil {
				t.Fatal(err)
			}
			if len(store.deleted) != 1 || store.deleted[0] != "https://push.test/revoked" {
				t.Fatalf("deleted = %v", store.deleted)
			}
		})
	}
}

func TestDeliverFailsClosedWithoutSourceWorkspaceSlug(t *testing.T) {
	store := &fakeStore{}
	sender := &fakeSender{status: 201}
	service := NewService(Config{PublicKey: "public", PrivateKey: "private"}, store, sender)

	if err := service.Deliver(context.Background(), inboxEvent()); err == nil {
		t.Fatal("expected empty source workspace slug to fail closed")
	}
	if len(sender.sent) != 0 {
		t.Fatal("sent push without a source workspace")
	}
}

func TestProtocolSenderNormalizesOptionalSubject(t *testing.T) {
	sender := NewProtocolSender(Config{PublicKey: " public ", PrivateKey: " private "})
	protocol, ok := sender.(*protocolSender)
	if !ok {
		t.Fatalf("sender type = %T", sender)
	}
	if protocol.config.Subject != "mailto:notifications@multica.ai" {
		t.Fatalf("subject = %q", protocol.config.Subject)
	}
}

func TestDeliverFailsClosedForMalformedInboxPayload(t *testing.T) {
	store := &fakeStore{slug: "studio-a"}
	sender := &fakeSender{status: 201}
	service := NewService(Config{PublicKey: "public", PrivateKey: "private"}, store, sender)
	event := inboxEvent()
	event.Payload = map[string]any{"recipient_id": "user-1"}

	if err := service.Deliver(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("sent malformed event")
	}
}
