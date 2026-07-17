package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestHermesPromptAggregateDoesNotEmitCallLevelUsageEvent(t *testing.T) {
	fakePath := filepath.Join(t.TempDir(), "hermes")
	writeTestExecutable(t, fakePath, []byte(fakeHermesACPUsageWithDefaultModelScript()))

	backend, err := New("hermes", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new hermes backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result

	if len(result.UsageEvents) != 0 {
		t.Fatalf("UsageEvents = %+v, Hermes ACP only returned aggregate prompt usage without native call identity", result.UsageEvents)
	}
	usage := result.Usage["nous:moonshotai/kimi-k2.6"]
	if usage.InputTokens != 17 || usage.OutputTokens != 5 || usage.CacheReadTokens != 3 {
		t.Fatalf("aggregate fallback usage = %+v, want input=17 output=5 cache_read=3", usage)
	}
}
