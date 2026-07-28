package runtime

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// makeToolCalls builds n GatewayToolCall values with predictable ids so a test
// can assert the outcome slice is in the same order as the input.
func makeToolCalls(n int) []GatewayToolCall {
	calls := make([]GatewayToolCall, n)
	for i := range calls {
		calls[i] = GatewayToolCall{
			ID:       fmt.Sprintf("c%d", i),
			Type:     "function",
			Function: GatewayToolCallFunction{Name: fmt.Sprintf("tool%d", i)},
		}
	}
	return calls
}

// TestDispatchToolCallsConcurrentPreservesOrder proves that even when later
// calls finish first, the outcome slice stays in call order — the property the
// Anthropic tool_use/tool_result positional match depends on.
func TestDispatchToolCallsConcurrentPreservesOrder(t *testing.T) {
	calls := makeToolCalls(6)
	outcomes := dispatchToolCallsConcurrent(context.Background(), 6, calls,
		func(_ context.Context, idx int, call GatewayToolCall) gatewayToolOutcome {
			// Earlier indices sleep longer, so if order came from completion time
			// the slice would be reversed. It must not be.
			time.Sleep(time.Duration(6-idx) * time.Millisecond)
			return gatewayToolOutcome{resultText: call.ID}
		})
	if len(outcomes) != len(calls) {
		t.Fatalf("got %d outcomes, want %d", len(outcomes), len(calls))
	}
	for i, call := range calls {
		if outcomes[i].resultText != call.ID {
			t.Fatalf("outcome[%d] = %q, want %q (order not preserved)", i, outcomes[i].resultText, call.ID)
		}
	}
}

// TestDispatchToolCallsConcurrentRunsInParallel deterministically proves the
// dispatch actually overlaps: with concurrency == N and a barrier that only
// releases once all N goroutines have entered, the run can only complete if all
// N ran at the same time.
func TestDispatchToolCallsConcurrentRunsInParallel(t *testing.T) {
	const n = 4
	calls := makeToolCalls(n)
	var entered sync.WaitGroup
	entered.Add(n)
	release := make(chan struct{})

	done := make(chan []gatewayToolOutcome, 1)
	go func() {
		done <- dispatchToolCallsConcurrent(context.Background(), n, calls,
			func(_ context.Context, idx int, call GatewayToolCall) gatewayToolOutcome {
				entered.Done() // signal this goroutine is running
				<-release      // block until every goroutine has entered
				return gatewayToolOutcome{resultText: call.ID}
			})
	}()

	// If dispatch were sequential, only one goroutine would ever enter and this
	// Wait would block forever — the test's 5s guard below catches that.
	waitDone := make(chan struct{})
	go func() { entered.Wait(); close(waitDone) }()
	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("dispatch did not run all tool calls concurrently")
	}
	close(release)

	outcomes := <-done
	for i, call := range calls {
		if outcomes[i].resultText != call.ID {
			t.Fatalf("outcome[%d] = %q, want %q", i, outcomes[i].resultText, call.ID)
		}
	}
}

// TestDispatchToolCallsConcurrentSequentialFallback proves concurrency <= 1
// restores strictly-sequential dispatch: no two dispatches are ever in flight
// at once.
func TestDispatchToolCallsConcurrentSequentialFallback(t *testing.T) {
	calls := makeToolCalls(5)
	var inflight int32
	var maxInflight int32
	outcomes := dispatchToolCallsConcurrent(context.Background(), 1, calls,
		func(_ context.Context, idx int, call GatewayToolCall) gatewayToolOutcome {
			cur := atomic.AddInt32(&inflight, 1)
			for {
				old := atomic.LoadInt32(&maxInflight)
				if cur <= old || atomic.CompareAndSwapInt32(&maxInflight, old, cur) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			atomic.AddInt32(&inflight, -1)
			return gatewayToolOutcome{resultText: call.ID}
		})
	if maxInflight != 1 {
		t.Fatalf("max in-flight = %d, want 1 (sequential fallback broken)", maxInflight)
	}
	for i, call := range calls {
		if outcomes[i].resultText != call.ID {
			t.Fatalf("outcome[%d] = %q, want %q", i, outcomes[i].resultText, call.ID)
		}
	}
}

// TestDispatchToolCallsConcurrentBoundsConcurrency proves the concurrency ceiling
// is respected: with 12 calls but a limit of 3, at most 3 run at once.
func TestDispatchToolCallsConcurrentBoundsConcurrency(t *testing.T) {
	calls := makeToolCalls(12)
	var inflight int32
	var maxInflight int32
	dispatchToolCallsConcurrent(context.Background(), 3, calls,
		func(_ context.Context, idx int, call GatewayToolCall) gatewayToolOutcome {
			cur := atomic.AddInt32(&inflight, 1)
			for {
				old := atomic.LoadInt32(&maxInflight)
				if cur <= old || atomic.CompareAndSwapInt32(&maxInflight, old, cur) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
			atomic.AddInt32(&inflight, -1)
			return gatewayToolOutcome{resultText: call.ID}
		})
	if maxInflight > 3 {
		t.Fatalf("max in-flight = %d, want <= 3 (concurrency ceiling exceeded)", maxInflight)
	}
	if maxInflight < 2 {
		t.Fatalf("max in-flight = %d, want >= 2 (dispatch did not run in parallel)", maxInflight)
	}
}
