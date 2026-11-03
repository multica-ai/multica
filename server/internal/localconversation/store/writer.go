package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

// ErrWriterClosed is returned for batches submitted after Close.
var ErrWriterClosed = errors.New("local conversation store: writer closed")

// NewEvent is one event to append. Seq and TurnSeq are assigned on commit.
type NewEvent struct {
	TurnID    string
	ItemID    string
	Type      string
	Final     bool
	Payload   json.RawMessage
	CreatedAt time.Time
}

// NewTurn starts a turn before the batch's events are appended.
type NewTurn struct {
	ID        string
	RequestID string
	// Remote, for a cloud agent's turn, is its execution state as JSON.
	Remote string
	// OriginPeerID and OriginClientKind name the device that asked for this turn.
	OriginPeerID     string
	OriginClientKind string
	StartedAt        time.Time
}

// TurnRemote replaces a cloud agent turn's execution state.
type TurnRemote struct {
	TurnID string
	Remote string
}

// NewMessage is a message projection row written with its final event.
type NewMessage struct {
	ID            string
	TurnID        string
	Role          string
	Content       string
	AttachmentIDs []string
	Steered       bool
	Failed        bool
	CreatedAt     time.Time
}

// TurnCompletion ends a turn.
type TurnCompletion struct {
	TurnID      string
	Status      string
	ErrorKind   string
	Error       string
	CompletedAt time.Time
	// ProviderTurnID is where the turn ends in the provider's own history:
	// the Codex turn id, or the last Claude transcript message uuid.
	ProviderTurnID string
}

// SummaryPatch changes the conversation summary. Nil fields keep their value.
// Any patch advances the scope sequence so subscribed lists see the change.
type SummaryPatch struct {
	Status              *string
	Title               *string
	LastMessagePreview  *string
	PendingInteractions *int64
	QueueCount          *int64
	BackgroundTasks     *int64
	UpdatedAt           time.Time
	// KeepRecency leaves updated_at, which orders the list, unchanged.
	KeepRecency bool
}

// Batch is everything one conversation commits at once. All of it lands in
// the same transaction or none of it does.
type Batch struct {
	ConversationID string
	StartTurn      *NewTurn
	Events         []NewEvent
	Messages       []NewMessage
	CompleteTurn   *TurnCompletion
	Summary        *SummaryPatch
	// ProviderSessionID records the native session the conversation resumes.
	ProviderSessionID *string
	// Attachments are files written by WriteAttachmentFile that events in
	// this batch reference.
	Attachments []NewAttachment
	// Receipt records the command this batch accepts; a receipt that already
	// exists rejects the whole batch with ErrDuplicateRequest.
	Receipt *CommandReceipt
	// Settings replaces the conversation's provider settings.
	Settings *ConversationSettings
	// Interactions and Resolutions open and close interaction requests; the
	// conversation's pending count in its summary follows them.
	// MoreTurns start after StartTurn in the same batch, such as one turn
	// per agent a message invokes.
	MoreTurns []NewTurn
	// Remotes update cloud agent turns.
	Remotes      []TurnRemote
	Interactions []NewInteraction
	Resolutions  []InteractionResolution
	// Metadata replaces the conversation's list metadata; the summary
	// always follows it.
	Metadata *ConversationMetadata
	// Queue changes the conversation's prompt queue.
	Queue *QueueChange
}

// ConversationMetadata is how a conversation is listed and where it runs.
type ConversationMetadata struct {
	TitleMode string
	// TitleGeneration is "", "pending", "complete" or "failed" for the
	// model-generated title.
	TitleGeneration string
	Cwd             string
	// ProjectID nil means the conversation belongs to no project.
	ProjectID *string
	Pinned    bool
	Archived  bool
}

// ConversationSettings are the provider settings a conversation runs with.
// A nil Model or ReasoningEffort means the provider default.
type ConversationSettings struct {
	Model           *string
	ReasoningEffort *string
	PermissionMode  string
}

