package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
)

func ensureRootScope(ctx context.Context, tx pgx.Tx, run *Run) error {
	if run.RootScopeID == "" {
		var existing string
		err := tx.QueryRow(ctx, `SELECT id::text FROM workflow_scope_instance WHERE workspace_id=$1 AND run_id=$2 AND definition_scope_id='root' AND parent_scope_id IS NULL ORDER BY generation DESC,created_at DESC LIMIT 1`, run.WorkspaceID, run.ID).Scan(&existing)
		if err == nil {
			run.RootScopeID = existing
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	} else {
		if _, err := parseUUIDText(run.RootScopeID); err != nil {
			run.RootScopeID = ""
		}
	}
	if run.RootScopeID != "" {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workflow_scope_instance WHERE id=$1 AND workspace_id=$2 AND run_id=$3 AND definition_scope_id='root' AND parent_scope_id IS NULL)`, run.RootScopeID, run.WorkspaceID, run.ID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			run.RootScopeID = ""
			var existing string
			err := tx.QueryRow(ctx, `SELECT id::text FROM workflow_scope_instance WHERE workspace_id=$1 AND run_id=$2 AND definition_scope_id='root' AND parent_scope_id IS NULL ORDER BY generation DESC,created_at DESC LIMIT 1`, run.WorkspaceID, run.ID).Scan(&existing)
			if err == nil {
				run.RootScopeID = existing
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
	}
	if run.RootScopeID == "" {
		run.RootScopeID = id()
	}
	_, err := tx.Exec(ctx, `INSERT INTO workflow_scope_instance(id,workspace_id,run_id,definition_scope_id,generation,rework_count,state,branch_states)
VALUES($1,$2,$3,'root',1,0,$4,'{}'::jsonb)
ON CONFLICT DO NOTHING`, run.RootScopeID, run.WorkspaceID, run.ID, normalizedScopeState(run.Status))
	return err
}

func normalizedScopeState(status string) string {
	if terminal(status) {
		return "completed"
	}
	return status
}

func ensureNormalizedScope(ctx context.Context, tx pgx.Tx, run *Run, node *NodeRun, scopeID string, generation int) error {
	definitionScopeID := node.ScopeDefinitionID
	if definitionScopeID == "" || definitionScopeID == "root" || scopeID == run.RootScopeID {
		node.ScopeDefinitionID = "root"
		return nil
	}
	parentScopeID, err := parseUUIDText(run.RootScopeID)
	if err != nil {
		return err
	}
	if _, err = parseUUIDText(scopeID); err != nil {
		return err
	}
	reworkCount := 0
	if run.ReworkCounts != nil {
		reworkCount = run.ReworkCounts[definitionScopeID]
	}
	_, err = tx.Exec(ctx, `INSERT INTO workflow_scope_instance(
id,workspace_id,run_id,definition_scope_id,parent_scope_id,generation,rework_count,state,branch_states
)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,'{}'::jsonb)
ON CONFLICT (workspace_id,run_id,definition_scope_id,parent_scope_id,generation) DO UPDATE SET
rework_count=EXCLUDED.rework_count,state=EXCLUDED.state`, scopeID, run.WorkspaceID, run.ID, definitionScopeID, parentScopeID, generation, reworkCount, normalizedScopeState(run.Status))
	return err
}

func persistNormalizedRun(ctx context.Context, tx pgx.Tx, run *Run) error {
	if err := ensureRootScope(ctx, tx, run); err != nil {
		return err
	}
	states := make(map[string]*NodeRun, len(run.Nodes))
	for i := range run.Nodes {
		states[run.Nodes[i].NodeID] = &run.Nodes[i]
	}
	definitions := make(map[string]Node, len(run.Graph.Nodes))
	for _, node := range run.Graph.Nodes {
		definitions[node.ID] = node
	}
	for i := range run.Nodes {
		nodeRun := &run.Nodes[i]
		if nodeRun.ActivationID == "" {
			continue
		}
		activationID, err := parseUUIDText(nodeRun.ActivationID)
		if err != nil {
			continue
		}
		scopeID := nodeRun.ScopeInstanceID
		if scopeID == "" {
			scopeID = run.RootScopeID
			nodeRun.ScopeInstanceID = scopeID
		}
		if _, err = parseUUIDText(scopeID); err != nil {
			continue
		}
		generation := nodeRun.Generation
		if generation <= 0 {
			generation = 1
			nodeRun.Generation = generation
		}
		if nodeRun.ScopeDefinitionID == "" {
			nodeRun.ScopeDefinitionID = "root"
		}
		if err = ensureNormalizedScope(ctx, tx, run, nodeRun, scopeID, generation); err != nil {
			return err
		}
		if nodeRun.ReplacesActivationID != "" {
			previousID, previousErr := parseUUIDText(nodeRun.ReplacesActivationID)
			if previousErr == nil {
				if _, previousErr = tx.Exec(ctx, `UPDATE workflow_node_activation
SET handled_by_activation_id=$2,
    status=CASE WHEN status IN ('queued','running','retry_wait') THEN 'failed' ELSE status END,
    reason_code=CASE WHEN status IN ('queued','running','retry_wait') THEN 'replaced_by_recovery' ELSE reason_code END,
    finished_at=CASE WHEN status IN ('queued','running','retry_wait') THEN COALESCE(finished_at,now()) ELSE finished_at END
WHERE workspace_id=$1 AND run_id=$3 AND id=$4`, run.WorkspaceID, activationID, run.ID, previousID); previousErr != nil {
					return previousErr
				}
			}
		}
		if generation > 1 {
			if _, err = tx.Exec(ctx, `UPDATE workflow_output SET superseded_at=COALESCE(superseded_at,now())
WHERE workspace_id=$1 AND activation_id IN (
  SELECT id FROM workflow_node_activation WHERE workspace_id=$1 AND run_id=$2 AND node_id=$3 AND generation<$4
)`, run.WorkspaceID, run.ID, nodeRun.NodeID, generation); err != nil {
				return err
			}
		}
		activationNo := nodeRun.ActivationNo
		if activationNo <= 0 {
			activationNo = 1
			nodeRun.ActivationNo = activationNo
		}
		definition := definitions[nodeRun.NodeID]
		inputSnapshot := normalizedNodeInputSnapshot(*run, definition, states)
		if _, err = tx.Exec(ctx, `INSERT INTO workflow_node_activation(id,workspace_id,run_id,node_id,scope_instance_id,generation,activation_no,automatic_retries_used,status,reason_code,issue_id,assignee_snapshot,input_snapshot,override,created_at,finished_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'{}'::jsonb,now(),CASE WHEN $9 IN ('succeeded','failed','cancelled','skipped') THEN now() ELSE NULL END)
ON CONFLICT (workspace_id,run_id,scope_instance_id,node_id,generation,activation_no) DO UPDATE SET
	  automatic_retries_used=EXCLUDED.automatic_retries_used,status=EXCLUDED.status,reason_code=EXCLUDED.reason_code,issue_id=EXCLUDED.issue_id,assignee_snapshot=EXCLUDED.assignee_snapshot,input_snapshot=EXCLUDED.input_snapshot,finished_at=EXCLUDED.finished_at`, activationID, run.WorkspaceID, run.ID, nodeRun.NodeID, scopeID, generation, activationNo, nodeRun.AutomaticRetriesUsed, nodeRun.Status, nodeRun.ReasonCode, nullableUUID(nodeRun.IssueID), body(nodeAssignee(definition)), inputSnapshot); err != nil {
			return err
		}

		if nodeRun.Output != "" && nodeRun.Status == "succeeded" {
			outputID, outputErr := normalizedOutputID(ctx, tx, run.WorkspaceID, activationID)
			if outputErr != nil {
				return outputErr
			}
			nodeRun.OutputID = outputID
			if outputID == "" {
				nodeRun.OutputID = id()
				outputHash := sha256.Sum256([]byte(nodeRun.Output))
				if _, err = tx.Exec(ctx, `INSERT INTO workflow_output(id,workspace_id,activation_id,schema_snapshot,"values",artifact_refs,content_hash)
VALUES($1,$2,$3,$4,$5,$6,$7)`, nodeRun.OutputID, run.WorkspaceID, activationID, body(definition.Outputs), normalizedOutputValues(nodeRun.Output), normalizedArtifactRefs(nodeRun.Output), hex.EncodeToString(outputHash[:])); err != nil {
					return err
				}
			}
			if _, err = tx.Exec(ctx, `UPDATE workflow_node_activation SET effective_output_id=$2 WHERE id=$1`, activationID, nodeRun.OutputID); err != nil {
				return err
			}
		}

		if nodeRun.Attempt > 0 {
			if _, err = tx.Exec(ctx, `INSERT INTO workflow_node_attempt(id,workspace_id,activation_id,attempt_no,task_id,status,error_class,error_detail,started_at,finished_at,effect_state)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,now(),CASE WHEN $6 IN ('completed','failed','cancelled') THEN now() ELSE NULL END,'{}'::jsonb)
