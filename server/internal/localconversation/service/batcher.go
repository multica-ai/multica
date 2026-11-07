// Package service is the Go-owned local conversation service: it batches
// provider output into durable commits and fans committed events out to
// subscribers.
package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store"
)

// ErrStorageBacklog means uncommitted events exceed the global budget; the
// caller stops the affected turn instead of buffering more (plan §6.2).
var ErrStorageBacklog = errors.New("local conversation: storage backlog exceeded")

// Committer is the durable write path, implemented by *store.Writer.
type Committer interface {
	Submit(ctx context.Context, batch store.Batch) (store.Committed, error)
}

// BatcherOptions sets the batching thresholds. Zero values use the plan's
// starting points: 32 KiB or 50 ms per conversation, 16 MiB in total.
type BatcherOptions struct {
	MaxBatchBytes   int
	MaxDelay        time.Duration
	MaxPendingBytes int
	// Published receives each durable commit, in commit order per
	// conversation. It runs on the conversation's flush goroutine.
	Published func(conversationID string, committed store.Committed)
	// Failed receives a commit failure. The batch is dropped: nothing in it
	// was published.
	Failed func(conversationID string, err error)
	// AfterFunc schedules the delay flush; tests replace it.
	AfterFunc func(d time.Duration, f func()) (stop func() bool)
}

// Batcher accumulates each conversation's events and commits them when the
// batch reaches MaxBatchBytes, MaxDelay after its first event, or on a
// critical event. Append never blocks on storage: one flush goroutine per
// conversation commits in order while new events gather in the next batch.
type Batcher struct {
	committer Committer
	opts      BatcherOptions

	mu      sync.Mutex
	convs   map[string]*conversationBatch
	pending int
	closed  bool
	flushes sync.WaitGroup
}

// conversationBatch is a conversation's batches in commit order. All but
// the last are sealed; the last may still accept events.
type conversationBatch struct {
	queue     []*pendingBatch
	stopTimer func() bool
	flushing  bool
}

type pendingBatch struct {
	batch  store.Batch
	bytes  int
	sealed bool
	// done receives the commit outcome for AppendWait callers.
	done chan commitResult
}

type commitResult struct {
	committed store.Committed
	err       error
}

// NewBatcher returns a batcher committing through committer.
func NewBatcher(committer Committer, opts BatcherOptions) *Batcher {
	if opts.MaxBatchBytes <= 0 {
		opts.MaxBatchBytes = 32 << 10
	}
	if opts.MaxDelay <= 0 {
		opts.MaxDelay = 50 * time.Millisecond
	}
	if opts.MaxPendingBytes <= 0 {
		opts.MaxPendingBytes = 16 << 20
	}
	if opts.AfterFunc == nil {
		opts.AfterFunc = func(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }
	}
	return &Batcher{committer: committer, opts: opts, convs: map[string]*conversationBatch{}}
}

// Append adds part to the conversation's open batch. Critical parts (turn
// completion, failures, interaction requests) are committed without waiting,
// together with everything appended before them. The pending budget applies
// only to non-critical parts: a turn completion must never be dropped, and
// it is small.
func (b *Batcher) Append(part store.Batch, critical bool) error {
	size := batchBytes(part)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return store.ErrWriterClosed
	}
	if !critical && b.pending > 0 && b.pending+size > b.opts.MaxPendingBytes {
		return ErrStorageBacklog
	}
	id := part.ConversationID
	c := b.convs[id]
	if c == nil {
		c = &conversationBatch{}
		b.convs[id] = c
	}
	open := c.open()
	// A batch starts and completes at most one turn; a second one goes into
	// the next batch so the store applies them in order.
	if open != nil && ((part.StartTurn != nil && open.batch.StartTurn != nil) || (part.CompleteTurn != nil && open.batch.CompleteTurn != nil)) {
		b.seal(id, c)
		open = nil
	}
	if open == nil {
		open = &pendingBatch{batch: store.Batch{ConversationID: id}}
		c.queue = append(c.queue, open)
		c.stopTimer = b.opts.AfterFunc(b.opts.MaxDelay, func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if c.open() == open {
				b.seal(id, c)
			}
		})
	}
	merge(&open.batch, part)
	open.bytes += size
	b.pending += size
	if critical || open.bytes >= b.opts.MaxBatchBytes {
		b.seal(id, c)
	}
	return nil
}

// AppendWait commits part in a batch of its own, after everything appended
// before it, and returns once it is durable. Commands that must not report
// "accepted" before their input is stored use it; isolating the batch keeps
// a rejected command (such as a duplicate request id) from taking other
// events down with it.
func (b *Batcher) AppendWait(ctx context.Context, part store.Batch) (store.Committed, error) {
	size := batchBytes(part)
	done := make(chan commitResult, 1)
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return store.Committed{}, store.ErrWriterClosed
	}
	id := part.ConversationID
	c := b.convs[id]
	if c == nil {
		c = &conversationBatch{}
		b.convs[id] = c
	}
	if open := c.open(); open != nil {
		open.sealed = true
	}
	own := &pendingBatch{batch: store.Batch{ConversationID: id}, bytes: size, done: done}
	merge(&own.batch, part)
	c.queue = append(c.queue, own)
	b.pending += size
	b.seal(id, c)
	b.mu.Unlock()

	select {
	case r := <-done:
		return r.committed, r.err
	case <-ctx.Done():
		// The batch is queued and will commit or fail on its own; the caller
		// stops waiting but must treat the outcome as unknown.
		return store.Committed{}, ctx.Err()
	}
}

