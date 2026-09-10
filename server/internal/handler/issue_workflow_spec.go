package handler

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

var workflowSpecKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type workflowApplyConflictError struct{ message string }

func (e workflowApplyConflictError) Error() string { return e.message }

type issueWorkflowSpecRequest struct {
	APIVersion    int                              `json:"api_version"`
	Name          string                           `json:"name"`
	InitialStatus string                           `json:"initial_status"`
	Statuses      []issueWorkflowStatusSpecRequest `json:"statuses"`
}

type issueWorkflowStatusSpecRequest struct {
	Key         string                    `json:"key"`
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Color       string                    `json:"color"`
	Phase       string                    `json:"phase"`
	EntryPolicy issueworkflow.EntryPolicy `json:"entry_policy"`
}

type normalizedWorkflowStatusSpec struct {
	issueWorkflowStatusSpecRequest
	position    float64
	outcome     pgtype.Text
	policyJSON  []byte
	policyValue issueworkflow.EntryPolicy
}

type normalizedWorkflowSpec struct {
	name          string
	initialStatus string
	statuses      []normalizedWorkflowStatusSpec
}

type issueWorkflowApplyPlan struct {
	Changed  bool     `json:"changed"`
	Created  []string `json:"created"`
	Updated  []string `json:"updated"`
	Restored []string `json:"restored"`
	Archived []string `json:"archived"`
}

type issueWorkflowApplyResponse struct {
	Workflow issueWorkflowDefinitionResponse `json:"workflow"`
	Statuses []issueWorkflowStatusResponse   `json:"statuses"`
	Mode     string                          `json:"mode"`
	Plan     issueWorkflowApplyPlan          `json:"plan"`
	DryRun   bool                            `json:"dry_run"`
}

func (h *Handler) normalizeWorkflowSpec(w http.ResponseWriter, r *http.Request, workspaceID string, spec issueWorkflowSpecRequest) (normalizedWorkflowSpec, bool) {
	if spec.APIVersion != 1 {
		writeError(w, http.StatusBadRequest, "workflow api_version must be 1")
		return normalizedWorkflowSpec{}, false
	}
	name := strings.TrimSpace(spec.Name)
	if name == "" || len([]rune(name)) > 64 {
		writeError(w, http.StatusBadRequest, "workflow name must be 1-64 characters")
		return normalizedWorkflowSpec{}, false
	}
	if len(spec.Statuses) == 0 || len(spec.Statuses) > 50 {
		writeError(w, http.StatusBadRequest, "workflow statuses must contain 1-50 entries")
		return normalizedWorkflowSpec{}, false
	}
	initial := strings.TrimSpace(spec.InitialStatus)
	seenKeys := make(map[string]struct{}, len(spec.Statuses))
	seenNames := make(map[string]struct{}, len(spec.Statuses))
	normalized := normalizedWorkflowSpec{name: name, initialStatus: initial, statuses: make([]normalizedWorkflowStatusSpec, 0, len(spec.Statuses))}
	for i, input := range spec.Statuses {
		input.Key = strings.TrimSpace(input.Key)
		input.Name = strings.TrimSpace(input.Name)
		input.Description = strings.TrimSpace(input.Description)
		input.Phase = strings.TrimSpace(input.Phase)
		if !workflowSpecKeyPattern.MatchString(input.Key) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("statuses[%d].key must match %s", i, workflowSpecKeyPattern.String()))
			return normalizedWorkflowSpec{}, false
		}
		if _, exists := seenKeys[input.Key]; exists {
			writeError(w, http.StatusBadRequest, "workflow status keys must be unique")
			return normalizedWorkflowSpec{}, false
		}
		seenKeys[input.Key] = struct{}{}
		if input.Name == "" || len([]rune(input.Name)) > 64 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("statuses[%d].name must be 1-64 characters", i))
			return normalizedWorkflowSpec{}, false
		}
		nameKey := strings.ToLower(input.Name)
		if _, exists := seenNames[nameKey]; exists {
			writeError(w, http.StatusBadRequest, "active workflow status names must be unique")
			return normalizedWorkflowSpec{}, false
		}
		seenNames[nameKey] = struct{}{}
		if len([]rune(input.Description)) > 256 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("statuses[%d].description must be at most 256 characters", i))
			return normalizedWorkflowSpec{}, false
		}
		color, err := normalizeColor(input.Color)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("statuses[%d].color: %s", i, err))
			return normalizedWorkflowSpec{}, false
		}
		input.Color = strings.ToLower(color)
		outcome, ok := workflowOutcome(input.Phase)
		if !ok {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("statuses[%d].phase must be backlog, unstarted, started, completed, or cancelled", i))
			return normalizedWorkflowSpec{}, false
		}
		policyJSON, policy, err := issueworkflow.EncodeEntryPolicy(input.EntryPolicy)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("statuses[%d].entry_policy: %s", i, err))
			return normalizedWorkflowSpec{}, false
		}
		if !h.validateEntryPolicyReferences(w, r, workspaceID, policy) {
			return normalizedWorkflowSpec{}, false
		}
		normalized.statuses = append(normalized.statuses, normalizedWorkflowStatusSpec{
			issueWorkflowStatusSpecRequest: input,
			position:                       float64(i), outcome: outcome, policyJSON: policyJSON, policyValue: policy,
		})
	}
	if _, ok := seenKeys[initial]; !ok {
		writeError(w, http.StatusBadRequest, "initial_status must reference a status key in this workflow")
		return normalizedWorkflowSpec{}, false
	}
	for _, status := range normalized.statuses {
		next := status.policyValue.NextStatusKey
		if next != "" {
			if _, exists := seenKeys[next]; !exists || next == status.Key {
				writeError(w, http.StatusBadRequest, "next_status_key must reference another status in this workflow")
				return normalizedWorkflowSpec{}, false
			}
		}
	}
	return normalized, true
}

