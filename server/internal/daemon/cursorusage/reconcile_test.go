package cursorusage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMatchUsageAggregatesMultipleDashboardEvents(t *testing.T) {
	t.Parallel()

	usage := TaskUsage{
		Model:            "claude-4.6-opus-high-thinking",
		InputTokens:      100,
		OutputTokens:     50,
		CacheReadTokens:  20,
		CacheWriteTokens: 10,
	}
	events := assignOccurrenceIndexes([]UsageEvent{
		{
			Timestamp: time.Now(), Model: "claude-4.6-opus-high-thinking", IsHeadless: true,
			InputTokens: 40, OutputTokens: 20, CacheReadTokens: 10, CacheWriteTokens: 5,
			ChargedCents: 10, HasChargedCents: true,
		},
		{
			Timestamp: time.Now(), Model: "claude-4.6-opus-high-thinking", IsHeadless: true,
			InputTokens: 60, OutputTokens: 30, CacheReadTokens: 10, CacheWriteTokens: 5,
			ChargedCents: 15.5, HasChargedCents: true,
		},
	})

	cost, keys, ok := matchUsage(usage, events)
	if !ok {
		t.Fatal("expected match")
	}
	want := CentsToUSDTicks(25.5)
	if cost != want {
		t.Fatalf("cost ticks = %d, want %d", cost, want)
	}
	if len(keys) != 2 {
		t.Fatalf("keys=%d want 2", len(keys))
	}
}

func TestApplyMatchesAcceptsCursorAgentEventMarkedNonHeadless(t *testing.T) {
	t.Parallel()

	now := time.UnixMilli(1_786_346_588_939)
	usage := []TaskUsage{{
		Model:            "cursor-grok-4.5-high-fast",
		InputTokens:      9723,
		OutputTokens:     1365,
		CacheReadTokens:  196224,
		CacheWriteTokens: 0,
	}}
	events := []UsageEvent{
		{
			Timestamp: now.Add(3 * time.Second), Model: "cursor-grok-4.5-high-fast",
			InputTokens: 12237, OutputTokens: 8794, CacheReadTokens: 1007680,
			ChargedCents: 121.49199676513672, HasChargedCents: true,
			IsHeadless: false, IsChargeable: true,
		},
		{
			Timestamp: now, Model: "cursor-grok-4.5-high-fast",
			InputTokens: 9723, OutputTokens: 1365, CacheReadTokens: 196224,
			ChargedCents: 25.968599319458008, HasChargedCents: true,
			// Real cursor-agent Dashboard rows currently report false here.
			IsHeadless: false, IsChargeable: true,
		},
	}

	matched := applyMatches(
		OpaqueClaimKey("user_real"),
		usage, events, now.Add(-time.Minute), now.Add(time.Minute),
	)
	if matched != 1 || !usage[0].HasCostUSDTicks {
		t.Fatalf("real non-headless Cursor event should match: matched=%d usage=%#v", matched, usage)
	}
	if usage[0].CostUSDTicks != CentsToUSDTicks(25.968599319458008) {
		t.Fatalf("cost ticks=%d", usage[0].CostUSDTicks)
	}
	if len(usage[0].OccurrenceKeys) != 1 {
		t.Fatalf("occurrence keys=%v", usage[0].OccurrenceKeys)
	}
}

func TestMatchUsageRejectsAmbiguousSameModelCandidates(t *testing.T) {
	t.Parallel()
	usage := TaskUsage{Model: "composer-1", InputTokens: 10, OutputTokens: 2}
	now := time.Now()
	events := assignOccurrenceIndexes([]UsageEvent{
		{
			Timestamp: now, Model: "composer-1", IsHeadless: true,
			InputTokens: 10, OutputTokens: 2, ChargedCents: 4, HasChargedCents: true,
		},
		{
			Timestamp: now.Add(time.Second), Model: "composer-1", IsHeadless: true,
			InputTokens: 10, OutputTokens: 2, ChargedCents: 9, HasChargedCents: true,
		},
	})
	if _, _, ok := matchUsage(usage, events); ok {
		t.Fatal("two independently matching same-model events must fail closed")
	}
}

