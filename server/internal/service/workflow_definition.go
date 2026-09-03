package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

var ErrInvalidWorkflowDefinition = errors.New("invalid workflow definition")

type WorkflowStageSpec struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type WorkflowDefinitionSpec struct {
	SchemaVersion int                 `json:"schema_version"`
	Stages        []WorkflowStageSpec `json:"stages"`
}

func ValidateWorkflowDefinition(raw json.RawMessage) (WorkflowDefinitionSpec, error) {
	var spec WorkflowDefinitionSpec
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&spec); err != nil {
		return spec, fmt.Errorf("%w: %v", ErrInvalidWorkflowDefinition, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return spec, fmt.Errorf("%w: trailing JSON", ErrInvalidWorkflowDefinition)
	}
	if spec.SchemaVersion != 1 {
		return spec, fmt.Errorf("%w: schema_version must be 1", ErrInvalidWorkflowDefinition)
	}
	if len(spec.Stages) < 1 || len(spec.Stages) > 32 {
		return spec, fmt.Errorf("%w: stages must contain 1 to 32 entries", ErrInvalidWorkflowDefinition)
	}

	seen := make(map[string]struct{}, len(spec.Stages))
	for i := range spec.Stages {
		spec.Stages[i].Key = strings.TrimSpace(spec.Stages[i].Key)
		spec.Stages[i].Name = strings.TrimSpace(spec.Stages[i].Name)
		if spec.Stages[i].Key == "" || spec.Stages[i].Name == "" {
			return spec, fmt.Errorf("%w: stage key and name are required", ErrInvalidWorkflowDefinition)
		}
		if _, ok := seen[spec.Stages[i].Key]; ok {
			return spec, fmt.Errorf("%w: duplicate stage key %q", ErrInvalidWorkflowDefinition, spec.Stages[i].Key)
		}
		seen[spec.Stages[i].Key] = struct{}{}
	}
	return spec, nil
}

type WorkflowService struct {
	Queries   *db.Queries
	TxStarter TxStarter
}

func NewWorkflowService(q *db.Queries, tx TxStarter) *WorkflowService {
	return &WorkflowService{Queries: q, TxStarter: tx}
}

type CreateWorkflowDefinitionParams struct {
	WorkspaceID pgtype.UUID
	Name        string
	Definition  json.RawMessage
	CreatedBy   pgtype.UUID
}

func (s *WorkflowService) CreateDefinition(ctx context.Context, p CreateWorkflowDefinitionParams) (db.WorkflowDefinition, error) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return db.WorkflowDefinition{}, fmt.Errorf("%w: workflow name is required", ErrInvalidWorkflowDefinition)
	}
	spec, err := ValidateWorkflowDefinition(p.Definition)
	if err != nil {
		return db.WorkflowDefinition{}, err
	}
	canonical, err := json.Marshal(spec)
	if err != nil {
		return db.WorkflowDefinition{}, fmt.Errorf("marshal workflow definition: %w", err)
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.WorkflowDefinition{}, fmt.Errorf("begin workflow definition tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	if err := qtx.LockWorkflowDefinitionVersionKey(ctx, db.LockWorkflowDefinitionVersionKeyParams{
		WorkspaceID: util.UUIDToString(p.WorkspaceID),
		Name:        name,
	}); err != nil {
		return db.WorkflowDefinition{}, fmt.Errorf("lock workflow definition version: %w", err)
	}

	version := int32(1)
	storedName := name
	latest, err := qtx.GetLatestWorkflowDefinitionVersionByName(ctx, db.GetLatestWorkflowDefinitionVersionByNameParams{
		WorkspaceID: p.WorkspaceID,
		Name:        name,
	})
	if err == nil {
		version = latest.Version + 1
		storedName = latest.Name
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return db.WorkflowDefinition{}, fmt.Errorf("get latest workflow definition: %w", err)
	}
	created, err := qtx.CreateWorkflowDefinition(ctx, db.CreateWorkflowDefinitionParams{
		ID:          dbid.NewV7(),
		WorkspaceID: p.WorkspaceID,
		Name:        storedName,
		Version:     version,
		Definition:  canonical,
		CreatedBy:   p.CreatedBy,
	})
	if err != nil {
		return db.WorkflowDefinition{}, fmt.Errorf("create workflow definition: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.WorkflowDefinition{}, fmt.Errorf("commit workflow definition: %w", err)
	}
	return created, nil
}
