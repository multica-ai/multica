package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	agentpkg "github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type sourceContextStreamUploader interface {
	UploadStream(ctx context.Context, key string, reader io.Reader, sizeBytes int64, contentType string, filename string) (string, error)
}

var errSourceContextResponseWritten = errors.New("source context response already written")
var errSourceContextBadRequest = errors.New("source context bad request")

func sourceContextBadRequest(message string) error {
	return fmt.Errorf("%w: %s", errSourceContextBadRequest, message)
}

type sourceContextPreviewResponse struct {
	SourceIssue     service.SourceContextIssueSnapshot     `json:"source_issue"`
	CommentThread   []service.SourceContextCommentSnapshot `json:"comment_thread"`
	AnchorCommentID string                                 `json:"anchor_comment_id"`
	CaptureToken    string                                 `json:"capture_token"`
	Limits          service.SourceContextLimitUsage        `json:"limits"`
}

type sourceContextAuthorState struct {
	Type         string  `json:"type"`
	ID           string  `json:"id"`
	CapturedName string  `json:"captured_name"`
	CurrentName  *string `json:"current_name,omitempty"`
	State        string  `json:"state"`
}

type sourceContextCurrentSource struct {
	IssueID         string `json:"issue_id"`
	AnchorCommentID string `json:"anchor_comment_id"`
	Identifier      string `json:"identifier"`
}

type sourceContextDetailResponse struct {
	ID                   string                        `json:"id"`
	Version              int16                         `json:"version"`
	Usage                string                        `json:"usage"`
	CapturedAt           string                        `json:"captured_at"`
	DisplayState         string                        `json:"display_state"`
	SourceIssueState     string                        `json:"source_issue_state"`
	CommentThreadState   string                        `json:"comment_thread_state"`
	AnchorCommentState   string                        `json:"anchor_comment_state"`
	CanOpenCurrentSource bool                          `json:"can_open_current_source"`
	ChangeReasons        []string                      `json:"change_reasons"`
	ChangeDetails        sourceContextChangeDetails    `json:"change_details"`
	Snapshot             service.SourceContextSnapshot `json:"snapshot"`
	CurrentSource        *sourceContextCurrentSource   `json:"current_source,omitempty"`
	SourceAuthorState    []sourceContextAuthorState    `json:"source_author_state"`
}

type sourceContextChangeDetails struct {
	ChangedCommentIDs            []string                                   `json:"changed_comment_ids"`
	AddedComments                []service.SourceContextCommentSnapshot     `json:"added_comments,omitempty"`
	RemovedCommentIDs            []string                                   `json:"removed_comment_ids,omitempty"`
	DescriptionAttachmentChanges []sourceContextDescriptionAttachmentChange `json:"description_attachment_changes"`
}

type sourceContextDescriptionAttachmentChange struct {
	Kind             string `json:"kind"`
	AttachmentID     string `json:"attachment_id"`
	Filename         string `json:"filename"`
	PreviousFilename string `json:"previous_filename,omitempty"`
}

const (
	sourceContextChangeIssueTitle                  = "issue_title"
	sourceContextChangeIssueDescription            = "issue_description"
	sourceContextChangeIssueDescriptionAttachments = "issue_description_attachments"
	sourceContextChangeCommentThread               = "comment_thread"
)

type sourceContextComparableAttachment struct {
	ID          string
	Filename    string
	ContentType string
	SizeBytes   int64
}

type sourceContextComparableThreadNode struct {
	ID       string
	ParentID *string
	Type     string
}

func comparableSourceContextAttachments(items []service.SourceContextAttachment) []sourceContextComparableAttachment {
	result := make([]sourceContextComparableAttachment, 0, len(items))
	for _, item := range items {
		id := item.ID
		if item.SourceAttachmentID != "" {
			id = item.SourceAttachmentID
		}
		result = append(result, sourceContextComparableAttachment{
			ID: id, Filename: item.Filename, ContentType: item.ContentType, SizeBytes: item.SizeBytes,
		})
	}
	return result
}

func jsonEqual(a, b any) bool {
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	return leftErr == nil && rightErr == nil && string(left) == string(right)
}

type sourceContextIssueChanges struct {
	Reasons                      []string
	DescriptionAttachmentChanges []sourceContextDescriptionAttachmentChange
}

func sourceContextIssueChangeDetails(captured, current service.SourceContextIssueSnapshot) sourceContextIssueChanges {
	reasons := make([]string, 0, 3)
	if captured.Title != current.Title {
		reasons = append(reasons, sourceContextChangeIssueTitle)
	}
	capturedBody, capturedReferences := sourceContextDescriptionProjection(captured.Description, captured.Attachments, current.Attachments)
	currentBody, currentReferences := sourceContextDescriptionProjection(current.Description, captured.Attachments, current.Attachments)
	if capturedBody != currentBody {
		reasons = append(reasons, sourceContextChangeIssueDescription)
	}
	attachmentChanges := sourceContextDescriptionAttachmentChanges(capturedReferences, currentReferences)
	if len(attachmentChanges) > 0 {
		reasons = append(reasons, sourceContextChangeIssueDescriptionAttachments)
	}
	return sourceContextIssueChanges{Reasons: reasons, DescriptionAttachmentChanges: attachmentChanges}
}

