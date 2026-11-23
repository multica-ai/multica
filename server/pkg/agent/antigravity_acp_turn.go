package agent

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

const antigravityACPBackgroundQuiet = 30 * time.Minute

var (
	// Agy can start a detached reply after the prompt or last tool settles.
	// Its usage_update closes that reply. Leave extra time while it is open.
	antigravityACPSettleQuiet = 3 * time.Second
	antigravityACPReplyQuiet  = 2 * time.Minute
)

type antigravityACPSupplement struct {
	ctx  context.Context
	text string
	sent chan error
}

// One task owns one ACP session. wait serializes replacement prompts and
// decides when the session can close. The reader continues handling stream
// updates and replies from superseded prompts.
type antigravityACPTurn struct {
	stream         *antigravityACPStream
	submissions    chan antigravityACPSupplement
	done           chan struct{}
	ready          atomic.Bool
	latestPromptID atomic.Int64
	promptUsage    acpUsageSnapshot // reader only
}

func (t *antigravityACPTurn) supplement(ctx context.Context, text string) error {
	if !t.ready.Load() {
		select {
		case <-t.done:
			return fmt.Errorf("agy acp turn ended: %w", context.Canceled)
		default:
			return errors.New("agy acp turn not started")
		}
	}
	req := antigravityACPSupplement{ctx: ctx, text: text, sent: make(chan error, 1)}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.done:
		return fmt.Errorf("agy acp turn ended: %w", context.Canceled)
	case t.submissions <- req:
		// Once accepted by wait, return the actual bounded write outcome.
		// Racing cancellation with a completed write must not trigger a
		// follow-up for guidance already sent to this session.
		return <-req.sent
	}
}

func (t *antigravityACPTurn) sendPrompt(c *hermesClient, sessionID, text string) (<-chan rpcResult, error) {
	_, pending, err := c.sendRequest("session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []map[string]string{{"type": "text", "text": text}},
	}, func(id int) { t.latestPromptID.Store(int64(id)) })
	if err != nil {
		return nil, err
	}
	return pending.ch, nil
}

func (t *antigravityACPTurn) wait(runCtx, transportCtx context.Context, c *hermesClient, id string, current <-chan rpcResult) Result {
	var result Result
	var earlier []<-chan rpcResult
	waitingForEarlier := false

	for {
		var settle <-chan time.Time
		var quiet time.Duration
		if current == nil {
			quiet = t.stream.quietPeriod()
			settle = time.After(quiet)
		}
		select {
		case req := <-t.submissions:
			err := req.ctx.Err()
			if err == nil {
				err = runCtx.Err()
			}
			if err == nil {
				var next <-chan rpcResult
				t.stream.deliverable.startBlock()
				next, err = t.sendPrompt(c, id, req.text)
				if err == nil {
					if current != nil {
						earlier = append(earlier, current)
					}
					current, waitingForEarlier = next, false
				}
			}
			req.sent <- err
		case response := <-current:
			current = nil
			if !waitingForEarlier {
				result = antigravityACPPromptResult(response)
				if result.Status == "completed" {
					earlier = nil
				}
			}
			if result.Status == "aborted" {
				return result
			}
			if result.Status == "failed" && len(earlier) > 0 {
				// A rejected replacement can leave the previous prompt
				// running. Let it settle before closing the connection.
				last := len(earlier) - 1
				current, earlier = earlier[last], earlier[:last]
				waitingForEarlier = true
			}
		case <-t.stream.activity:
			// Rearm the quiet period from the latest meaningful update.
		case <-settle:
			select {
			case <-t.stream.activity:
				continue
			default:
			}
			if quiet != t.stream.quietPeriod() {
				continue
			}
			if t.stream.activeTools.Load() > 0 {
				return Result{Status: "timeout", Error: "agy acp timed out waiting for background tools"}
			}
			return result
		case <-transportCtx.Done():
			// The reader fulfils pending RPCs before it publishes EOF.
			if current != nil {
				if waitingForEarlier {
					return result
				}
				select {
				case response := <-current:
					result = antigravityACPPromptResult(response)
				default:
					return Result{Status: "failed", Error: "agy acp connection closed during session/prompt"}
				}
			}
			if result.Status == "completed" && (t.stream.activeTools.Load() > 0 || t.stream.replyOpen.Load()) {
				return Result{Status: "failed", Error: "agy acp connection closed with background work still active"}
			}
			return result
		}
	}
}

func antigravityACPPromptResult(response rpcResult) Result {
	if response.err != nil {
		return Result{Status: "failed", Error: fmt.Sprintf("agy acp session/prompt failed: %v", response.err)}
	}
	prompt, err := parseACPPromptResult(response.result)
	if err != nil {
		return Result{Status: "failed", Error: fmt.Sprintf("agy acp session/prompt failed: %v", err)}
	}
	switch prompt.stopReason {
	case "end_turn":
		return Result{Status: "completed"}
	case "cancelled":
		return Result{Status: "aborted", Error: "agy acp cancelled the turn"}
	case "":
		return Result{Status: "failed", Error: "agy acp session/prompt response did not contain a stop reason"}
	default:
		return Result{Status: "failed", Error: fmt.Sprintf("agy acp stop reason %q", prompt.stopReason)}
	}
}

// agy reports usage per prompt, including the prompt replaced by a steer.
// Sum those results, then let the shared accumulator reconcile cumulative
// usage_update snapshots with the sum so mirrored counters are charged once.
func (t *antigravityACPTurn) recordPromptUsage(c *hermesClient, result hermesPromptResult) {
	c.usageMu.Lock()
	if result.modelID != "" && !acpUsagePresent(c.usage.TokenUsage) {
		c.usageModel = result.modelID
	}
	totals, next := &t.promptUsage, result.usage
	if next.has(acpUsageInput) {
		totals.inputNormalized = next.inputNormalized && (!totals.has(acpUsageInput) || totals.inputNormalized)
	}
	totals.fields |= next.fields
	totals.totalTokens += next.totalTokens
	totals.InputTokens += next.InputTokens
	totals.OutputTokens += next.OutputTokens
	totals.CacheReadTokens += next.CacheReadTokens
	totals.CacheWriteTokens += next.CacheWriteTokens
	totals.CostUSDTicks += next.CostUSDTicks
	c.usage.merge(*totals)
	c.usageMu.Unlock()
	c.notifyUsage()
}
