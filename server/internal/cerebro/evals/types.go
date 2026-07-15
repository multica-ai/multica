package evals

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Eval struct {
	ID            uuid.UUID       `json:"id"`
	WorkspaceID   uuid.UUID       `json:"workspace_id"`
	Key           string          `json:"key"`
	Version       string          `json:"version"`
	Title         string          `json:"title"`
	Description   string          `json:"description"`
	Status        string          `json:"status"`
	Owner         json.RawMessage `json:"owner"`
	Objective     string          `json:"objective"`
	Target        json.RawMessage `json:"target"`
	Datasets      json.RawMessage `json:"datasets"`
	Graders       json.RawMessage `json:"graders"`
	Thresholds    json.RawMessage `json:"thresholds"`
	Runner        json.RawMessage `json:"runner"`
	Source        json.RawMessage `json:"source"`
	CreatedByID   uuid.UUID       `json:"created_by_id"`
	CreatedByType string          `json:"created_by_type"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type EvalInput struct {
	Key         string          `json:"key"`
	Version     string          `json:"version"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Status      string          `json:"status"`
	Owner       json.RawMessage `json:"owner"`
	Objective   string          `json:"objective"`
	Target      json.RawMessage `json:"target"`
	Datasets    json.RawMessage `json:"datasets"`
	Graders     json.RawMessage `json:"graders"`
	Thresholds  json.RawMessage `json:"thresholds"`
	Runner      json.RawMessage `json:"runner"`
	Source      json.RawMessage `json:"source"`
}

type EvalRun struct {
	ID                 uuid.UUID       `json:"id"`
	WorkspaceID        uuid.UUID       `json:"workspace_id"`
	EvalID             uuid.UUID       `json:"eval_id"`
	EvalVersion        string          `json:"eval_version"`
	TargetVersion      string          `json:"target_version"`
	WorkflowID         *uuid.UUID      `json:"workflow_id,omitempty"`
	IssueID            *uuid.UUID      `json:"issue_id,omitempty"`
	Status             string          `json:"status"`
	Results            json.RawMessage `json:"results"`
	EvidenceArtifactID *uuid.UUID      `json:"evidence_artifact_id,omitempty"`
	CostCents          int64           `json:"cost_cents"`
	LatencyMS          int64           `json:"latency_ms"`
	CreatedByID        uuid.UUID       `json:"created_by_id"`
	CreatedByType      string          `json:"created_by_type"`
	StartedAt          *time.Time      `json:"started_at,omitempty"`
	CompletedAt        *time.Time      `json:"completed_at,omitempty"`
	CreatedAt          time.Time       `json:"created_at"`
}

type EvalRunInput struct {
	TargetVersion      string          `json:"target_version"`
	WorkflowID         *uuid.UUID      `json:"workflow_id"`
	IssueID            *uuid.UUID      `json:"issue_id"`
	Status             string          `json:"status"`
	Results            json.RawMessage `json:"results"`
	EvidenceArtifactID *uuid.UUID      `json:"evidence_artifact_id"`
	CostCents          int64           `json:"cost_cents"`
	LatencyMS          int64           `json:"latency_ms"`
	StartedAt          *time.Time      `json:"started_at"`
	CompletedAt        *time.Time      `json:"completed_at"`
}

type Binding struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	WorkflowID  uuid.UUID `json:"workflow_id"`
	EvalID      uuid.UUID `json:"eval_id"`
	Phase       string    `json:"phase"`
	Blocking    bool      `json:"blocking"`
	CreatedByID uuid.UUID `json:"created_by_id"`
	CreatedAt   time.Time `json:"created_at"`
	EvalKey     string    `json:"eval_key"`
	EvalVersion string    `json:"eval_version"`
	EvalTitle   string    `json:"eval_title"`
}

type BindingInput struct {
	WorkflowID uuid.UUID `json:"workflow_id"`
	EvalID     uuid.UUID `json:"eval_id"`
	Phase      string    `json:"phase"`
	Blocking   bool      `json:"blocking"`
}
