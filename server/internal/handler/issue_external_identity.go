package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/authority"
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
	Nonce                string                         `json:"nonce"`
	WriteReceiptProtocol string                         `json:"write_receipt_protocol,omitempty"`
	Aliases              []externalIdentityAliasRequest `json:"aliases"`
	TargetIssueID        *string                        `json:"target_issue_id"`
	Create               *CreateIssueRequest            `json:"create"`
	Metadata             json.RawMessage                `json:"metadata"`
}

func (h *Handler) UpsertIssueExternalIdentity(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var req upsertIssueExternalIdentityRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	receiptProtocol := req.WriteReceiptProtocol
	if receiptProtocol == "" {
		receiptProtocol = authority.WriteReceiptProtocolV1
	}
	if receiptProtocol != authority.WriteReceiptProtocolV1 && receiptProtocol != authority.WriteReceiptProtocolV2 {
		writeError(w, http.StatusBadRequest, "unsupported write receipt protocol")
		return
	}
	if len(req.Aliases) == 0 {
		writeError(w, http.StatusBadRequest, "at least one alias is required")
		return
	}
	for _, alias := range req.Aliases {
		if !service.IsValidExternalIdentityNamespace(strings.TrimSpace(alias.Namespace)) || alias.ExternalID == "" {
			writeError(w, http.StatusBadRequest, "invalid alias")
			return
		}
	}
	if _, err := authority.ValidateNonce(req.Nonce); err != nil {
		writeError(w, http.StatusBadRequest, "invalid nonce")
		return
	}
	if status, message := h.externalUpsertAuthorizationError(r, req.Aliases); status != 0 {
		writeError(w, status, message)
		return
	}
	receiptSigner := h.writeReceiptSigner
	if receiptSigner == nil {
		receiptSigner = h.AuthoritySigner
	}
	if receiptSigner == nil || authority.ValidateServerCommit(h.ServerCommit) != nil {
		writeError(w, http.StatusServiceUnavailable, "external identity write receipts are not configured")
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

	// Prove every receipt prerequisite that does not depend on the mutation
	// result before opening the mutation transaction. The final resource-bound
	// signature is prepared by BeforeCommit while rollback is still possible.
	var dbIdentity authority.DBIdentity
	if h.DB == nil {
		writeError(w, http.StatusInternalServerError, "failed to create external identity write receipt")
		return
	}
	if err := h.DB.QueryRow(r.Context(), `
		SELECT (pg_control_system()).system_identifier::text, d.oid::int8, current_database()::text
		FROM pg_catalog.pg_database d WHERE d.datname = current_database()
	`).Scan(&dbIdentity.SystemIdentifier, &dbIdentity.DatabaseOID, &dbIdentity.DatabaseName); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create external identity write receipt")
		return
	}
	digest := sha256.Sum256(body)
	receiptWorkspaceID := ""
	if receiptProtocol == authority.WriteReceiptProtocolV2 {
		receiptWorkspaceID = util.UUIDToString(wsUUID)
	}
	receiptStatement := authority.WriteReceiptStatement{
		Protocol: receiptProtocol, Operation: authority.OperationIssueUpsertExternal,
		RequestSHA256: fmt.Sprintf("%x", digest), ResourceID: "preflight", WorkspaceID: receiptWorkspaceID, Nonce: req.Nonce,
		DBIdentity: dbIdentity, IssuedAt: time.Now().UTC(), ServerCommit: h.ServerCommit,
	}
	if _, err := receiptSigner.SignWriteReceipt(receiptStatement); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create external identity write receipt")
		return
	}

	var receipt authority.WriteReceipt
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
			BroadcastPayload: func(issue db.Issue, _ []db.Attachment, _ []db.IssueLabel) map[string]any {
				return map[string]any{"issue": issueToResponse(issue, prefix)}
			},
		},
		BeforeCommit: func(prepared service.IssueExternalIdentityUpsertResult) error {
			statement := receiptStatement
			statement.ResourceID = util.UUIDToString(prepared.Issue.ID)
			var signErr error
			receipt, signErr = receiptSigner.SignWriteReceipt(statement)
			return signErr
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
	issue := issueToResponse(res.Issue, prefix)
	responseIssue := externalUpsertResponseIssue(issue, receiptProtocol)
	writeJSON(w, status, map[string]any{"issue": responseIssue, "receipt": receipt})
}

func externalUpsertResponseIssue(issue IssueResponse, receiptProtocol string) any {
	if receiptProtocol != authority.WriteReceiptProtocolV1 {
		return issue
	}
	return map[string]any{
		"id": issue.ID, "workspace_id": issue.WorkspaceID, "number": issue.Number, "identifier": issue.Identifier,
		"title": issue.Title, "description": issue.Description, "status": issue.Status, "priority": issue.Priority,
		"assignee_type": issue.AssigneeType, "assignee_id": issue.AssigneeID, "creator_type": issue.CreatorType,
		"creator_id": issue.CreatorID, "parent_issue_id": issue.ParentIssueID, "project_id": issue.ProjectID,
		"position": issue.Position, "stage": issue.Stage, "start_date": issue.StartDate, "due_date": issue.DueDate,
		"created_at": issue.CreatedAt, "updated_at": issue.UpdatedAt, "metadata": issue.Metadata,
		"reactions": issue.Reactions, "attachments": issue.Attachments, "labels": issue.Labels,
	}
}

func (h *Handler) externalUpsertAuthorizationError(r *http.Request, aliases []externalIdentityAliasRequest) (int, string) {
	if r.Header.Get("X-Actor-Source") == "task_token" {
		return http.StatusForbidden, "task-token actors cannot claim external identities"
	}
	configuredPrincipal, err := util.ParseUUID(strings.TrimSpace(h.cfg.ExternalUpsertPrincipalID))
	if err != nil || !configuredPrincipal.Valid {
		return http.StatusForbidden, "external identity upsert is not authorized"
	}
	authenticatedPrincipal, err := util.ParseUUID(strings.TrimSpace(requestUserID(r)))
	if err != nil || authenticatedPrincipal != configuredPrincipal {
		return http.StatusForbidden, "external identity upsert is not authorized"
	}
	allowed := make(map[string]struct{}, len(h.cfg.ExternalUpsertNamespaces))
	for _, namespace := range h.cfg.ExternalUpsertNamespaces {
		namespace = strings.ToLower(strings.TrimSpace(namespace))
		if service.IsValidExternalIdentityNamespace(namespace) {
			allowed[namespace] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return http.StatusForbidden, "external identity upsert is not authorized"
	}
	for _, alias := range aliases {
		namespace := strings.TrimSpace(alias.Namespace)
		if namespace != strings.ToLower(namespace) {
			return http.StatusForbidden, "external identity namespace is not authorized"
		}
		if _, ok := allowed[namespace]; !ok {
			return http.StatusForbidden, "external identity namespace is not authorized"
		}
	}
	return 0, ""
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