func workflowStatusSpecChanged(current db.IssueWorkflowStatus, desired normalizedWorkflowStatusSpec) (bool, bool, error) {
	currentPolicy, err := issueworkflow.DecodeEntryPolicy(current.EntryPolicy)
	if err != nil {
		return false, false, err
	}
	policyChanged := currentPolicy != desired.policyValue
	changed := current.Name != desired.Name || current.Description != desired.Description ||
		strings.ToLower(current.Color) != desired.Color || current.Position != desired.position ||
		current.Phase != desired.Phase || current.Outcome != desired.outcome || policyChanged || current.ArchivedAt.Valid
	return changed, policyChanged, nil
}

func applyWorkflowSpec(ctx context.Context, qtx *db.Queries, workspaceID, projectID pgtype.UUID, spec normalizedWorkflowSpec, expectedRevision *int64, allowArchive bool) (db.IssueWorkflow, []db.IssueWorkflowStatus, issueWorkflowApplyPlan, error) {
	project, err := qtx.LockProjectForIssueWorkflowApply(ctx, db.LockProjectForIssueWorkflowApplyParams{
		ProjectID: projectID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.IssueWorkflow{}, nil, issueWorkflowApplyPlan{}, err
	}
	wasCustom := project.DefaultIssueWorkflowID.Valid
	if expectedRevision != nil && !wasCustom {
		effective, err := issueworkflow.Effective(ctx, qtx, workspaceID, projectID)
		if err != nil {
			return db.IssueWorkflow{}, nil, issueWorkflowApplyPlan{}, fmt.Errorf("load inherited workflow revision: %w", err)
		}
		if effective.Revision != *expectedRevision {
			return db.IssueWorkflow{}, nil, issueWorkflowApplyPlan{}, workflowApplyConflictError{message: fmt.Sprintf("revision conflict: current effective revision is %d", effective.Revision)}
		}
	}
	custom, err := qtx.EnsureProjectIssueWorkflow(ctx, db.EnsureProjectIssueWorkflowParams{
		ProjectID: projectID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.IssueWorkflow{}, nil, issueWorkflowApplyPlan{}, fmt.Errorf("ensure project workflow: %w", err)
	}
	modeChanged := project.DefaultIssueWorkflowID != custom.ID
	if _, err := qtx.SetProjectIssueWorkflow(ctx, db.SetProjectIssueWorkflowParams{
		ProjectID: projectID, WorkspaceID: workspaceID, WorkflowID: custom.ID,
	}); err != nil {
		return db.IssueWorkflow{}, nil, issueWorkflowApplyPlan{}, fmt.Errorf("set project workflow: %w", err)
	}
	workflow, err := qtx.LockEditableIssueWorkflow(ctx, db.LockEditableIssueWorkflowParams{
		WorkflowID: custom.ID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.IssueWorkflow{}, nil, issueWorkflowApplyPlan{}, fmt.Errorf("lock project workflow: %w", err)
	}
	if expectedRevision != nil && wasCustom && workflow.Revision != *expectedRevision {
		return db.IssueWorkflow{}, nil, issueWorkflowApplyPlan{}, workflowApplyConflictError{message: fmt.Sprintf("revision conflict: current revision is %d", workflow.Revision)}
	}
	currentStatuses, err := qtx.ListIssueWorkflowStatuses(ctx, db.ListIssueWorkflowStatusesParams{
		WorkspaceID: workspaceID, WorkflowID: workflow.ID, IncludeArchived: true,
	})
	if err != nil {
		return db.IssueWorkflow{}, nil, issueWorkflowApplyPlan{}, err
	}
	byKey := make(map[string]db.IssueWorkflowStatus, len(currentStatuses))
	for _, status := range currentStatuses {
		byKey[status.SpecKey] = status
	}
	desiredKeys := make(map[string]struct{}, len(spec.statuses))
	plan := issueWorkflowApplyPlan{Changed: modeChanged, Created: []string{}, Updated: []string{}, Restored: []string{}, Archived: []string{}}
	resolved := make(map[string]db.IssueWorkflowStatus, len(spec.statuses))
	definitionChanged := workflow.Name != spec.name
	for _, desired := range spec.statuses {
		desiredKeys[desired.Key] = struct{}{}
		current, exists := byKey[desired.Key]
		if !exists {
			current, err = qtx.CreateIssueWorkflowStatus(ctx, db.CreateIssueWorkflowStatusParams{
				ID: dbid.NewV7(), WorkspaceID: workspaceID, WorkflowID: workflow.ID,
				SpecKey: desired.Key, Name: desired.Name, Description: desired.Description,
				Color: desired.Color, Position: desired.position, Phase: desired.Phase,
				Outcome: desired.outcome, EntryPolicy: desired.policyJSON,
			})
			if err != nil {
				return db.IssueWorkflow{}, nil, issueWorkflowApplyPlan{}, fmt.Errorf("create workflow status %q: %w", desired.Key, err)
			}
			plan.Created = append(plan.Created, desired.Key)
			definitionChanged = true
			resolved[desired.Key] = current
			continue
		}
		changed, policyChanged, err := workflowStatusSpecChanged(current, desired)
		if err != nil {
			return db.IssueWorkflow{}, nil, issueWorkflowApplyPlan{}, fmt.Errorf("decode workflow status %q: %w", desired.Key, err)
		}
		wasArchived := current.ArchivedAt.Valid
		if changed {
			current, err = qtx.UpdateIssueWorkflowStatusFromSpec(ctx, db.UpdateIssueWorkflowStatusFromSpecParams{
				Name: desired.Name, Description: desired.Description, Color: desired.Color,
				Position: desired.position, Phase: desired.Phase, Outcome: desired.outcome,
				EntryPolicy: desired.policyJSON, BumpEntryPolicyRevision: policyChanged,
				StatusID: current.ID, WorkspaceID: workspaceID, WorkflowID: workflow.ID,
			})
			if err != nil {
				return db.IssueWorkflow{}, nil, issueWorkflowApplyPlan{}, fmt.Errorf("update workflow status %q: %w", desired.Key, err)
			}
			if wasArchived {
				plan.Restored = append(plan.Restored, desired.Key)
			} else {
				plan.Updated = append(plan.Updated, desired.Key)
			}
			definitionChanged = true
		}
		resolved[desired.Key] = current
	}
	for _, current := range currentStatuses {
		if current.ArchivedAt.Valid {
			continue
		}
		if _, keep := desiredKeys[current.SpecKey]; keep {
			continue
		}
		if !allowArchive {
			return db.IssueWorkflow{}, nil, issueWorkflowApplyPlan{}, workflowApplyConflictError{message: fmt.Sprintf("apply would archive status %q; retry with allow_archive", current.SpecKey)}
		}
		if _, err := qtx.ArchiveIssueWorkflowStatus(ctx, db.ArchiveIssueWorkflowStatusParams{
			StatusID: current.ID, WorkspaceID: workspaceID, WorkflowID: workflow.ID,
		}); err != nil {
			return db.IssueWorkflow{}, nil, issueWorkflowApplyPlan{}, fmt.Errorf("archive workflow status %q: %w", current.SpecKey, err)
		}
		plan.Archived = append(plan.Archived, current.SpecKey)
		definitionChanged = true
	}
	initial := resolved[spec.initialStatus]
	if workflow.InitialStatusID != initial.ID {
		definitionChanged = true
	}
	workflow, err = qtx.SetIssueWorkflowDefinitionFromSpec(ctx, db.SetIssueWorkflowDefinitionFromSpecParams{
		Name: spec.name, InitialStatusID: initial.ID, BumpRevision: definitionChanged,
		WorkflowID: workflow.ID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.IssueWorkflow{}, nil, issueWorkflowApplyPlan{}, err
	}
	plan.Changed = plan.Changed || definitionChanged
	statuses, err := qtx.ListIssueWorkflowStatuses(ctx, db.ListIssueWorkflowStatusesParams{
		WorkspaceID: workspaceID, WorkflowID: workflow.ID, IncludeArchived: true,
	})
	return workflow, statuses, plan, err
}
