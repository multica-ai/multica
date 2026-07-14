package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type externalIdentityAliasRequest struct {
	Namespace  string `json:"namespace"`
	ExternalID string `json:"external_id"`
}

type upsertIssueExternalIdentityRequest struct {
	Aliases       []externalIdentityAliasRequest `json:"aliases"`
	TargetIssueID *string                        `json:"target_issue_id"`
	Create        *CreateIssueRequest            `json:"create"`
	Metadata      json.RawMessage                `json:"metadata"`
}

func (h *Handler) UpsertIssueExternalIdentity(w http.ResponseWriter, r *http.Request) {
	var req upsertIssueExternalIdentityRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Aliases) == 0 {
		writeError(w, http.StatusBadRequest, "at least one alias is required")
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user_id")
	if !ok {
		return
	}
	creatorType, actualCreatorID := h.resolveActor(r, userID, workspaceID)
	actualCreatorUUID, err := util.ParseUUID(actualCreatorID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid creator_id")
		return
	}

	aliases := make([]service.ExternalIdentityAlias, 0, len(req.Aliases))
	for _, alias := range req.Aliases {
		aliases = append(aliases, service.ExternalIdentityAlias{Namespace: alias.Namespace, ExternalID: alias.ExternalID})
	}

	var targetIssueID pgtype.UUID
	if req.TargetIssueID != nil {
		targetIssueID, ok = parseUUIDOrBadRequest(w, *req.TargetIssueID, "target_issue_id")
		if !ok {
			return
		}
	}

	createParams := service.IssueCreateParams{}
	if req.Create != nil {
		params, ok := h.issueCreateParamsFromExternalRequest(w, r, *req.Create, wsUUID, creatorType, actualCreatorUUID)
		if !ok {
			return
		}
		createParams = params
	}

	metadataPatch := []byte(nil)
	if len(req.Metadata) > 0 && string(req.Metadata) != "null" {
		metadataPatch = []byte(req.Metadata)
	}

	prefix := h.getIssuePrefix(r.Context(), wsUUID)
	res, err := h.IssueService.UpsertExternalIdentity(r.Context(), service.IssueExternalIdentityUpsertParams{
		WorkspaceID:   wsUUID,
		Aliases:       aliases,
		TargetIssueID: targetIssueID,
		Create:        createParams,
		MetadataPatch: metadataPatch,
		CreatorType:   creatorType,
		CreatorID:     userUUID,
		IssueCreateOpt: service.IssueCreateOpts{
			ActorID:  actualCreatorID,
			Platform: func() string { p, _, _ := middleware.ClientMetadataFromContext(r.Context()); return p }(),
			BroadcastPayload: func(issue db.Issue, _ []db.Attachment) map[string]any {
				return map[string]any{"issue": issueToResponse(issue, prefix)}
			},
		},
	})
	if errors.Is(err, service.ErrExternalIdentityConflict) {
		writeJSON(w, http.StatusConflict, map[string]string{
			"code":  "external_identity_conflict",
			"error": "external identity aliases resolve to conflicting issues",
		})
		return
	}
	if errors.Is(err, service.ErrExternalIdentityInvalid) || errors.Is(err, service.ErrExternalIdentityTargetNotFound) ||
		errors.Is(err, service.ErrParentIssueNotFound) || errors.Is(err, service.ErrProjectNotFound) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		slog.Warn("external identity upsert failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to upsert external identity")
		return
	}

	status := http.StatusOK
	if res.Created {
		status = http.StatusCreated
	}
	writeJSON(w, status, issueToResponse(res.Issue, prefix))
}

func (h *Handler) issueCreateParamsFromExternalRequest(w http.ResponseWriter, r *http.Request, req CreateIssueRequest, workspaceID pgtype.UUID, creatorType string, creatorID pgtype.UUID) (service.IssueCreateParams, bool) {
	if len(req.AttachmentIDs) > 0 {
		writeError(w, http.StatusBadRequest, "attachments are not supported for external identity upsert")
		return service.IssueCreateParams{}, false
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return service.IssueCreateParams{}, false
	}
	status := req.Status
	if status == "" {
		status = "todo"
	}
	priority := req.Priority
	if priority == "" {
		priority = "none"
	}
	if !validateIssueEnum(w, "status", status, validIssueStatuses) || !validateIssueEnum(w, "priority", priority, validIssuePriorities) {
		return service.IssueCreateParams{}, false
	}
	if req.Stage != nil && *req.Stage < 1 {
		writeError(w, http.StatusBadRequest, "stage must be >= 1")
		return service.IssueCreateParams{}, false
	}
	var assigneeType pgtype.Text
	var assigneeID pgtype.UUID
	if req.AssigneeType != nil {
		assigneeType = pgtype.Text{String: *req.AssigneeType, Valid: true}
	}
	if req.AssigneeID != nil {
		id, ok := parseUUIDOrBadRequest(w, *req.AssigneeID, "assignee_id")
		if !ok {
			return service.IssueCreateParams{}, false
		}
		assigneeID = id
	}
	if statusCode, msg := h.validateAssigneePair(r.Context(), r, uuidToString(workspaceID), assigneeType, assigneeID); statusCode != 0 {
		writeError(w, statusCode, msg)
		return service.IssueCreateParams{}, false
	}
	var parentIssueID pgtype.UUID
	var projectID pgtype.UUID
	if req.ParentIssueID != nil {
		id, ok := parseUUIDOrBadRequest(w, *req.ParentIssueID, "parent_issue_id")
		if !ok {
			return service.IssueCreateParams{}, false
		}
		parentIssueID = id
	}
	if req.ProjectID != nil {
		id, ok := parseUUIDOrBadRequest(w, *req.ProjectID, "project_id")
		if !ok {
			return service.IssueCreateParams{}, false
		}
		projectID = id
	}
	var startDate pgtype.Date
	if req.StartDate != nil && *req.StartDate != "" {
		d, err := util.ParseCalendarDate(*req.StartDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid start_date format, expected YYYY-MM-DD")
			return service.IssueCreateParams{}, false
		}
		startDate = d
	}
	var dueDate pgtype.Date
	if req.DueDate != nil && *req.DueDate != "" {
		d, err := util.ParseCalendarDate(*req.DueDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid due_date format, expected YYYY-MM-DD")
			return service.IssueCreateParams{}, false
		}
		dueDate = d
	}
	return service.IssueCreateParams{
		WorkspaceID:    workspaceID,
		Title:          req.Title,
		Description:    ptrToText(req.Description),
		Status:         status,
		Priority:       priority,
		AssigneeType:   assigneeType,
		AssigneeID:     assigneeID,
		CreatorType:    creatorType,
		CreatorID:      creatorID,
		ParentIssueID:  parentIssueID,
		ProjectID:      projectID,
		StartDate:      startDate,
		DueDate:        dueDate,
		Stage:          ptrToInt4(req.Stage),
		AllowDuplicate: true,
	}, true
}
