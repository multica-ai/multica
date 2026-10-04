package handler

import (
	"net/http"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The archived inbox's paged list and facets. The contract and the helpers
// are shared with the active inbox; see inbox_page.go.

func (h *Handler) ListArchivedInboxPage(w http.ResponseWriter, r *http.Request) {
	p, limit, scope, ok := parseInboxPageRequest(w, r, archivedInboxView)
	if !ok {
		return
	}
	rows, err := h.Queries.ListArchivedInboxPage(r.Context(), db.ListArchivedInboxPageParams(p))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load archived inbox page")
		return
	}
	page := make([]db.ListInboxPageRow, len(rows))
	for i, row := range rows {
		page[i] = db.ListInboxPageRow(row)
	}
	writeInboxPage(w, page, limit, scope, "invalid archived notification details")
}

func (h *Handler) GetArchivedInboxFacets(w http.ResponseWriter, r *http.Request) {
	p, ok := parseInboxFilters(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ArchivedInboxFacets(r.Context(), p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load archived inbox filters")
		return
	}
	resp := newInboxFacetsResponse()
	for _, row := range rows {
		resp.add(row.Dimension, row.Key, row.Count)
	}
	writeJSON(w, http.StatusOK, resp)
}