// Close commits every pending batch and waits for in-flight commits.
func (b *Batcher) Close() {
	b.mu.Lock()
	b.closed = true
	for id, c := range b.convs {
		b.seal(id, c)
	}
	b.mu.Unlock()
	b.flushes.Wait()
}

func (c *conversationBatch) open() *pendingBatch {
	if n := len(c.queue); n > 0 && !c.queue[n-1].sealed {
		return c.queue[n-1]
	}
	return nil
}

// seal closes the open batch and starts the flush goroutine. Callers hold b.mu.
func (b *Batcher) seal(id string, c *conversationBatch) {
	if open := c.open(); open != nil {
		open.sealed = true
	}
	if c.stopTimer != nil {
		c.stopTimer()
		c.stopTimer = nil
	}
	if c.flushing || len(c.queue) == 0 {
		return
	}
	c.flushing = true
	b.flushes.Add(1)
	go b.flushLoop(id, c)
}

func (b *Batcher) flushLoop(id string, c *conversationBatch) {
	defer b.flushes.Done()
	for {
		b.mu.Lock()
		if len(c.queue) == 0 || !c.queue[0].sealed {
			c.flushing = false
			if len(c.queue) == 0 && b.convs[id] == c {
				delete(b.convs, id)
			}
			b.mu.Unlock()
			return
		}
		next := c.queue[0]
		c.queue = c.queue[1:]
		b.mu.Unlock()

		committed, err := b.committer.Submit(context.Background(), next.batch)

		b.mu.Lock()
		b.pending -= next.bytes
		b.mu.Unlock()
		if next.done != nil {
			next.done <- commitResult{committed: committed, err: err}
		}
		if err != nil {
			if b.opts.Failed != nil {
				b.opts.Failed(id, err)
			}
			continue
		}
		if b.opts.Published != nil {
			b.opts.Published(id, committed)
		}
	}
}

func merge(dst *store.Batch, part store.Batch) {
	if part.ReadCompletion != nil {
		dst.ReadCompletion = part.ReadCompletion
	}
	if part.StartTurn != nil {
		dst.StartTurn = part.StartTurn
	}
	dst.Events = append(dst.Events, part.Events...)
	dst.Messages = append(dst.Messages, part.Messages...)
	if part.CompleteTurn != nil {
		dst.CompleteTurn = part.CompleteTurn
	}
	if part.ProviderSessionID != nil {
		dst.ProviderSessionID = part.ProviderSessionID
	}
	dst.Attachments = append(dst.Attachments, part.Attachments...)
	if part.Receipt != nil {
		dst.Receipt = part.Receipt
	}
	if part.Settings != nil {
		dst.Settings = part.Settings
	}
	if part.Metadata != nil {
		dst.Metadata = part.Metadata
	}
	if part.Queue != nil {
		dst.Queue = part.Queue
	}
	if part.Operation != nil {
		dst.Operation = part.Operation
	}
	dst.Submissions = append(dst.Submissions, part.Submissions...)
	dst.MoreTurns = append(dst.MoreTurns, part.MoreTurns...)
	dst.Remotes = append(dst.Remotes, part.Remotes...)
	dst.Interactions = append(dst.Interactions, part.Interactions...)
	dst.Resolutions = append(dst.Resolutions, part.Resolutions...)
	if part.Summary != nil {
		if dst.Summary == nil {
			s := *part.Summary
			dst.Summary = &s
		} else {
			mergeSummary(dst.Summary, part.Summary)
		}
	}
}

func mergeSummary(dst, src *store.SummaryPatch) {
	if src.Status != nil {
		dst.Status = src.Status
	}
	if src.Title != nil {
		dst.Title = src.Title
	}
	if src.LastMessagePreview != nil {
		dst.LastMessagePreview = src.LastMessagePreview
	}
	if src.PendingInteractions != nil {
		dst.PendingInteractions = src.PendingInteractions
	}
	if src.QueueCount != nil {
		dst.QueueCount = src.QueueCount
	}
	if !src.KeepRecency {
		dst.UpdatedAt = src.UpdatedAt
		dst.KeepRecency = false
	}
}

func isEmpty(b store.Batch) bool {
	return b.ReadCompletion == nil && b.StartTurn == nil && len(b.Events) == 0 && len(b.Messages) == 0 && b.CompleteTurn == nil && b.Summary == nil && b.ProviderSessionID == nil && len(b.Attachments) == 0 && b.Receipt == nil && b.Settings == nil && len(b.Interactions) == 0 && len(b.Resolutions) == 0 && b.Metadata == nil && b.Queue == nil && len(b.Remotes) == 0 && b.Operation == nil && len(b.Submissions) == 0
}

// batchBytes estimates a part's size for the batch and backlog budgets.
func batchBytes(b store.Batch) int {
	n := 0
	for _, c := range b.Submissions {
		n += len(c.Item) + 256
	}
	for _, e := range b.Events {
		n += len(e.Payload) + len(e.Type) + len(e.ItemID) + len(e.TurnID) + 64
	}
	for _, m := range b.Messages {
		n += len(m.Content) + 64
	}
	n += 128 * len(b.Attachments)
	for _, in := range b.Interactions {
		n += len(in.Request) + 128
	}
	for _, r := range b.Resolutions {
		n += len(r.Answer) + 128
	}
	if b.ReadCompletion != nil || b.StartTurn != nil || b.CompleteTurn != nil || b.Summary != nil {
		n += 128
	}
	return n
}