func TestMatchUsageAcceptsAuthoritativeZero(t *testing.T) {
	t.Parallel()
	usage := TaskUsage{Model: "composer-1", InputTokens: 10, OutputTokens: 2}
	events := assignOccurrenceIndexes([]UsageEvent{{
		Timestamp: time.Now(), Model: "composer-1", IsHeadless: true,
		InputTokens: 10, OutputTokens: 2, ChargedCents: 0, HasChargedCents: true,
	}})
	cost, _, ok := matchUsage(usage, events)
	if !ok {
		t.Fatal("expected match for authoritative zero")
	}
	if cost != 0 {
		t.Fatalf("cost=%d want 0", cost)
	}
}

func TestMatchUsageRejectsMissingChargedCents(t *testing.T) {
	t.Parallel()
	usage := TaskUsage{Model: "composer-1", InputTokens: 10, OutputTokens: 2}
	events := assignOccurrenceIndexes([]UsageEvent{{
		Timestamp: time.Now(), Model: "composer-1", IsHeadless: true,
		InputTokens: 10, OutputTokens: 2, ChargedCents: 9, HasChargedCents: false,
	}})
	if _, _, ok := matchUsage(usage, events); ok {
		t.Fatal("missing chargedCents must fail closed")
	}
}

func TestMatchUsageRejectsSubstringModelCompat(t *testing.T) {
	t.Parallel()
	usage := TaskUsage{Model: "gpt-5", InputTokens: 10, OutputTokens: 2}
	events := assignOccurrenceIndexes([]UsageEvent{{
		Timestamp: time.Now(), Model: "gpt-5-mini", IsHeadless: true,
		InputTokens: 10, OutputTokens: 2, ChargedCents: 3, HasChargedCents: true,
	}})
	if _, _, ok := matchUsage(usage, events); ok {
		t.Fatal("substring model match must not succeed")
	}
}

func TestMatchUsagePlaceholderRequiresUniqueModel(t *testing.T) {
	t.Parallel()
	usage := TaskUsage{Model: "cursor", InputTokens: 10, OutputTokens: 2}
	now := time.Now()
	unique := assignOccurrenceIndexes([]UsageEvent{{
		Timestamp: now, Model: "composer-1", IsHeadless: true,
		InputTokens: 10, OutputTokens: 2, ChargedCents: 4, HasChargedCents: true,
	}})
	cost, _, ok := matchUsage(usage, unique)
	if !ok || cost != CentsToUSDTicks(4) {
		t.Fatalf("unique placeholder match failed: ok=%v cost=%d", ok, cost)
	}

	ambiguous := assignOccurrenceIndexes([]UsageEvent{
		{
			Timestamp: now, Model: "composer-1", IsHeadless: true,
			InputTokens: 10, OutputTokens: 2, ChargedCents: 4, HasChargedCents: true,
		},
		{
			Timestamp: now, Model: "gpt-5", IsHeadless: true,
			InputTokens: 10, OutputTokens: 2, ChargedCents: 5, HasChargedCents: true,
		},
	})
	if _, _, ok := matchUsage(usage, ambiguous); ok {
		t.Fatal("ambiguous placeholder match must fail closed")
	}
}

