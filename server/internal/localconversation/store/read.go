package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

// TurnView is one turn as the GUI renders it: its messages, the final event
// of every finished item, and the events of items still in progress.
type TurnView struct {
	Turn     sqlitedb.Turn
	Messages []sqlitedb.Message
	// Events holds final events plus, for unfinished items and item-less
	// events of a running turn, every event in order.
	Events []sqlitedb.Event
	// ItemStarts is when each item began. A settled turn keeps only each item's
	// final event, and clients order and time items by their start.
	ItemStarts map[string]ItemStart
}

// ItemStart is an item's first event.
type ItemStart struct {
	Seq       int64
	CreatedAt string
}

// ConversationSummary adds derived list metadata without changing stored activity time.
type ConversationSummary struct {
	sqlitedb.Conversation
	LastTurnSentAt string
}

// Summary reads a conversation and its latest user turn's send time together.
func (s *Store) Summary(ctx context.Context, conversationID string) (ConversationSummary, error) {
	row, err := sqlitedb.New(s.read).GetConversationSummary(ctx, conversationID)
	if errors.Is(err, sql.ErrNoRows) {
		return ConversationSummary{}, fmt.Errorf("%w: conversation %s", ErrNotFound, conversationID)
	}
	return ConversationSummary{Conversation: row.Conversation, LastTurnSentAt: row.LastTurnSentAt}, err
}

// Snapshot is a bounded first screen of a conversation.
type Snapshot struct {
	Conversation        ConversationSummary
	Turns               []TurnView
	PendingInteractions []sqlitedb.InteractionRequest
	// SnapshotSeq is the last event included; subscribe with after_seq set to it.
	SnapshotSeq int64
	Queue       QueueState
	Submissions []Submission
	// HasOlderTurns reports whether turns before Turns[0] exist.
	HasOlderTurns bool
	// OpenBackgroundItems is the latest event of each background item still
	// running in a turn older than Turns. Later events of those items arrive with
	// no loaded turn, so a client needs them to know the item at all.
	OpenBackgroundItems []sqlitedb.Event
}

// Snapshot reads the latest turnLimit turns of a conversation in one read
// transaction, so SnapshotSeq matches exactly what was read.
func (s *Store) Snapshot(ctx context.Context, conversationID string, turnLimit int) (Snapshot, error) {
	tx, err := s.read.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Snapshot{}, err
	}
	defer tx.Rollback()
	q := sqlitedb.New(tx)

	row, err := q.GetConversationSummary(ctx, conversationID)
	if errors.Is(err, sql.ErrNoRows) {
		return Snapshot{}, fmt.Errorf("%w: conversation %s", ErrNotFound, conversationID)
	}
	if err != nil {
		return Snapshot{}, err
	}
	turns, err := q.ListRecentTurns(ctx, sqlitedb.ListRecentTurnsParams{ConversationID: conversationID, RowLimit: int64(turnLimit)})
	if err != nil {
		return Snapshot{}, err
	}
	views, err := turnViews(ctx, q, conversationID, reverseTurns(turns))
	if err != nil {
		return Snapshot{}, err
	}
	pending, err := q.ListPendingInteractions(ctx, conversationID)
	if err != nil {
		return Snapshot{}, err
	}
	background, err := q.ListBackgroundTaskEvents(ctx, conversationID)
	if err != nil {
		return Snapshot{}, err
	}
	loaded := make(map[string]bool, len(turns))
	for _, t := range turns {
		loaded[t.ID] = true
	}
	var openOutside []sqlitedb.Event
	for _, e := range latestOpenEvents(background) {
		if !loaded[e.TurnID.String] {
			openOutside = append(openOutside, e)
		}
	}
	queue, err := readQueue(ctx, q, conversationID)
	if err != nil {
		return Snapshot{}, err
	}
	rows, err := q.ListOpenSubmissions(ctx, conversationID)
	if err != nil {
		return Snapshot{}, err
	}
	submissions := make([]Submission, 0, len(rows))
	for _, row := range rows {
		v, err := submissionView(ctx, q, row)
		if err != nil {
			return Snapshot{}, err
		}
		submissions = append(submissions, v)
	}
	conv := ConversationSummary{Conversation: row.Conversation, LastTurnSentAt: row.LastTurnSentAt}
	snap := Snapshot{Queue: queue, Submissions: submissions, Conversation: conv, Turns: views, PendingInteractions: pending, SnapshotSeq: conv.LastSeq, OpenBackgroundItems: openOutside}
	if len(turns) > 0 {
		snap.HasOlderTurns = turns[len(turns)-1].TurnIndex > 0
	}
	return snap, tx.Commit()
}