func sourceContextIssueChangeReasons(captured, current service.SourceContextIssueSnapshot) []string {
	return sourceContextIssueChangeDetails(captured, current).Reasons
}

type sourceContextThreadChanges struct {
	Reasons           []string
	ChangedCommentIDs []string
	AddedComments     []service.SourceContextCommentSnapshot
	RemovedCommentIDs []string
}

func sourceContextThreadChangeDetails(captured, current service.SourceContextSnapshot) sourceContextThreadChanges {
	capturedThread := make([]sourceContextComparableThreadNode, 0, len(captured.CommentThread))
	currentThread := make([]sourceContextComparableThreadNode, 0, len(current.CommentThread))
	capturedComments := make(map[string]service.SourceContextCommentSnapshot, len(captured.CommentThread))
	currentComments := make(map[string]service.SourceContextCommentSnapshot, len(current.CommentThread))
	for _, comment := range captured.CommentThread {
		capturedThread = append(capturedThread, sourceContextComparableThreadNode{ID: comment.ID, ParentID: comment.ParentID, Type: comment.Type})
		capturedComments[comment.ID] = comment
	}
	for _, comment := range current.CommentThread {
		currentThread = append(currentThread, sourceContextComparableThreadNode{ID: comment.ID, ParentID: comment.ParentID, Type: comment.Type})
		currentComments[comment.ID] = comment
	}
	threadChanged := captured.AnchorCommentID != current.AnchorCommentID || !jsonEqual(capturedThread, currentThread)
	changedCommentIDs := make([]string, 0)
	for _, capturedComment := range captured.CommentThread {
		currentComment, exists := currentComments[capturedComment.ID]
		if !exists {
			continue
		}
		contentChanged := sourceContextMarkdownContentProjection(
			capturedComment.Content,
			capturedComment.Attachments,
			currentComment.Attachments,
		) != sourceContextMarkdownContentProjection(
			currentComment.Content,
			capturedComment.Attachments,
			currentComment.Attachments,
		)
		attachmentsChanged := !jsonEqual(
			comparableSourceContextAttachments(capturedComment.Attachments),
			comparableSourceContextAttachments(currentComment.Attachments),
		)
		structureChanged := capturedComment.Type != currentComment.Type || !jsonEqual(capturedComment.ParentID, currentComment.ParentID)
		if contentChanged || attachmentsChanged || structureChanged {
			changedCommentIDs = append(changedCommentIDs, capturedComment.ID)
			threadChanged = true
		}
	}
	reasons := make([]string, 0, 1)
	if threadChanged {
		reasons = append(reasons, sourceContextChangeCommentThread)
	}
	addedComments := make([]service.SourceContextCommentSnapshot, 0)
	for _, currentComment := range current.CommentThread {
		if _, exists := capturedComments[currentComment.ID]; !exists {
			addedComments = append(addedComments, currentComment)
		}
	}
	removedCommentIDs := make([]string, 0)
	for _, capturedComment := range captured.CommentThread {
		if _, exists := currentComments[capturedComment.ID]; !exists {
			removedCommentIDs = append(removedCommentIDs, capturedComment.ID)
		}
	}
	return sourceContextThreadChanges{
		Reasons: reasons, ChangedCommentIDs: changedCommentIDs,
		AddedComments: addedComments, RemovedCommentIDs: removedCommentIDs,
	}
}

func sourceContextThreadChangeReasons(captured, current service.SourceContextSnapshot) []string {
	return sourceContextThreadChangeDetails(captured, current).Reasons
}

