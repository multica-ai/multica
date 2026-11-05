package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/pkg/codegraph"
)

func (h *Handler) loadQueryCodeGraph(w http.ResponseWriter, r *http.Request, public bool) (codeGraphRepositoryRow, bool) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "codeGraphId"), "code graph id")
	if !ok {
		return codeGraphRepositoryRow{}, false
	}
	var row codeGraphRepositoryRow
	var err error
	if public {
		row, err = scanCodeGraphRepository(h.DB.QueryRow(r.Context(), `SELECT `+codeGraphSelectColumns()+` FROM code_graph_repository WHERE id=$1 AND public_read_enabled`, id))
	} else {
		ws := workspaceIDFromURL(r, "id")
		wsID, valid := parseUUIDOrBadRequest(w, ws, "workspace id")
		if !valid {
			return row, false
		}
		if _, valid = h.workspaceMember(w, r, ws); !valid {
			return row, false
		}
		row, err = scanCodeGraphRepository(h.DB.QueryRow(r.Context(), `SELECT `+codeGraphSelectColumns()+` FROM code_graph_repository WHERE id=$1 AND workspace_id=$2`, id, wsID))
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "CodeGraph unavailable")
		return row, false
	}
	if err != nil {
		writeError(w, 500, "Cannot load CodeGraph")
		return row, false
	}
	return row, true
}

func (h *Handler) QueryWorkspaceCodeGraph(w http.ResponseWriter, r *http.Request) {
	h.queryCodeGraph(w, r, false)
}
func (h *Handler) QueryPublicCodeGraph(w http.ResponseWriter, r *http.Request) {
	h.queryCodeGraph(w, r, true)
}

func (h *Handler) queryCodeGraph(w http.ResponseWriter, r *http.Request, public bool) {
	w.Header().Set("Cache-Control", "no-store")
	row, ok := h.loadQueryCodeGraph(w, r, public)
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
					writeError(w, 400, "Invalid source line")
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

func (h *Handler) SetCodeGraphSharing(w http.ResponseWriter, r *http.Request) {
	if !h.requireCodeGraphWriter(w, r, workspaceIDFromURL(r, "id")) {
		return
	}
	row, ok := h.loadQueryCodeGraph(w, r, false)
	if !ok {
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req) != nil || req.Enabled == nil {
		writeError(w, 400, "enabled boolean required")
		return
	}
	if *req.Enabled && !row.ActiveBuildID.Valid {
		writeError(w, 409, "Build the graph before enabling anonymous reads")
		return
	}
	_, err := h.DB.Exec(r.Context(), `UPDATE code_graph_repository SET public_read_enabled=$2,updated_at=now() WHERE id=$1 AND workspace_id=$3`, row.ID, *req.Enabled, row.WorkspaceID)
	if err != nil {
		writeError(w, 500, "Cannot update sharing")
		return
	}
	row.PublicReadEnabled = *req.Enabled
	writeJSON(w, 200, codeGraphRepositoryToResponse(row))
}
