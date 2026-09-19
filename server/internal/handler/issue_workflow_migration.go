package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type workflowMigrationRow struct {
	SourceStatusID string `json:"source_status_id"`
	SourceName     string `json:"source_name"`
	SourcePhase    string `json:"source_phase"`
	Count          int    `json:"count"`
	Required       bool   `json:"required"`
	TargetKey      string `json:"target_key"`
	PhaseChanged   bool   `json:"phase_changed"`
}
type workflowMigrationPlan struct {
	Rows            []workflowMigrationRow `json:"rows"`
	IssueCount      int                    `json:"issue_count"`
	ViewCount       int                    `json:"view_count"`
	BlockedIssueIDs []string               `json:"blocked_issue_ids"`
	Fingerprint     string                 `json:"fingerprint"`
}

// Administrative migrations deliberately bypass status-entry effects. All
// callers hold the exclusive catalog lock; ordinary writers take its shared
// counterpart before locking an issue or resolving its effective workflow.
func planWorkflowMigration(ctx context.Context, q *db.Queries, workspaceID, projectID pgtype.UUID, issues []db.Issue, sources, targets []db.IssueWorkflowStatus, req updateProjectIssueWorkflowRequest, previous db.IssueWorkflow) (workflowMigrationPlan, map[pgtype.UUID]db.IssueWorkflowStatus, error) {
	plan := workflowMigrationPlan{Rows: []workflowMigrationRow{}, BlockedIssueIDs: []string{}}
	byKey := map[string]db.IssueWorkflowStatus{}
	for _, s := range targets {
		if !s.ArchivedAt.Valid {
			byKey[s.SpecKey] = s
		}
	}
	counts := map[pgtype.UUID]int{}
	for _, i := range issues {
		counts[i.WorkflowStatusID]++
	}
	sourceIDs := map[string]bool{}
	for _, source := range sources {
		sourceIDs[uuidToString(source.ID)] = true
	}
	for source := range req.StatusMapping {
		if !sourceIDs[source] {
			return plan, nil, workflowApplyConflictError{"unknown source status in status_mapping"}
		}
	}
	resolved := map[pgtype.UUID]db.IssueWorkflowStatus{}
	for _, source := range sources {
		target, matched := byKey[source.SpecKey]
		// Stable keys preserve a copied node's identity, never match by display name
		// or lifecycle category. Changed lifecycle semantics need explicit selection.
		if target.Phase != source.Phase || target.Outcome != source.Outcome {
			matched = false
		}
		if key, explicit := req.StatusMapping[uuidToString(source.ID)]; explicit {
			var exists bool
			target, exists = byKey[key]
			if !exists {
				return plan, nil, workflowApplyConflictError{"mapping target is not an active status key: " + key}
			}
			matched = true
		}
		if matched {
			resolved[source.ID] = target
		}
		if matched && source.ID == target.ID && source.Phase == target.Phase && source.Outcome == target.Outcome {
			continue
		}
		row := workflowMigrationRow{SourceStatusID: uuidToString(source.ID), SourceName: source.Name, SourcePhase: source.Phase, Count: counts[source.ID], Required: counts[source.ID] > 0}
		if matched {
			row.TargetKey = target.SpecKey
			row.PhaseChanged = source.Phase != target.Phase || source.Outcome != target.Outcome
		}
		plan.Rows = append(plan.Rows, row)
		plan.IssueCount += row.Count
	}
	for _, i := range issues {
		target, found := resolved[i.WorkflowStatusID]
		if found && target.ID == i.WorkflowStatusID && issueworkflow.LegacyProjection(target) == i.Status {
			continue
		}
		active, err := q.IssueHasActiveWorkflowWork(ctx, db.IssueHasActiveWorkflowWorkParams{WorkspaceID: workspaceID, IssueID: i.ID})
		if err != nil {
			return plan, nil, err
		}
		if active.Bool {
			plan.BlockedIssueIDs = append(plan.BlockedIssueIDs, uuidToString(i.ID))
		}
	}
	// IDs created in dry-run are rolled back, so hash the requested spec and the
	// source snapshot, not transient destination UUIDs. A new issue or changed
	// status after preview invalidates confirmation instead of moving unseen work.
	raw, err := json.Marshal(struct {
		Issues   []db.Issue
		Sources  []db.IssueWorkflowStatus
		Previous db.IssueWorkflow
		Mode     string
		Spec     *issueWorkflowSpecRequest
	}{issues, sources, previous, req.Mode, req.Spec})
	if err != nil {
		return plan, nil, err
	}
	hash := sha256.Sum256(raw)
	plan.Fingerprint = hex.EncodeToString(hash[:])
	return plan, resolved, nil
}