func (h *Handler) issueSourceContextDetail(ctx context.Context, issue db.Issue) (*sourceContextDetailResponse, error) {
	row, err := h.Queries.GetIssueSourceContextByIssue(ctx, db.GetIssueSourceContextByIssueParams{WorkspaceID: issue.WorkspaceID, IssueID: issue.ID})
	if err != nil {
		return nil, err
	}
	var captured service.SourceContextSnapshot
	if err := json.Unmarshal(row.Snapshot, &captured); err != nil {
		return nil, fmt.Errorf("decode source context snapshot: %w", err)
	}
	response := &sourceContextDetailResponse{
		ID: uuidToString(row.ID), Version: row.SnapshotVersion, Usage: service.SourceContextUsage,
		CapturedAt: timestampToString(row.CapturedAt), DisplayState: "unchanged",
		SourceIssueState: "unchanged", CommentThreadState: "unchanged", AnchorCommentState: "unavailable", ChangeReasons: []string{},
		ChangeDetails: sourceContextChangeDetails{ChangedCommentIDs: []string{}, DescriptionAttachmentChanges: []sourceContextDescriptionAttachmentChange{}},
		Snapshot:      captured, SourceAuthorState: h.resolveSourceContextAuthors(ctx, issue.WorkspaceID, captured.CommentThread),
	}

	anchor, anchorErr := h.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
		ID: row.AnchorCommentID, WorkspaceID: issue.WorkspaceID,
	})
	anchorExists := anchorErr == nil && anchor.Type == "comment"
	if anchorExists {
		response.AnchorCommentState = "available"
		response.CurrentSource = &sourceContextCurrentSource{
			IssueID: uuidToString(anchor.IssueID), AnchorCommentID: uuidToString(row.AnchorCommentID), Identifier: captured.SourceIssue.Identifier,
		}
		response.CanOpenCurrentSource = true
	} else if errors.Is(anchorErr, pgx.ErrNoRows) || anchorErr == nil {
		response.AnchorCommentState = "deleted"
	}

	var currentIssue service.SourceContextIssueSnapshot
	var currentIssueErr error
	if anchorExists {
		currentIssue, _, currentIssueErr = service.BuildSourceIssueSnapshot(ctx, h.Queries, issue.WorkspaceID, anchor.IssueID)
	} else {
		currentIssue, _, currentIssueErr = service.BuildSourceIssueSnapshot(ctx, h.Queries, issue.WorkspaceID, row.SourceIssueID)
	}
	switch {
	case currentIssueErr == nil:
		issueChanges := sourceContextIssueChangeDetails(captured.SourceIssue, currentIssue)
		response.ChangeDetails.DescriptionAttachmentChanges = issueChanges.DescriptionAttachmentChanges
		response.ChangeReasons = append(response.ChangeReasons, issueChanges.Reasons...)
		if len(response.ChangeReasons) > 0 {
			response.SourceIssueState = "changed"
		}
		if anchorExists {
			response.CurrentSource.Identifier = currentIssue.Identifier
		}
	case errors.Is(currentIssueErr, service.ErrSourceIssueDeleted):
		response.SourceIssueState = "deleted"
		response.CanOpenCurrentSource = false
	default:
		response.SourceIssueState = "unavailable"
	}

	current, buildErr := service.BuildSourceContext(ctx, h.Queries, issue.WorkspaceID, row.AnchorCommentID)
	switch {
	case buildErr == nil || (errors.Is(buildErr, service.ErrSourceContextTooLarge) && current.Snapshot.AnchorCommentID != ""):
		threadChanges := sourceContextThreadChangeDetails(captured, current.Snapshot)
		response.ChangeDetails.ChangedCommentIDs = threadChanges.ChangedCommentIDs
		response.ChangeDetails.AddedComments = threadChanges.AddedComments
		response.ChangeDetails.RemovedCommentIDs = threadChanges.RemovedCommentIDs
		response.ChangeReasons = append(response.ChangeReasons, threadChanges.Reasons...)
		if len(threadChanges.Reasons) > 0 {
			response.CommentThreadState = "changed"
		}
	case errors.Is(buildErr, service.ErrAnchorCommentDeleted):
		response.AnchorCommentState = "deleted"
		response.CommentThreadState = "changed"
		response.CanOpenCurrentSource = false
		response.CurrentSource = nil
		currentThread, threadErr := service.BuildSourceContextThreadAtCaptureBoundary(ctx, h.Queries, issue.WorkspaceID, captured)
		if threadErr == nil {
			currentSnapshot := captured
			currentSnapshot.CommentThread = currentThread
			threadChanges := sourceContextThreadChangeDetails(captured, currentSnapshot)
			response.ChangeDetails.ChangedCommentIDs = threadChanges.ChangedCommentIDs
			response.ChangeDetails.AddedComments = threadChanges.AddedComments
			response.ChangeDetails.RemovedCommentIDs = threadChanges.RemovedCommentIDs
			response.ChangeReasons = append(response.ChangeReasons, threadChanges.Reasons...)
		} else {
			response.ChangeReasons = append(response.ChangeReasons, sourceContextChangeCommentThread)
			response.ChangeDetails.RemovedCommentIDs = []string{captured.AnchorCommentID}
		}
	case errors.Is(buildErr, service.ErrSourceIssueDeleted):
		response.SourceIssueState = "deleted"
		response.CommentThreadState = "unavailable"
		response.CanOpenCurrentSource = false
		response.CurrentSource = nil
	case errors.Is(buildErr, service.ErrSourceContextInvalid) && anchorExists:
		response.CommentThreadState = "changed"
		response.ChangeReasons = append(response.ChangeReasons, sourceContextChangeCommentThread)
	default:
		response.CommentThreadState = "unavailable"
	}
	if response.SourceIssueState == "deleted" {
		response.DisplayState = "deleted"
	} else if response.SourceIssueState == "unavailable" || response.CommentThreadState == "unavailable" {
		response.DisplayState = "unavailable"
	} else if response.SourceIssueState != "unchanged" || response.CommentThreadState != "unchanged" {
		response.DisplayState = "changed"
	}
	return response, nil
}

