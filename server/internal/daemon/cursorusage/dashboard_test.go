package cursorusage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchFilteredUsageEventsParsesFlexibleFields(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/dashboard/get-filtered-usage-events", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "https://cursor.com" {
			t.Errorf("missing Origin header")
		}
		if r.Header.Get("Cookie") == "" {
			t.Errorf("missing Cookie header")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if _, ok := body["userId"]; ok {
			t.Fatal("userId must not be sent")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalUsageEventsCount": 2,
			"usageEventsDisplay": []map[string]any{
				{
					"timestamp":    "1700000000123",
					"model":        "composer-1",
					"isHeadless":   true,
					"chargedCents": "0",
					"tokenUsage": map[string]any{
						"inputTokens":  1,
						"outputTokens": 2,
						"totalCents":   7.5,
					},
				},
				{
					"timestamp":  1700000000456,
					"model":      "gpt-5",
					"isHeadless": true,
					"tokenUsage": map[string]any{
						"inputTokens":  3,
						"outputTokens": 4,
						"totalCents":   1.2,
					},
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	events, err := c.FetchFilteredUsageEvents(
		context.Background(),
		"user_x%3A%3Atok",
		time.UnixMilli(1700000000000),
		time.UnixMilli(1700000001000),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("len=%d", len(events))
	}
	if !events[0].HasChargedCents || events[0].ChargedCents != 0 {
		t.Fatalf("event0 charged presence/value wrong: %#v", events[0])
	}
	if events[1].HasChargedCents {
		t.Fatalf("event1 must keep chargedCents absent when omitted: %#v", events[1])
	}
	if !events[0].Timestamp.Equal(time.UnixMilli(1700000000123).UTC()) {
		t.Fatalf("timestamp=%v", events[0].Timestamp)
	}
}

func TestFetchFilteredUsageEventsRejectsPartialResults(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(filteredUsageEventsPath, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalUsageEventsCount": 2,
			"usageEventsDisplay": []map[string]any{{
				"timestamp":    time.Now().UnixMilli(),
				"model":        "composer-1",
				"chargedCents": 1,
				"tokenUsage":   map[string]any{"inputTokens": 1},
			}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	_, err := c.FetchFilteredUsageEvents(context.Background(), "session", time.Now().Add(-time.Minute), time.Now())
	if err == nil || !strings.Contains(err.Error(), "returned 1 of 2 events") {
		t.Fatalf("expected partial-results error, got %v", err)
	}
}

func TestFetchFilteredUsageEventsRejectsPageLimit(t *testing.T) {
	events := make([]map[string]any, defaultFilteredPageSize)
	for i := range events {
		events[i] = map[string]any{
			"timestamp":    time.Now().UnixMilli(),
			"model":        "composer-1",
			"chargedCents": 1,
			"tokenUsage":   map[string]any{"inputTokens": 1},
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc(filteredUsageEventsPath, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalUsageEventsCount": 50*defaultFilteredPageSize + 1,
			"usageEventsDisplay":    events,
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := &Client{BaseURL: srv.URL, HTTPClient: srv.Client()}
	_, err := c.FetchFilteredUsageEvents(context.Background(), "session", time.Now().Add(-time.Minute), time.Now())
	if err == nil || !strings.Contains(err.Error(), "exceeded 50 pages") {
		t.Fatalf("expected page-limit error, got %v", err)
	}
}
