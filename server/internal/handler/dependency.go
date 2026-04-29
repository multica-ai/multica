package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type CreateIssueDependencyRequest struct {
	IssueID string `json:"issue_id"`
	Type    string `json:"type"`
}

func normalizeDependencyInput(currentIssueID, targetIssueID pgtype.UUID, dependencyType string) (pgtype.UUID, pgtype.UUID, string, error) {
	switch dependencyType {
	case "blocks":
		return currentIssueID, targetIssueID, "blocks", nil
	case "blocked_by":
		return targetIssueID, currentIssueID, "blocks", nil
	case "related":
		return currentIssueID, targetIssueID, "related", nil
	default:
		return pgtype.UUID{}, pgtype.UUID{}, "", errors.New("invalid dependency type")
	}
}

func dependencyExists(dependencies []db.IssueDependency, sourceIssueID, targetIssueID string, dependencyType string) bool {
	for _, dependency := range dependencies {
		issueID := uuidToString(dependency.IssueID)
		dependsOnIssueID := uuidToString(dependency.DependsOnIssueID)
		if dependency.Type != dependencyType {
			continue
		}
		if dependencyType == "related" {
			if (issueID == sourceIssueID && dependsOnIssueID == targetIssueID) || (issueID == targetIssueID && dependsOnIssueID == sourceIssueID) {
				return true
			}
			continue
		}
		if issueID == sourceIssueID && dependsOnIssueID == targetIssueID {
			return true
		}
	}
	return false
}

func (h *Handler) AddIssueDependency(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	var req CreateIssueDependencyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	targetIssueID := strings.TrimSpace(req.IssueID)
	if targetIssueID == "" {
		writeError(w, http.StatusBadRequest, "issue_id is required")
		return
	}
	if targetIssueID == issueID {
		writeError(w, http.StatusBadRequest, "issue cannot depend on itself")
		return
	}

	targetIssue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          parseUUID(targetIssueID),
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "dependency issue not found")
		return
	}

	sourceIssueID, dependsOnIssueID, storedType, err := normalizeDependencyInput(issue.ID, targetIssue.ID, req.Type)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	existingDependencies, err := h.Queries.ListIssueDependenciesForIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load dependencies")
		return
	}
	if dependencyExists(existingDependencies, uuidToString(sourceIssueID), uuidToString(dependsOnIssueID), storedType) {
		writeError(w, http.StatusBadRequest, "dependency already exists")
		return
	}

	if _, err := h.Queries.CreateIssueDependency(r.Context(), db.CreateIssueDependencyParams{
		IssueID:          sourceIssueID,
		DependsOnIssueID: dependsOnIssueID,
		Type:             storedType,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add dependency")
		return
	}

	updatedIssue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated issue")
		return
	}

	resp, err := h.buildIssueDetailResponse(r.Context(), updatedIssue)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated issue")
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	h.publish(protocol.EventIssueUpdated, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{
		"issue": resp,
	})
	h.publishIssueSnapshot(r.Context(), uuidToString(issue.WorkspaceID), actorType, actorID, targetIssue.ID)

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) RemoveIssueDependency(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	dependencyID := chi.URLParam(r, "dependencyId")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	dependency, err := h.Queries.GetIssueDependency(r.Context(), parseUUID(dependencyID))
	if err != nil {
		writeError(w, http.StatusNotFound, "dependency not found")
		return
	}

	currentIssueID := uuidToString(issue.ID)
	otherIssueID := ""
	switch {
	case uuidToString(dependency.IssueID) == currentIssueID:
		otherIssueID = uuidToString(dependency.DependsOnIssueID)
	case uuidToString(dependency.DependsOnIssueID) == currentIssueID:
		otherIssueID = uuidToString(dependency.IssueID)
	default:
		writeError(w, http.StatusForbidden, "dependency does not belong to this issue")
		return
	}

	if err := h.Queries.DeleteIssueDependency(r.Context(), dependency.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove dependency")
		return
	}

	updatedIssue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated issue")
		return
	}

	resp, err := h.buildIssueDetailResponse(r.Context(), updatedIssue)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated issue")
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	h.publish(protocol.EventIssueUpdated, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{
		"issue": resp,
	})
	h.publishIssueSnapshot(r.Context(), uuidToString(issue.WorkspaceID), actorType, actorID, parseUUID(otherIssueID))

	writeJSON(w, http.StatusOK, resp)
}
