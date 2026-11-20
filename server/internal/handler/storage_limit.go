package handler

import (
	"errors"
	"net/http"

	"github.com/multica-ai/multica/server/internal/service"
)

// Attachment storage limit (MUL-9489). The limit itself lives in
// service.StorageQuota; this file holds the HTTP side. Two paths add bytes on
// a person's request and are checked before writing anything: UploadFile and
// creating a sub-issue from a comment, which copies the source attachments.
// Files that arrive through a messaging channel are recorded, never refused.

const storageLimitReachedCode = "storage_limit_reached"

func (h *Handler) storageQuota() service.StorageQuota {
	q := service.StorageQuota{
		Sums:         h.Queries,
		Entitlements: h.Entitlements,
		Overages:     service.EntitlementOverages{Queries: h.Queries},
	}
	if h.Metrics != nil {
		q.Metrics = h.Metrics
	}
	return q
}

// writeStorageLimitReached answers a refused upload. It is a 403 like a
// locked paid feature: 413 already means one file over the per-file cap, and
// a 5xx would invite retries and availability alerts. The workspace's figures
// are included only for callers who may read storage usage: a guest, who
// cannot, gets the code and message alone.
func writeStorageLimitReached(w http.ResponseWriter, err error, withFigures bool) bool {
	var reached *service.StorageLimitReachedError
	if !errors.As(err, &reached) {
		return false
	}
	body := map[string]any{
		"code":  storageLimitReachedCode,
		"error": "This workspace has reached its attachment storage limit. Upgrade the plan or delete attachments you no longer need.",
	}
	if withFigures {
		body["used_bytes"] = reached.Used
		body["limit_bytes"] = reached.Limit
	}
	writeJSON(w, http.StatusForbidden, body)
	return true
}

// StorageUsageResponse is the storage limit as clients render it. action is
// off when no limit applies, including self-hosted deployments. While a limit
// applies limit_bytes is present; used_bytes and reached are absent when the
// usage could not be read in time, and reached is present only under
// enforce.
type StorageUsageResponse struct {
	Action     string `json:"action"`
	UsedBytes  *int64 `json:"used_bytes,omitempty"`
	LimitBytes *int64 `json:"limit_bytes,omitempty"`
	Reached    *bool  `json:"reached,omitempty"`
}

// GetStorageUsage — GET /api/workspaces/{id}/storage-usage
//
// Member-readable: every member uploads, and the upload prompt needs it.
func (h *Handler) GetStorageUsage(w http.ResponseWriter, r *http.Request) {
	wsID := workspaceIDFromURL(r, "id")
	if _, ok := h.requireWorkspaceMember(w, r, wsID, "workspace not found"); !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, wsID, "workspace id")
	if !ok {
		return
	}
	usage := h.storageQuota().Usage(r.Context(), workspaceID)
	writeJSON(w, http.StatusOK, StorageUsageResponse{
		Action:     string(usage.Action),
		UsedBytes:  usage.Used,
		LimitBytes: usage.Limit,
		Reached:    usage.Reached,
	})
}