func (h *Handler) resolveSourceContextAuthors(ctx context.Context, workspaceID pgtype.UUID, comments []service.SourceContextCommentSnapshot) []sourceContextAuthorState {
	seen := make(map[string]struct{})
	states := make([]sourceContextAuthorState, 0)
	for _, comment := range comments {
		key := comment.Author.Type + ":" + comment.Author.ID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		state := sourceContextAuthorState{Type: comment.Author.Type, ID: comment.Author.ID, CapturedName: comment.Author.Name, State: "unavailable"}
		authorID, parseErr := util.ParseUUID(comment.Author.ID)
		if parseErr != nil {
			states = append(states, state)
			continue
		}
		switch comment.Author.Type {
		case "member":
			if _, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: authorID, WorkspaceID: workspaceID}); errors.Is(err, pgx.ErrNoRows) {
				state.State = "no_longer_in_workspace"
			} else if err == nil {
				user, userErr := h.Queries.GetUser(ctx, authorID)
				if userErr != nil {
					states = append(states, state)
					continue
				}
				state.CurrentName = &user.Name
				if user.Name == comment.Author.Name {
					state.State = "unchanged"
				} else {
					state.State = "renamed"
				}
			}
		case "agent":
			agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: authorID, WorkspaceID: workspaceID})
			if errors.Is(err, pgx.ErrNoRows) {
				state.State = "deleted_agent"
			} else if err == nil {
				state.CurrentName = &agent.Name
				switch {
				case agent.ArchivedAt.Valid:
					state.State = "archived"
				case agent.Name != comment.Author.Name:
					state.State = "renamed"
				default:
					state.State = "unchanged"
				}
			}
		}
		states = append(states, state)
	}
	return states
}

func (h *Handler) PreviewCommentSubIssue(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	anchorCommentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "commentId"), "comment_id")
	if !ok {
		return
	}
	build, err := service.BuildSourceContext(r.Context(), h.Queries, wsUUID, anchorCommentID)
	if err != nil {
		h.writeSourceContextError(w, err, build.Limits)
		return
	}
	writeJSON(w, http.StatusOK, sourceContextPreviewResponse{
		SourceIssue: build.Snapshot.SourceIssue, CommentThread: build.Snapshot.CommentThread,
		AnchorCommentID: build.Snapshot.AnchorCommentID, CaptureToken: build.Token, Limits: build.Limits,
	})
}

type createCommentSubIssueRequest struct {
	Mode         string                   `json:"mode"`
	CaptureToken string                   `json:"capture_token"`
	Issue        *CreateIssueRequest      `json:"issue,omitempty"`
	QuickCreate  *QuickCreateIssueRequest `json:"quick_create,omitempty"`
}

