package runtime

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestToollessCompletionUsesSynchronousGateway(t *testing.T) {
	var batchCalls, syncCalls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ai/proxy/v1/messages/batches" || r.URL.Path == "/api/ai/proxy/v1/models" {
			batchCalls.Add(1)
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-test","firtal_supports_batch":true}]}`))
			return
		}
		if r.URL.Path == "/api/ai/proxy/v1/chat/completions" {
			syncCalls.Add(1)
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"sync answer"}}],"usage":{"prompt_tokens":4,"completion_tokens":2}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfg := FirtalGatewayRuntimeConfig{BaseURL: srv.URL, APIKey: "rk_test", Model: "claude-test", EntryMode: FirtalGatewayEntryModeCompat}
	e := &FirtalGatewayExecutor{gateway: NewGatewayClient(cfg, srv.Client()), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	task := db.AgentTaskQueue{}
	got, err := e.completeGatewayToolless(context.Background(), cfg, task, "claude-test", []GatewayMessage{{Role: "user", Content: "hello"}}, GatewayRequestMeta{TaskID: "task-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Output != "sync answer" || syncCalls.Load() != 1 || batchCalls.Load() != 0 {
		t.Fatalf("completion=%#v syncCalls=%d batchCalls=%d", got, syncCalls.Load(), batchCalls.Load())
	}
}
