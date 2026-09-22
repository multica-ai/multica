package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// RunNodeDetail is deliberately separate from Run. A run response carries
// the current state; this response carries the attempt history needed to
// explain retries, rework generations and late results.
type RunNodeDetail struct {
	RunID        string          `json:"run_id"`
	NodeID       string          `json:"node_id"`
	ActivationID string          `json:"activation_id,omitempty"`
	Generation   int             `json:"generation,omitempty"`
	Definition   Node            `json:"definition"`
	Current      NodeRun         `json:"current"`
	Attempts     []NodeRun       `json:"attempts"`
	Outputs      []RunNodeOutput `json:"outputs"`
}

type RunNodeOutput struct {
	ID           string     `json:"id"`
	ActivationID string     `json:"activation_id"`
	Schema       any        `json:"schema"`
	Values       any        `json:"values"`
	ArtifactRefs []any      `json:"artifact_refs"`
	ContentHash  string     `json:"content_hash,omitempty"`
	SupersededAt *time.Time `json:"superseded_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (s *Service) GetRunNode(ctx context.Context, ws, wid, rid, activationOrNodeID string) (RunNodeDetail, error) {
	run, err := s.GetRun(ctx, ws, wid, rid)
	if err != nil {
		return RunNodeDetail{}, err
	}

	definitionByID := make(map[string]Node, len(run.Graph.Nodes))
	for _, node := range run.Graph.Nodes {
		definitionByID[node.ID] = node
	}
	var current NodeRun
	for _, node := range run.Nodes {
		if node.NodeID == activationOrNodeID || node.ActivationID == activationOrNodeID {
			current = node
			break
		}
	}
	if current.NodeID == "" {
		// A rework or manual takeover can replace the current activation while
		// the old activation remains part of the audit trail. Prefer the
		// normalized activation projection and keep the legacy body as a
		// compatibility fallback for pre-v2 runs.
		normalized, found, normalizedErr := s.normalizedActivationByID(ctx, ws, rid, activationOrNodeID)
		if normalizedErr != nil {
			return RunNodeDetail{}, normalizedErr
		}
		if found {
			current = normalized
		} else {
			historical, historicalErr := nodeAttemptByActivation(ctx, s.DB, rid, activationOrNodeID)
			if historicalErr != nil {
				return RunNodeDetail{}, historicalErr
			}
			current = historical
		}
	}

	detail := RunNodeDetail{
		RunID:        run.ID,
		NodeID:       current.NodeID,
		ActivationID: current.ActivationID,
		Generation:   current.Generation,
		Definition:   definitionByID[current.NodeID],
		Current:      current,
		Attempts:     []NodeRun{},
		Outputs:      []RunNodeOutput{},
	}
	rows, queryErr := s.DB.Query(ctx, `SELECT body FROM workflow_node_run WHERE run_id=$1 AND node_id=$2 ORDER BY attempt`, rid, current.NodeID)
	if queryErr != nil {
		return RunNodeDetail{}, queryErr
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if scanErr := rows.Scan(&raw); scanErr != nil {
			return RunNodeDetail{}, scanErr
		}
		var attempt NodeRun
		if unmarshalErr := json.Unmarshal(raw, &attempt); unmarshalErr != nil {
			return RunNodeDetail{}, unmarshalErr
		}
		detail.Attempts = append(detail.Attempts, attempt)
	}
	if err = rows.Err(); err != nil {
		return RunNodeDetail{}, err
	}
	if len(detail.Attempts) == 0 && current.ActivationID != "" {
		attempts, attemptsErr := s.normalizedAttempts(ctx, ws, current.ActivationID)
		if attemptsErr != nil {
			return RunNodeDetail{}, attemptsErr
		}
		detail.Attempts = attempts
	}
	if len(detail.Attempts) == 0 && current.Attempt > 0 {
		detail.Attempts = append(detail.Attempts, current)
	}
	outputRows, outputErr := s.DB.Query(ctx, `SELECT output.id::text,output.activation_id::text,output.schema_snapshot,output."values",output.artifact_refs,output.content_hash,output.superseded_at,output.created_at
FROM workflow_output output
JOIN workflow_node_activation activation ON activation.id=output.activation_id AND activation.workspace_id=output.workspace_id
WHERE output.workspace_id=$1 AND activation.run_id=$2 AND activation.node_id=$3
ORDER BY output.created_at,output.id`, ws, rid, current.NodeID)
	if outputErr != nil {
		return RunNodeDetail{}, outputErr
	}
	defer outputRows.Close()
	for outputRows.Next() {
		var output RunNodeOutput
		var schemaRaw, valuesRaw, refsRaw []byte
		if scanErr := outputRows.Scan(&output.ID, &output.ActivationID, &schemaRaw, &valuesRaw, &refsRaw, &output.ContentHash, &output.SupersededAt, &output.CreatedAt); scanErr != nil {
			return RunNodeDetail{}, scanErr
		}
		if unmarshalErr := json.Unmarshal(schemaRaw, &output.Schema); unmarshalErr != nil {
			return RunNodeDetail{}, unmarshalErr
		}
		if unmarshalErr := json.Unmarshal(valuesRaw, &output.Values); unmarshalErr != nil {
			return RunNodeDetail{}, unmarshalErr
		}
		if unmarshalErr := json.Unmarshal(refsRaw, &output.ArtifactRefs); unmarshalErr != nil {
			return RunNodeDetail{}, unmarshalErr
		}
		detail.Outputs = append(detail.Outputs, output)
	}
	if outputErr = outputRows.Err(); outputErr != nil {
		return RunNodeDetail{}, outputErr
	}
	return detail, nil
}

func (s *Service) normalizedActivationByID(ctx context.Context, ws, runID, activationID string) (NodeRun, bool, error) {
	parsed, err := parseUUIDText(activationID)
	if err != nil {
		return NodeRun{}, false, nil
	}
	var node NodeRun
	var issueID, outputID, scopeID *string
	var reason string
	err = s.DB.QueryRow(ctx, `SELECT node_id,generation,activation_no,scope_instance_id::text,status,reason_code,issue_id::text,effective_output_id::text
FROM workflow_node_activation WHERE workspace_id=$1 AND run_id=$2 AND id=$3`, ws, runID, parsed).
		Scan(&node.NodeID, &node.Generation, &node.ActivationNo, &scopeID, &node.Status, &reason, &issueID, &outputID)
	if errors.Is(err, pgx.ErrNoRows) {
		return NodeRun{}, false, nil
	}
	if err != nil {
		return NodeRun{}, false, err
	}
	node.ActivationID = activationID
	if node.ActivationNo <= 0 {
		node.ActivationNo = 1
	}
	node.ScopeInstanceID = stringPtrValue(scopeID)
	node.IssueID = stringPtrValue(issueID)
	node.OutputID = stringPtrValue(outputID)
	node.ReasonCode = reason
	if node.OutputID != "" {
		var output []byte
		if outputErr := s.DB.QueryRow(ctx, `SELECT "values" FROM workflow_output WHERE workspace_id=$1 AND id=$2`, ws, uuid(node.OutputID)).Scan(&output); outputErr == nil {
			node.Output = string(output)
		} else if !errors.Is(outputErr, pgx.ErrNoRows) {
			return NodeRun{}, false, outputErr
		}
	}
	return node, true, nil
}

func (s *Service) normalizedAttempts(ctx context.Context, ws, activationID string) ([]NodeRun, error) {
	parsed, err := parseUUIDText(activationID)
	if err != nil {
		return nil, nil
	}
	rows, err := s.DB.Query(ctx, `SELECT attempt_no,task_id::text,status,error_class,error_detail FROM workflow_node_attempt WHERE workspace_id=$1 AND activation_id=$2 ORDER BY attempt_no`, ws, parsed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	attempts := make([]NodeRun, 0)
	for rows.Next() {
		var attempt NodeRun
		var taskID *string
		if err := rows.Scan(&attempt.Attempt, &taskID, &attempt.Status, &attempt.ReasonCode, &attempt.Error); err != nil {
			return nil, err
		}
		if attempt.Status == "completed" {
			attempt.Status = "succeeded"
		}
		attempt.TaskID = stringPtrValue(taskID)
		attempt.ActivationID = activationID
		attempts = append(attempts, attempt)
	}
	return attempts, rows.Err()
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nodeAttemptByActivation(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, runID, activationID string) (NodeRun, error) {
	var raw []byte
	err := q.QueryRow(ctx, `SELECT body FROM workflow_node_run WHERE run_id=$1 AND body->>'activation_id'=$2 ORDER BY attempt DESC LIMIT 1`, runID, activationID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return NodeRun{}, &Error{Status: 404, Message: "workflow node activation not found", Code: "workflow_node_not_found"}
	}
	if err != nil {
		return NodeRun{}, err
	}
	var node NodeRun
	if err = json.Unmarshal(raw, &node); err != nil {
		return NodeRun{}, err
	}
	return node, nil
}