func (h *Handler) CreateCommentSubIssue(w http.ResponseWriter, r *http.Request) {
	var req createCommentSubIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
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
	anchorCommentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "commentId"), "comment_id")
	if !ok {
		return
	}
	wantDigest, tokenIssueID, validToken := service.ParseSourceContextToken(strings.TrimSpace(req.CaptureToken))
	if !validToken {
		writeJSON(w, http.StatusConflict, map[string]any{"code": "source_context_changed", "error": "source context preview is invalid; refresh and try again"})
		return
	}
	build, err := service.BuildSourceContext(r.Context(), h.Queries, wsUUID, anchorCommentID)
	if errors.Is(err, service.ErrAnchorCommentDeleted) {
		if _, issueErr := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: tokenIssueID, WorkspaceID: wsUUID}); errors.Is(issueErr, pgx.ErrNoRows) {
			err = service.ErrSourceIssueDeleted
		}
	}
	if err != nil {
		h.writeSourceContextError(w, err, build.Limits)
		return
	}
	if build.Snapshot.SourceIssue.ID != util.UUIDToString(tokenIssueID) || build.Digest != wantDigest {
		h.writeSourceContextError(w, service.ErrSourceContextChanged, build.Limits)
		return
	}
	var preparedAgent *preparedAgentCommentSubIssue
	switch req.Mode {
	case "manual":
		if req.Issue == nil {
			writeError(w, http.StatusBadRequest, "issue is required for manual mode")
			return
		}
		if strings.TrimSpace(req.Issue.Title) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"code": "invalid_request", "error": "title is required"})
			return
		}
	case "agent":
		if req.QuickCreate == nil {
			writeError(w, http.StatusBadRequest, "quick_create is required for agent mode")
			return
		}
		if err := service.ValidateSourceContextAgentInput(build, userUUID, strings.TrimSpace(req.QuickCreate.Prompt)); err != nil {
			h.writeSourceContextError(w, err, build.Limits)
			return
		}
		preparedAgent, err = h.prepareAgentCommentSubIssue(w, r, wsUUID, *req.QuickCreate)
		if err != nil {
			if !errors.Is(err, errSourceContextResponseWritten) {
				h.writeSourceContextError(w, err, service.SourceContextLimitUsage{})
			}
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "mode must be manual or agent")
		return
	}

	contextID := dbid.NewV7()
	capture, objectKeys, err := h.cloneSourceContext(r.Context(), wsUUID, userUUID, contextID, build)
	if err != nil {
		slog.Warn("source context capture failed", append(sourceContextAuditAttrs(r, contextID, build.Snapshot.SourceIssue.ID, build.Snapshot.AnchorCommentID, userUUID, req.Mode, build.Limits, "failed"), "error", err)...)
		writeJSON(w, http.StatusBadGateway, map[string]any{"code": "source_context_attachment_copy_failed", "error": "failed to copy source attachments"})
		return
	}
	cleanup := func() {
		for _, key := range objectKeys {
			h.Storage.Delete(context.Background(), key)
		}
	}
	cleanupIfUnpersisted := func() {
		_, lookupErr := h.Queries.GetIssueSourceContextByID(r.Context(), db.GetIssueSourceContextByIDParams{
			WorkspaceID: wsUUID, ID: capture.ID,
		})
		switch {
		case errors.Is(lookupErr, pgx.ErrNoRows):
			cleanup()
		case lookupErr != nil:
			// A commit error can be ambiguous. Never delete an object while DB
			// ownership is also uncertain; the durable intent reconciler performs
			// the same reference check before deleting it.
			slog.Warn("source context compensation deferred after ownership lookup failed", append(sourceContextAuditAttrs(r, capture.ID, capture.Snapshot.SourceIssue.ID, capture.Snapshot.AnchorCommentID, userUUID, req.Mode, build.Limits, "deferred_cleanup"), "error", lookupErr)...)
		}
	}

	switch req.Mode {
	case "manual":
		if err := h.createManualCommentSubIssue(w, r, wsUUID, userUUID, *req.Issue, capture, build.Limits); err != nil {
			cleanupIfUnpersisted()
			slog.Warn("source context capture failed", append(sourceContextAuditAttrs(r, capture.ID, capture.Snapshot.SourceIssue.ID, capture.Snapshot.AnchorCommentID, userUUID, req.Mode, build.Limits, "failed"), "error", err)...)
			if !errors.Is(err, errSourceContextResponseWritten) {
				h.writeSourceContextError(w, err, service.SourceContextLimitUsage{})
			}
		}
	case "agent":
		if err := h.createAgentCommentSubIssue(w, r, wsUUID, userUUID, *preparedAgent, capture, build.Limits); err != nil {
			cleanupIfUnpersisted()
			slog.Warn("source context capture failed", append(sourceContextAuditAttrs(r, capture.ID, capture.Snapshot.SourceIssue.ID, capture.Snapshot.AnchorCommentID, userUUID, req.Mode, build.Limits, "failed"), "error", err)...)
			if !errors.Is(err, errSourceContextResponseWritten) {
				h.writeSourceContextError(w, err, service.SourceContextLimitUsage{})
			}
		}
	}
}

func sourceContextAuditAttrs(r *http.Request, contextID pgtype.UUID, sourceIssueID, anchorCommentID string, capturedBy pgtype.UUID, mode string, limits service.SourceContextLimitUsage, result string) []any {
	return append(logger.RequestAttrs(r),
		"source_context_id", util.UUIDToString(contextID),
		"source_issue_id", sourceIssueID,
		"anchor_comment_id", anchorCommentID,
		"captured_by_user_id", util.UUIDToString(capturedBy),
		"mode", mode,
		"comment_count", limits.CommentCount,
		"text_bytes", limits.TextBytes,
		"attachment_count", limits.AttachmentCount,
		"attachment_bytes", limits.AttachmentBytes,
		"result", result,
	)
}

