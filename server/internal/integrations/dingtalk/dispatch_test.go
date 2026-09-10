package dingtalk

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func dispatchMsg(conv, id string) inboundJob {
	return inboundJob{callback: botCallbackData{
		ConversationId: conv, ConversationType: convTypeP2P,
		SenderStaffId: "staff-1", MsgId: id, Msgtype: "text",
		Text: botCallbackText{Content: "hello"},
	}}
}

func TestDispatcher_SerialPerConversation(t *testing.T) {
	var mu sync.Mutex
	var got []string
	done := make(chan struct{}, 8)
	d := newDispatcher(func(_ context.Context, job inboundJob) {
		// Jitter so an ordering bug actually reorders.
		time.Sleep(time.Duration(len(job.callback.MsgId)%3) * time.Millisecond)
		mu.Lock()
		got = append(got, job.callback.MsgId)
		mu.Unlock()
		done <- struct{}{}
	}, nil)

	want := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("m-%d", i)
		want = append(want, id)
		d.enqueue("conv-A", dispatchMsg("conv-A", id))
	}
	for i := 0; i < 5; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for jobs")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order broken: got %v, want %v", got, want)
		}
	}
}

func TestDispatcher_ParallelAcrossConversations(t *testing.T) {
	blockA := make(chan struct{})
	sawB := make(chan struct{})
	d := newDispatcher(func(_ context.Context, job inboundJob) {
		switch job.callback.ConversationId {
		case "conv-A":
			<-blockA
		case "conv-B":
			close(sawB)
		}
	}, nil)

	d.enqueue("conv-A", dispatchMsg("conv-A", "a1"))
	d.enqueue("conv-B", dispatchMsg("conv-B", "b1"))

	select {
	case <-sawB:
		// conv-B ran while conv-A is still blocked — parallel across convs.
	case <-time.After(2 * time.Second):
		t.Fatal("conv-B was starved by conv-A's slow job")
	}
	close(blockA)
}

func TestDispatcher_OverflowDrops(t *testing.T) {
	release := make(chan struct{})
	var handled int
	var mu sync.Mutex
	d := newDispatcher(func(_ context.Context, _ inboundJob) {
		<-release
		mu.Lock()
		handled++
		mu.Unlock()
	}, nil)

	// One job occupies the worker; then fill the queue past its depth.
	total := maxDispatchQueueDepth + 5
	for i := 0; i < total+1; i++ {
		d.enqueue("conv-A", dispatchMsg("conv-A", fmt.Sprintf("m-%d", i)))
	}
	close(release)

	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		n := handled
		mu.Unlock()
		if n > maxDispatchQueueDepth+1 {
			t.Fatalf("handled %d jobs, want at most %d (overflow must drop)", n, maxDispatchQueueDepth+1)
		}
		select {
		case <-deadline:
			if n == 0 {
				t.Fatal("no jobs handled at all")
			}
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func TestDispatcher_GlobalPendingLimitBoundsDistinctConversations(t *testing.T) {
	release := make(chan struct{})
	done := make(chan string, 3)
	d := newDispatcher(func(_ context.Context, job inboundJob) {
		<-release
		done <- job.callback.MsgId
	}, nil)
	d.maxPending = 2

	d.enqueue("conv-A", dispatchMsg("conv-A", "first"))
	d.enqueue("conv-B", dispatchMsg("conv-B", "second"))
	d.enqueue("conv-C", dispatchMsg("conv-C", "dropped"))

	d.mu.Lock()
	if d.pending != 2 {
		t.Fatalf("accepted pending messages = %d, want 2", d.pending)
	}
	d.mu.Unlock()
	close(release)

	got := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case id := <-done:
			got[id] = true
		case <-time.After(time.Second):
			t.Fatalf("accepted messages did not finish: %v", got)
		}
	}
	if !got["first"] || !got["second"] || got["dropped"] {
		t.Fatalf("handled messages = %v", got)
	}
	select {
	case id := <-done:
		t.Fatalf("global overflow message was handled: %q", id)
	case <-time.After(20 * time.Millisecond):
	}

	deadline := time.Now().Add(time.Second)
	for {
		d.mu.Lock()
		pending := d.pending
		d.mu.Unlock()
		if pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending count did not return to zero: %d", pending)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestDispatcher_WorkerExitsAndRestarts(t *testing.T) {
	done := make(chan string, 2)
	d := newDispatcher(func(_ context.Context, job inboundJob) {
		done <- job.callback.MsgId
	}, nil)

	d.enqueue("conv-A", dispatchMsg("conv-A", "first"))
	select {
	case id := <-done:
		if id != "first" {
			t.Fatalf("got %q", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first job never ran")
	}
	// Give the worker a moment to drain and exit, then enqueue again.
	time.Sleep(20 * time.Millisecond)
	d.enqueue("conv-A", dispatchMsg("conv-A", "second"))
	select {
	case id := <-done:
		if id != "second" {
			t.Fatalf("got %q", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not restart for a drained conversation")
	}
}

func TestDispatcher_DrainAndCloseFinishesAcceptedJobsAndRejectsNewOnes(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	done := make(chan string, 2)
	d := newDispatcher(func(_ context.Context, job inboundJob) {
		started <- struct{}{}
		<-release
		done <- job.callback.MsgId
	}, nil)

	d.enqueue("conv-A", dispatchMsg("conv-A", "first"))
	d.enqueue("conv-A", dispatchMsg("conv-A", "second"))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first accepted job did not start")
	}

	closed := make(chan bool, 1)
	go func() {
		closed <- d.drainAndClose(context.Background())
	}()
	deadline := time.Now().Add(time.Second)
	for !d.isClosed() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !d.isClosed() {
		t.Fatal("dispatcher did not enter closing state")
	}
	d.enqueue("conv-A", dispatchMsg("conv-A", "late"))
	close(release)

	select {
	case ok := <-closed:
		if !ok {
			t.Fatal("dispatcher did not drain accepted jobs")
		}
	case <-time.After(time.Second):
		t.Fatal("dispatcher close did not complete")
	}

	var got []string
	for i := 0; i < 2; i++ {
		select {
		case id := <-done:
			got = append(got, id)
		case <-time.After(time.Second):
			t.Fatalf("accepted jobs did not finish: %v", got)
		}
	}
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("drained jobs = %v, want [first second]", got)
	}
	select {
	case id := <-done:
		t.Fatalf("closed dispatcher accepted late job %q", id)
	default:
	}
}

func TestDispatcher_CloseDeadlineCancelsInFlightJob(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	d := newDispatcher(func(ctx context.Context, _ inboundJob) {
		close(started)
		<-ctx.Done()
		close(cancelled)
	}, nil)
	d.enqueue("conv-A", dispatchMsg("conv-A", "first"))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("job did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_ = d.drainAndClose(ctx)
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("close deadline did not cancel the in-flight job")
	}
}
