package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/dagcore"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DAGService appends events to the DAG event log and maintains projections.
// It is the write-side of the optimized DAG core integration.
type DAGService struct {
	Queries   *db.Queries
	TxStarter TxStarter
}

// NewDAGService creates a DAG service with the required dependencies.
func NewDAGService(q *db.Queries, tx TxStarter) *DAGService {
	return &DAGService{Queries: q, TxStarter: tx}
}

// AppendEvent validates a DAG core event and persists it, then updates
// projections based on the operation type. All work happens in one transaction.
func (s *DAGService) AppendEvent(ctx context.Context, workspaceID pgtype.UUID, event dagcore.Event) (db.DagEvent, error) {
	if err := dagcore.ValidateEvent(event); err != nil {
		return db.DagEvent{}, fmt.Errorf("dag event validation: %w", err)
	}

	var result db.DagEvent
	err := s.runInTx(ctx, func(q *db.Queries) error {
		dvtJSON, err := json.Marshal(event.DVT)
		if err != nil {
			return fmt.Errorf("marshal dvt: %w", err)
		}
		payloadJSON, err := json.Marshal(event.Payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}

		evt, err := q.CreateDAGEvent(ctx, db.CreateDAGEventParams{
			WorkspaceID: workspaceID,
			RecordIds:   event.RecordIDs,
			AgentID:     event.AgentID,
			Dvt:         dvtJSON,
			Operation:   string(event.Operation),
			Payload:     payloadJSON,
			Reason:      event.Reason,
		})
		if err != nil {
			return fmt.Errorf("create dag event: %w", err)
		}
		result = evt

		// Maintain projections based on operation
		switch event.Operation {
		case dagcore.OperationCreate:
			if err := s.projectCreate(ctx, q, workspaceID, event, evt.ID); err != nil {
				return err
			}
		case dagcore.OperationLink:
			if err := s.projectLink(ctx, q, workspaceID, event, evt.ID); err != nil {
				return err
			}
		case dagcore.OperationUnlink:
			if err := s.projectUnlink(ctx, q, workspaceID, event); err != nil {
				return err
			}
		case dagcore.OperationAssert:
			if err := s.projectFact(ctx, q, workspaceID, event, evt.ID); err != nil {
				return err
			}
		}
		return nil
	})
	return result, err
}

// ProjectRecord creates or updates a record projection.
func (s *DAGService) ProjectRecord(ctx context.Context, workspaceID pgtype.UUID, recordID, recordType string, createdEventID pgtype.UUID, tombstonedEventID pgtype.UUID) (db.DagRecordProjection, error) {
	var result db.DagRecordProjection
	err := s.runInTx(ctx, func(q *db.Queries) error {
		var tid pgtype.UUID
		if tombstonedEventID.Valid {
			tid = tombstonedEventID
		}
		rec, err := q.UpsertDAGRecordProjection(ctx, db.UpsertDAGRecordProjectionParams{
			WorkspaceID:       workspaceID,
			ID:                recordID,
			Type:              recordType,
			CreatedEventID:    createdEventID,
			TombstonedEventID: tid,
		})
		if err != nil {
			return fmt.Errorf("upsert record projection: %w", err)
		}
		result = rec
		return nil
	})
	return result, err
}

// ProjectLink creates or updates a link projection.
func (s *DAGService) ProjectLink(ctx context.Context, workspaceID pgtype.UUID, fromID, toID, linkType string, eventID pgtype.UUID, active bool) (db.DagLinkProjection, error) {
	var result db.DagLinkProjection
	err := s.runInTx(ctx, func(q *db.Queries) error {
		link, err := q.UpsertDAGLinkProjection(ctx, db.UpsertDAGLinkProjectionParams{
			WorkspaceID: workspaceID,
			FromID:      fromID,
			ToID:        toID,
			Type:        linkType,
			EventID:     eventID,
			Active:      active,
		})
		if err != nil {
			return fmt.Errorf("upsert link projection: %w", err)
		}
		result = link
		return nil
	})
	return result, err
}

