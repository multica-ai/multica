package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/cursorusage"
)

func TestCursorCostReconcilerUsesCostOnlyEndpoint(t *testing.T) {
	t.Setenv("MULTICA_CURSOR_DASHBOARD_USAGE", "1")
	start := time.UnixMilli(1_700_000_100_000)
	end := start.Add(20 * time.Second)

	var usageReports atomic.Int64
	var costReports atomic.Int64
	var costAttempts atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/api/dashboard/get-filtered-usage-events", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalUsageEventsCount": 1,
			"usageEventsDisplay": []map[string]any{
				{
					"timestamp":    start.Add(time.Second).UnixMilli(),
					"model":        "composer-1",
					"isHeadless":   true,
					"chargedCents": 0,
					"tokenUsage": map[string]any{
						"inputTokens":  3,
						"outputTokens": 2,
						"totalCents":   50,
					},
				},
			},
		})
	})
	mux.HandleFunc("/api/daemon/tasks/task-async-1/usage", func(w http.ResponseWriter, r *http.Request) {
		usageReports.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/api/daemon/tasks/task-async-1/usage/cost", func(w http.ResponseWriter, r *http.Request) {
		attempt := costAttempts.Add(1)
		var req struct {
			AccountKey  string                      `json:"account_key"`
			Corrections []CursorUsageCostCorrection `json:"corrections"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.AccountKey != cursorusage.OpaqueClaimKey("user_x") || len(req.Corrections) != 1 {
			t.Fatalf("unexpected cost payload: %#v", req)
		}
		if req.Corrections[0].CostUSDTicks != 0 {
			t.Fatalf("expected authoritative zero: %#v", req.Corrections[0])
		}
		if len(req.Corrections[0].OccurrenceKeys) == 0 {
			t.Fatal("missing occurrence keys")
		}
		if attempt == 1 {
			http.Error(w, "transient", http.StatusServiceUnavailable)
			return
		}
		costReports.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL)
	r := newCursorCostReconciler(client, slog.Default())
	r.enrich = &cursorusage.Enricher{
		Client:           &cursorusage.Client{BaseURL: srv.URL, HTTPClient: srv.Client()},
		ReadSessionToken: func() (string, error) { return "user_x%3A%3Atok", nil },
		Attempts:         2,
		Sleep:            func(context.Context, time.Duration) error { return nil },
		Logger:           slog.Default(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.start(ctx)

	r.enqueue(cursorCostJob{
		taskID: "task-async-1",
		start:  start,
		end:    end,
		usage: []TaskUsageEntry{{
			Provider:     "cursor",
			Model:        "composer-1",
			InputTokens:  3,
			OutputTokens: 2,
		}},
	})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if costReports.Load() == 1 {
			if costAttempts.Load() != 2 {
				t.Fatalf("cost correction attempts=%d want 2", costAttempts.Load())
			}
			if usageReports.Load() != 0 {
				t.Fatalf("cost reconcile must not call full usage endpoint, got %d", usageReports.Load())
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for async cost report")
}

func TestEnqueueCursorCostAfterInitialUsageReport(t *testing.T) {
	t.Setenv("MULTICA_CURSOR_DASHBOARD_USAGE", "1")
	var mu sync.Mutex
	var order []string
	eventTime := time.Now()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/daemon/tasks/task-order/usage", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		order = append(order, "usage")
		mu.Unlock()
		// Hold the response so a premature worker cannot finish first.
		time.Sleep(80 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/api/daemon/tasks/task-order/usage/cost", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		order = append(order, "cost")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/api/dashboard/get-filtered-usage-events", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalUsageEventsCount": 1,
			"usageEventsDisplay": []map[string]any{
				{
					"timestamp":    eventTime.UnixMilli(),
					"model":        "composer-1",
					"isHeadless":   true,
					"chargedCents": 1,
					"tokenUsage":   map[string]any{"inputTokens": 1, "outputTokens": 1},
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL)
	d := &Daemon{
		client:      client,
		logger:      slog.Default(),
		cursorCosts: newCursorCostReconciler(client, slog.Default()),
	}
	d.cursorCosts.enrich = &cursorusage.Enricher{
		Client:           &cursorusage.Client{BaseURL: srv.URL, HTTPClient: srv.Client()},
		ReadSessionToken: func() (string, error) { return "user_x%3A%3Atok", nil },
		Attempts:         2,
		Sleep:            func(context.Context, time.Duration) error { return nil },
		Logger:           slog.Default(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d.cursorCosts.start(ctx)

	result := TaskResult{
		Usage: []TaskUsageEntry{{
			Provider: "cursor", Model: "composer-1", InputTokens: 1, OutputTokens: 1,
		}},
		ShouldReconcileCursorCost: true,
		CursorCostWindowStart:     time.Now().Add(-time.Minute),
		CursorCostWindowEnd:       time.Now(),
	}

	// Mirror handleTask ordering: report usage, and only then enqueue.
	if err := d.client.ReportTaskUsage(ctx, "task-order", result.Usage); err != nil {
		t.Fatal(err)
	}
	d.cursorCosts.enqueue(cursorCostJob{
		taskID: "task-order",
		start:  result.CursorCostWindowStart,
		end:    result.CursorCostWindowEnd,
		usage:  append([]TaskUsageEntry(nil), result.Usage...),
	})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := len(order) >= 2
		snapshot := append([]string(nil), order...)
		mu.Unlock()
		if done {
			if snapshot[0] != "usage" || snapshot[1] != "cost" {
				t.Fatalf("order=%v want [usage cost]", snapshot)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out, order=%v", order)
}

func TestNeedsCursorCostEnrichmentSkipsPresent(t *testing.T) {
	t.Parallel()
	if needsCursorCostEnrichment([]TaskUsageEntry{{
		Model: "composer-1", InputTokens: 1, CostUSDTicksPresent: true, CostUSDTicks: 0,
	}}) {
		t.Fatal("already-present cost should not re-enrich")
	}
	if !needsCursorCostEnrichment([]TaskUsageEntry{{
		Model: "cursor", InputTokens: 1,
	}}) {
		t.Fatal("placeholder model without cost should enrich")
	}
}
