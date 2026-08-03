package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/accessdiagnostics"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/taskmandate"
	"github.com/multica-ai/multica/server/internal/middleware"
)

type taskMandateResponse struct {
	EnforcementEnabled   bool                            `json:"enforcement_enabled"`
	TaskID               string                          `json:"task_id"`
	AgentID              string                          `json:"agent_id"`
	AllowedTools         []string                        `json:"allowed_tools"`
	IssuedAt             string                          `json:"issued_at"`
	ExpiresAt            string                          `json:"expires_at"`
	Status               string                          `json:"status"`
	ClaimGeneration      int64                           `json:"claim_generation"`
	LifecycleState       taskmandate.ClaimLifecycleState `json:"lifecycle_state"`
	Producer             *string                         `json:"producer,omitempty"`
	Finalizer            *string                         `json:"finalizer,omitempty"`
	InventoryVersion     *string                         `json:"inventory_version,omitempty"`
	DiscoveryVersion     *string                         `json:"discovery_version,omitempty"`
	OfferedCount         int                             `json:"offered_count"`
	AuthorizedCount      int                             `json:"authorized_count"`
	FinalizedGrantDigest *string                         `json:"grant_digest,omitempty"`
	Verdict              taskmandate.Verdict             `json:"verdict"`
	Diagnostics          []accessdiagnostics.Diagnostic  `json:"diagnostics"`
}

// GetTaskMandateByUser exposes the immutable access snapshot next to the task
// transcript. It performs the same workspace ownership check as task messages;
// a caller outside the task workspace receives the same 404.
func (h *Handler) GetTaskMandateByUser(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	taskUUID, ok := parseUUIDOrBadRequest(w, taskID, "task_id")
	if !ok {
		return
	}

	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	workspaceID := h.TaskService.ResolveTaskWorkspaceID(r.Context(), task)
	if workspaceID == "" || workspaceID != middleware.WorkspaceIDFromContext(r.Context()) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	snapshot, err := taskmandate.NewStoreDB(h.DB).Get(r.Context(), taskUUID, workspaceUUID, task.AgentID)
	if errors.Is(err, taskmandate.ErrMissing) || errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":   "task access snapshot not found",
			"verdict": taskmandate.VerdictForError(taskmandate.ErrMissing),
			"diagnostics": []accessdiagnostics.Diagnostic{{
				Code: accessdiagnostics.CodeTaskMissing, State: accessdiagnostics.StateUnavailable,
				Title: "Task access snapshot missing", Message: "No persisted Task Mandate exists for this task.",
				AffectedCapability: "task:" + taskID, SourcePolicy: "Task Mandate",
				RecoveryAction: "Retry the task claim and investigate Task Mandate persistence before enabling enforcement.",
			}},
		})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "failed to load task access snapshot",
			"verdict": taskmandate.VerdictForError(err),
		})
		return
	}
	status := "active"
	verdict := taskmandate.AllowedVerdict()
	enforcementEnabled := h.taskMandateEnforcementEnabled(r.Context(), workspaceUUID)
	if !snapshot.ExpiresAt.After(time.Now()) {
		status = "expired"
		if enforcementEnabled {
			verdict = taskmandate.VerdictForError(taskmandate.ErrExpired)
		}
	}
	var decisionLedgerError string
	var loadDecisionDiagnostics func(context.Context, pgtype.UUID) ([]cerebrodb.ListTaskAccessDecisionDiagnosticsRow, error)
	if h.CerebroQueries != nil {
		loadDecisionDiagnostics = func(ctx context.Context, taskID pgtype.UUID) ([]cerebrodb.ListTaskAccessDecisionDiagnosticsRow, error) {
			rows, err := h.CerebroQueries.ListTaskAccessDecisionDiagnostics(ctx, taskID)
			if err != nil {
				decisionLedgerError = err.Error()
			}
			return rows, err
		}
	}
	decisionEvidence, ledgerUnavailable := loadTaskDecisionEvidence(r.Context(), taskUUID, loadDecisionDiagnostics)
	diagnostics := accessdiagnostics.BuildTaskDiagnostics(accessdiagnostics.TaskInput{
		EnforcementEnabled: enforcementEnabled,
		Status:             status,
		LifecycleState:     string(snapshot.LifecycleState),
		OfferedCount:       snapshot.OfferedCount,
		AuthorizedCount:    snapshot.AuthorizedCount,
		VerdictAllowed:     verdict.Allowed,
		VerdictCode:        string(verdict.Code),
		VerdictMessage:     verdict.Message,
		RecoveryAction:     string(verdict.RecoveryAction),
		Ledger:             decisionEvidence,
		LedgerError:        decisionLedgerError,
		LedgerUnavailable:  ledgerUnavailable && decisionLedgerError == "",
	})
	writeJSON(w, http.StatusOK, taskMandateResponse{
		EnforcementEnabled:   enforcementEnabled,
		TaskID:               uuidToString(snapshot.TaskID),
		AgentID:              uuidToString(snapshot.AgentID),
		AllowedTools:         snapshot.AllowedTools,
		IssuedAt:             snapshot.IssuedAt.UTC().Format(time.RFC3339Nano),
		ExpiresAt:            snapshot.ExpiresAt.UTC().Format(time.RFC3339Nano),
		Status:               status,
		ClaimGeneration:      snapshot.ClaimGeneration,
		LifecycleState:       snapshot.LifecycleState,
		Producer:             snapshot.Producer,
		Finalizer:            snapshot.Finalizer,
		InventoryVersion:     snapshot.InventoryVersion,
		DiscoveryVersion:     snapshot.DiscoveryVersion,
		OfferedCount:         snapshot.OfferedCount,
		AuthorizedCount:      snapshot.AuthorizedCount,
		FinalizedGrantDigest: snapshot.FinalizedGrantDigest,
		Verdict:              verdict,
		Diagnostics:          diagnostics,
	})
}

func loadTaskDecisionEvidence(
	ctx context.Context,
	taskID pgtype.UUID,
	load func(context.Context, pgtype.UUID) ([]cerebrodb.ListTaskAccessDecisionDiagnosticsRow, error),
) ([]accessdiagnostics.DecisionEvidence, bool) {
	if load == nil {
		return nil, true
	}
	rows, err := load(ctx, taskID)
	if err != nil {
		return nil, true
	}
	evidence := make([]accessdiagnostics.DecisionEvidence, 0, len(rows))
	for _, row := range rows {
		item := accessdiagnostics.DecisionEvidence{
			ObservedToolName:      row.ObservedToolName,
			CanonicalCapabilityID: row.CanonicalCapabilityID,
			Decision:              row.Decision,
			PolicyDecision:        row.PolicyDecision,
			LegacyPath:            row.LegacyPath,
			ReasonCode:            row.ReasonCode,
			Reason:                row.Reason,
		}
		if row.CreatedAt.Valid {
			item.CreatedAt = row.CreatedAt.Time
		}
		evidence = append(evidence, item)
	}
	return evidence, false
}
