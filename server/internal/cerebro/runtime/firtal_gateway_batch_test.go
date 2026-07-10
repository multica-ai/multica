package runtime

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestGatewayBatchEligibilityRequiresExplicitToollessRound(t *testing.T) {
	eligible := db.AgentTaskQueue{Context: json.RawMessage(`{"type":"inbox_round","run_id":"r1","batch_tool_mode":"none"}`)}
	if !gatewayBatchTaskEligible(eligible, false) {
		t.Fatal("explicit tool-less round should be eligible")
	}
	for name, task := range map[string]db.AgentTaskQueue{
		"ordinary task":               {},
		"round without explicit mode": {Context: json.RawMessage(`{"type":"inbox_round","run_id":"r1"}`)},
		"coding mode":                 {Context: json.RawMessage(`{"type":"inbox_round","run_id":"r1","batch_tool_mode":"tools"}`)},
	} {
		if gatewayBatchTaskEligible(task, false) {
			t.Fatalf("%s must use synchronous execution", name)
		}
	}
	if gatewayBatchTaskEligible(eligible, true) {
		t.Fatal("a task with callable tools must use synchronous execution")
	}
}

func TestCompleteBatchRequiresModelCapability(t *testing.T) {
	var creates atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ai/proxy/v1/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-test","firtal_supports_batch":false}]}`))
			return
		}
		creates.Add(1)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewGatewayClient(FirtalGatewayRuntimeConfig{BaseURL: srv.URL, APIKey: "rk_test", Model: "claude-test"}, srv.Client())
	_, accepted, err := c.CompleteBatch(context.Background(), "claude-test", []GatewayMessage{{Role: "user", Content: "hello"}}, GatewayRequestMeta{}, time.Millisecond)
	if err == nil || accepted {
		t.Fatalf("unsupported model: accepted=%v err=%v", accepted, err)
	}
	if creates.Load() != 0 {
		t.Fatal("batch create must not be called without capability")
	}
}

func TestCompleteBatchSubmitsPollsAndParsesResult(t *testing.T) {
	var polls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ai/proxy/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-test","firtal_supports_batch":true}]}`))
		case "/api/ai/proxy/v1/messages/batches":
			_, _ = w.Write([]byte(`{"id":"batch_1","processing_status":"in_progress"}`))
		case "/api/ai/proxy/v1/messages/batches/batch_1":
			polls.Add(1)
			_, _ = w.Write([]byte(`{"id":"batch_1","processing_status":"ended"}`))
		case "/api/ai/proxy/v1/messages/batches/batch_1/results":
			_, _ = w.Write([]byte(`{"custom_id":"task-1","result":{"type":"succeeded","message":{"model":"claude-test","content":[{"type":"text","text":"batch answer"}],"usage":{"input_tokens":10,"output_tokens":3}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewGatewayClient(FirtalGatewayRuntimeConfig{BaseURL: srv.URL, APIKey: "rk_test", Model: "claude-test", MaxTokens: 100}, srv.Client())
	got, accepted, err := c.CompleteBatch(context.Background(), "claude-test", []GatewayMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "hello"}}, GatewayRequestMeta{TaskID: "task-1"}, time.Millisecond)
	if err != nil || !accepted {
		t.Fatalf("CompleteBatch: accepted=%v err=%v", accepted, err)
	}
	if got.Output != "batch answer" || got.Usage.InputTokens != 10 || got.Usage.OutputTokens != 3 {
		t.Fatalf("completion = %#v", got)
	}
	if polls.Load() == 0 {
		t.Fatal("batch status was not polled")
	}
}

func TestCompleteBatchCancelsAcceptedJobOnTimeout(t *testing.T) {
	var cancelled atomic.Bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/ai/proxy/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-test","firtal_supports_batch":true}]}`))
		case r.URL.Path == "/api/ai/proxy/v1/messages/batches":
			_, _ = w.Write([]byte(`{"id":"batch_1","processing_status":"in_progress"}`))
		case r.URL.Path == "/api/ai/proxy/v1/messages/batches/batch_1" && r.Method == http.MethodDelete:
			cancelled.Store(true)
			_, _ = w.Write([]byte(`{"id":"batch_1","processing_status":"canceling"}`))
		case r.URL.Path == "/api/ai/proxy/v1/messages/batches/batch_1":
			_, _ = w.Write([]byte(`{"id":"batch_1","processing_status":"in_progress"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewGatewayClient(FirtalGatewayRuntimeConfig{BaseURL: srv.URL, APIKey: "rk_test", Model: "claude-test"}, srv.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, accepted, err := c.CompleteBatch(ctx, "claude-test", []GatewayMessage{{Role: "user", Content: "hello"}}, GatewayRequestMeta{TaskID: "task-1"}, time.Millisecond)
	if err == nil || !accepted || !cancelled.Load() {
		t.Fatalf("timeout: accepted=%v cancelled=%v err=%v", accepted, cancelled.Load(), err)
	}
}

func TestToollessRoundFallsBackToSynchronousWhenCapabilityLookupFails(t *testing.T) {
	var syncCalls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ai/proxy/v1/models":
			http.Error(w, "catalog unavailable", http.StatusServiceUnavailable)
		case "/api/ai/proxy/v1/chat/completions":
			syncCalls.Add(1)
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"sync answer"}}],"usage":{"prompt_tokens":4,"completion_tokens":2}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := FirtalGatewayRuntimeConfig{BaseURL: srv.URL, APIKey: "rk_test", Model: "claude-test", EntryMode: FirtalGatewayEntryModeCompat}
	client := NewGatewayClient(cfg, srv.Client())
	e := &FirtalGatewayExecutor{gateway: client, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	task := db.AgentTaskQueue{Context: json.RawMessage(`{"type":"inbox_round","run_id":"r1","batch_tool_mode":"none"}`)}
	got, err := e.completeGatewayToolless(context.Background(), cfg, task, "claude-test", []GatewayMessage{{Role: "user", Content: "hello"}}, GatewayRequestMeta{TaskID: "task-1"})
	if err != nil {
		t.Fatalf("completeGatewayToolless: %v", err)
	}
	if got.Output != "sync answer" || syncCalls.Load() != 1 {
		t.Fatalf("completion=%#v syncCalls=%d", got, syncCalls.Load())
	}
}
