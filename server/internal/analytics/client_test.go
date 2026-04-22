package analytics

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestNoopClient(t *testing.T) {
	c := NoopClient{}
	c.Capture(Event{Name: "foo"})
	c.Close()
}

func TestPostHogClient_Batching(t *testing.T) {
	var (
		mu       sync.Mutex
		received [][]captureItem
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/batch/" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var payload capturePayload
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		if payload.APIKey != "test-key" {
			t.Errorf("api_key = %q, want test-key", payload.APIKey)
		}
		mu.Lock()
		received = append(received, payload.Batch)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewPostHogClient(PostHogConfig{
		APIKey:     "test-key",
		Host:       srv.URL,
		BatchSize:  2,
		FlushEvery: time.Hour, // irrelevant, we hit the size trigger
	})

	c.Capture(Event{Name: "signup", DistinctID: "u1", WorkspaceID: "w1"})
	c.Capture(Event{Name: "workspace_created", DistinctID: "u1", WorkspaceID: "w1"})
	c.Close() // drains

	mu.Lock()
	defer mu.Unlock()
	total := 0
	for _, b := range received {
		total += len(b)
	}
	if total != 2 {
		t.Fatalf("received %d events, want 2 (batches=%d)", total, len(received))
	}
	// Both events should carry workspace_id in properties.
	for _, batch := range received {
		for _, item := range batch {
			if item.Properties["workspace_id"] != "w1" {
				t.Errorf("missing workspace_id on event %s", item.Event)
			}
			if item.DistinctID != "u1" {
				t.Errorf("distinct_id = %q, want u1", item.DistinctID)
			}
		}
	}
}

func TestPostHogClient_DropsWhenFull(t *testing.T) {
	// Handler blocks so batches never flush — queue will fill up.
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)

	c := NewPostHogClient(PostHogConfig{
		APIKey:     "test-key",
		Host:       srv.URL,
		QueueSize:  2,
		BatchSize:  1,
		FlushEvery: time.Hour,
	})
	defer c.Close()

	// First event may be consumed by the worker (which is now blocked in send).
	// Next events will sit in the queue (cap=2) until it's full and then drop.
	for i := 0; i < 20; i++ {
		c.Capture(Event{Name: "spam", DistinctID: "u"})
	}
	// Give the worker a chance to pick up at least one.
	time.Sleep(50 * time.Millisecond)
	if c.dropped.Load() == 0 {
		t.Fatalf("expected some drops when queue saturated")
	}
}

func TestEmailDomain(t *testing.T) {
	cases := map[string]string{
		"a@example.com":       "example.com",
		"user@Company.co.uk":  "company.co.uk",
		"":                    "",
		"no-at":               "",
		"trailing@":           "",
	}
	for in, want := range cases {
		if got := emailDomain(in); got != want {
			t.Errorf("emailDomain(%q) = %q, want %q", in, got, want)
		}
	}
}
