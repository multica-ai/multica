package mobilepush

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type fakeStore struct {
	targets []db.ListMobilePushTargetsRow
	pending []db.MobilePushReceipt
	saved   []db.CreateMobilePushReceiptParams
	invalid []db.InvalidateMobilePushSubscriptionParams
	deleted []string
}

func (s *fakeStore) ListMobilePushTargets(context.Context, pgtype.UUID) ([]db.ListMobilePushTargetsRow, error) {
	return s.targets, nil
}
func (s *fakeStore) CreateMobilePushReceipt(_ context.Context, p db.CreateMobilePushReceiptParams) error {
	s.saved = append(s.saved, p)
	return nil
}
func (s *fakeStore) ListMobilePushReceipts(context.Context) ([]db.MobilePushReceipt, error) {
	return s.pending, nil
}
func (s *fakeStore) DeleteMobilePushReceipt(_ context.Context, id string) error {
	s.deleted = append(s.deleted, id)
	return nil
}
func (s *fakeStore) InvalidateMobilePushSubscription(_ context.Context, p db.InvalidateMobilePushSubscriptionParams) error {
	s.invalid = append(s.invalid, p)
	return nil
}
func (*fakeStore) PruneMobilePushSubscriptions(context.Context) error { return nil }

func TestRegisterIsBoundedAndMemberOnly(t *testing.T) {
	n := New(&fakeStore{}, "")
	bus := events.New()
	n.Register(bus)
	if bus.SubscriberCount(protocol.EventInboxNew) != 1 {
		t.Fatal("not registered")
	}
	id := "11111111-1111-4111-8111-111111111111"
	for _, payload := range []any{nil, map[string]any{"id": id, "recipient_type": "agent"}, map[string]any{"id": "bad", "recipient_type": "member"}} {
		bus.Publish(events.Event{Type: protocol.EventInboxNew, Payload: payload})
	}
	if len(n.queue) != 0 {
		t.Fatal("invalid event queued")
	}
	for range 300 {
		bus.Publish(events.Event{Type: protocol.EventInboxNew, Payload: map[string]any{"id": id, "recipient_type": "member"}})
	}
	if len(n.queue) != 256 {
		t.Fatalf("queue length %d", len(n.queue))
	}
}

func TestDeliverPersistsTicketAndSendsOnlyGenericInboxData(t *testing.T) {
	updated := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	store := &fakeStore{targets: []db.ListMobilePushTargetsRow{{Token: "ExpoPushToken[test]", Slug: "team", UpdatedAt: updated}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/send" || r.Header.Get("Authorization") != "Bearer configured-token" {
			t.Error("incorrect endpoint or authorization")
		}
		var messages []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&messages); err != nil {
			t.Fatal(err)
		}
		if len(messages) != 1 || messages[0]["body"] != "You have new inbox activity." {
			t.Fatal("unexpected message")
		}
		data := messages[0]["data"].(map[string]any)
		if len(data) != 1 || data["workspace"] != "team" {
			t.Fatal("unexpected navigation payload")
		}
		_, _ = w.Write([]byte(`{"data":[{"status":"ok","id":"receipt"}]}`))
	}))
	defer server.Close()
	n := New(store, "configured-token")
	n.endpoint = server.URL
	id, _ := util.ParseUUID("11111111-1111-4111-8111-111111111111")
	n.deliver(context.Background(), id)
	if len(store.saved) != 1 || store.saved[0].SubscriptionUpdatedAt != updated {
		t.Fatal("ticket must retain registration generation")
	}
}

func TestTicketAndReceiptInvalidateOnlyTheirRegistration(t *testing.T) {
	for _, phase := range []string{"ticket", "receipt"} {
		t.Run(phase, func(t *testing.T) {
			updated := pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true}
			store := &fakeStore{targets: []db.ListMobilePushTargetsRow{{Token: "ExpoPushToken[test]", UpdatedAt: updated}}, pending: []db.MobilePushReceipt{{ID: "receipt", Token: "ExpoPushToken[test]", SubscriptionUpdatedAt: updated, CreatedAt: updated}}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if phase == "ticket" {
					_, _ = w.Write([]byte(`{"data":[{"status":"error","details":{"error":"DeviceNotRegistered"}}]}`))
				} else {
					_, _ = w.Write([]byte(`{"data":{"receipt":{"status":"error","details":{"error":"DeviceNotRegistered"}}}}`))
				}
			}))
			defer server.Close()
			n := New(store, "")
			n.endpoint = server.URL
			if phase == "ticket" {
				n.deliver(context.Background(), pgtype.UUID{})
			} else {
				n.receipts(context.Background())
			}
			if len(store.invalid) != 1 || store.invalid[0].UpdatedAt != updated {
				t.Fatal("must invalidate exact registration")
			}
			if phase == "receipt" && len(store.deleted) != 1 {
				t.Fatal("receipt not removed")
			}
		})
	}
}

func TestProviderFailureDoesNotInvalidateDeviceOrLoseReceipt(t *testing.T) {
	store := &fakeStore{pending: []db.MobilePushReceipt{{ID: "pending", CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	n := New(store, "")
	n.endpoint = server.URL
	n.receipts(context.Background())
	if len(store.invalid) != 0 || len(store.deleted) != 0 {
		t.Fatal("temporary failure discarded registration or receipt")
	}
}

func TestValidToken(t *testing.T) {
	for _, token := range []string{"ExpoPushToken[abc_123-]", "ExponentPushToken[abc]"} {
		if !ValidToken(token) {
			t.Fatal(token)
		}
	}
	for _, token := range []string{"", "ExpoPushToken[]", "https://example.com", "ExpoPushToken[abc]\n"} {
		if ValidToken(token) {
			t.Fatal(token)
		}
	}
}