func (h *Handler) cloneSourceContext(ctx context.Context, workspaceID, userID, contextID pgtype.UUID, build service.SourceContextBuild) (service.SourceContextCapture, []string, error) {
	if len(build.Rows) == 0 {
		capture, err := service.PrepareSourceContextCapture(build, contextID, workspaceID, userID, time.Now(), nil)
		return capture, nil, err
	}
	uploader, ok := h.Storage.(sourceContextStreamUploader)
	if h.Storage == nil || !ok {
		return service.SourceContextCapture{}, nil, errors.New("storage does not support streaming upload")
	}
	clones := make([]service.SourceContextClone, 0, len(build.Rows))
	keys := make([]string, 0, len(build.Rows))
	cleanup := func() {
		for _, key := range keys {
			h.Storage.Delete(context.Background(), key)
		}
	}
	for _, source := range build.Rows {
		sourceKey := h.Storage.KeyFromURL(source.Url)
		reader, err := h.Storage.GetReader(ctx, sourceKey)
		if err != nil {
			cleanup()
			return service.SourceContextCapture{}, nil, err
		}
		cloneID := dbid.NewV7()
		cloneKey := fmt.Sprintf("workspaces/%s/source-context/%s%s", util.UUIDToString(workspaceID), util.UUIDToString(cloneID), path.Ext(source.Filename))
		if _, err := h.Queries.RecordSourceContextObjectIntent(ctx, db.RecordSourceContextObjectIntentParams{
			StorageKey: cloneKey, WorkspaceID: workspaceID, SourceContextID: contextID,
			AttachmentID: cloneID, ObjectUrl: h.Storage.ObjectURL(cloneKey),
		}); err != nil {
			_ = reader.Close()
			cleanup()
			return service.SourceContextCapture{}, nil, fmt.Errorf("record source context object intent: %w", err)
		}
		url, uploadErr := uploader.UploadStream(ctx, cloneKey, reader, source.SizeBytes, source.ContentType, source.Filename)
		closeErr := reader.Close()
		if uploadErr != nil || closeErr != nil {
			cleanup()
			if uploadErr != nil {
				return service.SourceContextCapture{}, nil, uploadErr
			}
			return service.SourceContextCapture{}, nil, closeErr
		}
		keys = append(keys, cloneKey)
		clones = append(clones, service.SourceContextClone{ID: cloneID, Filename: source.Filename, URL: url, ContentType: source.ContentType, SizeBytes: source.SizeBytes})
	}
	capture, err := service.PrepareSourceContextCapture(build, contextID, workspaceID, userID, time.Now(), clones)
	if err != nil {
		cleanup()
		return service.SourceContextCapture{}, nil, err
	}
	return capture, keys, nil
}

func (h *Handler) createManualCommentSubIssue(w http.ResponseWriter, r *http.Request, workspaceID, userID pgtype.UUID, input CreateIssueRequest, capture service.SourceContextCapture, limits service.SourceContextLimitUsage) error {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return sourceContextBadRequest("title is required")
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "todo"
	}
	var ok bool
	status, ok = h.resolveIssueStatusKey(w, r, workspaceID, status)
	if !ok {
		return errSourceContextResponseWritten
	}
	priority := strings.TrimSpace(input.Priority)
	if priority == "" {
		priority = "none"
	}
	if !validateIssueEnum(w, "priority", priority, validIssuePriorities) {
		return errSourceContextResponseWritten
	}
	var assigneeType pgtype.Text
	var assigneeID pgtype.UUID
	if input.AssigneeType != nil && strings.TrimSpace(*input.AssigneeType) != "" {
		assigneeType = pgtype.Text{String: strings.TrimSpace(*input.AssigneeType), Valid: true}
	}
	if input.AssigneeID != nil && strings.TrimSpace(*input.AssigneeID) != "" {
		parsed, err := util.ParseUUID(strings.TrimSpace(*input.AssigneeID))
		if err != nil {
			return sourceContextBadRequest("invalid assignee_id")
		}
		assigneeID = parsed
	}
	if code, message := h.validateAssigneePair(r.Context(), r, util.UUIDToString(workspaceID), assigneeType, assigneeID, scopeNoDelegation()); code != 0 {
		writeError(w, code, message)
		return errSourceContextResponseWritten
	}
	var projectID pgtype.UUID
	if input.ProjectID != nil && strings.TrimSpace(*input.ProjectID) != "" {
		parsed, err := util.ParseUUID(strings.TrimSpace(*input.ProjectID))
		if err != nil {
			return sourceContextBadRequest("invalid project_id")
		}
		projectID = parsed
	}
	attachmentIDs, ok := parseUUIDSliceOrBadRequest(w, input.AttachmentIDs, "attachment_ids")
	if !ok {
		return errSourceContextResponseWritten
	}
	labelIDs, ok := parseUUIDSliceOrBadRequest(w, input.LabelIDs, "label_ids")
	if !ok {
		return errSourceContextResponseWritten
	}
	var startDate, dueDate pgtype.Date
	if input.StartDate != nil && *input.StartDate != "" {
		parsed, err := util.ParseCalendarDate(*input.StartDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid start_date format, expected YYYY-MM-DD")
			return errSourceContextResponseWritten
		}
		startDate = parsed
	}
	if input.DueDate != nil && *input.DueDate != "" {
		parsed, err := util.ParseCalendarDate(*input.DueDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid due_date format, expected YYYY-MM-DD")
			return errSourceContextResponseWritten
		}
		dueDate = parsed
	}
	var stage pgtype.Int4
	if input.Stage != nil {
		if *input.Stage < 1 {
			writeError(w, http.StatusBadRequest, "stage must be at least 1")
			return errSourceContextResponseWritten
		}
		stage = pgtype.Int4{Int32: *input.Stage, Valid: true}
	}
	prefix := h.getIssuePrefix(r.Context(), workspaceID)
	result, err := h.IssueService.Create(r.Context(), service.IssueCreateParams{
		WorkspaceID: workspaceID, Title: title, Description: ptrToText(input.Description), Status: status, Priority: priority,
		AssigneeType: assigneeType, AssigneeID: assigneeID, CreatorType: "member", CreatorID: userID,
		ParentIssueID: capture.SourceIssueID, ProjectID: projectID, StartDate: startDate, DueDate: dueDate,
		AttachmentIDs: attachmentIDs, LabelIDs: labelIDs, Stage: stage,
		AllowDuplicate: input.AllowDuplicate, SourceContext: &capture,
	}, service.IssueCreateOpts{
		ActorID: util.UUIDToString(userID),
		BroadcastPayload: func(issue db.Issue, _ []db.Attachment, labels []db.IssueLabel) map[string]any {
			response := issueToResponse(issue, prefix)
			labelResponses := labelsToResponse(labels)
			response.Labels = &labelResponses
			return map[string]any{"issue": response}
		},
	})
	if err != nil {
		return err
	}
	response := issueToResponse(result.Issue, prefix)
	h.fillStatusCategory(r.Context(), workspaceID, &response)
	labelResponses := labelsToResponse(result.Labels)
	response.Labels = &labelResponses
	slog.Info("sub-issue created from captured comment", append(sourceContextAuditAttrs(r, capture.ID, capture.Snapshot.SourceIssue.ID, capture.Snapshot.AnchorCommentID, userID, "manual", limits, "success"), "target_issue_id", uuidToString(result.Issue.ID))...)
	writeJSON(w, http.StatusCreated, response)
	return nil
}

