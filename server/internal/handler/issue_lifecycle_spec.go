package handler

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuelifecycle"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

var lifecycleSpecKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type lifecycleApplyConflictError struct{ message string }

func (e lifecycleApplyConflictError) Error() string { return e.message }

type issueLifecycleSpecRequest struct {
	APIVersion    int                               `json:"api_version"`
	Name          string                            `json:"name"`
	InitialStatus string                            `json:"initial_status"`
	Statuses      []issueLifecycleStatusSpecRequest `json:"statuses"`
}

type issueLifecycleStatusSpecRequest struct {
	Key         string                     `json:"key"`
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	Color       string                     `json:"color"`
	Phase       string                     `json:"phase"`
	EntryPolicy issuelifecycle.EntryPolicy `json:"entry_policy"`
}

type normalizedLifecycleStatusSpec struct {
	issueLifecycleStatusSpecRequest
	position    float64
	outcome     pgtype.Text
	policyJSON  []byte
	policyValue issuelifecycle.EntryPolicy
}

type normalizedLifecycleSpec struct {
	name          string
	initialStatus string
	statuses      []normalizedLifecycleStatusSpec
}

type issueLifecycleApplyPlan struct {
	Changed  bool     `json:"changed"`
	Created  []string `json:"created"`
	Updated  []string `json:"updated"`
	Restored []string `json:"restored"`
	Archived []string `json:"archived"`
}

type issueLifecycleApplyResponse struct {
	Lifecycle issueLifecycleDefinitionResponse `json:"lifecycle"`
	Statuses  []issueLifecycleStatusResponse   `json:"statuses"`
	Mode      string                           `json:"mode"`
	Plan      issueLifecycleApplyPlan          `json:"plan"`
	DryRun    bool                             `json:"dry_run"`
}

func (h *Handler) normalizeLifecycleSpec(w http.ResponseWriter, r *http.Request, workspaceID string, spec issueLifecycleSpecRequest) (normalizedLifecycleSpec, bool) {
	if spec.APIVersion != 1 {
		writeError(w, http.StatusBadRequest, "lifecycle api_version must be 1")
		return normalizedLifecycleSpec{}, false
	}
	name := strings.TrimSpace(spec.Name)
	if name == "" || len([]rune(name)) > 64 {
		writeError(w, http.StatusBadRequest, "lifecycle name must be 1-64 characters")
		return normalizedLifecycleSpec{}, false
	}
	if len(spec.Statuses) == 0 || len(spec.Statuses) > 50 {
		writeError(w, http.StatusBadRequest, "lifecycle statuses must contain 1-50 entries")
		return normalizedLifecycleSpec{}, false
	}
	initial := strings.TrimSpace(spec.InitialStatus)
	seenKeys := make(map[string]struct{}, len(spec.Statuses))
	seenNames := make(map[string]struct{}, len(spec.Statuses))
	normalized := normalizedLifecycleSpec{name: name, initialStatus: initial, statuses: make([]normalizedLifecycleStatusSpec, 0, len(spec.Statuses))}
	for i, input := range spec.Statuses {
		input.Key = strings.TrimSpace(input.Key)
		input.Name = strings.TrimSpace(input.Name)
		input.Description = strings.TrimSpace(input.Description)
		input.Phase = strings.TrimSpace(input.Phase)
		if !lifecycleSpecKeyPattern.MatchString(input.Key) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("statuses[%d].key must match %s", i, lifecycleSpecKeyPattern.String()))
			return normalizedLifecycleSpec{}, false
		}
		if _, exists := seenKeys[input.Key]; exists {
			writeError(w, http.StatusBadRequest, "lifecycle status keys must be unique")
			return normalizedLifecycleSpec{}, false
		}
		seenKeys[input.Key] = struct{}{}
		if input.Name == "" || len([]rune(input.Name)) > 64 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("statuses[%d].name must be 1-64 characters", i))
			return normalizedLifecycleSpec{}, false
		}
		nameKey := strings.ToLower(input.Name)
		if _, exists := seenNames[nameKey]; exists {
			writeError(w, http.StatusBadRequest, "active lifecycle status names must be unique")
			return normalizedLifecycleSpec{}, false
		}
		seenNames[nameKey] = struct{}{}
		if len([]rune(input.Description)) > 256 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("statuses[%d].description must be at most 256 characters", i))
			return normalizedLifecycleSpec{}, false
		}
		color, err := normalizeColor(input.Color)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("statuses[%d].color: %s", i, err))
			return normalizedLifecycleSpec{}, false
		}
		input.Color = strings.ToLower(color)
		outcome, ok := lifecycleOutcome(input.Phase)
		if !ok {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("statuses[%d].phase must be backlog, unstarted, started, completed, or cancelled", i))
			return normalizedLifecycleSpec{}, false
		}
		policyJSON, policy, err := issuelifecycle.EncodeEntryPolicy(input.EntryPolicy)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("statuses[%d].entry_policy: %s", i, err))
			return normalizedLifecycleSpec{}, false
		}
		if !h.validateEntryPolicyReferences(w, r, workspaceID, policy) {
			return normalizedLifecycleSpec{}, false
		}
		normalized.statuses = append(normalized.statuses, normalizedLifecycleStatusSpec{
			issueLifecycleStatusSpecRequest: input,
			position:                        float64(i), outcome: outcome, policyJSON: policyJSON, policyValue: policy,
		})
	}
	if _, ok := seenKeys[initial]; !ok {
		writeError(w, http.StatusBadRequest, "initial_status must reference a status key in this lifecycle")
		return normalizedLifecycleSpec{}, false
	}
	return normalized, true
}

