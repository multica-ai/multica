package sharecrm

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

const (
	dispatchJobTimeout    = 120 * time.Second
	maxDispatchWorkers    = 8
	maxDispatchQueueDepth = 256
	maxDispatchPending    = 2048
)

// dispatcher serializes inbound jobs per conversation so SSE read never blocks
// on the engine pipeline (DingTalk/Slack parity).
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

func (d *dispatcher) enqueue(convID string, msg channel.InboundMessage) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	if d.pending >= d.maxPending {
		d.mu.Unlock()
		d.logger.Warn("sharecrm dispatch: installation queue full, dropping",
			"conversation_id", convID, "msg_id", msg.MessageID)
		return
	}
	if len(d.queues[convID]) >= maxDispatchQueueDepth {
		d.mu.Unlock()
		d.logger.Warn("sharecrm dispatch: conversation queue full, dropping",
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

func (d *dispatcher) drain(convID string) {
	defer d.workers.Done()
	for {
		d.mu.Lock()
		q := d.queues[convID]
		if len(q) == 0 || d.closed {
			delete(d.queues, convID)
			delete(d.active, convID)
			d.mu.Unlock()
			return
		}
		msg := q[0]
		d.queues[convID] = q[1:]
		d.pending--
		d.mu.Unlock()

		select {
		case d.sem <- struct{}{}:
		case <-d.ctx.Done():
			return
		}
		func() {
			defer func() { <-d.sem }()
			ctx, cancel := context.WithTimeout(context.Background(), dispatchJobTimeout)
			defer cancel()
			d.handle(ctx, msg)
		}()
	}
}

func (d *dispatcher) startClose() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	d.cancel()
	d.mu.Unlock()
}

func (d *dispatcher) isClosed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed
}

func (d *dispatcher) waitClosed(ctx context.Context) bool {
	done := make(chan struct{})
	go func() {
		d.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}
