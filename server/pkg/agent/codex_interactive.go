package agent

import (
	"context"
	"fmt"
	"time"
)

func (c *codexClient) resetInteractiveTurn(gate *codexTurnNotificationGate) {
	c.notificationMu.Lock()
	defer c.notificationMu.Unlock()
	gate.previousTurnID = c.activeTurnID()
	gate.strictNextTurn = true
	gate.started, gate.turnID = false, ""
	c.turnCompleted = false
	c.agentMessageStreams = nil
	c.agentMessageOrder = nil
	c.toolProgress = nil
	c.turnErrorMu.Lock()
	c.turnError = ""
	c.turnErrorMu.Unlock()
	// Thread tokenUsage total is cumulative across activities. Keep the last
	// accepted snapshot so the next turn adds only the new usage delta.
}

func interruptInteractiveCodexTurn(ctx context.Context, c *codexClient, threadID string, done <-chan bool, timeout time.Duration) (bool, error) {
	select {
	case aborted := <-done:
		return aborted, nil
	default:
	}
	if timeout <= 0 {
		timeout = defaultCodexTurnInterruptTimeout
	}
	interruptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, err := c.request(interruptCtx, "turn/interrupt", map[string]any{"threadId": threadID, "turnId": c.activeTurnID()})
	if err != nil {
		// Completion can win just before the native request is handled.
		select {
		case aborted := <-done:
			return aborted, nil
		default:
		}
		return false, fmt.Errorf("codex interrupt: %w", err)
	}
	select {
	case aborted := <-done:
		return aborted, nil
	case <-interruptCtx.Done():
		return false, fmt.Errorf("codex interrupt outcome unconfirmed: %w", interruptCtx.Err())
	case <-c.processDone:
		return false, errCodexProcessExited
	}
}
