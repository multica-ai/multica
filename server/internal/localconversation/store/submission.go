package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

var ErrExecutionBusy = errors.New("local conversation store: foreground execution is occupied")

// Submission is durable admission state, independent of the client's prediction.
type Submission struct {
	RequestID     string             `json:"request_id"`
	Revision      int64              `json:"revision"`
	State         string             `json:"state"`
	Item          json.RawMessage    `json:"item,omitempty"`
	Position      *int64             `json:"position"`
	BlockedReason string             `json:"blocked_reason,omitempty"`
	Attempt       *SubmissionAttempt `json:"attempt"`
	MessageID     string             `json:"message_id,omitempty"`
}

type SubmissionAttempt struct {
	AttemptID string          `json:"attempt_id"`
	RequestID string          `json:"request_id"`
	TurnIDs   []string        `json:"turn_ids"`
	Delivery  string          `json:"delivery"`
	Settings  json.RawMessage `json:"settings"`
}

// SubmissionChange joins admission, queue mutation and execution ownership in a batch.
type SubmissionChange struct {
	RequestID         string
	Item              json.RawMessage
	State             string
	Reason            string
	Claim             *SubmissionAttempt
	Delivery          string
	Release           bool
	ExpectedAttemptID string
	Legacy            bool
}

func submissionView(ctx context.Context, q *sqlitedb.Queries, row sqlitedb.PromptSubmission) (Submission, error) {
	v := Submission{RequestID: row.RequestID, Revision: row.Revision, State: row.State, Item: json.RawMessage(row.Item), BlockedReason: row.BlockedReason, MessageID: row.MessageID}
	if row.State == "queued" {
		if position, err := q.GetQueueItemPosition(ctx, sqlitedb.GetQueueItemPositionParams{ConversationID: row.ConversationID, RequestID: row.RequestID}); err == nil {
			v.Position = &position
		} else if !errors.Is(err, sql.ErrNoRows) {
			return v, err
		}
	}
	if row.AttemptID != "" {
		a, err := q.GetAttempt(ctx, sqlitedb.GetAttemptParams{ConversationID: row.ConversationID, AttemptID: row.AttemptID})
		if err != nil {
			return v, err
		}
		v.Attempt = &SubmissionAttempt{AttemptID: a.AttemptID, RequestID: a.RequestID, Delivery: a.Delivery, Settings: json.RawMessage(a.Settings), TurnIDs: []string{}}
		if err := json.Unmarshal([]byte(a.TurnIds), &v.Attempt.TurnIDs); err != nil {
			return v, err
		}
	}
	return v, nil
}

// SubmissionState reads the queue and submission records from one SQLite snapshot.
func (s *Store) SubmissionState(ctx context.Context, conversationID string, requestIDs []string) (int64, QueueState, []Submission, error) {
	tx, err := s.read.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, QueueState{}, nil, err
	}
	defer tx.Rollback()
	q := sqlitedb.New(tx)
	conv, err := q.GetConversation(ctx, conversationID)
	if err != nil {
		return 0, QueueState{}, nil, err
	}
	queue, err := readQueue(ctx, q, conversationID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, QueueState{}, nil, err
	}
	rows := []sqlitedb.PromptSubmission{}
	if requestIDs == nil {
		rows, err = q.ListOpenSubmissions(ctx, conversationID)
	} else {
		for _, id := range requestIDs {
			row, e := q.GetSubmission(ctx, sqlitedb.GetSubmissionParams{ConversationID: conversationID, RequestID: id})
			if errors.Is(e, sql.ErrNoRows) {
				continue
			}
			if e != nil {
				return 0, QueueState{}, nil, e
			}
			rows = append(rows, row)
		}
	}
	if err != nil {
		return 0, QueueState{}, nil, err
	}
	views := make([]Submission, 0, len(rows))
	for _, row := range rows {
		v, e := submissionView(ctx, q, row)
		if e != nil {
			return 0, QueueState{}, nil, e
		}
		views = append(views, v)
	}
	return conv.LastSeq, queue, views, tx.Commit()
}

func (s *Store) Execution(ctx context.Context, conversationID string) (sqlitedb.ConversationExecution, error) {
	row, err := sqlitedb.New(s.read).GetExecution(ctx, conversationID)
	if errors.Is(err, sql.ErrNoRows) {
		return row, ErrNotFound
	}
	return row, err
}