func TestEnrichUsageCostsHappyPathNoAuthMe(t *testing.T) {
	t.Parallel()

	start := time.UnixMilli(1_700_000_000_000)
	end := start.Add(30 * time.Second)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/me", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("auth/me must not be required for usage-events")
	})
	mux.HandleFunc("/api/dashboard/get-filtered-usage-events", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if _, ok := body["userId"]; ok {
			t.Fatal("userId must be omitted")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalUsageEventsCount": 1,
			"usageEventsDisplay": []map[string]any{
				{
					"timestamp":    start.Add(5 * time.Second).UnixMilli(),
					"model":        "composer-1",
					"isHeadless":   true,
					"isChargeable": true,
					"chargedCents": 12.34,
					"tokenUsage": map[string]any{
						"inputTokens":      10,
						"outputTokens":     4,
						"cacheReadTokens":  1,
						"cacheWriteTokens": 2,
						"totalCents":       99,
					},
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	e := &Enricher{
		Client:           &Client{BaseURL: srv.URL, HTTPClient: srv.Client()},
		ReadSessionToken: func() (string, error) { return "user_01ABC%3A%3Afake", nil },
		Attempts:         2,
		Sleep:            func(context.Context, time.Duration) error { return nil },
	}
	out := e.EnrichUsageCosts(context.Background(), "task-1", start, end, []TaskUsage{{
		Model:            "composer-1",
		InputTokens:      10,
		OutputTokens:     4,
		CacheReadTokens:  1,
		CacheWriteTokens: 2,
	}})
	if len(out) != 1 || !out[0].HasCostUSDTicks {
		t.Fatalf("expected authoritative cost: %#v", out)
	}
	if out[0].AccountKey != OpaqueClaimKey("user_01ABC") || len(out[0].OccurrenceKeys) != 1 {
		t.Fatalf("expected opaque account/occurrence metadata: %#v", out[0])
	}
	if out[0].AccountKey == "user_01ABC" || strings.Contains(out[0].OccurrenceKeys[0], "composer-1") {
		t.Fatalf("raw Cursor identity leaked into claim keys: %#v", out[0])
	}
	want := CentsToUSDTicks(12.34)
	if out[0].CostUSDTicks != want {
		t.Fatalf("CostUSDTicks=%d want %d (must use chargedCents, not totalCents)", out[0].CostUSDTicks, want)
	}
}

func TestEnrichUsageCostsRejectsCandidateThatBecomesAmbiguous(t *testing.T) {
	t.Parallel()

	start := time.UnixMilli(1_700_000_200_000)
	end := start.Add(30 * time.Second)
	var calls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc(filteredUsageEventsPath, func(w http.ResponseWriter, r *http.Request) {
		events := []map[string]any{{
			"timestamp":    start.Add(5 * time.Second).UnixMilli(),
			"model":        "composer-1",
			"chargedCents": 4,
			"tokenUsage":   map[string]any{"inputTokens": 10, "outputTokens": 2},
		}}
		if calls.Add(1) >= 2 {
			events = append(events, map[string]any{
				"timestamp":    start.Add(6 * time.Second).UnixMilli(),
				"model":        "composer-1",
				"chargedCents": 9,
				"tokenUsage":   map[string]any{"inputTokens": 10, "outputTokens": 2},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalUsageEventsCount": len(events),
			"usageEventsDisplay":    events,
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	e := &Enricher{
		Client:           &Client{BaseURL: srv.URL, HTTPClient: srv.Client()},
		ReadSessionToken: func() (string, error) { return "user_01ABC%3A%3Afake", nil },
		Attempts:         2,
		Sleep:            func(context.Context, time.Duration) error { return nil },
	}
	out := e.EnrichUsageCosts(context.Background(), "task-lag", start, end, []TaskUsage{{
		Model: "composer-1", InputTokens: 10, OutputTokens: 2,
	}})
	if out[0].HasCostUSDTicks {
		t.Fatalf("a first-snapshot candidate that becomes ambiguous must fail closed: %#v", out[0])
	}
}

func TestEnrichUsageCostsWaitsForPaddedWindowToClose(t *testing.T) {
	now := time.Now()
	var sleeps []time.Duration

	mux := http.NewServeMux()
	mux.HandleFunc(filteredUsageEventsPath, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalUsageEventsCount": 1,
			"usageEventsDisplay": []map[string]any{{
				"timestamp": now.UnixMilli(), "model": "composer-1", "chargedCents": 1,
				"tokenUsage": map[string]any{"inputTokens": 1, "outputTokens": 1},
			}},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	e := &Enricher{
		Client:           &Client{BaseURL: srv.URL, HTTPClient: srv.Client()},
		ReadSessionToken: func() (string, error) { return "user_01ABC%3A%3Afake", nil },
		Attempts:         2,
		Sleep: func(_ context.Context, d time.Duration) error {
			sleeps = append(sleeps, d)
			return nil
		},
	}
	out := e.EnrichUsageCosts(context.Background(), "task-window", now.Add(-time.Second), now, []TaskUsage{{
		Model: "composer-1", InputTokens: 1, OutputTokens: 1,
	}})
	if !out[0].HasCostUSDTicks {
		t.Fatalf("stable event should reconcile after the window closes: %#v", out[0])
	}
	if len(sleeps) < 2 || sleeps[0] < 14*time.Second {
		t.Fatalf("sleep calls=%v, first call must wait for the +15s window", sleeps)
	}
}

func TestEnrichUsageCostsRetriesUntilAllModelsMatched(t *testing.T) {
	t.Parallel()

	start := time.UnixMilli(1_700_000_100_000)
	end := start.Add(30 * time.Second)
	var calls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/api/dashboard/get-filtered-usage-events", func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		events := []map[string]any{{
			"timestamp":    start.Add(5 * time.Second).UnixMilli(),
			"model":        "composer-1",
			"isHeadless":   true,
			"chargedCents": 10,
			"tokenUsage":   map[string]any{"inputTokens": 10, "outputTokens": 2},
		}}
		if n >= 2 {
			events = append(events, map[string]any{
				"timestamp":    start.Add(6 * time.Second).UnixMilli(),
				"model":        "gpt-5",
				"isHeadless":   true,
				"chargedCents": 20,
				"tokenUsage":   map[string]any{"inputTokens": 8, "outputTokens": 3},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalUsageEventsCount": len(events),
			"usageEventsDisplay":    events,
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	e := &Enricher{
		Client:           &Client{BaseURL: srv.URL, HTTPClient: srv.Client()},
		ReadSessionToken: func() (string, error) { return "user_01ABC%3A%3Afake", nil },
		Attempts:         3,
		Sleep:            func(context.Context, time.Duration) error { return nil },
	}
	out := e.EnrichUsageCosts(context.Background(), "task-multi", start, end, []TaskUsage{
		{Model: "composer-1", InputTokens: 10, OutputTokens: 2},
		{Model: "gpt-5", InputTokens: 8, OutputTokens: 3},
	})
	if calls.Load() < 2 {
		t.Fatalf("expected retry after partial match, calls=%d", calls.Load())
	}
	if !out[0].HasCostUSDTicks || !out[1].HasCostUSDTicks {
		t.Fatalf("both models should reconcile across attempts: %#v", out)
	}
	if out[0].CostUSDTicks != CentsToUSDTicks(10) || out[1].CostUSDTicks != CentsToUSDTicks(20) {
		t.Fatalf("unexpected costs: %#v", out)
	}
	if out[0].OccurrenceKeys[0] == out[1].OccurrenceKeys[0] {
		t.Fatal("models must not share the same occurrence key")
	}
}

func TestEnabledRequiresExplicitOptIn(t *testing.T) {
	t.Setenv(envCursorDashboardUsage, "")
	if Enabled() {
		t.Fatal("empty value must stay disabled")
	}
	t.Setenv(envCursorDashboardUsage, "true")
	if Enabled() {
		t.Fatal("only the documented value 1 enables Dashboard access")
	}
	t.Setenv(envCursorDashboardUsage, "1")
	if !Enabled() {
		t.Fatal("value 1 must enable Dashboard access")
	}
}

func TestAccountKeyFromSessionToken(t *testing.T) {
	t.Parallel()
	if got := AccountKeyFromSessionToken("user_01ABC%3A%3Ajwt"); got != "user_01ABC" {
		t.Fatalf("got %q", got)
	}
	if got := AccountKeyFromSessionToken("user_01ABC::jwt"); got != "user_01ABC" {
		t.Fatalf("got %q", got)
	}
}
