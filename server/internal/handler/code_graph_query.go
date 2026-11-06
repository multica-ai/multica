package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/pkg/codegraph"
)

func (h *Handler) loadQueryCodeGraph(w http.ResponseWriter, r *http.Request) (codeGraphRepositoryRow, bool) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "codeGraphId"), "code graph id")
	if !ok {
		return codeGraphRepositoryRow{}, false
	}
	ws := workspaceIDFromURL(r, "id")
	wsID, ok := parseUUIDOrBadRequest(w, ws, "workspace id")
	if !ok {
		return codeGraphRepositoryRow{}, false
	}
	if _, ok := h.workspaceMember(w, r, ws); !ok {
		return codeGraphRepositoryRow{}, false
	}
	row, err := scanCodeGraphRepository(h.DB.QueryRow(r.Context(), "SELECT "+codeGraphSelectColumns()+" FROM code_graph_repository WHERE id=$1 AND workspace_id=$2", id, wsID))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "CodeGraph unavailable")
		return row, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Cannot load CodeGraph")
		return row, false
	}
	return row, true
}

func (h *Handler) QueryWorkspaceCodeGraph(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	row, ok := h.loadQueryCodeGraph(w, r)
	if !ok {
		return
	}
	values := r.URL.Query()
	input := codegraph.QueryInput{BuildID: values.Get("build_id"), Operation: values.Get("op"), Query: values.Get("q"), SymbolID: values.Get("symbol_id"), Path: values.Get("path")}
	if input.Operation == "read" {
		for key, target := range map[string]*int{"start_line": &input.StartLine, "end_line": &input.EndLine} {
			if raw := values.Get(key); raw != "" {
				value, err := strconv.Atoi(raw)
				if err != nil || value < 1 {
					writeError(w, http.StatusBadRequest, "Invalid source line")
					return
				}
				*target = value
			}
		}
	}
	result, status, err := h.executeCodeGraphQuery(r.Context(), row, input)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, status, result)
}