type preparedAgentCommentSubIssue struct {
	agentID, squadID, runtimeID pgtype.UUID
	prompt, priority, dueDate   string
	projectID                   pgtype.UUID
	attachmentIDs               []pgtype.UUID
}

func (h *Handler) prepareAgentCommentSubIssue(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, input QuickCreateIssueRequest) (*preparedAgentCommentSubIssue, error) {
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		return nil, sourceContextBadRequest("prompt is required")
	}
	hasAgent := strings.TrimSpace(input.AgentID) != ""
	hasSquad := strings.TrimSpace(input.SquadID) != ""
	if hasAgent == hasSquad {
		return nil, sourceContextBadRequest("exactly one of agent_id or squad_id is required")
	}
	priority := strings.ToLower(strings.TrimSpace(input.Priority))
	if priority != "" && priority != "urgent" && priority != "high" && priority != "medium" && priority != "low" {
		return nil, sourceContextBadRequest("invalid priority")
	}
	var agentID, squadID pgtype.UUID
	if hasSquad {
		parsed, err := util.ParseUUID(strings.TrimSpace(input.SquadID))
		if err != nil {
			return nil, sourceContextBadRequest("invalid squad_id")
		}
		squadID = parsed
		squad, err := h.Queries.GetSquadInWorkspace(r.Context(), db.GetSquadInWorkspaceParams{ID: squadID, WorkspaceID: workspaceID})
		if err != nil || squad.ArchivedAt.Valid {
			return nil, sourceContextBadRequest("squad not found or archived")
		}
		agentID = squad.LeaderID
	} else {
		parsed, err := util.ParseUUID(strings.TrimSpace(input.AgentID))
		if err != nil {
			return nil, sourceContextBadRequest("invalid agent_id")
		}
		agentID = parsed
	}
	if status, message := h.validateAssigneePair(r.Context(), r, util.UUIDToString(workspaceID), pgtype.Text{String: "agent", Valid: true}, agentID, scopeNoDelegation()); status != 0 {
		writeError(w, status, message)
		return nil, errSourceContextResponseWritten
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: workspaceID})
	if err != nil {
		return nil, sourceContextBadRequest("agent not found")
	}
	verdict, err := service.AgentReadiness(r.Context(), h.Queries, agent)
	if err != nil {
		return nil, err
	}
	if !verdict.Ready() {
		writeAgentUnavailable(w, verdict.Detail, verdict.Reason)
		return nil, errSourceContextResponseWritten
	}
	if status, payload := h.checkQuickCreateDaemonVersion(r.Context(), agent.RuntimeID); status != 0 {
		writeJSON(w, status, payload)
		return nil, errSourceContextResponseWritten
	}
	runtime, err := h.Queries.GetAgentRuntime(r.Context(), agent.RuntimeID)
	if err != nil || !runtimeHasCapability(runtime.Metadata, protocol.DaemonCapabilitySourceContextQuickCreateV1) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"code": "source_context_quick_create_unsupported", "error": "selected agent runtime must be updated before using captured context"})
		return nil, errSourceContextResponseWritten
	}
	dueDate := strings.TrimSpace(input.DueDate)
	if dueDate != "" {
		parsed, err := util.ParseCalendarDate(dueDate)
		if err != nil {
			return nil, sourceContextBadRequest("invalid due_date")
		}
		dueDate = parsed.Time.Format("2006-01-02")
	}
	if priority != "" || dueDate != "" {
		if status, payload := h.checkQuickCreateDaemonVersionAtLeast(r.Context(), agent.RuntimeID, agentpkg.MinQuickCreateFieldsCLIVersion); status != 0 {
			writeJSON(w, status, payload)
			return nil, errSourceContextResponseWritten
		}
	}
	attachmentIDs, ok := parseUUIDSliceOrBadRequest(w, input.AttachmentIDs, "attachment_ids")
	if !ok {
		return nil, errSourceContextResponseWritten
	}
	var projectID pgtype.UUID
	if strings.TrimSpace(input.ProjectID) != "" {
		parsed, err := util.ParseUUID(strings.TrimSpace(input.ProjectID))
		if err != nil {
			return nil, sourceContextBadRequest("invalid project_id")
		}
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: parsed, WorkspaceID: workspaceID}); err != nil {
			return nil, sourceContextBadRequest("project not found")
		}
		projectID = parsed
	}
	return &preparedAgentCommentSubIssue{
		agentID: agentID, squadID: squadID, runtimeID: agent.RuntimeID,
		prompt: prompt, priority: priority, dueDate: dueDate,
		projectID: projectID, attachmentIDs: attachmentIDs,
	}, nil
}