// TurnsBefore pages history backwards from beforeIndex (exclusive).
func (s *Store) TurnsBefore(ctx context.Context, conversationID string, beforeIndex int64, limit int) ([]TurnView, error) {
	tx, err := s.read.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := sqlitedb.New(tx)
	turns, err := q.ListTurnsBefore(ctx, sqlitedb.ListTurnsBeforeParams{ConversationID: conversationID, BeforeIndex: beforeIndex, RowLimit: int64(limit)})
	if err != nil {
		return nil, err
	}
	views, err := turnViews(ctx, q, conversationID, reverseTurns(turns))
	if err != nil {
		return nil, err
	}
	return views, tx.Commit()
}

// EventsAfter returns up to limit events with afterSeq < seq <= throughSeq,
// for catch-up replay. Every event in the range is returned, final or not.
func (s *Store) EventsAfter(ctx context.Context, conversationID string, afterSeq, throughSeq int64, limit int) ([]sqlitedb.Event, error) {
	return sqlitedb.New(s.read).ListEventsAfter(ctx, sqlitedb.ListEventsAfterParams{
		ConversationID: conversationID, AfterSeq: afterSeq, ThroughSeq: throughSeq, RowLimit: int64(limit),
	})
}

// SummariesAfter returns conversation summaries changed after afterScopeSeq,
// oldest change first, and the scope's current sequence.
func (s *Store) SummariesAfter(ctx context.Context, scopeID, afterScopeSeq int64, limit int) ([]ConversationSummary, int64, error) {
	tx, err := s.read.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	q := sqlitedb.New(tx)
	scopeSeq, err := q.GetScopeSeq(ctx, scopeID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, err
	}
	rows, err := q.ListSummariesAfter(ctx, sqlitedb.ListSummariesAfterParams{ScopeID: scopeID, AfterScopeSeq: afterScopeSeq, RowLimit: int64(limit)})
	if err != nil {
		return nil, 0, err
	}
	summaries := make([]ConversationSummary, len(rows))
	for i, row := range rows {
		summaries[i] = ConversationSummary{Conversation: row.Conversation, LastTurnSentAt: row.LastTurnSentAt}
	}
	return summaries, scopeSeq, tx.Commit()
}

// RunningTurns lists turns still marked running, for restart reconciliation.
func (s *Store) RunningTurns(ctx context.Context) ([]sqlitedb.Turn, error) {
	return sqlitedb.New(s.read).ListRunningTurns(ctx)
}

