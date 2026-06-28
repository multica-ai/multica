package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (h *Handler) DaemonWebSocket(w http.ResponseWriter, r *http.Request) {
	if h.DaemonHub == nil {
		writeError(w, http.StatusServiceUnavailable, "daemon websocket unavailable")
		return
	}

	runtimeIDs := parseRuntimeIDs(r)
	if len(runtimeIDs) == 0 {
		writeError(w, http.StatusBadRequest, "runtime_ids required")
		return
	}

	workspaceIDs := make([]string, 0, len(runtimeIDs))
	seenWorkspaceIDs := make(map[string]struct{}, len(runtimeIDs))
	for _, runtimeID := range runtimeIDs {
		rt, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
		if !ok {
			return
		}
		if daemonID := middleware.DaemonIDFromContext(r.Context()); daemonID != "" && rt.DaemonID.Valid && rt.DaemonID.String != daemonID {
			writeError(w, http.StatusNotFound, "runtime not found")
			return
		}
		workspaceID := uuidToString(rt.WorkspaceID)
		if workspaceID != "" {
			if _, ok := seenWorkspaceIDs[workspaceID]; !ok {
				seenWorkspaceIDs[workspaceID] = struct{}{}
				workspaceIDs = append(workspaceIDs, workspaceID)
			}
		}
	}

	primaryWorkspaceID := ""
	if len(workspaceIDs) > 0 {
		primaryWorkspaceID = workspaceIDs[0]
	}

	h.DaemonHub.HandleWebSocket(w, r, daemonws.ClientIdentity{
		DaemonID:      middleware.DaemonIDFromContext(r.Context()),
		UserID:        requestUserID(r),
		WorkspaceID:   primaryWorkspaceID,
		WorkspaceIDs:  workspaceIDs,
		RuntimeIDs:    runtimeIDs,
		ClientVersion: r.Header.Get("X-Client-Version"),
	})

	// Scenario-C fix: after WS registration, query for tasks that were
	// cancelled while the daemon was disconnected and proactively push
	// daemon:task_cancelled frames. The daemon can then abort those tasks
	// immediately instead of waiting for watchTaskCancellation (up to 5 s).
	go h.pushCancelledTasksOnReconnect(r.Context(), runtimeIDs)
}

// pushCancelledTasksOnReconnect queries for tasks cancelled during the
// disconnection window and pushes daemon:task_cancelled frames so the
// daemon can abort them without waiting for the next poll cycle.
func (h *Handler) pushCancelledTasksOnReconnect(ctx context.Context, runtimeIDs []string) {
	uuids := make([]pgtype.UUID, 0, len(runtimeIDs))
	for _, id := range runtimeIDs {
		uuids = append(uuids, parseUUID(id))
	}

	cancelled, err := h.Queries.ListCancelledTasksForRuntimes(ctx, db.ListCancelledTasksForRuntimesParams{
		RuntimeIds: uuids,
	})
	if err != nil {
		slog.Debug("push-cancelled-on-reconnect: query failed", "error", err)
		return
	}

	for _, task := range cancelled {
		h.DaemonHub.NotifyTaskCancelled(
			uuidToString(task.RuntimeID),
			uuidToString(task.ID),
		)
	}
}

func parseRuntimeIDs(r *http.Request) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(raw string) {
		for _, part := range strings.Split(raw, ",") {
			id := strings.TrimSpace(part)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	for _, raw := range r.URL.Query()["runtime_id"] {
		add(raw)
	}
	for _, raw := range r.URL.Query()["runtime_ids"] {
		add(raw)
	}
	return out
}
