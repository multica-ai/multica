package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

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

// blockedIssuePredicate matches issues that are still waiting: at least one
// unfinished blocker, and the issue itself not finished. A done or cancelled
// issue whose earlier stage is still open is not waiting for anything, so the
// Blocked filter leaves it out. Finished means the done or closed category,
// the same rule issue_has_open_blockers applies to blockers.
const blockedIssuePredicate = `(issue_has_open_blockers(i.workspace_id,i.id)
 AND i.status NOT IN ('done','cancelled')
 AND NOT EXISTS(SELECT 1 FROM issue_status s WHERE s.workspace_id=i.workspace_id AND s.key=i.status AND s.category IN ('done','closed')))`

type IssueBlockerResponse struct {
	private   bool
	projectID pgtype.UUID
	IssueRefResponse
	Via    string `json:"via"`
	Closed bool   `json:"closed"`
}

func writeIssueBlockerError(w http.ResponseWriter, err error) bool {
	if errors.Is(err, service.ErrIssueBlockerNotFound) {
		writeError(w, http.StatusBadRequest, err.Error())
		return true
	}
	if message, ok := issueBlockerConflict(err); ok {
		writeError(w, http.StatusBadRequest, message)
		return true
	}
	return false
}

// issueBlockerConflict reports a dependency rule the written issue itself would
// break, and the sentence to show for it.
func issueBlockerConflict(err error) (string, bool) {
	if errors.Is(err, service.ErrIssueBlockerSelf) {
		return err.Error(), true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && strings.HasPrefix(pgErr.ConstraintName, "issue_blocker_") {
		return pgErr.Message, true
	}
	return "", false
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
			// A guest's hidden set also holds ordinary projects and NoProject,
			// so visibility is checked for every row, not only private ones.
			// Without a resolved hidden set nothing is listed; the count stays.
			if accessErr != nil || !hidden.CanSeeProject(row.ProjectID) {
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

// issueDependents lists the issues waiting on issue, hiding the ones the
// reader cannot see the same way the blocked_by projection does.
func (h *Handler) issueDependents(ctx context.Context, issue db.Issue, prefix string, hidden projectaccess.Hidden) ([]IssueBlockerResponse, error) {
	rows, err := h.Queries.ListIssueDependents(ctx, db.ListIssueDependentsParams{WorkspaceID: issue.WorkspaceID, IssueID: issue.ID})
	if err != nil {
		return nil, err
	}
	refs := make([]IssueBlockerResponse, 0, len(rows))
	for _, row := range rows {
		if !hidden.CanSeeProject(row.ProjectID) {
			continue
		}
		refs = append(refs, IssueBlockerResponse{private: row.Private, projectID: row.ProjectID, IssueRefResponse: IssueRefResponse{ID: uuidToString(row.ID), Identifier: fmt.Sprintf("%s-%d", prefix, row.Number), Title: row.Title, Status: row.Status}, Via: row.Via, Closed: row.Closed.Bool})
	}
	return refs, nil
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

// validateBlockerVisibility rejects an ID that is not an issue the caller can
// open in this workspace. A hidden issue and a missing one get the same 400, so
// the answer never tells the caller whether a private issue exists.
func (h *Handler) validateBlockerVisibility(w http.ResponseWriter, r *http.Request, ws pgtype.UUID, ids []pgtype.UUID) bool {
	if len(ids) == 0 {
		return true
	}
	hidden, ok := h.requestHiddenProjects(w, r, uuidToString(ws))
	if !ok {
		return false
	}
	refs, err := h.Queries.ListIssueRefsInWorkspace(r.Context(), db.ListIssueRefsInWorkspaceParams{WorkspaceID: ws, Ids: ids})
	if err != nil {
		writeError(w, 500, "failed to resolve blocker access")
		return false
	}
	visible := make(map[pgtype.UUID]bool, len(refs))
	for _, ref := range refs {
		visible[ref.ID] = hidden.CanSeeProject(ref.ProjectID)
	}
	for _, id := range ids {
		if !visible[id] {
			writeError(w, 400, "blocker issue not found in this workspace")
			return false
		}
	}
	return true
}

// issueBlockerEdit is the blocker change an issue update makes. It is read
// once, from the exact keys that decide whether the blocker graph is written,
// and the transaction writes these parsed IDs: the IDs that pass the checks
// are the IDs written.
type issueBlockerEdit struct {
	add, remove []pgtype.UUID
}

func (e issueBlockerEdit) changes() bool {
	return len(e.add)+len(e.remove) > 0
}

func (h *Handler) parseIssueBlockerEdit(w http.ResponseWriter, r *http.Request, ws pgtype.UUID, fields map[string]json.RawMessage) (issueBlockerEdit, bool) {
	var edit issueBlockerEdit
	var ok bool
	if edit.add, ok = h.parseBlockerField(w, r, ws, fields["blocked_by"]); !ok {
		return issueBlockerEdit{}, false
	}
	if edit.remove, ok = h.parseBlockerField(w, r, ws, fields["remove_blocked_by"]); !ok {
		return issueBlockerEdit{}, false
	}
	return edit, true
}

func (h *Handler) parseBlockerField(w http.ResponseWriter, r *http.Request, ws pgtype.UUID, raw json.RawMessage) ([]pgtype.UUID, bool) {
	var values []string
	if raw != nil && json.Unmarshal(raw, &values) != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return nil, false
	}
	ids, ok := h.parseBlockerIDs(w, values)
	if !ok || !h.validateBlockerVisibility(w, r, ws, ids) {
		return nil, false
	}
	return ids, true
}
