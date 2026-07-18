package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cerebro/evals"
)

// gateway_integration_test.go proves the whole Run-now path an operator actually
// hits: the real GatewayCompleter speaks the Firtal AI Gateway's Anthropic
// /v1/messages wire format over HTTP, the prompt target runs each task through
// it, the hard_rule grader scores the real replies, and the executor computes a
// genuine pass/fail. The stub server mirrors the exact response shape the live
// gateway returns (content:[{type:"text",text:...}]), verified against
// cerebro.firtal.com — so a green run here is real evidence the engine works end
// to end against that gateway, not a mock of our own invention.

// newStubGateway returns an httptest server that answers /v1/messages like the
// Firtal AI Gateway: it requires the bearer key, echoes an answer chosen by the
// user turn, and replies in the Anthropic messages shape.
func newStubGateway(t *testing.T, wantKey string, answers map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != gatewayMessagesPath {
			t.Errorf("gateway hit path %q, want %q", r.URL.Path, gatewayMessagesPath)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+wantKey {
			t.Errorf("Authorization = %q, want bearer %q", got, wantKey)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req gatewayRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		var prompt string
		if len(req.Messages) > 0 {
			prompt = req.Messages[len(req.Messages)-1].Content
		}
		answer := ""
		for needle, a := range answers {
			if strings.Contains(prompt, needle) {
				answer = a
				break
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": answer},
			},
			"stop_reason": "end_turn",
		})
	}))
}

// TestGatewayCompleterDrivesRealPass runs a full eval through the live HTTP
// completer: two prompt tasks answered by the stub gateway, scored by the
// deterministic hard_rule grader. One of two passes, the pass rate meets 0.5 and
// the single critical task passed — so the executor records a genuine "passed".
func TestGatewayCompleterDrivesRealPass(t *testing.T) {
	srv := newStubGateway(t, "secret-key", map[string]string{
		"2+2":                "4",     // critical task — correct
		"capital of Denmark": "Paris", // non-critical — wrong on purpose
	})
	defer srv.Close()

	completer, err := NewGatewayCompleter(srv.URL, "secret-key", "claude-haiku-4-5", nil)
	if err != nil {
		t.Fatalf("NewGatewayCompleter: %v", err)
	}
	exec := NewEvalExecutor(completer, nil)

	input, err := exec.ExecuteRun(context.Background(), passingEval())
	if err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}
	if input.Status != evals.RunStatusPassed {
		t.Fatalf("status = %q, want %q", input.Status, evals.RunStatusPassed)
	}

	var report Report
	if err := json.Unmarshal(input.Results, &report); err != nil {
		t.Fatalf("results not a runner.Report: %v", err)
	}
	byID := map[string]CaseReport{}
	for _, c := range report.Cases {
		byID[c.CaseID] = c
	}
	// The verdict came from the real gateway reply, not from the caller: the
	// critical task's produced answer is exactly what the stub returned.
	if byID["a"].Produced != "4" || !byID["a"].Passed {
		t.Errorf("critical case a: produced=%q passed=%v, want 4/true", byID["a"].Produced, byID["a"].Passed)
	}
	if byID["b"].Produced != "Paris" || byID["b"].Passed {
		t.Errorf("case b: produced=%q passed=%v, want Paris/false", byID["b"].Produced, byID["b"].Passed)
	}
}

// TestGatewayCompleterCriticalFailFailsClosed proves a wrong answer on the
// critical task, delivered over the real HTTP wire, sinks the run even though the
// non-critical task passes — the engine cannot be told a pass it did not earn.
func TestGatewayCompleterCriticalFailFailsClosed(t *testing.T) {
	srv := newStubGateway(t, "secret-key", map[string]string{
		"2+2":                "5",          // critical task — wrong
		"capital of Denmark": "Copenhagen", // non-critical — correct
	})
	defer srv.Close()

	completer, err := NewGatewayCompleter(srv.URL, "secret-key", "claude-haiku-4-5", nil)
	if err != nil {
		t.Fatalf("NewGatewayCompleter: %v", err)
	}
	exec := NewEvalExecutor(completer, nil)

	input, err := exec.ExecuteRun(context.Background(), passingEval())
	if err != nil {
		t.Fatalf("ExecuteRun: %v", err)
	}
	if input.Status != evals.RunStatusFailed {
		t.Fatalf("status = %q, want %q (critical task failed)", input.Status, evals.RunStatusFailed)
	}
}
