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

	if got := selectTaskWarmPool(pool, false, false); got != pool {
		t.Fatalf("selectTaskWarmPool(pool, false, false) = %v, want %v", got, pool)
	}
	if got := selectTaskWarmPool(pool, true, false); got != nil {
		t.Fatalf("selectTaskWarmPool(pool, true, false) = %v, want nil for custom profile command", got)
	}
	if got := selectTaskWarmPool(pool, false, true); got != nil {
		t.Fatalf("selectTaskWarmPool(pool, false, true) = %v, want nil for worktree mode", got)
	}
	if got := selectTaskWarmPool(nil, false, false); got != nil {
		t.Fatalf("selectTaskWarmPool(nil, false, false) = %v, want nil", got)
	}
}
