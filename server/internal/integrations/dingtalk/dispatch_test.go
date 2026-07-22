package dingtalk

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func dispatchMsg(conv, id string) channel.InboundMessage {
	return channel.InboundMessage{
		MessageID: id,
		Source:    channel.Source{ChatID: conv},
	}
}

func TestDispatcher_SerialPerConversation(t *testing.T) {
	var mu sync.Mutex
	var got []string
	done := make(chan struct{}, 8)
	d := newDispatcher(func(_ context.Context, msg channel.InboundMessage) {
		// Jitter so an ordering bug actually reorders.
		time.Sleep(time.Duration(len(msg.MessageID)%3) * time.Millisecond)
		mu.Lock()
		got = append(got, msg.MessageID)
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
	d := newDispatcher(func(_ context.Context, msg channel.InboundMessage) {
		switch msg.Source.ChatID {
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
	d := newDispatcher(func(_ context.Context, _ channel.InboundMessage) {
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

func TestDispatcher_WorkerExitsAndRestarts(t *testing.T) {
	done := make(chan string, 2)
	d := newDispatcher(func(_ context.Context, msg channel.InboundMessage) {
		done <- msg.MessageID
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
