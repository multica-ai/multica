package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/storage"
)

// PresignUploadRequest is the body of POST /api/upload-file/presign.
// Called by clients that want to upload a file bigger than maxUploadSize
// (100 MB) and therefore cannot go through the multipart path. The
// flow is:
//
//  1. POST /api/upload-file/presign with filename + content_type +
//     size_bytes + optional issue_id / comment_id. Server creates the
//     attachment row with size_bytes=0 (sentinel for "upload in flight")
//     and returns a pre-signed PUT URL.
//  2. Client PUT <url> with the file bytes and the matching
//     Content-Type header.
//  3. POST /api/attachments/{id}/confirm. Server HeadObjects the key,
//     updates size_bytes on the attachment row, and returns the final
//     attachment response.
//
// Steps 2 and 3 can happen hours apart; we don't garbage-collect
// "upload in flight" attachments. They remain with size_bytes=0 if the
// client never confirms, and look like orphan rows — acceptable for
// now.
type PresignUploadRequest struct {
	Filename    string  `json:"filename"`
	ContentType string  `json:"content_type"`
	SizeBytes   int64   `json:"size_bytes"`
	IssueID     *string `json:"issue_id,omitempty"`
	CommentID   *string `json:"comment_id,omitempty"`
}

// PresignUploadResponse echoes the client-driven upload payload plus
// the attachment record so the client can reference it later without
// a second round trip.
type PresignUploadResponse struct {
	Attachment     AttachmentResponse        `json:"attachment"`
	Upload         storage.PresignedUpload   `json:"upload"`
}

// presignMinSize is the smallest size we'll issue a pre-signed URL
// for. Anything under this should just go through the ordinary
// POST /api/upload-file multipart path — presign is more complex for
// the client, so we only recommend it when the file is too big to fit
// in a single request body.
const presignMinSize = 50 << 20 // 50 MB

// presignMaxSize caps pre-signed uploads. 16 GB is plenty for kernel
// dumps (the largest we've seen is ~2 GB) while still preventing an
// attacker from using the endpoint to stage arbitrarily large objects
// against our bucket. Adjust via env if needed.
const presignMaxSize = 16 << 30 // 16 GiB

// presignExpiry is how long the pre-signed PUT URL stays valid. We
// give generous headroom so a flaky network / retries have a chance
// to complete an upload of a multi-GB dump.
const presignExpiry = 30 * time.Minute

// PresignUpload issues a pre-signed S3 URL for direct client upload.
// Storage backends without presign support (LocalStorage) reject with
// 501 and the client should fall back to POST /api/upload-file.
func (h *Handler) PresignUpload(w http.ResponseWriter, r *http.Request) {
	if h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "file upload not configured")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace context is required")
		return
	}
	if _, err := h.getWorkspaceMember(r.Context(), userID, workspaceID); err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return
	}

	var req PresignUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Filename == "" {
		writeError(w, http.StatusBadRequest, "filename is required")
		return
	}
	if req.SizeBytes <= 0 {
		writeError(w, http.StatusBadRequest, "size_bytes is required and must be > 0")
		return
	}
	if req.SizeBytes < presignMinSize {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("file smaller than %d bytes should use POST /api/upload-file directly", presignMinSize))
		return
	}
	if req.SizeBytes > presignMaxSize {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("file exceeds pre-sign cap of %d bytes", presignMaxSize))
		return
	}
	if req.ContentType == "" {
		req.ContentType = "application/octet-stream"
	}

	// Validate optional issue_id / comment_id belong to this workspace.
	var issueID, commentID pgtype.UUID
	if req.IssueID != nil && *req.IssueID != "" {
		issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          parseUUID(*req.IssueID),
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			writeError(w, http.StatusForbidden, "invalid issue_id")
			return
		}
		issueID = issue.ID
	}
	if req.CommentID != nil && *req.CommentID != "" {
		comment, err := h.Queries.GetComment(r.Context(), parseUUID(*req.CommentID))
		if err != nil || uuidToString(comment.WorkspaceID) != workspaceID {
			writeError(w, http.StatusForbidden, "invalid comment_id")
			return
		}
		commentID = comment.ID
	}

	// Generate the storage key. Shape matches the body-upload path so
	// KeyFromURL and Delete handle both identically.
	attID, err := uuid.NewV7()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	objName := attID.String() + path.Ext(req.Filename)
	key := "workspaces/" + workspaceID + "/" + objName

	presigned, err := h.Storage.PresignPut(r.Context(), key, req.ContentType, req.Filename, presignExpiry)
	if err != nil {
		if errors.Is(err, storage.ErrPresignUnsupported) {
			writeError(w, http.StatusNotImplemented,
				"configured storage backend does not support pre-signed uploads")
			return
		}
		slog.Error("PresignPut failed",
			append(logger.RequestAttrs(r), "key", key, "error", err)...)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to issue upload URL: %v", err))
		return
	}

	uploaderType, uploaderID := h.resolveActor(r, userID, workspaceID)
	link := h.Storage.PublicURL(key)

	att, err := h.Queries.CreateAttachment(r.Context(), db.CreateAttachmentParams{
		ID:           pgtype.UUID{Bytes: attID, Valid: true},
		WorkspaceID:  parseUUID(workspaceID),
		IssueID:      issueID,
		CommentID:    commentID,
		UploaderType: uploaderType,
		UploaderID:   parseUUID(uploaderID),
		Filename:     req.Filename,
		Url:          link,
		ContentType:  req.ContentType,
		// size_bytes deliberately left at 0 until /confirm lands. 0 is
		// the "upload in flight" sentinel — real uploads always have
		// size >= 1.
		SizeBytes: 0,
	})
	if err != nil {
		slog.Error("CreateAttachment for presign failed",
			append(logger.RequestAttrs(r), "key", key, "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create attachment record")
		return
	}

	writeJSON(w, http.StatusOK, PresignUploadResponse{
		Attachment: h.attachmentToResponse(att),
		Upload:     *presigned,
	})
}

// ConfirmAttachmentUpload finalises an attachment after the client has
// successfully PUT the bytes via the pre-signed URL from
// /api/upload-file/presign. Does a HEAD against storage to make sure
// the object actually exists, then updates size_bytes on the
// attachment row.
func (h *Handler) ConfirmAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	if h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "file upload not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if _, err := h.getWorkspaceMember(r.Context(), userID, workspaceID); err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return
	}

	attID := chi.URLParam(r, "id")
	att, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{
		ID:          parseUUID(attID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}
	// No additional workspace check needed — GetAttachment filters on workspace_id.

	key := h.Storage.KeyFromURL(att.Url)
	size, err := h.Storage.StatObject(r.Context(), key)
	if err != nil {
		slog.Warn("ConfirmAttachmentUpload: StatObject failed",
			append(logger.RequestAttrs(r), "key", key, "error", err)...)
		writeError(w, http.StatusBadRequest,
			"object not found in storage — client PUT either failed or has not reached the bucket yet; retry after the PUT succeeds")
		return
	}

	updated, err := h.Queries.UpdateAttachmentSize(r.Context(), db.UpdateAttachmentSizeParams{
		ID:        att.ID,
		SizeBytes: size,
	})
	if err != nil {
		slog.Error("UpdateAttachmentSize failed",
			append(logger.RequestAttrs(r), "id", attID, "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to finalise attachment")
		return
	}

	writeJSON(w, http.StatusOK, h.attachmentToResponse(updated))
}