func lifecycleStatusSpecChanged(current db.IssueLifecycleStatus, desired normalizedLifecycleStatusSpec) (bool, bool, error) {
	currentPolicy, err := issuelifecycle.DecodeEntryPolicy(current.EntryPolicy)
	if err != nil {
		return false, false, err
	}
	policyChanged := currentPolicy != desired.policyValue
	changed := current.Name != desired.Name || current.Description != desired.Description ||
		strings.ToLower(current.Color) != desired.Color || current.Position != desired.position ||
		current.Phase != desired.Phase || current.Outcome != desired.outcome || policyChanged || current.ArchivedAt.Valid
	return changed, policyChanged, nil
}

func applyLifecycleSpec(ctx context.Context, qtx *db.Queries, workspaceID, projectID pgtype.UUID, spec normalizedLifecycleSpec, expectedRevision *int64, allowArchive bool) (db.IssueLifecycle, []db.IssueLifecycleStatus, issueLifecycleApplyPlan, error) {
	project, err := qtx.LockProjectForIssueLifecycleApply(ctx, db.LockProjectForIssueLifecycleApplyParams{
		ProjectID: projectID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.IssueLifecycle{}, nil, issueLifecycleApplyPlan{}, err
	}
	wasCustom := project.DefaultIssueLifecycleID.Valid
	if expectedRevision != nil && !wasCustom {
		effective, err := issuelifecycle.Effective(ctx, qtx, workspaceID, projectID)
		if err != nil {
			return db.IssueLifecycle{}, nil, issueLifecycleApplyPlan{}, fmt.Errorf("load inherited lifecycle revision: %w", err)
		}
		if effective.Revision != *expectedRevision {
			return db.IssueLifecycle{}, nil, issueLifecycleApplyPlan{}, lifecycleApplyConflictError{message: fmt.Sprintf("revision conflict: current effective revision is %d", effective.Revision)}
		}
	}
	custom, err := qtx.EnsureProjectIssueLifecycle(ctx, db.EnsureProjectIssueLifecycleParams{
		ProjectID: projectID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.IssueLifecycle{}, nil, issueLifecycleApplyPlan{}, fmt.Errorf("ensure project lifecycle: %w", err)
	}
	modeChanged := project.DefaultIssueLifecycleID != custom.ID
	if _, err := qtx.SetProjectIssueLifecycle(ctx, db.SetProjectIssueLifecycleParams{
		ProjectID: projectID, WorkspaceID: workspaceID, LifecycleID: custom.ID,
	}); err != nil {
		return db.IssueLifecycle{}, nil, issueLifecycleApplyPlan{}, fmt.Errorf("set project lifecycle: %w", err)
	}
	lifecycle, err := qtx.LockEditableIssueLifecycle(ctx, db.LockEditableIssueLifecycleParams{
		LifecycleID: custom.ID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.IssueLifecycle{}, nil, issueLifecycleApplyPlan{}, fmt.Errorf("lock project lifecycle: %w", err)
	}
	if expectedRevision != nil && wasCustom && lifecycle.Revision != *expectedRevision {
		return db.IssueLifecycle{}, nil, issueLifecycleApplyPlan{}, lifecycleApplyConflictError{message: fmt.Sprintf("revision conflict: current revision is %d", lifecycle.Revision)}
	}
	currentStatuses, err := qtx.ListIssueLifecycleStatuses(ctx, db.ListIssueLifecycleStatusesParams{
		WorkspaceID: workspaceID, LifecycleID: lifecycle.ID, IncludeArchived: true,
	})
	if err != nil {
		return db.IssueLifecycle{}, nil, issueLifecycleApplyPlan{}, err
	}
	byKey := make(map[string]db.IssueLifecycleStatus, len(currentStatuses))
	for _, status := range currentStatuses {
		byKey[status.SpecKey] = status
	}
	desiredKeys := make(map[string]struct{}, len(spec.statuses))
	plan := issueLifecycleApplyPlan{Changed: modeChanged, Created: []string{}, Updated: []string{}, Restored: []string{}, Archived: []string{}}
	resolved := make(map[string]db.IssueLifecycleStatus, len(spec.statuses))
	definitionChanged := lifecycle.Name != spec.name
	for _, desired := range spec.statuses {
		desiredKeys[desired.Key] = struct{}{}
		current, exists := byKey[desired.Key]
		if !exists {
			current, err = qtx.CreateIssueLifecycleStatus(ctx, db.CreateIssueLifecycleStatusParams{
				ID: dbid.NewV7(), WorkspaceID: workspaceID, LifecycleID: lifecycle.ID,
				SpecKey: desired.Key, Name: desired.Name, Description: desired.Description,
				Color: desired.Color, Position: desired.position, Phase: desired.Phase,
				Outcome: desired.outcome, EntryPolicy: desired.policyJSON,
			})
			if err != nil {
				return db.IssueLifecycle{}, nil, issueLifecycleApplyPlan{}, fmt.Errorf("create lifecycle status %q: %w", desired.Key, err)
			}
			plan.Created = append(plan.Created, desired.Key)
			definitionChanged = true
			resolved[desired.Key] = current
			continue
		}
		changed, policyChanged, err := lifecycleStatusSpecChanged(current, desired)
		if err != nil {
			return db.IssueLifecycle{}, nil, issueLifecycleApplyPlan{}, fmt.Errorf("decode lifecycle status %q: %w", desired.Key, err)
		}
		wasArchived := current.ArchivedAt.Valid
		if changed {
			current, err = qtx.UpdateIssueLifecycleStatusFromSpec(ctx, db.UpdateIssueLifecycleStatusFromSpecParams{
				Name: desired.Name, Description: desired.Description, Color: desired.Color,
				Position: desired.position, Phase: desired.Phase, Outcome: desired.outcome,
				EntryPolicy: desired.policyJSON, BumpEntryPolicyRevision: policyChanged,
				StatusID: current.ID, WorkspaceID: workspaceID, LifecycleID: lifecycle.ID,
			})
			if err != nil {
				return db.IssueLifecycle{}, nil, issueLifecycleApplyPlan{}, fmt.Errorf("update lifecycle status %q: %w", desired.Key, err)
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
			return db.IssueLifecycle{}, nil, issueLifecycleApplyPlan{}, lifecycleApplyConflictError{message: fmt.Sprintf("apply would archive status %q; retry with allow_archive", current.SpecKey)}
		}
		if _, err := qtx.ArchiveIssueLifecycleStatus(ctx, db.ArchiveIssueLifecycleStatusParams{
			StatusID: current.ID, WorkspaceID: workspaceID, LifecycleID: lifecycle.ID,
		}); err != nil {
			return db.IssueLifecycle{}, nil, issueLifecycleApplyPlan{}, fmt.Errorf("archive lifecycle status %q: %w", current.SpecKey, err)
		}
		plan.Archived = append(plan.Archived, current.SpecKey)
		definitionChanged = true
	}
	initial := resolved[spec.initialStatus]
	if lifecycle.InitialStatusID != initial.ID {
		definitionChanged = true
	}
	lifecycle, err = qtx.SetIssueLifecycleDefinitionFromSpec(ctx, db.SetIssueLifecycleDefinitionFromSpecParams{
		Name: spec.name, InitialStatusID: initial.ID, BumpRevision: definitionChanged,
		LifecycleID: lifecycle.ID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.IssueLifecycle{}, nil, issueLifecycleApplyPlan{}, err
	}
	plan.Changed = plan.Changed || definitionChanged
	statuses, err := qtx.ListIssueLifecycleStatuses(ctx, db.ListIssueLifecycleStatusesParams{
		WorkspaceID: workspaceID, LifecycleID: lifecycle.ID, IncludeArchived: true,
	})
	return lifecycle, statuses, plan, err
}
