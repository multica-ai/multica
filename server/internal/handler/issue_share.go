package handler

import (
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const issueShareTokenHeader = "X-Issue-Share-Token"

type issueShareResponse struct {
	Token *string `json:"token"`
}

func (h *Handler) loadIssueShareManager(w http.ResponseWriter, r *http.Request) (db.Issue, pgtype.UUID, bool) {
	w.Header().Set("Cache-Control", "private, no-store")
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return db.Issue{}, pgtype.UUID{}, false
	}
	member, ok := h.workspaceMember(w, r, uuidToString(issue.WorkspaceID))
	if !ok {
		return db.Issue{}, pgtype.UUID{}, false
	}
	switch member.Role {
	case "owner", "admin":
		return issue, member.UserID, true
	case "member":
		if (issue.CreatorType == "member" && issue.CreatorID == member.UserID) ||
			(issue.AssigneeType.Valid && issue.AssigneeType.String == "member" && issue.AssigneeID == member.UserID) {
			return issue, member.UserID, true
		}
	}
	writeError(w, http.StatusForbidden, "insufficient permissions to manage issue sharing")
	return db.Issue{}, pgtype.UUID{}, false
}

func (h *Handler) GetIssueShare(w http.ResponseWriter, r *http.Request) {
	issue, _, ok := h.loadIssueShareManager(w, r)
	if !ok {
		return
	}

	token, err := h.Queries.GetIssueShareToken(r.Context(), db.GetIssueShareTokenParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
	})
	switch {
	case isNotFound(err):
		writeJSON(w, http.StatusOK, issueShareResponse{})
	case err != nil:
		writeError(w, http.StatusInternalServerError, "failed to load issue share")
	default:
		writeJSON(w, http.StatusOK, issueShareResponse{Token: &token})
	}
}

func (h *Handler) CreateIssueShare(w http.ResponseWriter, r *http.Request) {
	issue, userID, ok := h.loadIssueShareManager(w, r)
	if !ok {
		return
	}

	token, err := generateShareCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue share")
		return
	}
	token, err = h.Queries.CreateIssueShare(r.Context(), db.CreateIssueShareParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, ProjectID: issue.ProjectID,
		UserID: userID, Token: token,
	})
	switch {
	case isNotFound(err):
		writeError(w, http.StatusConflict, "issue sharing permissions or scope changed")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "failed to create issue share")
	default:
		writeJSON(w, http.StatusOK, issueShareResponse{Token: &token})
	}
}

func (h *Handler) RevokeIssueShare(w http.ResponseWriter, r *http.Request) {
	issue, userID, ok := h.loadIssueShareManager(w, r)
	if !ok {
		return
	}
	token := r.Header.Get(issueShareTokenHeader)
	if !validIssueShareToken(token) {
		writeError(w, http.StatusBadRequest, "invalid issue share token")
		return
	}

	count, err := h.Queries.RevokeIssueShare(r.Context(), db.RevokeIssueShareParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, UserID: userID, Token: token,
	})
	switch {
	case err != nil:
		writeError(w, http.StatusInternalServerError, "failed to revoke issue share")
	case count == 0:
		writeError(w, http.StatusConflict, "issue share has changed")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) GetSharedIssue(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	userID, ok := parseUUIDOrBadRequest(w, requestUserID(r), "user id")
	if !ok {
		return
	}
	token := r.Header.Get(issueShareTokenHeader)
	if !validIssueShareToken(token) {
		writeError(w, http.StatusNotFound, "issue share not found")
		return
	}
	issue, err := h.Queries.GetSharedIssue(r.Context(), db.GetSharedIssueParams{
		ViewerID: userID, Token: token,
	})
	switch {
	case isNotFound(err):
		writeError(w, http.StatusNotFound, "issue share not found")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "failed to load shared issue")
	default:
		writeJSON(w, http.StatusOK, json.RawMessage(issue))
	}
}

func validIssueShareToken(token string) bool {
	if len(token) != 24 {
		return false
	}
	_, err := hex.DecodeString(token)
	return err == nil
}
