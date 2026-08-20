package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const clientUsageBodyLimit = 16 * 1024

var clientVersionPattern = regexp.MustCompile(`^[\x20-\x7e]{1,64}$`)

type clientUsageRequest struct {
	InstallID string `json:"install_id"`
}

// UpsertClientUsage records at most one row per user, client installation, and
// UTC day. Repeated reports refresh the same row; an activity-only report never
// clears a runtime snapshot already collected earlier that day.
func (h *Handler) UpsertClientUsage(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, clientUsageBodyLimit))
	decoder.DisallowUnknownFields()
	var req clientUsageRequest
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	installID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.InstallID), "install_id")
	if !ok {
		return
	}

	clientType, clientVersion, clientOS := middleware.ClientMetadataFromContext(r.Context())
	clientType = strings.ToLower(strings.TrimSpace(clientType))
	if clientType != "web" {
		writeError(w, http.StatusBadRequest, "client platform must be web")
		return
	}
	clientVersion = strings.TrimSpace(clientVersion)
	if clientVersion == "" {
		clientVersion = "unknown"
	}
	if !clientVersionPattern.MatchString(clientVersion) {
		writeError(w, http.StatusBadRequest, "invalid client version")
		return
	}
	clientOS = normalizeClientUsageOS(clientOS)

	workspaceUUID := pgtype.UUID{}
	queries := h.Queries
	var tx pgx.Tx
	if workspaceID := h.resolveWorkspaceID(r); workspaceID != "" {
		workspaceUUID, ok = parseUUIDOrBadRequest(w, workspaceID, "workspace id")
		if !ok {
			return
		}
		var err error
		tx, err = h.TxStarter.Begin(r.Context())
		if err != nil {
			slog.Error("failed to begin client usage transaction", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to record client usage")
			return
		}
		defer tx.Rollback(r.Context())
		queries = h.Queries.WithTx(tx)
		// Share the explicit workspace delete/create lock protocol: if deletion
		// wins, the row is gone and this report fails; if reporting wins, deletion
		// waits and clears the context after the upsert commits.
		if _, err := queries.LockWorkspaceForChatSessionCreate(r.Context(), workspaceUUID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusForbidden, "workspace not found")
				return
			}
			slog.Error("failed to lock client usage workspace", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to record client usage")
			return
		}
		if _, err := queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
			UserID: userUUID, WorkspaceID: workspaceUUID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusForbidden, "workspace not found")
				return
			}
			slog.Error("failed to validate client usage workspace", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to record client usage")
			return
		}
	}

	if _, err := queries.UpsertClientUsageDaily(r.Context(), db.UpsertClientUsageDailyParams{
		UserID:          userUUID,
		ClientType:      clientType,
		InstallID:       installID,
		WorkspaceID:     workspaceUUID,
		ClientVersion:   clientVersion,
		Os:              clientOS,
		HasRuntimeProbe: false,
		ProbeResult:     pgtype.Text{},
		RuntimeCount:    pgtype.Int4{},
		ProviderSummary: nil,
		OnlineCount:     pgtype.Int4{},
		OfflineCount:    pgtype.Int4{},
	}); err != nil {
		slog.Error("failed to upsert client usage", "error", err, "client_type", clientType)
		writeError(w, http.StatusInternalServerError, "failed to record client usage")
		return
	}
	if tx != nil {
		if err := tx.Commit(r.Context()); err != nil {
			slog.Error("failed to commit client usage", "error", err, "client_type", clientType)
			writeError(w, http.StatusInternalServerError, "failed to record client usage")
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func normalizeClientUsageOS(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "macos", "windows", "linux", "ios", "android", "chromeos":
		return value
	default:
		return "unknown"
	}
}
