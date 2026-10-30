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
	// Hold the first request so the worker is observably busy before filling the
	// queue. Releasing it during cleanup avoids paying the client's flush timeout.
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	}))
	defer srv.Close()

	c := NewPostHogClient(PostHogConfig{
		APIKey:     "test-key",
		Host:       srv.URL,
		QueueSize:  2,
		BatchSize:  1,
		FlushEvery: time.Hour,
	})
	defer func() {
		close(release)
		c.Close()
	}()

	// The first event is consumed by the worker and blocks in the handler. Two
	// more fill the queue, so the final capture must be dropped.
	c.Capture(Event{Name: "spam", DistinctID: "u"})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start the first request")
	}
	for i := 0; i < 3; i++ {
		c.Capture(Event{Name: "spam", DistinctID: "u"})
	}
	if c.dropped.Load() == 0 {
		t.Fatalf("expected some drops when queue saturated")
	}
}

func TestEmailDomain(t *testing.T) {
	cases := map[string]string{
		"a@example.com":      "example.com",
		"user@Company.co.uk": "company.co.uk",
		"":                   "",
		"no-at":              "",
		"trailing@":          "",
	}
	for in, want := range cases {
		if got := emailDomain(in); got != want {
			t.Errorf("emailDomain(%q) = %q, want %q", in, got, want)
		}
	}
}
