package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type ReviewAssetResponse struct {
	ID           string   `json:"id"`
	IssueID      string   `json:"issue_id"`
	WorkspaceID  string   `json:"workspace_id"`
	AssetGroupID string   `json:"asset_group_id"`
	Name         string   `json:"name"`
	AssetType    string   `json:"asset_type"`
	SrcURL       string   `json:"src_url"`
	ThumbnailURL *string  `json:"thumbnail_url,omitempty"`
	Width        *int32   `json:"width"`
	Height       *int32   `json:"height"`
	Duration     *float32 `json:"duration"`
	Version      int32    `json:"version"`
	Status       string   `json:"status"`
	UploadedBy   *string  `json:"uploaded_by"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

type ReviewCommentResponse struct {
	ID         string          `json:"id"`
	AssetID    string          `json:"asset_id"`
	AuthorID   string          `json:"author_id"`
	Content    string          `json:"content"`
	Timestamp  *float32        `json:"timestamp"`
	Shapes     json.RawMessage `json:"shapes"`
	Resolved   bool            `json:"resolved"`
	ResolvedBy *string         `json:"resolved_by"`
	ResolvedAt *string         `json:"resolved_at"`
	ParentID   *string         `json:"parent_id"`
	CreatedAt  string          `json:"created_at"`
	UpdatedAt  string          `json:"updated_at"`
}

func (h *Handler) reviewAssetToResponse(a db.ReviewAsset) ReviewAssetResponse {
	fileURL := a.FileUrl // fallback: raw key
	if presigner, ok := h.Storage.(storage.Presigner); ok {
		if signed, err := presigner.PresignGet(context.Background(), a.FileUrl, 30*time.Minute); err == nil {
			fileURL = signed
		} else {
			slog.Warn("review asset presign failed, returning raw key", "asset_id", uuidToString(a.ID), "error", err)
		}
	} else {
		slog.Warn("storage does not support presigned GETs; review asset src_url will be a raw key", "asset_id", uuidToString(a.ID))
	}
	return ReviewAssetResponse{
		ID:           uuidToString(a.ID),
		IssueID:      uuidToString(a.IssueID),
		WorkspaceID:  uuidToString(a.WorkspaceID),
		AssetGroupID: uuidToString(a.AssetGroupID),
		Name:         a.Name,
		AssetType:    a.AssetType,
		SrcURL:       fileURL,
		ThumbnailURL: textToPtr(a.ThumbnailUrl),
		Width:        int4ToPtr(a.Width),
		Height:       int4ToPtr(a.Height),
		Duration:     float4ToPtr(a.Duration),
		Version:      a.Version,
		Status:       a.Status,
		UploadedBy:   uuidToPtr(a.UploadedBy),
		CreatedAt:    timestampToString(a.CreatedAt),
		UpdatedAt:    timestampToString(a.UpdatedAt),
	}
}

func reviewCommentToResponse(c db.ReviewComment) ReviewCommentResponse {
	return ReviewCommentResponse{
		ID:         uuidToString(c.ID),
		AssetID:    uuidToString(c.AssetID),
		AuthorID:   uuidToString(c.AuthorID),
		Content:    c.Content,
		Timestamp:  float4ToPtr(c.Timestamp),
		Shapes:     c.Shapes,
		Resolved:   c.Resolved,
		ResolvedBy: uuidToPtr(c.ResolvedBy),
		ResolvedAt: timestampToPtr(c.ResolvedAt),
		ParentID:   uuidToPtr(c.ParentID),
		CreatedAt:  timestampToString(c.CreatedAt),
		UpdatedAt:  timestampToString(c.UpdatedAt),
	}
}

func float4ToPtr(f pgtype.Float4) *float32 {
	if !f.Valid {
		return nil
	}
	return &f.Float32
}

type PresignReviewAssetUploadRequest struct {
	IssueID         string `json:"issue_id"`
	Filename        string `json:"filename"`
	ContentType     string `json:"content_type"`
	PreviousAssetID string `json:"previous_asset_id,omitempty"`
}

type PresignReviewAssetUploadResponse struct {
	UploadURL string              `json:"upload_url"`
	Asset     ReviewAssetResponse `json:"asset"`
}

func (h *Handler) PresignReviewAssetUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIDStr := h.resolveWorkspaceID(r)
	requester, ok := h.requireWorkspaceRole(w, r, workspaceIDStr, "workspace not found", "owner", "admin", "member")
	if !ok {
		return
	}

	var req PresignReviewAssetUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	issueUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "issue_id")
	if !ok {
		return
	}

	// Verify issue exists
	issue, err := h.Queries.GetIssue(ctx, issueUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}

	var previousAssetUUID pgtype.UUID
	if req.PreviousAssetID != "" {
		parsed, ok := parseUUIDOrBadRequest(w, req.PreviousAssetID, "previous_asset_id")
		if !ok {
			return
		}
		previousAssetUUID = parsed
	}

	assetType := "image"
	if req.ContentType == "video/mp4" || req.ContentType == "video/webm" || req.ContentType == "video/quicktime" {
		assetType = "video"
	}

	// For S3 we can generate a presigned URL. For local, we fallback to a direct upload URL
	var uploadURL string
	fileKey := "reviews/" + util.UUIDToString(issueUUID) + "/" + uuid.New().String() + "_" + req.Filename

	if presigner, ok := h.Storage.(storage.UploadPresigner); ok {
		uploadURL, err = presigner.PresignPut(ctx, fileKey, req.ContentType, 15*time.Minute)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to generate upload url")
			return
		}
	} else {
		// Fallback for local storage (the client will upload to our direct endpoint)
		uploadURL = h.cfg.PublicURL + "/api/reviews/assets/direct-upload?key=" + fileKey
	}

	// Determine version and group ID
	var assetGroupID pgtype.UUID
	version := int32(1)
	if previousAssetUUID.Valid {
		prev, err := h.Queries.GetReviewAsset(ctx, previousAssetUUID)
		if err == nil {
			assetGroupID = prev.AssetGroupID
			version = prev.Version + 1
		} else {
			assetGroupID = util.MustParseUUID(uuid.New().String())
		}
	} else {
		assetGroupID = util.MustParseUUID(uuid.New().String())
	}

	// Create pending asset
	asset, err := h.Queries.CreateReviewAsset(ctx, db.CreateReviewAssetParams{
		IssueID:      issueUUID,
		WorkspaceID:  issue.WorkspaceID,
		Name:         req.Filename,
		AssetType:    assetType,
		FileUrl:      fileKey, // Store key, we can resolve full URL on fetch
		Version:      version,
		UploadedBy:   requester.ID,
		AssetGroupID: assetGroupID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create asset record")
		return
	}

	writeJSON(w, http.StatusOK, PresignReviewAssetUploadResponse{
		UploadURL: uploadURL,
		Asset:     h.reviewAssetToResponse(asset),
	})
}

func (h *Handler) DirectUploadReviewAsset(w http.ResponseWriter, r *http.Request) {
	// Stub for local storage direct upload fallback
	writeError(w, http.StatusNotImplemented, "direct upload not implemented yet")
}

func (h *Handler) CompleteReviewAssetUpload(w http.ResponseWriter, r *http.Request) {
	// Stub for upload completion (triggers ffprobe metadata extraction in background)
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// Comments endpoints

func (h *Handler) ListReviewComments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	assetUUID, ok := parseUUIDOrBadRequest(w, r.URL.Query().Get("asset_id"), "asset_id")
	if !ok {
		return
	}

	comments, err := h.Queries.ListReviewCommentsByAsset(ctx, assetUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fetch comments")
		return
	}

	var res []ReviewCommentResponse
	for _, c := range comments {
		res = append(res, reviewCommentToResponse(c))
	}
	if res == nil {
		res = []ReviewCommentResponse{}
	}

	writeJSON(w, http.StatusOK, res)
}

type CreateReviewCommentRequest struct {
	AssetID   string          `json:"asset_id"`
	Content   string          `json:"content"`
	Timestamp *float32        `json:"timestamp"`
	Shapes    json.RawMessage `json:"shapes"`
	ParentID  *string         `json:"parent_id"`
}

func (h *Handler) CreateReviewComment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceIDStr := h.resolveWorkspaceID(r)
	requester, ok := h.requireWorkspaceRole(w, r, workspaceIDStr, "workspace not found", "owner", "admin", "member")
	if !ok {
		return
	}
	userID := uuidToString(requester.UserID)

	var req CreateReviewCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	assetUUID, ok := parseUUIDOrBadRequest(w, req.AssetID, "asset_id")
	if !ok {
		return
	}

	var parentUUID pgtype.UUID
	if req.ParentID != nil {
		parentUUID = util.MustParseUUID(*req.ParentID)
	}

	var shapes json.RawMessage
	if len(req.Shapes) > 0 {
		shapes = req.Shapes
	} else {
		shapes = json.RawMessage(`[]`)
	}

	var timestamp pgtype.Float4
	if req.Timestamp != nil {
		timestamp = pgtype.Float4{Float32: *req.Timestamp, Valid: true}
	}

	comment, err := h.Queries.CreateReviewComment(ctx, db.CreateReviewCommentParams{
		AssetID:   assetUUID,
		AuthorID:  requester.ID,
		Content:   req.Content,
		Timestamp: timestamp,
		Shapes:    shapes,
		ParentID:  parentUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create comment")
		return
	}

	resp := reviewCommentToResponse(comment)
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID != "" {
		asset, _ := h.Queries.GetReviewAsset(ctx, assetUUID)
		issue, _ := h.Queries.GetIssue(ctx, asset.IssueID)
		h.publish(protocol.EventReviewCommentCreated, workspaceID, "member", userID, map[string]any{
			"comment":      resp,
			"issue_id":     util.UUIDToString(asset.IssueID),
			"issue_title":  issue.Title,
			"issue_status": issue.Status,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListReviewAssets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	issueUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "issue_id")
	if !ok {
		return
	}

	assets, err := h.Queries.ListReviewAssetsByIssue(ctx, issueUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list review assets")
		return
	}

	var res []ReviewAssetResponse
	for _, a := range assets {
		res = append(res, h.reviewAssetToResponse(a))
	}
	if res == nil {
		res = []ReviewAssetResponse{}
	}

	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	writeJSON(w, http.StatusOK, res)
}

type UpdateReviewAssetStatusRequest struct {
	Status string `json:"status"` // pending, approved, changes_requested
}

func (h *Handler) UpdateReviewAssetStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	assetUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "assetId"), "assetId")
	if !ok {
		return
	}

	var req UpdateReviewAssetStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if req.Status != "pending" && req.Status != "approved" && req.Status != "changes_requested" {
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}

	asset, err := h.Queries.UpdateReviewAssetStatus(ctx, db.UpdateReviewAssetStatusParams{
		ID:     assetUUID,
		Status: req.Status,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update asset status")
		return
	}

	// userID, ok := requireUserID(w, r)
	// We might not have parsed it above if not needed, let's grab it for the event
	userID := r.Header.Get("X-User-ID") // fallback, but usually we use requireUserID
	if userID == "" {
		if u, ok := requireUserID(w, r); ok {
			userID = u
		}
	}

	resp := h.reviewAssetToResponse(asset)
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID != "" {
		issue, _ := h.Queries.GetIssue(ctx, asset.IssueID)
		h.publish(protocol.EventReviewAssetUpdated, workspaceID, "member", userID, map[string]any{
			"asset":        resp,
			"issue_id":     util.UUIDToString(asset.IssueID),
			"issue_title":  issue.Title,
			"issue_status": issue.Status,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

type BulkApproveReviewAssetsRequest struct {
	IssueID string `json:"issue_id"`
}

func (h *Handler) BulkApproveReviewAssets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req BulkApproveReviewAssetsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	issueUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "issue_id")
	if !ok {
		return
	}

	err := h.Queries.BulkApproveReviewAssets(ctx, issueUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to bulk approve assets")
		return
	}

	userID := ""
	if u, ok := requireUserID(w, r); ok {
		userID = u
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID != "" {
		issue, _ := h.Queries.GetIssue(ctx, issueUUID)
		// Empty payload will force clients to refetch
		h.publish(protocol.EventReviewAssetUpdated, workspaceID, "member", userID, map[string]any{
			"issue_id":     util.UUIDToString(issueUUID),
			"issue_title":  issue.Title,
			"issue_status": issue.Status,
		})
	}

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *Handler) DownloadReviewAsset(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	assetUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "assetId"), "assetId")
	if !ok {
		return
	}

	asset, err := h.Queries.GetReviewAsset(ctx, assetUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "asset not found")
		return
	}

	workspaceIDStr := uuidToString(asset.WorkspaceID)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceIDStr, "workspace not found", "owner", "admin", "member"); !ok {
		return
	}

	if h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "storage not configured")
		return
	}

	// review_assets.file_url stores a bare S3 key (e.g. "reviews/<issueId>/<uuid>_file.jpg"),
	// NOT a full URL. Presign it directly.
	key := asset.FileUrl
	presigner, ok := h.Storage.(storage.Presigner)
	if !ok {
		writeError(w, http.StatusInternalServerError, "storage does not support presigned downloads")
		return
	}
	signedURL, err := presigner.PresignGet(ctx, key, h.attachmentDownloadURLTTL())
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to create download URL")
		return
	}
	http.Redirect(w, r, signedURL, http.StatusFound)
}

func (h *Handler) ResolveReviewComment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	commentUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "commentId"), "commentId")
	if !ok {
		return
	}
	workspaceIDStr := h.resolveWorkspaceID(r)
	requester, ok := h.requireWorkspaceRole(w, r, workspaceIDStr, "workspace not found", "owner", "admin", "member")
	if !ok {
		return
	}
	userID := uuidToString(requester.UserID)

	comment, err := h.Queries.ResolveReviewComment(ctx, db.ResolveReviewCommentParams{
		ID:         commentUUID,
		ResolvedBy: requester.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve comment")
		return
	}

	resp := reviewCommentToResponse(comment)
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID != "" {
		h.publish(protocol.EventReviewCommentResolved, workspaceID, "member", userID, map[string]any{"comment": resp})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) UnresolveReviewComment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	commentUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "commentId"), "commentId")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	comment, err := h.Queries.UnresolveReviewComment(ctx, commentUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unresolve comment")
		return
	}

	resp := reviewCommentToResponse(comment)
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID != "" {
		h.publish(protocol.EventReviewCommentUnresolved, workspaceID, "member", userID, map[string]any{"comment": resp})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListPendingReviewIssueIDs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	issueUUIDs, err := h.Queries.ListPendingReviewIssueIDs(ctx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pending review issue ids")
		return
	}

	res := make([]string, len(issueUUIDs))
	for i, id := range issueUUIDs {
		res[i] = util.UUIDToString(id)
	}

	writeJSON(w, http.StatusOK, res)
}

// DeleteReviewAsset deletes a single version by asset ID.
// Comments cascade via the FK constraint.
func (h *Handler) DeleteReviewAsset(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	assetID := chi.URLParam(r, "assetId")
	assetUUID, ok := parseUUIDOrBadRequest(w, assetID, "assetId")
	if !ok {
		return
	}
	if err := h.Queries.DeleteReviewAsset(ctx, assetUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete review asset")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteReviewAssetGroup deletes all versions in an asset group.
func (h *Handler) DeleteReviewAssetGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	groupID := chi.URLParam(r, "groupId")
	groupUUID, ok := parseUUIDOrBadRequest(w, groupID, "groupId")
	if !ok {
		return
	}
	if err := h.Queries.DeleteReviewAssetGroup(ctx, groupUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete review asset group")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
