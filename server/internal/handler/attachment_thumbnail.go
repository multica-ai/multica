package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/thumbnail"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"golang.org/x/sync/singleflight"
)

const (
	// Below both bounds the original is already cheap for a tile to load.
	thumbnailMinSourceBytes  = 512 << 10
	thumbnailMinSourcePixels = 4_000_000
	thumbnailMaxSourceBytes  = 64 << 20
	thumbnailMaxStoredBytes  = 8 << 20

	thumbnailGenerateTimeout = 2 * time.Minute
	// The checks after a thumbnail write each get their own budget, so a
	// render that used up its timeout still confirms and cleans up.
	thumbnailConfirmTimeout = 10 * time.Second
	thumbnailCleanupTimeout = 30 * time.Second
	// The bytes behind an attachment id never change, so a browser may reuse a
	// thumbnail for as long as a signed original URL stays valid.
	thumbnailCacheControl = "private, max-age=1800"
)

var (
	// Decoding a 16 MP original holds ~110 MB, so generations are capped.
	thumbnailSlots  = make(chan struct{}, 2)
	thumbnailFlight singleflight.Group
)

var errThumbnailUnavailable = errors.New("thumbnail not available")

// hasThumbnail reports whether a gets a thumbnail: a large raster image, so
// clients keep loading small images and every other file type as before.
func hasThumbnail(store storage.Storage, a db.Attachment) bool {
	if store == nil || !thumbnail.SupportedContentType(a.ContentType) || a.SizeBytes > thumbnailMaxSourceBytes {
		return false
	}
	if _, ok := thumbnail.StorageKey(store.KeyFromURL(a.Url)); !ok {
		return false
	}
	return a.SizeBytes >= thumbnailMinSourceBytes ||
		(a.Width.Valid && a.Height.Valid && int64(a.Width.Int32)*int64(a.Height.Int32) >= thumbnailMinSourcePixels)
}

func (h *Handler) attachmentThumbnailURL(a db.Attachment, id string) string {
	if !hasThumbnail(h.Storage, a) {
		return ""
	}
	return "/api/attachments/" + id + "/thumbnail"
}

// GetAttachmentThumbnail — GET /api/attachments/{id}/thumbnail
//
// Serves a downscaled copy of a large image attachment for grid tiles. Upload
// renders it in the background; a request that finds none (older uploads, a
// render still running or one that failed) renders it then. It is stored next
// to the original. Access mirrors /api/attachments/{id}/download, so it loads
// as a native <img> src. Clients fall back to the original on any error.
func (h *Handler) GetAttachmentThumbnail(w http.ResponseWriter, r *http.Request) {
	att, ok := h.loadAttachmentForDownload(w, r)
	if !ok {
		return
	}
	store := h.Storage
	if store == nil {
		writeFeatureDisabled(w, "storage_not_configured", "storage not configured")
		return
	}
	if !hasThumbnail(store, att) {
		writeError(w, http.StatusNotFound, "thumbnail not available")
		return
	}

	thumb, err := h.loadAttachmentThumbnail(r.Context(), store, att)
	if err != nil {
		if writeRequestCancellation(w, r, err) {
			return
		}
		if errors.Is(err, errThumbnailUnavailable) {
			writeError(w, http.StatusNotFound, "thumbnail not available")
			return
		}
		slog.Warn("failed to build attachment thumbnail", "id", uuidToString(att.ID), "error", err)
		writeError(w, http.StatusBadGateway, "failed to build thumbnail")
		return
	}

	w.Header().Set("Content-Type", thumb.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(thumb.Data)))
	w.Header().Set("Cache-Control", thumbnailCacheControl)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	h.setAttachmentPreviewSecurityHeaders(w)
	if _, err := w.Write(thumb.Data); err != nil && !isRequestCancellation(r, err) {
		slog.Warn("failed to write attachment thumbnail", "id", uuidToString(att.ID), "error", err)
	}
}

// GetAttachmentOriginal — GET /api/attachments/{id}/original
//
// Serves the original bytes of an image that has a thumbnail, always through
// the API. A tile shows the thumbnail, so copying the image from it reads the
// original from script, and web script can't read the storage CDN, which
// sends no CORS headers. /download redirects there in CDN and presign modes.
func (h *Handler) GetAttachmentOriginal(w http.ResponseWriter, r *http.Request) {
	att, ok := h.loadAttachmentForDownload(w, r)
	if !ok {
		return
	}
	store := h.Storage
	if store == nil {
		writeFeatureDisabled(w, "storage_not_configured", "storage not configured")
		return
	}
	if !hasThumbnail(store, att) {
		writeError(w, http.StatusNotFound, "original not available")
		return
	}
	h.proxyAttachmentDownload(w, r, att, store.KeyFromURL(att.Url), false)
}

// prewarmAttachmentThumbnail renders a new upload's thumbnail in the
// background, so the first grid that shows it does not wait for the render.
func (h *Handler) prewarmAttachmentThumbnail(att db.Attachment) {
	store := h.Storage
	if !hasThumbnail(store, att) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), thumbnailGenerateTimeout)
		defer cancel()
		if _, err := h.loadAttachmentThumbnail(ctx, store, att); err != nil && !errors.Is(err, errThumbnailUnavailable) {
			slog.Warn("failed to prewarm attachment thumbnail", "id", uuidToString(att.ID), "error", err)
		}
	}()
}

