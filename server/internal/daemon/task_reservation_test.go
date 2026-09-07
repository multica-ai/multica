package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskReservationAtomicWake(t *testing.T) {
	slots := newTaskSlotSemaphore(2)
	var active, peak atomic.Int32
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := &taskExecutionReservation{slots: slots, wakeup: make(chan struct{}, 1)}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := r.acquire(ctx); err != nil {
				t.Error(err)
				return
			}
			a := active.Add(1)
			for old := peak.Load(); a > old; old = peak.Load() {
				if peak.CompareAndSwap(old, a) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			active.Add(-1)
			r.release()
			r.release()
		}()
	}
	wg.Wait()
	if peak.Load() > 2 || len(slots) != 2 {
		t.Fatalf("wake oversubscribed/leaked: peak=%d slots=%d", peak.Load(), len(slots))
	}
	t.Logf("20 simultaneous wakes, peak=%d/2; 2/2 slots returned", peak.Load())
}

func TestTaskReservationParkingAndCancelledWake(t *testing.T) {
	slots := newTaskSlotSemaphore(1)
	r := &taskExecutionReservation{slots: slots, slot: <-slots, held: true, wakeup: make(chan struct{}, 1)}
	r.release()
	if len(slots) != 1 {
		t.Fatal("parked run retained capacity")
	}
	occupied := <-slots
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.acquire(ctx); err == nil {
		t.Fatal("cancelled waiter acquired")
	}
	r.release()
	slots <- occupied
	if len(slots) != 1 {
		t.Fatal("cancelled waiter leaked a slot")
	}
}

func TestLocalPathQueuePriorityFIFOAndAging(t *testing.T) {
	l := NewLocalPathLocker()
	holder, err := l.Acquire(context.Background(), "shared", "holder", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	type request struct {
		id       string
		priority int32
		at       time.Time
	}
	requests := []request{{"low", 1, now}, {"same-b", 3, now}, {"aged", 0, now.Add(-2 * time.Hour)}, {"emergency", 4, now}, {"same-a", 3, now}}
	order := make(chan string, len(requests))
	for _, req := range requests {
		parked := make(chan struct{})
		go func(r request) {
			release, err := l.AcquireOrdered(context.Background(), "shared", r.id, r.priority, r.at, func(string) { close(parked) })
			if err != nil {
				t.Error(err)
				return
			}
			order <- r.id
			release()
		}(req)
		<-parked
	}
	// A separate resource must proceed even with a blocked queue head.
	other, err := l.Acquire(context.Background(), "independent", "other", nil)
	if err != nil {
		t.Fatal(err)
	}
	other()
	holder()
	for _, expected := range []string{"emergency", "aged", "same-a", "same-b", "low"} {
		select {
		case got := <-order:
			if got != expected {
				t.Fatalf("got %s, want %s", got, expected)
			}
		case <-time.After(time.Second):
			t.Fatal("queue did not resume")
		}
	}
}

func TestLocalPathQueueCancelledWaiterDoesNotBlockSuccessor(t *testing.T) {
	l := NewLocalPathLocker()
	holder, _ := l.Acquire(context.Background(), "shared", "holder", nil)
	ctx, cancel := context.WithCancel(context.Background())
	parked := make(chan struct{})
	done := make(chan error, 1)
	go func() { _, err := l.Acquire(ctx, "shared", "cancelled", func(string) { close(parked) }); done <- err }()
	<-parked
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancel not honored")
	}
	holder()
	release, err := l.Acquire(context.Background(), "shared", "next", nil)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if l.Holder("shared") != "" {
		t.Fatal("phantom holder")
	}
}
