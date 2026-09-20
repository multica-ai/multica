package push

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExpoClientSendReturnsUnregisteredTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("Authorization = %q", got)
		}
		var messages []Message
		if err := json.NewDecoder(r.Body).Decode(&messages); err != nil {
			t.Fatal(err)
		}
		if len(messages) != 2 || messages[1].Data["url"] != "multica:///ws/inbox/n1" {
			t.Fatalf("messages = %#v", messages)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"status":"ok","id":"ticket-1"},{"status":"error","details":{"error":"DeviceNotRegistered"}}]}`))
	}))
	defer server.Close()

	client := &ExpoClient{Endpoint: server.URL, AccessToken: "secret", HTTPClient: server.Client()}
	invalid, err := client.Send(context.Background(), []Message{
		{To: "ExpoPushToken[first]", Title: "First"},
		{To: "ExpoPushToken[stale]", Title: "Second", Data: map[string]any{"url": "multica:///ws/inbox/n1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(invalid) != 1 || invalid[0] != "ExpoPushToken[stale]" {
		t.Fatalf("invalid = %#v", invalid)
	}
}

func TestExpoClientSendRejectsOversizedBatch(t *testing.T) {
	messages := make([]Message, 101)
	_, err := (&ExpoClient{}).Send(context.Background(), messages)
	if err == nil {
		t.Fatal("expected oversized batch error")
	}
}
