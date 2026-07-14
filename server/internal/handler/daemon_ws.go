package handler

import (
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/middleware"
)

func (h *Handler) DaemonWebSocket(w http.ResponseWriter, r *http.Request) {
	if h.DaemonHub == nil {
		writeError(w, http.StatusServiceUnavailable, "daemon websocket unavailable")
		return
	}

	runtimeIDs := parseRuntimeIDs(r)
	userID := requestUserID(r)
	if len(runtimeIDs) == 0 && userID == "" {
		writeError(w, http.StatusBadRequest, "runtime_ids or user identity required")
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
		UserID:        userID,
		WorkspaceID:   primaryWorkspaceID,
		WorkspaceIDs:  workspaceIDs,
		RuntimeIDs:    runtimeIDs,
		ClientVersion: r.Header.Get("X-Client-Version"),
		Capabilities:  r.Header.Get("X-Client-Capabilities"),
	})
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