// CommittedEvent is an appended event with its assigned sequence numbers.
type CommittedEvent struct {
	ConversationID string
	Seq            int64
	TurnID         string
	TurnSeq        int64
	ItemID         string
	Type           string
	Final          bool
	Payload        json.RawMessage
	CreatedAt      time.Time
}

// Committed is the durable result of one batch, ready to publish.
type Committed struct {
	Events []CommittedEvent
	// ScopeSeq is the new scope sequence when the summary changed, else 0.
	ScopeSeq int64
	// Summary is the conversation row after the batch when the summary changed.
	Summary *sqlitedb.Conversation
}

// WriterOptions bounds the writer.
type WriterOptions struct {
	// QueueSize bounds batches waiting for the writer goroutine.
	QueueSize int
	// MaxGroup bounds batches merged into one transaction.
	MaxGroup int
}

// Writer is the only goroutine that writes conversation data. Batches that
// are ready at the same time are committed in one transaction, so the number
// of synchronous disk flushes follows the commit rate, not the number of
// active conversations.
type Writer struct {
	store    *Store
	maxGroup int
	queue    chan *writeRequest
	done     chan struct{}
	closeMu  sync.RWMutex
	closed   bool
}

type writeRequest struct {
	ctx    context.Context
	batch  Batch
	result chan writeResult
}

type writeResult struct {
	committed Committed
	err       error
}

// NewWriter starts the writer goroutine.
func (s *Store) NewWriter(opts WriterOptions) *Writer {
	if opts.QueueSize <= 0 {
		opts.QueueSize = 256
	}
	if opts.MaxGroup <= 0 {
		opts.MaxGroup = 64
	}
	w := &Writer{store: s, maxGroup: opts.MaxGroup, queue: make(chan *writeRequest, opts.QueueSize), done: make(chan struct{})}
	go w.run()
	return w
}

// Submit commits batch and returns once it is durable. It blocks while the
// writer queue is full, bounded by ctx.
func (w *Writer) Submit(ctx context.Context, batch Batch) (Committed, error) {
	req := &writeRequest{ctx: ctx, batch: batch, result: make(chan writeResult, 1)}
	w.closeMu.RLock()
	if w.closed {
		w.closeMu.RUnlock()
		return Committed{}, ErrWriterClosed
	}
	select {
	case w.queue <- req:
		w.closeMu.RUnlock()
	case <-ctx.Done():
		w.closeMu.RUnlock()
		return Committed{}, ctx.Err()
	}
	// Once queued the batch is committed or rejected by the writer; waiting
	// for that outcome keeps the caller from publishing an unknown state.
	res := <-req.result
	return res.committed, res.err
}

// Close stops accepting batches and waits for queued batches to finish.
func (w *Writer) Close() {
	w.closeMu.Lock()
	if !w.closed {
		w.closed = true
		close(w.queue)
	}
	w.closeMu.Unlock()
	<-w.done
}

func (w *Writer) run() {
	defer close(w.done)
	for first := range w.queue {
		group := []*writeRequest{first}
	drain:
		for len(group) < w.maxGroup {
			select {
			case req, ok := <-w.queue:
				if !ok {
					break drain
				}
				group = append(group, req)
			default:
				break drain
			}
		}
		w.commitGroup(group)
	}
}

