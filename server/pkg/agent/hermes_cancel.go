package agent

import (
	"context"
	"encoding/json"
	"time"
)

const hermesPromptCancelGrace = 2 * time.Second

// requestHermesTurn keeps the viewer transport alive long enough to cancel an
// admitted prompt. Killing a Hermes viewer alone does not stop its persistent
// owner. Other ACP backends keep their existing request/cancellation semantics.
// stopTransport must close stdin (unblocking writers) and cancel the owned
// process context; the caller still owns reaping and joining the pipe readers.
func requestHermesTurn(ctx, transportCtx context.Context, c *hermesClient, method string, params any, stopTransport func()) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type response struct {
		result json.RawMessage
		err    error
	}
	done := make(chan response, 1)
	sent := make(chan struct{})
	go func() {
		result, err := c.requestAndNotifySent(transportCtx, method, params, func() { close(sent) })
		done <- response{result, err}
	}()
	select {
	case res := <-done:
		return res.result, res.err
	case <-ctx.Done():
	}

	// Setup has no active prompt to cancel. Closing the pipe also bounds a
	// request blocked in Write, which a context timeout alone cannot interrupt.
	if method != "session/prompt" {
		stopTransport()
		<-done
		return nil, ctx.Err()
	}

	timer := time.NewTimer(hermesPromptCancelGrace)
	defer timer.Stop()
	select {
	case res := <-done:
		// A response already received wins over cancellation, including the
		// post-response notification drain/background learning period.
		return res.result, res.err
	case <-sent:
		// A cancel must never overtake the complete prompt frame.
	case <-timer.C:
		stopTransport()
		<-done
		return nil, ctx.Err()
	}

	data, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/cancel",
		"params":  map[string]any{"sessionId": params.(map[string]any)["sessionId"]},
	})
	type cancelWrite struct {
		err    error
		active bool
	}
	written := make(chan cancelWrite, 1)
	go func() {
		// Serialize the active check AND submission with response removal.
		// A completed prompt must not become cancellable again while this
		// goroutine waits for another transport writer. The timer below closes
		// stdin to release both locks if that writer or this write blocks.
		c.writeMu.Lock()
		defer c.writeMu.Unlock()
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, pending := range c.pending {
			if pending.method == "session/prompt" {
				_, err := c.stdin.Write(append(data, '\n'))
				written <- cancelWrite{err: err, active: true}
				return
			}
		}
		written <- cancelWrite{}
	}()
	select {
	case write := <-written:
		if !write.active {
			res := <-done
			return res.result, res.err
		}
		if write.err == nil {
			// No acknowledgement is expected for a notification. Wait only for
			// the original prompt response, preserving final usage if supplied.
			select {
			case <-done:
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	case <-timer.C:
		stopTransport()
		<-written
		<-done
		return nil, ctx.Err()
	}
	stopTransport()
	<-done
	return nil, ctx.Err()
}