func (h *Handler) loadAttachmentThumbnail(ctx context.Context, store storage.Storage, att db.Attachment) (thumbnail.Thumbnail, error) {
	objectKey := store.KeyFromURL(att.Url)
	key, ok := thumbnail.StorageKey(objectKey)
	if !ok {
		return thumbnail.Thumbnail{}, errThumbnailUnavailable
	}
	if thumb, ok := readStoredThumbnail(ctx, store, key); ok {
		return thumb, nil
	}

	// One generation per object, shared by every concurrent request for it and
	// detached from any single request so one cancelled tile does not fail the
	// others waiting on the same result.
	ch := thumbnailFlight.DoChan(key, func() (any, error) {
		genCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), thumbnailGenerateTimeout)
		defer cancel()
		return generateAttachmentThumbnail(genCtx, store, objectKey, key, func(ctx context.Context) (bool, error) {
			return h.attachmentExists(ctx, att)
		})
	})
	select {
	case res := <-ch:
		if res.Err != nil {
			return thumbnail.Thumbnail{}, res.Err
		}
		return res.Val.(thumbnail.Thumbnail), nil
	case <-ctx.Done():
		return thumbnail.Thumbnail{}, ctx.Err()
	}
}

func readStoredThumbnail(ctx context.Context, store storage.Storage, key string) (thumbnail.Thumbnail, bool) {
	reader, err := store.GetReader(ctx, key)
	if err != nil {
		return thumbnail.Thumbnail{}, false
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, thumbnailMaxStoredBytes+1))
	if err != nil || len(data) == 0 || len(data) > thumbnailMaxStoredBytes {
		return thumbnail.Thumbnail{}, false
	}
	switch contentType := http.DetectContentType(data); contentType {
	case "image/jpeg", "image/png":
		return thumbnail.Thumbnail{Data: data, ContentType: contentType}, true
	}
	return thumbnail.Thumbnail{}, false
}

// attachmentExists reports whether att's row is still in the database. It
// reads through h.Queries, the primary, so a delete that has committed is seen.
func (h *Handler) attachmentExists(ctx context.Context, att db.Attachment) (bool, error) {
	_, err := h.Queries.GetAttachment(ctx, db.GetAttachmentParams{ID: att.ID, WorkspaceID: att.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// generateAttachmentThumbnail renders and stores the thumbnail of objectKey
// under key. exists is checked after the write attempt: a delete of the
// attachment may have removed the original and its thumbnail while this
// render was running, and the write would then leave a thumbnail nothing
// references. Deletes remove the row before the objects, so for a write that
// has landed by the time it returns, either this check sees the row gone and
// removes the thumbnail, or the delete's object cleanup runs after the write
// and removes it. A write that returned an error may still have landed, so it
// is checked the same way. The check and the removal each run once, best
// effort under their own timeouts: a write that lands only after both
// cleanups, or a removal that fails, still leaves the object behind.
func generateAttachmentThumbnail(ctx context.Context, store storage.Storage, objectKey, key string, exists func(context.Context) (bool, error)) (thumbnail.Thumbnail, error) {
	select {
	case thumbnailSlots <- struct{}{}:
		defer func() { <-thumbnailSlots }()
	case <-ctx.Done():
		return thumbnail.Thumbnail{}, ctx.Err()
	}
	// A render this caller missed, such as the upload prewarm, may have stored
	// it while this one waited for a slot.
	if thumb, ok := readStoredThumbnail(ctx, store, key); ok {
		return thumb, nil
	}

	reader, err := store.GetReader(ctx, objectKey)
	if err != nil {
		return thumbnail.Thumbnail{}, fmt.Errorf("open original: %w", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, thumbnailMaxSourceBytes+1))
	if err != nil {
		return thumbnail.Thumbnail{}, fmt.Errorf("read original: %w", err)
	}
	if len(data) > thumbnailMaxSourceBytes {
		return thumbnail.Thumbnail{}, errThumbnailUnavailable
	}

	thumb, err := thumbnail.Generate(data)
	if errors.Is(err, thumbnail.ErrUnsupported) {
		return thumbnail.Thumbnail{}, fmt.Errorf("%w: %v", errThumbnailUnavailable, err)
	}
	if err != nil {
		return thumbnail.Thumbnail{}, err
	}
	filename := "thumbnail.jpg"
	if thumb.ContentType == "image/png" {
		filename = "thumbnail.png"
	}
	if _, err := store.Upload(ctx, key, thumb.Data, thumb.ContentType, filename); err != nil {
		// Still serve it; the next request renders it again.
		slog.Warn("failed to store attachment thumbnail", "key", key, "error", err)
	}
	ok, err := confirmThumbnailOwner(ctx, exists)
	if ok {
		return thumb, nil
	}
	// Gone, or unknown: remove the stored copy either way. A live attachment
	// renders it again on its next request.
	removeThumbnail(ctx, store, key)
	if err != nil {
		slog.Warn("failed to confirm attachment for thumbnail", "key", key, "error", err)
		return thumb, nil
	}
	return thumbnail.Thumbnail{}, errThumbnailUnavailable
}

func confirmThumbnailOwner(ctx context.Context, exists func(context.Context) (bool, error)) (bool, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), thumbnailConfirmTimeout)
	defer cancel()
	return exists(ctx)
}

func removeThumbnail(ctx context.Context, store storage.Storage, key string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), thumbnailCleanupTimeout)
	defer cancel()
	if err := store.DeleteObject(ctx, key); err != nil {
		slog.Warn("failed to remove attachment thumbnail", "key", key, "error", err)
	}
}