func turnViews(ctx context.Context, q *sqlitedb.Queries, conversationID string, turns []sqlitedb.Turn) ([]TurnView, error) {
	background, err := q.ListBackgroundTaskEvents(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	openTasks := map[string]string{}
	for _, event := range background {
		if event.Final == 1 {
			delete(openTasks, event.ItemID.String)
		} else {
			openTasks[event.ItemID.String] = event.TurnID.String
		}
	}
	backgroundTurns := map[string]bool{}
	for _, turnID := range openTasks {
		backgroundTurns[turnID] = true
	}
	views := make([]TurnView, 0, len(turns))
	for _, t := range turns {
		messages, err := q.ListTurnMessages(ctx, sqlitedb.ListTurnMessagesParams{ConversationID: conversationID, TurnID: t.ID})
		if err != nil {
			return nil, err
		}
		turnID := sql.NullString{String: t.ID, Valid: true}
		var events []sqlitedb.Event
		if t.Status == "running" || backgroundTurns[t.ID] {
			all, err := q.ListTurnEvents(ctx, sqlitedb.ListTurnEventsParams{ConversationID: conversationID, TurnID: turnID})
			if err != nil {
				return nil, err
			}
			events = runningTurnEvents(all)
		} else {
			events, err = q.ListTurnFinalEvents(ctx, sqlitedb.ListTurnFinalEventsParams{ConversationID: conversationID, TurnID: turnID})
			if err != nil {
				return nil, err
			}
		}
		starts, err := q.ListTurnItemStarts(ctx, sqlitedb.ListTurnItemStartsParams{ConversationID: conversationID, TurnID: turnID})
		if err != nil {
			return nil, err
		}
		itemStarts := make(map[string]ItemStart, len(starts))
		for _, s := range starts {
			if first, ok := s.FirstSeq.(int64); ok {
				itemStarts[s.ItemID.String] = ItemStart{Seq: first, CreatedAt: s.CreatedAt}
			}
		}
		views = append(views, TurnView{Turn: t, Messages: messages, Events: events, ItemStarts: itemStarts})
	}
	return views, nil
}

// runningTurnEvents keeps final events, every event of items that have not
// finished, and item-less events, dropping deltas already replaced by a final
// event.
func runningTurnEvents(all []sqlitedb.Event) []sqlitedb.Event {
	finished := map[string]bool{}
	for _, e := range all {
		if e.Final == 1 && e.ItemID.Valid {
			finished[e.ItemID.String] = true
		}
	}
	kept := make([]sqlitedb.Event, 0, len(all))
	for _, e := range all {
		if e.Final == 1 || !e.ItemID.Valid || !finished[e.ItemID.String] {
			kept = append(kept, e)
		}
	}
	return kept
}

func reverseTurns(turns []sqlitedb.Turn) []sqlitedb.Turn {
	out := make([]sqlitedb.Turn, len(turns))
	for i, t := range turns {
		out[len(turns)-1-i] = t
	}
	return out
}

// LastSeq returns the conversation's last committed event sequence.
func (s *Store) LastSeq(ctx context.Context, conversationID string) (int64, error) {
	conv, err := sqlitedb.New(s.read).GetConversation(ctx, conversationID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: conversation %s", ErrNotFound, conversationID)
	}
	return conv.LastSeq, err
}

// CommittedFromRow converts a stored event to the form the writer publishes.
func CommittedFromRow(e sqlitedb.Event) CommittedEvent {
	created, _ := time.Parse(time.RFC3339Nano, e.CreatedAt)
	return CommittedEvent{
		ConversationID: e.ConversationID, Seq: e.Seq, TurnID: e.TurnID.String, TurnSeq: e.TurnSeq.Int64,
		ItemID: e.ItemID.String, Type: e.Type, Final: e.Final == 1, Payload: json.RawMessage(e.Payload), CreatedAt: created,
	}
}

// TurnEvents returns every event of one turn in order.
func (s *Store) TurnEvents(ctx context.Context, conversationID, turnID string) ([]sqlitedb.Event, error) {
	return sqlitedb.New(s.read).ListTurnEvents(ctx, sqlitedb.ListTurnEventsParams{ConversationID: conversationID, TurnID: sql.NullString{String: turnID, Valid: true}})
}

// Conversation returns one conversation row.
func (s *Store) Conversation(ctx context.Context, conversationID string) (sqlitedb.Conversation, error) {
	conv, err := sqlitedb.New(s.read).GetConversation(ctx, conversationID)
	if errors.Is(err, sql.ErrNoRows) {
		return conv, fmt.Errorf("%w: conversation %s", ErrNotFound, conversationID)
	}
	return conv, err
}

// ScopeConversations lists a scope's conversations, most recently updated
// first.
func (s *Store) ScopeConversations(ctx context.Context, scopeID int64) ([]sqlitedb.Conversation, error) {
	return sqlitedb.New(s.read).ListScopeConversations(ctx, scopeID)
}

// Messages returns a conversation's messages in chronological order.
func (s *Store) Messages(ctx context.Context, conversationID string) ([]sqlitedb.Message, error) {
	return sqlitedb.New(s.read).ListConversationMessages(ctx, conversationID)
}

// Turn returns one turn of a conversation.
func (s *Store) Turn(ctx context.Context, conversationID, turnID string) (sqlitedb.Turn, error) {
	turn, err := sqlitedb.New(s.read).GetTurn(ctx, turnID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && turn.ConversationID != conversationID) {
		return sqlitedb.Turn{}, fmt.Errorf("%w: turn %s", ErrNotFound, turnID)
	}
	return turn, err
}

// OpenBackgroundTasks returns the provider task ids of the conversation's
// background tasks that have no final event yet.
func (s *Store) OpenBackgroundTasks(ctx context.Context, conversationID string) ([]string, error) {
	rows, err := s.OpenBackgroundTaskItems(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range rows {
		var payload struct {
			TaskID string `json:"task_id"`
		}
		_ = json.Unmarshal([]byte(e.Payload), &payload)
		if payload.TaskID != "" {
			ids = append(ids, payload.TaskID)
		}
	}
	return ids, nil
}

// ConversationsWithBackgroundTasks lists conversations showing running
// background tasks.
func (s *Store) ConversationsWithBackgroundTasks(ctx context.Context) ([]string, error) {
	return sqlitedb.New(s.read).ListConversationsWithBackgroundTasks(ctx)
}

// OpenBackgroundTaskItems returns the latest event of each background task
// item that has no final event yet, in the order the items started.
func (s *Store) OpenBackgroundTaskItems(ctx context.Context, conversationID string) ([]sqlitedb.Event, error) {
	rows, err := sqlitedb.New(s.read).ListBackgroundTaskEvents(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	return latestOpenEvents(rows), nil
}

// latestOpenEvents keeps the last event of each item with no final event.
func latestOpenEvents(rows []sqlitedb.Event) []sqlitedb.Event {
	latest := map[string]sqlitedb.Event{}
	var order []string
	for _, e := range rows {
		id := e.ItemID.String
		if e.Final == 1 {
			delete(latest, id)
			continue
		}
		if _, seen := latest[id]; !seen {
			order = append(order, id)
		}
		latest[id] = e
	}
	var open []sqlitedb.Event
	for _, id := range order {
		if e, ok := latest[id]; ok {
			open = append(open, e)
			delete(latest, id)
		}
	}
	return open
}

// ConversationTurns lists a conversation's turns in order.
func (s *Store) ConversationTurns(ctx context.Context, conversationID string) ([]sqlitedb.Turn, error) {
	return sqlitedb.New(s.read).ListConversationTurns(ctx, conversationID)
}
