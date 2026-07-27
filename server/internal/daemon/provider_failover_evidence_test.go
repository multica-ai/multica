package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/providerfailover"
)

// td-836aa9: a terminal FAILURE disposition forwards the daemon-observed
// side-effect evidence on the /fail callback so the server can decide whether a
// cross-provider handoff is safe.
func TestReportTaskResult_ForwardsFailoverEvidence(t *testing.T) {
	t.Parallel()

	rec := &reportTaskResultRecorder{}
	srv := httptest.NewServer(rec.handler(t))
	t.Cleanup(srv.Close)

	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
	d.reportTaskResult(context.Background(), "task-ev", TaskResult{
		Status:           "blocked",
		Comment:          "rate limit reached",
		FailureReason:    "agent_error.provider_capacity_or_rate_limit",
		FailoverEvidence: &providerfailover.SideEffectEvidence{ObservedToolCalls: 2, PartialUserOutput: true, Complete: true},
	}, slog.Default())

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.path != "/api/daemon/tasks/task-ev/fail" {
		t.Fatalf("expected /fail endpoint, got %s", rec.path)
	}
	ev, ok := rec.payload["failover_evidence"].(map[string]any)
	if !ok {
		t.Fatalf("failover_evidence missing from fail body: %+v", rec.payload)
	}
	if ev["observed_tool_calls"].(float64) != 2 {
		t.Errorf("observed_tool_calls = %v, want 2", ev["observed_tool_calls"])
	}
	if ev["partial_user_output"] != true {
		t.Errorf("partial_user_output = %v, want true", ev["partial_user_output"])
	}
	if ev["complete"] != true {
		t.Errorf("complete = %v, want true", ev["complete"])
	}
}

// td-836aa9: a cancelled disposition never carries failover evidence — a
// cancelled run may have been interrupted mid-tool, and cancelled never triggers
// failover in the first place. The daemon must not report an evidence object
// (and thus never let a cancelled run claim a proven-empty surface).
func TestReportTaskResult_CancelledCarriesNoFailoverEvidence(t *testing.T) {
	t.Parallel()

	rec := &reportTaskResultRecorder{}
	srv := httptest.NewServer(rec.handler(t))
	t.Cleanup(srv.Close)

	d := &Daemon{client: NewClient(srv.URL), logger: slog.Default()}
	d.reportTaskResult(context.Background(), "task-cancel", TaskResult{
		Status:  "cancelled",
		Comment: "task cancelled by server",
		// No FailoverEvidence: runTask never attaches it on the cancelled path.
	}, slog.Default())

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.payload["failure_reason"] != "cancelled" {
		t.Fatalf("failure_reason = %v, want cancelled", rec.payload["failure_reason"])
	}
	if _, ok := rec.payload["failover_evidence"]; ok {
		t.Fatalf("cancelled run must not report failover evidence, got %+v", rec.payload["failover_evidence"])
	}
}

// td-836aa9: the direct-runner-error path (runTask returned an error, not a
// TaskResult) forwards evidence ONLY when runTask attached it — i.e. only when
// tool activity is known. A pre-execution failure carries no TaskResult and thus
// no evidence (nil), so the server keeps active failover fail-closed rather than
// falsely claiming a complete, empty side-effect surface. A backend-start
// failure carries complete zero-tool evidence because the run never streamed.
func TestHandleTask_DirectRunnerErrorEvidence(t *testing.T) {
	cases := []struct {
		name         string
		result       TaskResult
		wantEvidence bool
	}{
		{
			name:         "pre-execution error: tool activity unknown, no evidence",
			result:       TaskResult{},
			wantEvidence: false,
		},
		{
			name:         "backend start failure: zero-tool evidence is complete",
			result:       TaskResult{FailoverEvidence: &providerfailover.SideEffectEvidence{ObservedToolCalls: 0, Complete: true}},
			wantEvidence: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &reportTaskResultRecorder{}
			srv := httptest.NewServer(rec.handler(t))
			t.Cleanup(srv.Close)

			d := &Daemon{
				client:             NewClient(srv.URL),
				logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
				runtimeIndex:       map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "codex"}},
				cancelPollInterval: time.Hour, // keep the status poll from firing during the test
			}
			res := tc.result
			d.runner = taskRunnerFunc(func(_ context.Context, _ Task, _ string, _ int, _ *slog.Logger) (TaskResult, error) {
				return res, errors.New("runner exited")
			})

			d.handleTask(context.Background(), Task{ID: "task-dre", RuntimeID: "rt-1"}, 0)

			rec.mu.Lock()
			defer rec.mu.Unlock()
			if rec.path != "/api/daemon/tasks/task-dre/fail" {
				t.Fatalf("expected /fail endpoint, got %s", rec.path)
			}
			if _, ok := rec.payload["failover_evidence"]; ok != tc.wantEvidence {
				t.Fatalf("failover_evidence present = %v, want %v (payload=%+v)", ok, tc.wantEvidence, rec.payload)
			}
		})
	}
}

// td-836aa9: the client omits the evidence field entirely when nil, so an older
// server ignores nothing new and a newer server keeps active failover
// fail-closed (evidence never proven). A non-nil evidence is serialized.
func TestClientFailTask_EvidenceSerialization(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var lastBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		_ = json.Unmarshal(body, &lastBody)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL)

	if err := c.FailTask(context.Background(), "t1", "boom", "", "", "agent_error.provider_quota_limit", nil); err != nil {
		t.Fatalf("FailTask(nil evidence): %v", err)
	}
	mu.Lock()
	_, present := lastBody["failover_evidence"]
	mu.Unlock()
	if present {
		t.Fatal("nil evidence must be omitted from the fail body (backward compatibility)")
	}

	if err := c.FailTask(context.Background(), "t1", "boom", "", "", "agent_error.provider_quota_limit",
		&providerfailover.SideEffectEvidence{ObservedToolCalls: 1, PartialUserOutput: false, Complete: true}); err != nil {
		t.Fatalf("FailTask(evidence): %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	ev, ok := lastBody["failover_evidence"].(map[string]any)
	if !ok {
		t.Fatalf("failover_evidence missing: %+v", lastBody)
	}
	if ev["observed_tool_calls"].(float64) != 1 || ev["complete"] != true {
		t.Errorf("evidence body = %+v, want observed_tool_calls=1 complete=true", ev)
	}
}
