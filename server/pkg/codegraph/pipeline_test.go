package codegraph

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestActiveBudgetExcludesSlotWait(t *testing.T) {
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	b := ActiveBudget{Remaining: 200 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- b.Run(ctx, gate, func(ctx context.Context) error {
			deadline, _ := ctx.Deadline()
			if time.Until(deadline) < 100*time.Millisecond {
				return errors.New("slot wait consumed budget")
			}
			return nil
		})
	}()
	select {
	case err := <-done:
		t.Fatalf("ran without a slot: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	<-gate
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestActiveBudgetCumulativeDeadlineAndWaitCancellation(t *testing.T) {
	gate := make(chan struct{}, 1)
	b := ActiveBudget{Remaining: 100 * time.Millisecond}
	if err := b.Run(context.Background(), gate, func(context.Context) error { time.Sleep(30 * time.Millisecond); return nil }); err != nil {
		t.Fatal(err)
	}
	if b.Remaining >= 90*time.Millisecond {
		t.Fatal("execution did not spend budget")
	}
	if err := b.Run(context.Background(), gate, func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
	if err := b.Run(context.Background(), gate, func(context.Context) error { t.Error("exhausted budget executed"); return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("exhausted budget: %v", err)
	}
	gate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fresh := ActiveBudget{Remaining: time.Second}
	if err := fresh.Run(ctx, gate, func(context.Context) error { t.Error("cancelled waiter executed"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if fresh.Remaining != time.Second {
		t.Fatal("cancelled wait spent budget")
	}
	<-gate
}

func TestPipelineResourceLimits(t *testing.T) {
	p := NewPipeline()
	if cap(p.Builds) != 3 {
		t.Fatal("unbounded prefetch")
	}
	for _, tc := range []struct {
		name string
		gate chan struct{}
		max  int32
	}{{"clone", p.Clone, 2}, {"extract", p.Extract, 1}, {"publish", p.Publish, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			var active, peak atomic.Int32
			var workers sync.WaitGroup
			for range 8 {
				workers.Add(1)
				go func() {
					defer workers.Done()
					b := ActiveBudget{Remaining: time.Second}
					if err := b.Run(context.Background(), tc.gate, func(context.Context) error {
						n := active.Add(1)
						defer active.Add(-1)
						for old := peak.Load(); n > old; old = peak.Load() {
							if peak.CompareAndSwap(old, n) {
								break
							}
						}
						time.Sleep(10 * time.Millisecond)
						return nil
					}); err != nil {
						t.Error(err)
					}
				}()
			}
			workers.Wait()
			if peak.Load() > tc.max || peak.Load() == 0 {
				t.Fatalf("peak %d > limit %d", peak.Load(), tc.max)
			}
		})
	}
}
