package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func issueBlockerGraphTouched(fields map[string]json.RawMessage) bool {
	for _, key := range []string{"blocked_by", "remove_blocked_by", "parent_issue_id", "stage"} {
		if _, ok := fields[key]; ok {
			return true
		}
	}
	return false
}

type IssueBlockerResponse struct {
	private   bool
	projectID pgtype.UUID
	IssueRefResponse
	Via    string `json:"via"`
	Closed bool   `json:"closed"`
}

func writeIssueBlockerError(w http.ResponseWriter, err error) bool {
	if errors.Is(err, service.ErrIssueBlockerNotFound) {
		writeError(w, 400, err.Error())
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && strings.HasPrefix(pgErr.ConstraintName, "issue_blocker_") {
		writeError(w, http.StatusBadRequest, pgErr.Message)
		return true
	}
	return false
}

func (h *Handler) parseBlockerIDs(w http.ResponseWriter, values []string) ([]pgtype.UUID, bool) {
	if len(values) > 100 {
		writeError(w, http.StatusBadRequest, "at most 100 blockers per request")
		return nil, false
	}
	ids := make([]pgtype.UUID, 0, len(values))
	for _, value := range values {
		id, ok := parseUUIDOrBadRequest(w, value, "blocked_by")
		if !ok {
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

// A page loads all its blocker references in one query; single-item callers
// can lazily fill the one missing issue through the same response path.
func (h *Handler) newIssueBlockerFiller(ctx context.Context, ws pgtype.UUID, ids []pgtype.UUID) func(*IssueResponse) {
	type projection struct {
		refs  []IssueBlockerResponse
		count int
	}
	cache := map[string]*projection{}
	var hidden projectaccess.Hidden
	var accessErr error
	if request, ok := ctx.Value(blockerViewerKey{}).(*http.Request); ok {
		hidden, accessErr = h.hiddenProjects(request, uuidToString(ws))
	} else {
		hidden, accessErr = projectaccess.ForPrincipals(ctx, h.Queries, ws)
	}

	load := func(ids []pgtype.UUID) {
		for _, id := range ids {
			cache[uuidToString(id)] = &projection{refs: []IssueBlockerResponse{}}
		}
		rows, err := h.Queries.ListIssueBlockers(ctx, db.ListIssueBlockersParams{WorkspaceID: ws, IssueIds: ids})
		if err != nil {
			return
		}
		prefix := h.getIssuePrefix(ctx, ws)
		for _, row := range rows {
			key := uuidToString(row.IssueID)
			if !row.Closed.Bool {
				cache[key].count++
			}
			if row.Private && (accessErr != nil || !hidden.CanSeeProject(row.ProjectID)) {
				continue
			}
			cache[key].refs = append(cache[key].refs, IssueBlockerResponse{private: row.Private, projectID: row.ProjectID, IssueRefResponse: IssueRefResponse{ID: uuidToString(row.ID), Identifier: fmt.Sprintf("%s-%d", prefix, row.Number), Title: row.Title, Status: row.Status}, Via: row.Via, Closed: row.Closed.Bool})
		}
	}
	if len(ids) > 0 {
		load(ids)
	}
	return func(resp *IssueResponse) {
		if _, ok := cache[resp.ID]; !ok {
			load([]pgtype.UUID{parseUUID(resp.ID)})
		}
		resp.BlockedBy = cache[resp.ID].refs
		resp.BlockedCount = cache[resp.ID].count
	}
}

type blockerViewerKey struct{}

func (h *Handler) withBlockerViewer(r *http.Request) *http.Request {
	if _, ok := r.Context().Value(blockerViewerKey{}).(*http.Request); ok {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), blockerViewerKey{}, r))
}

// A workspace broadcast cannot carry an authorized writer's cross-project
// private references. Readers resolve those through their own detail request.
func publicBlockerResponse(issue IssueResponse) IssueResponse {
	refs := make([]IssueBlockerResponse, 0, len(issue.BlockedBy))
	for _, ref := range issue.BlockedBy {
		if ref.private && (issue.ProjectID == nil || *issue.ProjectID != uuidToString(ref.projectID)) {
			continue
		}
		refs = append(refs, ref)
	}
	issue.BlockedBy = refs
	return issue
}

func (h *Handler) validateBlockerVisibility(w http.ResponseWriter, r *http.Request, ws pgtype.UUID, ids []string) bool {
	if len(ids) == 0 {
		return true
	}
	hidden, ok := h.requestHiddenProjects(w, r, uuidToString(ws))
	if !ok {
		return false
	}
	if hidden.Empty() {
		return true
	}
	for _, id := range ids {
		project, err := h.Queries.GetIssueProjectID(r.Context(), parseUUID(id))
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			writeError(w, 500, "failed to resolve blocker access")
			return false
		}
		if !hidden.CanSeeProject(project) {
			writeError(w, 400, "blocker issue not found in this workspace")
			return false
		}
	}
	return true
}
