package qoderruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func spanEvent(id string, credits float64) map[string]any {
	return map[string]any{"id": id, "type": "span.model_request_end", "model_usage": map[string]any{"credits": credits}}
}

func (h *harness) runWith(t *testing.T, events ...map[string]any) {
	t.Helper()
	ctx := context.Background()
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	h.events = append(h.events, events...)
	h.finish("end_turn")
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if h.complete == nil {
		t.Fatal("run did not complete")
	}
}

func (h *harness) wantCredits(t *testing.T, micros int64) {
	t.Helper()
	if len(h.usage) != 1 {
		t.Fatalf("usage reports = %#v, want one", h.usage)
	}
	u := h.usage[0]
	if u["provider"] != "qoder_cloud" || u["credits_micros"] != float64(micros) {
		t.Fatalf("usage = %#v, want qoder_cloud %d credits_micros", u, micros)
	}
	if h.usageAfterFinish {
		t.Fatal("usage was reported after the task finished")
	}
}

func TestCreditsFallBackToModelSpans(t *testing.T) {
	h := setup(t)
	h.runWith(t, spanEvent("evt_span_1", 0.03), spanEvent("evt_span_2", 0.02))
	h.wantCredits(t, 50000)
}

func TestSessionCreditsOverrideSpans(t *testing.T) {
	h := setup(t)
	h.sessionUsage = `{"model_credits":0.35,"sandbox_runtime_credits":0.05,"total_credits":0.4}`
	h.runWith(t,
		spanEvent("evt_span_1", 0.03),
		map[string]any{"id": "evt_usage", "type": "session.usage", "model_credits": 0.35, "sandbox_runtime_credits": 0, "total_credits": 0.35},
	)
	h.wantCredits(t, 400000)
}

func TestSessionUsageEventUsedWhenSessionHasNoTotal(t *testing.T) {
	h := setup(t)
	h.runWith(t,
		spanEvent("evt_span_1", 0.03),
		map[string]any{"id": "evt_usage", "type": "session.usage", "total_credits": 0.35},
	)
	h.wantCredits(t, 350000)
}

func TestNoCreditsNoUsageReport(t *testing.T) {
	h := setup(t)
	h.runWith(t)
	if len(h.usage) != 0 {
		t.Fatalf("unexpected usage report: %#v", h.usage)
	}
}

func TestTerminalTaskReportsCredits(t *testing.T) {
	for _, status := range []string{"cancelled", "failed", "completed"} {
		t.Run(status, func(t *testing.T) {
			h := setup(t)
			if err := h.b.step(context.Background()); err != nil {
				t.Fatal(err)
			}
			h.sessionUsage = `{"total_credits":0.4}`
			h.status = status
			if err := h.b.step(context.Background()); err != nil {
				t.Fatal(err)
			}
			h.wantCredits(t, 400000)
			if h.cancels != 1 || h.b.state.first() != nil || h.complete != nil || h.failure != nil {
				t.Fatal("terminal task was not cleaned up without redelivering its result")
			}
		})
	}
}