func (h *Handler) createAgentCommentSubIssue(w http.ResponseWriter, r *http.Request, workspaceID, userID pgtype.UUID, prepared preparedAgentCommentSubIssue, capture service.SourceContextCapture, limits service.SourceContextLimitUsage) error {
	// Recheck the capability after potentially long streaming copies. A runtime
	// can re-register during the copy; the final enqueue must still fail closed.
	runtime, err := h.Queries.GetAgentRuntime(r.Context(), prepared.runtimeID)
	if err != nil || !runtimeHasCapability(runtime.Metadata, protocol.DaemonCapabilitySourceContextQuickCreateV1) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"code": "source_context_quick_create_unsupported", "error": "selected agent runtime must be updated before using captured context"})
		return errSourceContextResponseWritten
	}
	task, err := h.TaskService.EnqueueQuickCreateTaskWithSourceContext(r.Context(), workspaceID, userID, prepared.agentID, prepared.squadID, prepared.prompt, prepared.priority, prepared.dueDate, prepared.projectID, capture.SourceIssueID, prepared.attachmentIDs, capture)
	if err != nil {
		return err
	}
	slog.Info("sub-issue quick-create enqueued from captured comment", append(sourceContextAuditAttrs(r, capture.ID, capture.Snapshot.SourceIssue.ID, capture.Snapshot.AnchorCommentID, userID, "agent", limits, "success"), "task_id", uuidToString(task.ID))...)
	writeJSON(w, http.StatusAccepted, QuickCreateIssueResponse{TaskID: uuidToString(task.ID)})
	return nil
}

func (h *Handler) writeSourceContextError(w http.ResponseWriter, err error, limits service.SourceContextLimitUsage) {
	status := http.StatusInternalServerError
	code := "source_context_capture_failed"
	message := "failed to capture source context"
	switch {
	case errors.Is(err, service.ErrSourceContextChanged):
		status, code = http.StatusConflict, "source_context_changed"
		message = err.Error()
	case errors.Is(err, service.ErrAnchorCommentDeleted):
		status, code = http.StatusConflict, "anchor_comment_deleted"
		message = err.Error()
	case errors.Is(err, service.ErrSourceIssueDeleted):
		status, code = http.StatusConflict, "source_issue_deleted"
		message = err.Error()
	case errors.Is(err, service.ErrSourceContextInvalid):
		status, code = http.StatusConflict, "source_context_invalid_path"
		message = err.Error()
	case errors.Is(err, service.ErrSourceContextTooLarge):
		status, code = http.StatusUnprocessableEntity, "source_context_too_large"
		message = err.Error()
	case errors.Is(err, service.ErrSourceContextAlreadyAttached):
		status, code = http.StatusConflict, "source_context_already_attached"
		message = err.Error()
	case errors.Is(err, service.ErrActiveDuplicate):
		status, code = http.StatusConflict, "active_duplicate_issue"
		message = err.Error()
	case errors.Is(err, service.ErrParentIssueNotFound), errors.Is(err, service.ErrProjectNotFound):
		status = http.StatusBadRequest
		message = err.Error()
	case errors.Is(err, errSourceContextBadRequest):
		status, code = http.StatusBadRequest, "invalid_request"
		message = err.Error()
	case err == nil:
		return
	}
	payload := map[string]any{"code": code, "error": message}
	if errors.Is(err, service.ErrSourceContextTooLarge) {
		payload["limits"] = limits
	}
	writeJSON(w, status, payload)
}