// ProjectFact creates a fact projection.
func (s *DAGService) ProjectFact(ctx context.Context, workspaceID pgtype.UUID, predicate string, args []string, eventID pgtype.UUID, groundedBy []string, confidence *float64) (db.DagFactProjection, error) {
	var result db.DagFactProjection
	err := s.runInTx(ctx, func(q *db.Queries) error {
		argsJSON, err := json.Marshal(args)
		if err != nil {
			return fmt.Errorf("marshal args: %w", err)
		}
		var conf pgtype.Float8
		if confidence != nil {
			conf = pgtype.Float8{Float64: *confidence, Valid: true}
		}
		fact, err := q.CreateDAGFactProjection(ctx, db.CreateDAGFactProjectionParams{
			WorkspaceID: workspaceID,
			Predicate:   predicate,
			Args:        argsJSON,
			EventID:     eventID,
			GroundedBy:  groundedBy,
			Confidence:  conf,
		})
		if err != nil {
			return fmt.Errorf("create fact projection: %w", err)
		}
		result = fact
		return nil
	})
	return result, err
}

// DetectConflicts evaluates grounded facts for contradictions and returns conflict states.
func (s *DAGService) DetectConflicts(ctx context.Context, workspaceID pgtype.UUID, facts []dagcore.Fact) ([]dagcore.ConflictState, error) {
	return dagcore.DetectContradictions(facts), nil
}

// runInTx executes fn inside a single DB transaction.
func (s *DAGService) runInTx(ctx context.Context, fn func(*db.Queries) error) error {
	if s.TxStarter == nil {
		return fn(s.Queries)
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(s.Queries.WithTx(tx)); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// projection helpers used during AppendEvent

func (s *DAGService) projectCreate(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, event dagcore.Event, eventID pgtype.UUID) error {
	// Expect payload to contain record type
	recType, _ := event.Payload["type"].(string)
	if recType == "" {
		recType = "unknown"
	}
	for _, recordID := range event.RecordIDs {
		_, err := q.UpsertDAGRecordProjection(ctx, db.UpsertDAGRecordProjectionParams{
			WorkspaceID:    workspaceID,
			ID:             recordID,
			Type:           recType,
			CreatedEventID: eventID,
		})
		if err != nil {
			return fmt.Errorf("project create for %s: %w", recordID, err)
		}
	}
	return nil
}

func (s *DAGService) projectLink(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, event dagcore.Event, eventID pgtype.UUID) error {
	linkType, _ := event.Payload["link_type"].(string)
	if linkType == "" {
		linkType = "relates"
	}
	if len(event.RecordIDs) < 2 {
		return fmt.Errorf("link operation requires at least 2 record ids")
	}
	fromID := event.RecordIDs[0]
	toID := event.RecordIDs[1]
	_, err := q.UpsertDAGLinkProjection(ctx, db.UpsertDAGLinkProjectionParams{
		WorkspaceID: workspaceID,
		FromID:      fromID,
		ToID:        toID,
		Type:        linkType,
		EventID:     eventID,
		Active:      true,
	})
	return err
}

func (s *DAGService) projectUnlink(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, event dagcore.Event) error {
	linkType, _ := event.Payload["link_type"].(string)
	if linkType == "" {
		linkType = "relates"
	}
	if len(event.RecordIDs) < 2 {
		return fmt.Errorf("unlink operation requires at least 2 record ids")
	}
	fromID := event.RecordIDs[0]
	toID := event.RecordIDs[1]
	_, err := q.UpsertDAGLinkProjection(ctx, db.UpsertDAGLinkProjectionParams{
		WorkspaceID: workspaceID,
		FromID:      fromID,
		ToID:        toID,
		Type:        linkType,
		EventID:     pgtype.UUID{},
		Active:      false,
	})
	return err
}

func (s *DAGService) projectFact(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, event dagcore.Event, eventID pgtype.UUID) error {
	predicate, _ := event.Payload["predicate"].(string)
	if predicate == "" {
		predicate = "asserts"
	}
	var args []string
	if rawArgs, ok := event.Payload["args"].([]any); ok {
		for _, a := range rawArgs {
			if s, ok := a.(string); ok {
				args = append(args, s)
			}
		}
	}
	var groundedBy []string
	if raw, ok := event.Payload["grounded_by"].([]any); ok {
		for _, g := range raw {
			if s, ok := g.(string); ok {
				groundedBy = append(groundedBy, s)
			}
		}
	}
	var confidence *float64
	if raw, ok := event.Payload["confidence"].(float64); ok {
		confidence = &raw
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("marshal fact args: %w", err)
	}
	var conf pgtype.Float8
	if confidence != nil {
		conf = pgtype.Float8{Float64: *confidence, Valid: true}
	}
	_, err = q.CreateDAGFactProjection(ctx, db.CreateDAGFactProjectionParams{
		WorkspaceID: workspaceID,
		Predicate:   predicate,
		Args:        argsJSON,
		EventID:     eventID,
		GroundedBy:  groundedBy,
		Confidence:  conf,
	})
	return err
}
