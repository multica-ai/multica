package daemon

import (
	"io"
	"log/slog"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestSelectTaskWarmPoolDisablesCustomProfileCommands(t *testing.T) {
	t.Parallel()

	pool := agent.NewWarmSessionPool(agent.WarmSessionPoolConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	defer pool.Close()

	if got := selectTaskWarmPool(pool, false); got != pool {
		t.Fatalf("selectTaskWarmPool(pool, false) = %v, want %v", got, pool)
	}
	if got := selectTaskWarmPool(pool, true); got != nil {
		t.Fatalf("selectTaskWarmPool(pool, true) = %v, want nil for custom profile command", got)
	}
	if got := selectTaskWarmPool(nil, false); got != nil {
		t.Fatalf("selectTaskWarmPool(nil, false) = %v, want nil", got)
	}
}