func TestSessionCreditsRequireContinuationBaseline(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want *int64
	}{
		{"new session", `{}`, new(int64(1250000))},
		{"known zero", `{"continued":true,"credits":{"base":0}}`, new(int64(1250000))},
		{"known prior usage", `{"continued":true,"credits":{"base":500000}}`, new(int64(750000))},
		{"missing baseline", `{"continued":true,"credits":{}}`, nil},
		{"journal before credits", `{"continued":true}`, nil},
		{"unreliable old snapshot", `{"continued":true,"credits":{"session":1250000}}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r run
			if err := json.Unmarshal([]byte(tc.raw), &r); err != nil {
				t.Fatal(err)
			}
			// Recovery must preserve an explicitly known zero baseline too.
			raw, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			r = run{}
			if err := json.Unmarshal(raw, &r); err != nil {
				t.Fatal(err)
			}
			r.setSessionCredits(1.25)
			if tc.want == nil {
				if r.Credits.Session != nil {
					t.Fatalf("session history charged without a baseline: %d", *r.Credits.Session)
				}
			} else if r.Credits.Session == nil || *r.Credits.Session != *tc.want {
				t.Fatalf("session delta = %v, want %d", r.Credits.Session, *tc.want)
			}
		})
	}
}

func TestContinuedRunWithoutBaselineReportsOnlyCurrentSpans(t *testing.T) {
	h := setup(t)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.b.state.first().Continued = true
	h.sessionUsage = `{"total_credits":1.25}`
	h.events = append(h.events, spanEvent("evt_span", 0.05))
	h.finish("end_turn")
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.wantCredits(t, 50000)
}

func TestCancelledCreditsRetryBeforeAcknowledging(t *testing.T) {
	h := setup(t)
	ctx := context.Background()
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	h.status, h.sessionUsage, h.usageStatus = "cancelled", `{"total_credits":0.4}`, http.StatusServiceUnavailable
	if err := h.b.step(ctx); err == nil {
		t.Fatal("failed usage report was not retried")
	}
	if h.cancelAcks != 0 || h.b.state.first() == nil {
		t.Fatal("usage was discarded before a successful report")
	}
	h.usageStatus, h.cancelAckStatus = 0, http.StatusServiceUnavailable
	if err := h.b.step(ctx); err == nil {
		t.Fatal("failed cancellation acknowledgement was not retried")
	}
	h.wantCredits(t, 400000)
	// Recover after reporting but before acknowledgement: do not re-report
	// usage or lose the pending cancellation acknowledgement.
	var err error
	h.b.state, err = readJournal(h.b.path, "test")
	if err != nil {
		t.Fatal(err)
	}
	h.cancelAckStatus = 0
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	h.wantCredits(t, 400000)
	if h.cancelAcks != 2 || h.b.state.first() != nil {
		t.Fatal("cancellation acknowledgement did not recover")
	}
}

func TestContinuedSessionReportsOnlyItsOwnCredits(t *testing.T) {
	h := setup(t)
	var events []map[string]any
	var metadata map[string]string
	total := 0.0
	posts := 0
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/sessions" && r.Method == http.MethodPost:
			var req struct {
				Metadata map[string]string `json:"metadata"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			metadata = req.Metadata
			json.NewEncoder(w).Encode(map[string]string{"id": "sess_1"})
		case r.URL.Path == "/sessions/sess_1" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"status": "idle", "agent": "agent_1", "environment_id": "env_1", "metadata": metadata, "usage": map[string]any{"total_credits": total}})
		case r.URL.Path == "/sessions/sess_1/events" && r.Method == http.MethodPost:
			var req struct {
				Events []map[string]any `json:"events"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
				return
			}
			posts++
			req.Events[0]["id"] = fmt.Sprint("evt_user_", posts)
			events = append(events, req.Events[0])
			json.NewEncoder(w).Encode(map[string]any{"data": req.Events})
		case r.URL.Path == "/sessions/sess_1/events" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(eventListPage(r, events))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer q.Close()
	h.b.qoder.base = q.URL
	ctx := context.Background()
	turn := func(n int) {
		t.Helper()
		if err := h.b.step(ctx); err != nil {
			t.Fatalf("turn %d: %v", n, err)
		}
		total += 0.25
		events = append(events, map[string]any{"id": fmt.Sprint("evt_idle_", n), "type": "session.status_idle", "stop_reason": map[string]string{"type": "end_turn"}})
		if err := h.b.step(ctx); err != nil {
			t.Fatalf("finish turn %d: %v", n, err)
		}
		if h.complete == nil {
			t.Fatalf("turn %d did not complete", n)
		}
	}
	turn(1)
	h.wantCredits(t, 250000)

	h.task = newTask("task-2", "sess_1")
	h.task.IssueID = "issue-1"
	h.status, h.complete, h.usage = "dispatched", nil, nil
	turn(2)
	if run := h.b.state.first(); run != nil {
		t.Fatalf("run still active: %#v", run)
	}
	h.wantCredits(t, 250000)
}
