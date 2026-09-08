//go:build linux || darwin

package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestCursorBackgroundUncapturedDisabledToolWatchdog(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "cursor-agent")
	const script = `#!/bin/sh
cat >/dev/null
printf '%s\n' '{"type":"tool_call","subtype":"started","call_id":"bg","tool_call":{"shellToolCall":{"args":{}}}}'
printf '%s\n' '{"type":"tool_call","subtype":"completed","call_id":"bg","tool_call":{"shellToolCall":{"result":{"isBackground":true,"success":{"pid":-1}}}}}'
exec sleep 30
`
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	backend, err := agent.New("cursor", agent.Config{ExecutablePath: fake, Logger: slog.Default()})
	if err != nil {
		t.Fatal(err)
	}
	d := newTestDaemon(t)
	d.cfg.AgentIdleWatchdog = 50 * time.Millisecond
	d.cfg.AgentToolWatchdog = 0
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, _, err := d.executeAndDrain(ctx, backend, "test", agent.ExecOptions{}, slog.Default(), "uncaptured-background", "", new(atomic.Int32))
	if err != nil || result.Status != "idle_watchdog" || ctx.Err() != nil {
		t.Fatalf("uncaptured background escaped idle budget: result=%+v err=%v context=%v", result, err, ctx.Err())
	}
}
