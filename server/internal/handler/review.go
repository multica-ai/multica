package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type ReviewAssetResponse struct {
	ID           string  `json:"id"`
	IssueID      string  `json:"issue_id"`
	WorkspaceID  string  `json:"workspace_id"`
	Name         string  `json:"name"`
	AssetType    string  `json:"asset_type"`
	FileURL      string  `json:"file_url"`
	ThumbnailURL *string `json:"thumbnail_url"`
	Width        *int32  `json:"width"`
	Height       *int32  `json:"height"`
	Duration     *float32 `json:"duration"`
	Version      int32   `json:"version"`
	Status       string  `json:"status"`
	UploadedBy   *string `json:"uploaded_by"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type ReviewCommentResponse struct {
	ID         string  `json:"id"`
	AssetID    string  `json:"asset_id"`
	AuthorID   string  `json:"author_id"`
	Content    string  `json:"content"`
	Timestamp  *float32 `json:"timestamp"`
	Shapes     json.RawMessage `json:"shapes"`
	Resolved   bool    `json:"resolved"`
	ResolvedBy *string `json:"resolved_by"`
	ResolvedAt *string `json:"resolved_at"`
	ParentID   *string `json:"parent_id"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
}

func reviewAssetToResponse(a db.ReviewAsset) ReviewAssetResponse {
	return ReviewAssetResponse{
		ID:           uuidToString(a.ID),
		IssueID:      uuidToString(a.IssueID),
		WorkspaceID:  uuidToString(a.WorkspaceID),
		Name:         a.Name,
		AssetType:    a.AssetType,
		FileURL:      a.FileUrl,
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
	IssueID     string `json:"issue_id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
}

type PresignReviewAssetUploadResponse struct {
	UploadURL string              `json:"upload_url"`
	Asset     ReviewAssetResponse `json:"asset"`
}

func (h *Handler) PresignReviewAssetUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req PresignReviewAssetUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	issueUUID, ok := parseUUIDOrBadRequest(w, req.IssueID, "issue_id")
	if !ok {
		return
	}

	// Verify issue exists
	issue, err := h.Queries.GetIssue(ctx, issueUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return
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

	// Create pending asset
	asset, err := h.Queries.CreateReviewAsset(ctx, db.CreateReviewAssetParams{
		IssueID:     issueUUID,
		WorkspaceID: issue.WorkspaceID,
		Name:        req.Filename,
		AssetType:   assetType,
		FileUrl:     fileKey, // Store key, we can resolve full URL on fetch
		Version:     1,
		UploadedBy:  util.MustParseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create asset record")
		return
	}

	writeJSON(w, http.StatusOK, PresignReviewAssetUploadResponse{
		UploadURL: uploadURL,
		Asset:     reviewAssetToResponse(asset),
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
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

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
		AuthorID:  util.MustParseUUID(userID),
		Content:   req.Content,
		Timestamp: timestamp,
		Shapes:    shapes,
		ParentID:  parentUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create comment")
		return
	}

	writeJSON(w, http.StatusOK, reviewCommentToResponse(comment))
}
