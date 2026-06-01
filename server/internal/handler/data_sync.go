package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ExportWorkspaceData returns the canonical workspace export manifest as JSON.
func (h *Handler) ExportWorkspaceData(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	manifest, err := h.DataSyncService.BuildExportManifest(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	filename := fmt.Sprintf("workspace-export-%s.json", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(manifest)
}

// DryRunWorkspaceImport validates import payload and returns structured result.
func (h *Handler) DryRunWorkspaceImport(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	var payload service.WorkspaceImportPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.DataSyncService.DryRunImport(r.Context(), workspaceID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ApplyWorkspaceImport applies import payload and returns created/failed summary.
func (h *Handler) ApplyWorkspaceImport(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var payload service.WorkspaceImportPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	txService := service.NewDataSyncService(h.Queries.WithTx(tx))
	result, err := txService.ApplyImport(r.Context(), workspaceID, "member", userID, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(result.Errors) > 0 || result.Failed > 0 {
		writeJSON(w, http.StatusOK, normalizeRolledBackImportResult(result))
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to apply import")
		return
	}

	h.publish(protocol.EventIssueImported, workspaceID, "member", userID, map[string]any{
		"created": result.Created,
		"source":  payload.SourceType,
	})
	writeJSON(w, http.StatusOK, result)
}

func normalizeRolledBackImportResult(result *service.WorkspaceImportResult) *service.WorkspaceImportResult {
	if result == nil {
		return nil
	}
	normalized := *result
	normalized.Created = 0
	normalized.Summary = fmt.Sprintf("apply rolled back: %d created, %d failed", normalized.Created, normalized.Failed)
	return &normalized
}