ON CONFLICT (activation_id,attempt_no) DO UPDATE SET task_id=EXCLUDED.task_id,status=EXCLUDED.status,error_class=EXCLUDED.error_class,error_detail=EXCLUDED.error_detail,finished_at=EXCLUDED.finished_at`, id(), run.WorkspaceID, activationID, nodeRun.Attempt, nullableUUID(nodeRun.TaskID), normalizedAttemptStatus(nodeRun.Status), nodeRun.ReasonCode, nodeRun.Error); err != nil {
				return err
			}
		}
		for _, edge := range run.Graph.Edges {
			if !normalFlowEdge(edge) || edge.Source != nodeRun.NodeID {
				continue
			}
			target := states[edge.Target]
			if target == nil || target.Status == "skipped" {
				continue
			}
			targetScopeID := run.RootScopeID
			if target.ScopeInstanceID != "" {
				if _, targetScopeErr := parseUUIDText(target.ScopeInstanceID); targetScopeErr == nil {
					targetScopeID = target.ScopeInstanceID
				}
			}
			if _, err = tx.Exec(ctx, `INSERT INTO workflow_transition(id,workspace_id,run_id,source_activation_id,outlet,target_scope_id,generation,branch_id,payload_ref)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (source_activation_id,outlet,target_scope_id,generation) DO UPDATE SET branch_id=EXCLUDED.branch_id,payload_ref=EXCLUDED.payload_ref`, id(), run.WorkspaceID, run.ID, activationID, edge.ID, targetScopeID, generation, edge.Target, body(map[string]any{"target": edge.Target, "target_port": edge.TargetPort})); err != nil {
				return err
			}
		}
	}
	_, err := tx.Exec(ctx, `UPDATE workflow_scope_instance SET state=$3,rework_count=$4 WHERE workspace_id=$1 AND run_id=$2`, run.WorkspaceID, run.ID, normalizedScopeState(run.Status), totalReworkCount(run.ReworkCounts))
	return err
}

func parseUUIDText(value string) (pgtype.UUID, error) {
	if value == "" {
		return pgtype.UUID{}, errors.New("empty uuid")
	}
	return util.ParseUUID(value)
}

func normalizedOutputID(ctx context.Context, tx pgx.Tx, workspaceID string, activationID any) (string, error) {
	var outputID string
	err := tx.QueryRow(ctx, `SELECT id::text FROM workflow_output WHERE workspace_id=$1 AND activation_id=$2 AND superseded_at IS NULL ORDER BY created_at DESC,id DESC LIMIT 1`, workspaceID, activationID).Scan(&outputID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return outputID, err
}

func normalizedOutputValues(output string) []byte {
	var value any
	if json.Unmarshal([]byte(output), &value) == nil {
		return body(value)
	}
	return body(map[string]any{"text": output})
}

func normalizedArtifactRefs(output string) []byte {
	var value any
	if json.Unmarshal([]byte(output), &value) != nil {
		return []byte("[]")
	}
	refs := make([]map[string]any, 0)
	var visit func(any)
	visit = func(candidate any) {
		switch typed := candidate.(type) {
		case []any:
			for _, item := range typed {
				visit(item)
			}
		case map[string]any:
			if artifactID, ok := typed["artifact_id"].(string); ok && artifactID != "" {
				refs = append(refs, typed)
			} else if artifactID, ok := typed["id"].(string); ok && artifactID != "" {
				if _, hasURL := typed["url"]; hasURL {
					refs = append(refs, typed)
				}
			}
			for _, item := range typed {
				visit(item)
			}
		}
	}
	visit(value)
	return body(refs)
}

func normalizedAttemptStatus(status string) string {
	switch status {
	case "succeeded":
		return "completed"
	case "skipped":
		return "cancelled"
	default:
		return status
	}
}

func normalizedNodeInputSnapshot(run Run, definition Node, states map[string]*NodeRun) []byte {
	if definition.Type == "start" && run.InputValues != nil {
		return body(run.InputValues)
	}
	raw := inputs(run, definition, states)
	var value any
	if json.Unmarshal([]byte(raw), &value) == nil {
		return body(value)
	}
	return body(map[string]any{"text": raw})
}

func totalReworkCount(counts map[string]int) int {
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}
