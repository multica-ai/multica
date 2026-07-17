package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenAIEUExecuteEmitsSingleCallUsageEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"done"}}],"usage":{"prompt_tokens":21,"completion_tokens":6}}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OPENAI_EU_ENDPOINT", srv.URL)
	t.Setenv("OPENAI_EU_API_KEY", "test-key")

	backend := &openaiEUBackend{cfg: Config{Env: map[string]string{"MULTICA_TASK_ID": "eu-task"}}}
	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{Model: "gpt-4o", Timeout: time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if len(result.UsageEvents) != 1 {
		t.Fatalf("UsageEvents = %+v, want one OpenAI EU call event", result.UsageEvents)
	}
	event := result.UsageEvents[0]
	if event.EventID != "openai-eu:eu-task:call:1" {
		t.Fatalf("event ID = %q", event.EventID)
	}
	if event.Provider != openaiEUProvider || event.Model != "gpt-4o" || event.InputTokens != 21 || event.OutputTokens != 6 {
		t.Fatalf("usage event = %+v", event)
	}
	if event.ContextTokens != 21 || event.Source != ModelUsageSourceFinalResponse || event.Completeness != ModelUsageTokensOnly || event.CounterSemantics != ModelUsageCounterDelta {
		t.Fatalf("usage event semantics = %+v", event)
	}
}
