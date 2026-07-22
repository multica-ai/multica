package dingtalk

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// Tunables for the inbound dispatcher. The job timeout leaves the media
// ingest budget (mediaIngestTimeout) plus pipeline margin.
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

	mu     sync.Mutex
	queues map[string][]channel.InboundMessage
	active map[string]bool
}

func newDispatcher(handle func(ctx context.Context, msg channel.InboundMessage), logger *slog.Logger) *dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &dispatcher{
		handle: handle,
		logger: logger,
		sem:    make(chan struct{}, maxDispatchWorkers),
		queues: make(map[string][]channel.InboundMessage),
		active: make(map[string]bool),
	}
}

// enqueue appends msg to its conversation's queue and starts a drain worker
// for the conversation when none is running. Never blocks the caller.
func (d *dispatcher) enqueue(convID string, msg channel.InboundMessage) {
	d.mu.Lock()
	if len(d.queues[convID]) >= maxDispatchQueueDepth {
		d.mu.Unlock()
		d.logger.Warn("dingtalk dispatch: conversation queue full, dropping message",
			"conversation_id", convID, "msg_id", msg.MessageID)
		return
	}
	d.queues[convID] = append(d.queues[convID], msg)
	start := !d.active[convID]
	if start {
		d.active[convID] = true
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
	for {
		d.mu.Lock()
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

		d.sem <- struct{}{}
		ctx, cancel := context.WithTimeout(context.Background(), dispatchJobTimeout)
		d.handle(ctx, msg)
		cancel()
		<-d.sem
	}
}