func applyWorkflowMigration(ctx context.Context, q *db.Queries, issues []db.Issue, resolved map[pgtype.UUID]db.IssueWorkflowStatus, actor issueworkflow.TransitionActor) error {
	for _, previous := range issues {
		target, ok := resolved[previous.WorkflowStatusID]
		if !ok {
			return workflowApplyConflictError{fmt.Sprintf("choose a target status for %s", uuidToString(previous.WorkflowStatusID))}
		}
		if previous.WorkflowID == target.WorkflowID && previous.WorkflowStatusID == target.ID && previous.Status == issueworkflow.LegacyProjection(target) {
			continue
		}
		current, err := q.MigrateIssueWorkflowBinding(ctx, db.MigrateIssueWorkflowBindingParams{WorkspaceID: previous.WorkspaceID, IssueID: previous.ID, StatusID: target.ID})
		if err != nil {
			return err
		}
		if _, _, _, err = issueworkflow.RecordTransition(ctx, q, &previous, current, actor, "workflow_migrated"); err != nil {
			return err
		}
	}
	return nil
}

// Each project's override replaces only that project's status predicate. A
// default-workflow status may still be referenced by unrelated projects.
func viewStatusReferences(view db.IssueView, projectID string) []string {
	if view.ScopeType == "project" && uuidToString(view.ScopeID) != projectID {
		return nil
	}
	var query struct {
		Statuses []string                     `json:"statusFilters"`
		Projects []string                     `json:"projectFilters"`
		Mappings map[string]map[string]string `json:"statusFilterMappings"`
	}
	if json.Unmarshal(view.Query, &query) != nil {
		return nil
	}
	if len(query.Projects) > 0 && !issueTableContainsString(query.Projects, projectID) {
		return nil
	}
	for i, source := range query.Statuses {
		if target, ok := query.Mappings[projectID][source]; ok {
			query.Statuses[i] = target
		}
	}
	return query.Statuses
}

func migratedViewQuery(view db.IssueView, projectID string, resolved map[pgtype.UUID]db.IssueWorkflowStatus) ([]byte, bool, error) {
	var query map[string]json.RawMessage
	if err := json.Unmarshal(view.Query, &query); err != nil {
		return nil, false, err
	}
	if len(viewStatusReferences(view, projectID)) == 0 {
		return view.Query, false, nil
	}
	var base []string
	if err := json.Unmarshal(query["statusFilters"], &base); err != nil || len(base) == 0 {
		return view.Query, false, nil
	}
	mappings := map[string]map[string]string{}
	if raw := query["statusFilterMappings"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &mappings); err != nil {
			return nil, false, err
		}
	}
	projectMappings := mappings[projectID]
	if projectMappings == nil {
		projectMappings = map[string]string{}
	}
	changed := false
	next := make([]string, 0, len(base))
	for _, original := range base {
		value := original
		if mapped, ok := projectMappings[original]; ok {
			value = mapped
		}
		for source, target := range resolved {
			if value == uuidToString(source) && source != target.ID {
				value = uuidToString(target.ID)
				changed = true
				break
			}
		}
		next = append(next, value)
		if original != value {
			projectMappings[original] = value
		} else {
			delete(projectMappings, original)
		}
	}
	if !changed {
		return view.Query, false, nil
	}
	if view.ScopeType == "project" {
		query["statusFilters"], _ = json.Marshal(next)
	} else {
		mappings[projectID] = projectMappings
		query["statusFilterMappings"], _ = json.Marshal(mappings)
	}
	raw, err := json.Marshal(query)
	return raw, true, err
}
