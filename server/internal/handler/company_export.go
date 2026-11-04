package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
)

// Company as code (spec 4.9). Owners and admins only: the export carries agent
// instructions and the import creates agents, and neither is a member's to read.

// CompanyImportRequest is the body of POST /api/company/import.
type CompanyImportRequest struct {
	Document  service.CompanyExport `json:"document"`
	RuntimeID string                `json:"runtime_id"`
}

// ExportCompany returns the workspace's structure as a document.
//
//	GET /api/company/export
func (h *Handler) ExportCompany(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.companyActorFor(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	doc, err := service.ExportCompany(r.Context(), h.Queries, wsUUID, time.Now())
	if err != nil {
		slog.Warn("export company failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to export the company")
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// ImportCompany recreates an export in the workspace. dry_run=true returns the
// plan and writes nothing. The runtime is needed to create agents.
//
//	POST /api/company/import?dry_run=true {document, runtime_id}
func (h *Handler) ImportCompany(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	actor, ok := h.companyActorFor(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var req CompanyImportRequest
	if !readCappedJSON(w, r, companyImportBodyLimit, &req, "invalid request body") {
		return
	}
	opts := service.ImportOptions{
		DryRun:  r.URL.Query().Get("dry_run") == "true",
		ActorID: actor.ID,
		Kinds:   h.proposalService().Kinds,
		Issues:  h.IssueService,
	}
	if req.RuntimeID != "" {
		runtimeID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
		if !ok {
			return
		}
		opts.RuntimeID = runtimeID
	}
	report, err := service.ImportCompany(r.Context(), h.Queries, h.TxStarter, wsUUID, req.Document, opts)
	if err != nil {
		slog.Warn("import company failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}