func (s *Store) Executions(ctx context.Context) ([]sqlitedb.ConversationExecution, error) {
	return sqlitedb.New(s.read).ListExecutions(ctx)
}

func putSubmission(ctx context.Context, q *sqlitedb.Queries, row sqlitedb.PromptSubmission) error {
	return q.PutSubmission(ctx, sqlitedb.PutSubmissionParams{ConversationID: row.ConversationID, RequestID: row.RequestID, Item: row.Item, State: row.State, Revision: row.Revision, AttemptID: row.AttemptID, MessageID: row.MessageID, BlockedReason: row.BlockedReason, UpdatedAt: row.UpdatedAt})
}
func putAttempt(ctx context.Context, q *sqlitedb.Queries, a sqlitedb.PromptAttempt) error {
	return q.PutAttempt(ctx, sqlitedb.PutAttemptParams{ConversationID: a.ConversationID, RequestID: a.RequestID, AttemptID: a.AttemptID, SubmissionID: a.SubmissionID, Delivery: a.Delivery, TurnIds: a.TurnIds, Settings: a.Settings})
}

// applySubmissions runs inside the writer transaction, before event sequence assignment.
func applySubmissions(ctx context.Context, q *sqlitedb.Queries, b *Batch, started []NewTurn) error {
	if b.Queue == nil && len(b.Submissions) == 0 && len(started) == 0 && b.CompleteTurn == nil {
		return nil
	}
	changed := map[string]bool{}
	now := time.Now().UTC()
	if len(b.Events) > 0 {
		now = b.Events[len(b.Events)-1].CreatedAt
	}
	load := func(id string) (sqlitedb.PromptSubmission, error) {
		return q.GetSubmission(ctx, sqlitedb.GetSubmissionParams{ConversationID: b.ConversationID, RequestID: id})
	}
	save := func(row sqlitedb.PromptSubmission) error {
		row.Revision++
		row.UpdatedAt = formatTime(now)
		changed[row.RequestID] = true
		return putSubmission(ctx, q, row)
	}
	if c := b.Queue; c != nil {
		if c.Append != nil {
			if _, err := load(c.Append.RequestID); errors.Is(err, sql.ErrNoRows) {
				if err := save(sqlitedb.PromptSubmission{ConversationID: b.ConversationID, RequestID: c.Append.RequestID, Item: string(c.Append.Item), State: "queued"}); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		}
		if c.Remove != "" && !c.RemovedAt.IsZero() {
			row, err := load(c.Remove)
			if err != nil {
				return err
			}
			if row.State == "starting" {
				return ErrExecutionBusy
			}
			row.State = "removed"
			if err := save(row); err != nil {
				return err
			}
		}
	}
	for _, c := range b.Submissions {
		if c.Legacy {
			if c.Claim != nil {
				if _, err := q.GetExecution(ctx, b.ConversationID); err == nil {
					return ErrExecutionBusy
				} else if !errors.Is(err, sql.ErrNoRows) {
					return err
				}
				turns, err := q.ListConversationTurns(ctx, b.ConversationID)
				if err != nil {
					return err
				}
				for _, t := range turns {
					if t.Status == "running" {
						return ErrExecutionBusy
					}
				}
				if err := q.ClaimExecution(ctx, sqlitedb.ClaimExecutionParams{ConversationID: b.ConversationID, RequestID: c.RequestID, AttemptID: c.Claim.AttemptID}); err != nil {
					return err
				}
				if err := putAttempt(ctx, q, sqlitedb.PromptAttempt{ConversationID: b.ConversationID, RequestID: c.Claim.RequestID, AttemptID: c.Claim.AttemptID, Delivery: "not_sent", TurnIds: "[]", Settings: "{}"}); err != nil {
					return err
				}
			} else {
				a, err := q.GetAttempt(ctx, sqlitedb.GetAttemptParams{ConversationID: b.ConversationID, AttemptID: c.ExpectedAttemptID})
				if err != nil {
					return err
				}
				if c.Delivery != "" {
					if a.Delivery != "unknown" {
						a.Delivery = c.Delivery
					}
					if err := putAttempt(ctx, q, a); err != nil {
						return err
					}
				}
				if c.Release {
					if err := q.ReleaseExecution(ctx, sqlitedb.ReleaseExecutionParams{ConversationID: b.ConversationID, AttemptID: a.AttemptID}); err != nil {
						return err
					}
				}
			}
			continue
		}
		row, err := load(c.RequestID)
		if errors.Is(err, sql.ErrNoRows) && len(c.Item) > 0 {
			row = sqlitedb.PromptSubmission{ConversationID: b.ConversationID, RequestID: c.RequestID, Item: string(c.Item), State: "queued"}
		} else if err != nil {
			return err
		}
		if c.ExpectedAttemptID != "" && row.AttemptID != c.ExpectedAttemptID {
			continue
		}
		if c.Claim != nil {
			if _, err := q.GetExecution(ctx, b.ConversationID); err == nil {
				return ErrExecutionBusy
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			turns, err := q.ListConversationTurns(ctx, b.ConversationID)
			if err != nil {
				return err
			}
			for _, t := range turns {
				if t.Status == "running" {
					return ErrExecutionBusy
				}
			}
			if err := q.ClaimExecution(ctx, sqlitedb.ClaimExecutionParams{ConversationID: b.ConversationID, RequestID: c.RequestID, AttemptID: c.Claim.AttemptID}); err != nil {
				return err
			}
			ids, _ := json.Marshal(c.Claim.TurnIDs)
			settings := string(c.Claim.Settings)
			if settings == "" {
				settings = "{}"
			}
			if err := putAttempt(ctx, q, sqlitedb.PromptAttempt{ConversationID: b.ConversationID, RequestID: c.Claim.RequestID, SubmissionID: c.RequestID, AttemptID: c.Claim.AttemptID, Delivery: "not_sent", TurnIds: string(ids), Settings: settings}); err != nil {
				return err
			}
			row.AttemptID = c.Claim.AttemptID
			row.State = "starting"
			row.BlockedReason = ""
		}
		if c.State != "" {
			row.State = c.State
			row.BlockedReason = c.Reason
		}
		if c.Delivery != "" && row.AttemptID != "" {
			a, err := q.GetAttempt(ctx, sqlitedb.GetAttemptParams{ConversationID: b.ConversationID, AttemptID: row.AttemptID})
			if err != nil {
				return err
			}
			if a.Delivery != "unknown" {
				a.Delivery = c.Delivery
			}
			if err := putAttempt(ctx, q, a); err != nil {
				return err
			}
		}
		if c.Release {
			if err := q.ReleaseExecution(ctx, sqlitedb.ReleaseExecutionParams{ConversationID: b.ConversationID, AttemptID: row.AttemptID}); err != nil {
				return err
			}
		}
		if err := save(row); err != nil {
			return err
		}
	}
	for _, t := range started {
		a, err := q.GetAttemptByRequest(ctx, sqlitedb.GetAttemptByRequestParams{ConversationID: b.ConversationID, RequestID: t.RequestID})
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		ids := []string{}
		if err := json.Unmarshal([]byte(a.TurnIds), &ids); err != nil {
			return err
		}
		found := false
		for _, id := range ids {
			found = found || id == t.ID
		}
		if !found {
			ids = append(ids, t.ID)
		}
		encoded, _ := json.Marshal(ids)
		a.TurnIds = string(encoded)
		a.Delivery = "intent_recorded"
		if err := putAttempt(ctx, q, a); err != nil {
			return err
		}
		if a.SubmissionID == "" {
			continue
		}
		for i, event := range b.Events {
			if event.Type != "user.message" || event.TurnID != t.ID {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return err
			}
			if payload==nil{return errors.New("user message payload must be an object")}
            payload["submission_id"] = a.SubmissionID
			encoded, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			b.Events[i].Payload = encoded
		}
		row, err := load(a.SubmissionID)
		if err != nil {
			return err
		}
		row.State = "attached"
		row.MessageID = "msg-" + t.RequestID
		if err := save(row); err != nil {
			return err
		}
		items, err := q.ListPromptQueueItems(ctx, b.ConversationID)
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.RequestID != a.SubmissionID {
				continue
			}
			queue, err := q.GetPromptQueue(ctx, b.ConversationID)
			if err != nil {
				return err
			}
			change := QueueChange{Revision: queue.Revision + 1, Paused: queue.Paused == 1, Error: queue.Error, Remove: a.SubmissionID}
			if err := applyQueue(ctx, q, b.ConversationID, &change); err != nil {
				return err
			}
			count := int64(len(items) - 1)
			if b.Summary == nil {
				b.Summary = &SummaryPatch{UpdatedAt: now}
			}
			b.Summary.QueueCount = &count
			ids := []string{}
			for _, queued := range items {
				if queued.RequestID != a.SubmissionID {
					ids = append(ids, queued.RequestID)
				}
			}
			payload, _ := json.Marshal(map[string]any{"revision": change.Revision, "paused": change.Paused, "error": change.Error, "count": count, "reason": "dispatched", "request_ids": ids})
			b.Events = append(b.Events, NewEvent{Type: "queue.updated", Payload: payload, CreatedAt: now})
		}
	}
	if c := b.CompleteTurn; c != nil {
		slot, err := q.GetExecution(ctx, b.ConversationID)
		if err == nil {
			a, err := q.GetAttempt(ctx, sqlitedb.GetAttemptParams{ConversationID: b.ConversationID, AttemptID: slot.AttemptID})
			if err != nil {
				return err
			}
			ids := []string{}
			if err := json.Unmarshal([]byte(a.TurnIds), &ids); err != nil {
				return err
			}
			owns, settled := false, true
			unknown := c.ErrorKind == "outcome_unknown"
			unknownReason := c.Error
			for _, id := range ids {
				if id == c.TurnID {
					owns = true
					continue
				}
				t, err := q.GetTurn(ctx, id)
				if err != nil {
					return err
				}
				settled = settled && t.Status != "running"
				unknown = unknown || t.ErrorKind == "outcome_unknown"
				if t.ErrorKind == "outcome_unknown" {
					unknownReason = t.Error
				}
			}
			if owns && unknown {
				a.Delivery = "unknown"
				if err := putAttempt(ctx, q, a); err != nil {
					return err
				}
			}
			if owns && settled {
				if unknown && a.SubmissionID != "" {
					row, err := load(slot.RequestID)
					if err != nil {
						return err
					}
					row.State = "recovery_required"
					row.BlockedReason = unknownReason
					a.Delivery = "unknown"
					if err := putAttempt(ctx, q, a); err != nil {
						return err
					}
					if err := save(row); err != nil {
						return err
					}
				} else {
					a.Delivery = "confirmed"
					if err := putAttempt(ctx, q, a); err != nil {
						return err
					}
					if err := q.ReleaseExecution(ctx, sqlitedb.ReleaseExecutionParams{ConversationID: b.ConversationID, AttemptID: slot.AttemptID}); err != nil {
						return err
					}
				}
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	queue, err := q.GetPromptQueue(ctx, b.ConversationID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	ids := make([]string, 0, len(changed))
	for id := range changed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		row, err := load(id)
		if err != nil {
			return err
		}
		view, err := submissionView(ctx, q, row)
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"submission": view, "queue_revision": queue.Revision})
		b.Events = append(b.Events, NewEvent{Type: "submission.updated", Payload: payload, CreatedAt: now})
	}
	return nil
}

func (s *Store) Attempt(ctx context.Context, conversationID, attemptID string) (sqlitedb.PromptAttempt, error) {
	return sqlitedb.New(s.read).GetAttempt(ctx, sqlitedb.GetAttemptParams{ConversationID: conversationID, AttemptID: attemptID})
}
func (s *Store) Attempts(ctx context.Context, conversationID, submissionID string) ([]sqlitedb.PromptAttempt, error) {
	return sqlitedb.New(s.read).ListAttempts(ctx, sqlitedb.ListAttemptsParams{ConversationID: conversationID, SubmissionID: submissionID})
}

func (s *Store) QueueOperation(ctx context.Context, id, operation string) (sqlitedb.QueueOperation, error) {
	row, err := sqlitedb.New(s.read).GetQueueOperation(ctx, sqlitedb.GetQueueOperationParams{ConversationID: id, OperationID: operation})
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return row, err
}
