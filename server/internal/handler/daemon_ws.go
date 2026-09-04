package handler

import (
	"log/slog"
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

	identity, ok := h.buildDaemonWebSocketIdentity(w, r, runtimeIDs, userID)
	if !ok {
		return
	}
	h.DaemonHub.HandleWebSocket(w, r, identity)
}

// buildDaemonWebSocketIdentity authenticates the connection's entire runtime
// set with one narrow query and seeds the connection-scoped heartbeat leases.
// Runtime ownership is immutable, so the heartbeat hot path can safely use
// this fixed scope without re-reading agent_runtime every 15 seconds.
func (h *Handler) buildDaemonWebSocketIdentity(w http.ResponseWriter, r *http.Request, runtimeIDs []string, userID string) (daemonws.ClientIdentity, bool) {
	identity := daemonws.ClientIdentity{
		DaemonID:      middleware.DaemonIDFromContext(r.Context()),
		UserID:        userID,
		RuntimeIDs:    runtimeIDs,
		RuntimeLeases: make(map[string]*daemonws.RuntimeLease, len(runtimeIDs)),
		ClientVersion: r.Header.Get("X-Client-Version"),
		Capabilities:  r.Header.Get("X-Client-Capabilities"),
	}
	if len(runtimeIDs) == 0 {
		return identity, true
	}

	runtimeUUIDs, ok := parseUUIDSliceOrBadRequest(w, runtimeIDs, "runtime_ids")
	if !ok {
		return daemonws.ClientIdentity{}, false
	}
	rows, err := h.Queries.GetAgentRuntimeHeartbeatLeases(r.Context(), runtimeUUIDs)
	if err != nil {
		slog.Warn("load daemon websocket runtime leases failed", "runtimes", len(runtimeIDs), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load runtimes")
		return daemonws.ClientIdentity{}, false
	}
	byID := make(map[string]int, len(rows))
	for index, row := range rows {
		byID[uuidToString(row.ID)] = index
	}

	workspaceIDs := make([]string, 0, len(runtimeIDs))
	seenWorkspaceIDs := make(map[string]struct{}, len(runtimeIDs))
	for runtimeIndex, runtimeID := range runtimeIDs {
		index, found := byID[uuidToString(runtimeUUIDs[runtimeIndex])]
		if !found {
			writeError(w, http.StatusNotFound, "runtime not found")
			return daemonws.ClientIdentity{}, false
		}
		rt := rows[index]
		if identity.DaemonID != "" && rt.DaemonID.Valid && rt.DaemonID.String != identity.DaemonID {
			writeError(w, http.StatusNotFound, "runtime not found")
			return daemonws.ClientIdentity{}, false
		}
		workspaceID := uuidToString(rt.WorkspaceID)
		if !h.requireDaemonWorkspaceAccess(w, r, workspaceID) {
			return daemonws.ClientIdentity{}, false
		}
		if workspaceID != "" {
			if _, ok := seenWorkspaceIDs[workspaceID]; !ok {
				seenWorkspaceIDs[workspaceID] = struct{}{}
				workspaceIDs = append(workspaceIDs, workspaceID)
			}
		}
		identity.RuntimeLeases[runtimeID] = daemonws.NewRuntimeLease(
			workspaceID,
			rt.Status,
			rt.LastSeenAt.Time,
			rt.LastSeenAt.Valid,
		)
	}

	primaryWorkspaceID := ""
	if len(workspaceIDs) > 0 {
		primaryWorkspaceID = workspaceIDs[0]
	}
	identity.WorkspaceID = primaryWorkspaceID
	identity.WorkspaceIDs = workspaceIDs
	return identity, true
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