// commitGroup applies each batch inside its own savepoint so one invalid
// batch is rejected without failing the others, then commits once.
func (w *Writer) commitGroup(group []*writeRequest) {
	results := make([]writeResult, len(group))
	fail := func(err error) {
		for i, req := range group {
			if results[i].err == nil {
				results[i] = writeResult{err: err}
			}
			req.result <- results[i]
		}
	}

	ctx := context.Background()
	tx, err := w.store.write.BeginTx(ctx, nil)
	if err != nil {
		fail(fmt.Errorf("begin write transaction: %w", err))
		return
	}
	for i, req := range group {
		if err := req.ctx.Err(); err != nil {
			results[i] = writeResult{err: err}
			continue
		}
		if _, err := tx.ExecContext(ctx, "SAVEPOINT batch"); err != nil {
			_ = tx.Rollback()
			fail(err)
			return
		}
		committed, err := applyBatch(ctx, sqlitedb.New(tx), req.batch)
		if err != nil {
			if _, rbErr := tx.ExecContext(ctx, "ROLLBACK TO batch"); rbErr != nil {
				_ = tx.Rollback()
				fail(errors.Join(err, rbErr))
				return
			}
			results[i] = writeResult{err: err}
		} else {
			results[i] = writeResult{committed: committed}
		}
		if _, err := tx.ExecContext(ctx, "RELEASE batch"); err != nil {
			_ = tx.Rollback()
			fail(err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		// Nothing in the group is durable; report the commit failure to every
		// batch, including those that applied cleanly.
		for i := range results {
			results[i] = writeResult{err: fmt.Errorf("commit: %w", err)}
		}
	}
	for i, req := range group {
		req.result <- results[i]
	}
}

func applyBatch(ctx context.Context, q *sqlitedb.Queries, b Batch) (Committed, error) {
	conv, err := q.GetConversation(ctx, b.ConversationID)
	if errors.Is(err, sql.ErrNoRows) {
		return Committed{}, fmt.Errorf("%w: conversation %s", ErrNotFound, b.ConversationID)
	}
	if err != nil {
		return Committed{}, err
	}

	if r := b.Receipt; r != nil {
		if r.ScopeID != conv.ScopeID {
			return Committed{}, fmt.Errorf("receipt %s belongs to another scope", r.RequestID)
		}
		if err := insertReceipt(ctx, q, *r); err != nil {
			return Committed{}, err
		}
	}

	turnSeqs := map[string]int64{}
	var started []NewTurn
	if b.StartTurn != nil {
		started = append([]NewTurn{*b.StartTurn}, b.MoreTurns...)
	}
	for i, t := range started {
		err := q.InsertTurn(ctx, sqlitedb.InsertTurnParams{
			ID: t.ID, ConversationID: conv.ID, TurnIndex: conv.TurnCount + int64(i), RequestID: t.RequestID,
			Remote: t.Remote, OriginPeerID: t.OriginPeerID, OriginClientKind: t.OriginClientKind, Status: "running", StartedAt: formatTime(t.StartedAt),
		})
		if err != nil {
			return Committed{}, fmt.Errorf("start turn %s: %w", t.ID, err)
		}
		turnSeqs[t.ID] = 0
	}
	if len(started) > 0 {
		if err := q.SetConversationTurnCount(ctx, sqlitedb.SetConversationTurnCountParams{TurnCount: conv.TurnCount + int64(len(started)), ID: conv.ID}); err != nil {
			return Committed{}, err
		}
	}
	turnSeq := func(turnID string) (int64, error) {
		if seq, ok := turnSeqs[turnID]; ok {
			return seq, nil
		}
		turn, err := q.GetTurn(ctx, turnID)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && turn.ConversationID != conv.ID) {
			return 0, fmt.Errorf("%w: turn %s in conversation %s", ErrNotFound, turnID, conv.ID)
		}
		if err != nil {
			return 0, err
		}
		turnSeqs[turnID] = turn.LastTurnSeq
		return turn.LastTurnSeq, nil
	}

	for _, r := range b.Remotes {
		if _, err := turnSeq(r.TurnID); err != nil {
			return Committed{}, err
		}
		if err := q.SetTurnRemote(ctx, sqlitedb.SetTurnRemoteParams{Remote: r.Remote, ID: r.TurnID}); err != nil {
			return Committed{}, err
		}
	}

	for _, a := range b.Attachments {
		if a.ConversationID != conv.ID || a.ScopeID != conv.ScopeID {
			return Committed{}, fmt.Errorf("attachment %s belongs to another conversation", a.ID)
		}
		err := q.InsertAttachment(ctx, sqlitedb.InsertAttachmentParams{
			ID: a.ID, ScopeID: a.ScopeID, ConversationID: a.ConversationID, Path: a.Name, FileName: a.FileName,
			Size: a.Size, MediaType: a.MediaType, CreatedAt: formatTime(a.CreatedAt),
		})
		if err != nil {
			return Committed{}, err
		}
	}

	committed := Committed{Events: make([]CommittedEvent, 0, len(b.Events))}
	seq := conv.LastSeq
	for _, e := range b.Events {
		if !json.Valid(e.Payload) {
			return Committed{}, fmt.Errorf("event %s payload is not JSON", e.Type)
		}
		seq++
		ce := CommittedEvent{ConversationID: conv.ID, Seq: seq, TurnID: e.TurnID, ItemID: e.ItemID, Type: e.Type, Final: e.Final, Payload: e.Payload, CreatedAt: e.CreatedAt}
		params := sqlitedb.InsertEventParams{
			ConversationID: conv.ID, Seq: seq, Type: e.Type, Final: boolInt(e.Final),
			Payload: string(e.Payload), CreatedAt: formatTime(e.CreatedAt),
		}
		if e.TurnID != "" {
			last, err := turnSeq(e.TurnID)
			if err != nil {
				return Committed{}, err
			}
			ce.TurnSeq = last + 1
			turnSeqs[e.TurnID] = ce.TurnSeq
			params.TurnID = sql.NullString{String: e.TurnID, Valid: true}
			params.TurnSeq = sql.NullInt64{Int64: ce.TurnSeq, Valid: true}
		}
		if e.ItemID != "" {
			params.ItemID = sql.NullString{String: e.ItemID, Valid: true}
		}
		if err := q.InsertEvent(ctx, params); err != nil {
			return Committed{}, err
		}
		committed.Events = append(committed.Events, ce)
	}
	if seq != conv.LastSeq {
		if err := q.AdvanceConversationSeq(ctx, sqlitedb.AdvanceConversationSeqParams{LastSeq: seq, ID: conv.ID}); err != nil {
			return Committed{}, err
		}
	}
	for turnID, last := range turnSeqs {
		if err := q.AdvanceTurnSeq(ctx, sqlitedb.AdvanceTurnSeqParams{LastTurnSeq: last, ID: turnID}); err != nil {
			return Committed{}, err
		}
	}

	for _, m := range b.Messages {
		if _, err := turnSeq(m.TurnID); err != nil {
			return Committed{}, err
		}
		ids, err := json.Marshal(nonNil(m.AttachmentIDs))
		if err != nil {
			return Committed{}, err
		}
		err = q.InsertMessage(ctx, sqlitedb.InsertMessageParams{
			ID: m.ID, ConversationID: conv.ID, TurnID: m.TurnID, Role: m.Role, Content: m.Content,
			AttachmentIds: string(ids), Steered: boolInt(m.Steered), Failed: boolInt(m.Failed), CreatedAt: formatTime(m.CreatedAt),
		})
		if err != nil {
			return Committed{}, err
		}
	}

	if id := b.ProviderSessionID; id != nil {
		if err := q.SetProviderSessionID(ctx, sqlitedb.SetProviderSessionIDParams{ProviderSessionID: *id, ID: conv.ID}); err != nil {
			return Committed{}, err
		}
	}

	if st := b.Settings; st != nil {
		err := q.UpdateConversationSettings(ctx, sqlitedb.UpdateConversationSettingsParams{
			Model: nullString(st.Model), ReasoningEffort: nullString(st.ReasoningEffort), PermissionMode: st.PermissionMode, ID: conv.ID,
		})
		if err != nil {
			return Committed{}, err
		}
	}

	if m := b.Metadata; m != nil {
		err := q.UpdateConversationMetadata(ctx, sqlitedb.UpdateConversationMetadataParams{
			TitleMode: m.TitleMode, TitleGeneration: m.TitleGeneration, Cwd: m.Cwd, ProjectID: nullString(m.ProjectID),
			Pinned: boolInt(m.Pinned), Archived: boolInt(m.Archived), ID: conv.ID,
		})
		if err != nil {
			return Committed{}, err
		}
		if b.Summary == nil {
			b.Summary = &SummaryPatch{UpdatedAt: metadataTime(b)}
		}
	}

	if c := b.Queue; c != nil {
		if err := applyQueue(ctx, q, conv.ID, c); err != nil {
			return Committed{}, err
		}
	}

	if c := b.CompleteTurn; c != nil {
		if _, err := turnSeq(c.TurnID); err != nil {
			return Committed{}, err
		}
		err := q.CompleteTurn(ctx, sqlitedb.CompleteTurnParams{
			Status: c.Status, ErrorKind: c.ErrorKind, Error: c.Error,
			CompletedAt: sql.NullString{String: formatTime(c.CompletedAt), Valid: true}, ID: c.TurnID,
		})
		if err != nil {
			return Committed{}, err
		}
		if c.ProviderTurnID != "" {
			if err := q.SetProviderTurnID(ctx, sqlitedb.SetProviderTurnIDParams{ProviderTurnID: c.ProviderTurnID, ID: c.TurnID}); err != nil {
				return Committed{}, err
			}
		}
	}

	pending, pendingChanged, err := applyInteractions(ctx, q, conv, b)
	if err != nil {
		return Committed{}, err
	}
	if pendingChanged {
		if b.Summary == nil {
			b.Summary = &SummaryPatch{UpdatedAt: interactionTime(b)}
		}
		b.Summary.PendingInteractions = &pending
	}
	if touchesBackgroundTasks(&b) {
		count, err := q.CountOpenBackgroundTasks(ctx, conv.ID)
		if err != nil {
			return Committed{}, err
		}
		if count != conv.BackgroundTasks {
			if b.Summary == nil {
				b.Summary = &SummaryPatch{KeepRecency: true}
			}
			b.Summary.BackgroundTasks = &count
		}
	}

	if p := b.Summary; p != nil {
		scopeSeq, err := q.BumpScopeSeq(ctx, conv.ScopeID)
		if err != nil {
			return Committed{}, err
		}
		next := conv
		next.LastSeq = seq
		next.SummarySeq = scopeSeq
		if !p.KeepRecency {
			next.UpdatedAt = formatTime(p.UpdatedAt)
		}
		if p.Status != nil {
			next.Status = *p.Status
		}
		if p.Title != nil {
			next.Title = *p.Title
		}
		if p.LastMessagePreview != nil {
			next.LastMessagePreview = *p.LastMessagePreview
		}
		if p.PendingInteractions != nil {
			next.PendingInteractions = *p.PendingInteractions
		}
		if p.QueueCount != nil {
			next.QueueCount = *p.QueueCount
		}
		if p.BackgroundTasks != nil {
			next.BackgroundTasks = *p.BackgroundTasks
		}
		if m := b.Metadata; m != nil {
			next.TitleMode, next.TitleGeneration, next.Cwd, next.ProjectID = m.TitleMode, m.TitleGeneration, m.Cwd, nullString(m.ProjectID)
			next.Pinned, next.Archived = boolInt(m.Pinned), boolInt(m.Archived)
		}
		if b.StartTurn != nil {
			next.TurnCount = conv.TurnCount + int64(len(started))
		}
		err = q.UpdateConversationSummary(ctx, sqlitedb.UpdateConversationSummaryParams{
			Status: next.Status, Title: next.Title, LastMessagePreview: next.LastMessagePreview,
			PendingInteractions: next.PendingInteractions, QueueCount: next.QueueCount, BackgroundTasks: next.BackgroundTasks,
			SummarySeq: next.SummarySeq, UpdatedAt: next.UpdatedAt, ID: conv.ID,
		})
		if err != nil {
			return Committed{}, err
		}
		committed.ScopeSeq = scopeSeq
		committed.Summary = &next
	}
	return committed, nil
}

// touchesBackgroundTasks reports whether the batch starts, updates or ends a
// background task item.
func touchesBackgroundTasks(b *Batch) bool {
	for _, e := range b.Events {
		if e.ItemID == "" {
			continue
		}
		var payload struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(e.Payload, &payload) == nil && payload.Kind == "background_task" {
			return true
		}
	}
	return false
}

func boolInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

func nonNil(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

// metadataTime is when a metadata-only batch happened: its latest event.
func metadataTime(b Batch) time.Time {
	t := time.Now()
	for i, e := range b.Events {
		if i == 0 || e.CreatedAt.After(t) {
			t = e.CreatedAt
		}
	}
	return t
}
