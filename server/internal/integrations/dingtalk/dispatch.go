package dingtalk

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// Tunables for the inbound dispatcher. Remote media resolution is detached by
// the shared Router, so this timeout bounds only the synchronous ingest path.
const (
	dispatchJobTimeout = 120 * time.Second
	maxDispatchWorkers = 8
	// maxDispatchQueueDepth bounds one conversation's backlog purely as a
	// memory-safety backstop. It is set far above any realistic human burst so
	// the overflow drop is effectively unreachable in practice — a single
	// conversation would need hundreds of un-drained turns to hit it. Overflow
	// (should it ever happen) drops the newest message with a warn log; the
	// caller (the socket read loop) must never block, so blocking backpressure
	// is not an option here.
	maxDispatchQueueDepth = 256
	// A per-conversation limit alone still permits one waiting drain goroutine
	// per distinct conversation. Bound all accepted-but-not-finished messages
	// for one installation so both queued payloads and semaphore waiters have a
	// deterministic ceiling.
	maxDispatchPending = 2048
)

// dispatcher decouples inbound processing from the Stream read loop: frames
// are ACKed immediately and jobs run on per-conversation serial queues, so a
// slow media download can neither starve ping/system frames nor reorder a
// conversation's transcript. Cross-conversation jobs run in parallel, bounded
// by a global semaphore per installation. The per-conversation queue is bounded
// only as a memory backstop (maxDispatchQueueDepth), set high enough that a
// real human burst never reaches it; the engine's dedup makes any duplicate
// delivery harmless.
//
// Jobs run on context.Background() with their own deadline, deliberately
// detached from the socket's run context: a gateway redial must not cancel an
// in-flight append.
type dispatcher struct {
	handle func(ctx context.Context, msg channel.InboundMessage)
	logger *slog.Logger
	sem    chan struct{}
	ctx    context.Context
	cancel context.CancelFunc

	mu         sync.Mutex
	queues     map[string][]channel.InboundMessage
	active     map[string]bool
	closed     bool
	pending    int
	maxPending int
	workers    sync.WaitGroup
	done       chan struct{}
}

func newDispatcher(handle func(ctx context.Context, msg channel.InboundMessage), logger *slog.Logger) *dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &dispatcher{
		handle:     handle,
		logger:     logger,
		sem:        make(chan struct{}, maxDispatchWorkers),
		ctx:        ctx,
		cancel:     cancel,
		queues:     make(map[string][]channel.InboundMessage),
		active:     make(map[string]bool),
		maxPending: maxDispatchPending,
		done:       make(chan struct{}),
	}
}

// enqueue appends msg to its conversation's queue and starts a drain worker
// for the conversation when none is running. Never blocks the caller.
func (d *dispatcher) enqueue(convID string, msg channel.InboundMessage) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		d.logger.Debug("dingtalk dispatch: dispatcher closed, dropping message",
			"conversation_id", convID, "msg_id", msg.MessageID)
		return
	}
	if d.pending >= d.maxPending {
		d.mu.Unlock()
		d.logger.Warn("dingtalk dispatch: installation queue full, dropping message",
			"conversation_id", convID, "msg_id", msg.MessageID)
		return
	}
	if len(d.queues[convID]) >= maxDispatchQueueDepth {
		d.mu.Unlock()
		d.logger.Warn("dingtalk dispatch: conversation queue full, dropping message",
			"conversation_id", convID, "msg_id", msg.MessageID)
		return
	}
	d.queues[convID] = append(d.queues[convID], msg)
	d.pending++
	start := !d.active[convID]
	if start {
		d.active[convID] = true
		d.workers.Add(1)
	}
	d.mu.Unlock()
	if start {
		go d.drain(convID)
	}
}

// drain runs the conversation's jobs strictly in order and exits when the
// queue is empty (a later enqueue starts a fresh worker). The semaphore
// bounds concurrently running jobs across all conversations; waiting on it
// keeps this conversation's order intact.
func (d *dispatcher) drain(convID string) {
	defer d.workers.Done()
	for {
		d.mu.Lock()
		if d.ctx.Err() != nil {
			d.pending -= len(d.queues[convID])
			delete(d.queues, convID)
			delete(d.active, convID)
			d.mu.Unlock()
			return
		}
		q := d.queues[convID]
		if len(q) == 0 {
			delete(d.queues, convID)
			delete(d.active, convID)
			d.mu.Unlock()
			return
		}
		msg := q[0]
		d.queues[convID] = q[1:]
		d.mu.Unlock()

		select {
		case d.sem <- struct{}{}:
		case <-d.ctx.Done():
			d.mu.Lock()
			d.pending -= 1 + len(d.queues[convID])
			delete(d.queues, convID)
			delete(d.active, convID)
			d.mu.Unlock()
			return
		}
		ctx, cancel := context.WithTimeout(d.ctx, dispatchJobTimeout)
		d.handle(ctx, msg)
		cancel()
		<-d.sem
		d.mu.Lock()
		d.pending--
		d.mu.Unlock()
	}
}

// drainAndClose stops accepting new jobs and waits for already-ACKed messages
// to finish. If ctx expires, it cancels every active job and leaves the worker
// joins to complete asynchronously; callers are never held past their shutdown
// budget. It is idempotent.
func (d *dispatcher) drainAndClose(ctx context.Context) bool {
	d.startClose()
	return d.waitClosed(ctx)
}

// startClose atomically stops new enqueues and starts the asynchronous worker
// join. Splitting this from waitClosed lets the owning dispatchSlot publish the
// closing state before a reconnect decides whether the queue can be reused.
func (d *dispatcher) startClose() {
	d.mu.Lock()
	if !d.closed {
		d.closed = true
		go func() {
			d.workers.Wait()
			d.cancel()
			close(d.done)
		}()
	}
	d.mu.Unlock()
}

func (d *dispatcher) waitClosed(ctx context.Context) bool {
	select {
	case <-d.done:
		return true
	case <-ctx.Done():
		d.cancel()
		select {
		case <-d.done:
			return true
		default:
			return false
		}
	}
}

func (d *dispatcher) isClosed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed
}
