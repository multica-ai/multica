package handler

import (
	"encoding/json"
	"net/http"

	"github.com/multica-ai/multica/server/internal/autosubscribe"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type AutoSubscribePreferencesResponse struct {
	WorkspaceID string                        `json:"workspace_id"`
	Preferences map[autosubscribe.Source]bool `json:"preferences"`
}

type UpdateAutoSubscribePreferencesRequest struct {
	Preferences map[string]bool `json:"preferences"`
}

var supportedAutoSubscribePreferenceKeys = map[autosubscribe.Source]bool{
	autosubscribe.SourceIssueCreator:            true,
	autosubscribe.SourceIssueAssignee:           true,
	autosubscribe.SourceCommentAuthor:           true,
	autosubscribe.SourceIssueDescriptionMention: true,
	autosubscribe.SourceCommentMention:          true,
	autosubscribe.SourceQuickCreateRequester:    true,
}

func autoSubscribePreferencesToMap(p autosubscribe.Preferences) map[autosubscribe.Source]bool {
	return map[autosubscribe.Source]bool{
		autosubscribe.SourceIssueCreator:            p.IssueCreator,
		autosubscribe.SourceIssueAssignee:           p.IssueAssignee,
		autosubscribe.SourceCommentAuthor:           p.CommentAuthor,
		autosubscribe.SourceIssueDescriptionMention: p.IssueDescriptionMention,
		autosubscribe.SourceCommentMention:          p.CommentMention,
		autosubscribe.SourceQuickCreateRequester:    p.QuickCreateRequester,
	}
}

func applyAutoSubscribePreferencePatch(p autosubscribe.Preferences, patch map[string]bool) (autosubscribe.Preferences, string, bool) {
	for key, value := range patch {
		source := autosubscribe.Source(key)
		if !supportedAutoSubscribePreferenceKeys[source] {
			return p, key, false
		}
		switch source {
		case autosubscribe.SourceIssueCreator:
			p.IssueCreator = value
		case autosubscribe.SourceIssueAssignee:
			p.IssueAssignee = value
		case autosubscribe.SourceCommentAuthor:
			p.CommentAuthor = value
		case autosubscribe.SourceIssueDescriptionMention:
			p.IssueDescriptionMention = value
		case autosubscribe.SourceCommentMention:
			p.CommentMention = value
		case autosubscribe.SourceQuickCreateRequester:
			p.QuickCreateRequester = value
		}
	}
	return p, "", true
}

func (h *Handler) currentWorkspaceID(w http.ResponseWriter, r *http.Request) (string, bool) {
	workspaceID := ctxWorkspaceID(r.Context())
	if workspaceID == "" {
		workspaceID = h.resolveWorkspaceID(r)
	}
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace id is required")
		return "", false
	}
	return workspaceID, true
}

func (h *Handler) GetMyAutoSubscribePreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID, ok := h.currentWorkspaceID(w, r)
	if !ok {
		return
	}

	prefs, err := autosubscribe.LoadPreferences(r.Context(), h.Queries, parseUUID(workspaceID), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load auto-subscribe preferences")
		return
	}

	writeJSON(w, http.StatusOK, AutoSubscribePreferencesResponse{
		WorkspaceID: workspaceID,
		Preferences: autoSubscribePreferencesToMap(prefs),
	})
}

func (h *Handler) UpdateMyAutoSubscribePreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID, ok := h.currentWorkspaceID(w, r)
	if !ok {
		return
	}

	var req UpdateAutoSubscribePreferencesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Preferences == nil {
		writeError(w, http.StatusBadRequest, "preferences field is required")
		return
	}

	prefs, err := autosubscribe.LoadPreferences(r.Context(), h.Queries, parseUUID(workspaceID), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load auto-subscribe preferences")
		return
	}
	prefs, invalidKey, ok := applyAutoSubscribePreferencePatch(prefs, req.Preferences)
	if !ok {
		writeError(w, http.StatusBadRequest, "unsupported auto-subscribe preference: "+invalidKey)
		return
	}

	row, err := h.Queries.UpsertAutoSubscribePreference(r.Context(), db.UpsertAutoSubscribePreferenceParams{
		WorkspaceID:             parseUUID(workspaceID),
		UserID:                  parseUUID(userID),
		IssueCreator:            prefs.IssueCreator,
		IssueAssignee:           prefs.IssueAssignee,
		CommentAuthor:           prefs.CommentAuthor,
		IssueDescriptionMention: prefs.IssueDescriptionMention,
		CommentMention:          prefs.CommentMention,
		QuickCreateRequester:    prefs.QuickCreateRequester,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update auto-subscribe preferences")
		return
	}

	writeJSON(w, http.StatusOK, AutoSubscribePreferencesResponse{
		WorkspaceID: workspaceID,
		Preferences: autoSubscribePreferencesToMap(autosubscribe.FromRow(row)),
	})
}
