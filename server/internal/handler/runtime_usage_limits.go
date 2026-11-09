package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	maxUsageLimitsBodyBytes = 16 << 10
	maxUsageLimitsWindows   = 16
)

// ReportRuntimeUsageLimits stores the plan-limit snapshot a daemon read for
// the provider login behind a runtime, and tells clients to refetch runtimes.
func (h *Handler) ReportRuntimeUsageLimits(w http.ResponseWriter, r *http.Request) {
	rt, ok := h.requireDaemonRuntimeAccess(w, r, chi.URLParam(r, "runtimeId"))
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUsageLimitsBodyBytes)
	var snapshot agent.RateLimitSnapshot
	if err := json.NewDecoder(r.Body).Decode(&snapshot); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if snapshot.Provider != rt.Provider {
		writeError(w, http.StatusBadRequest, "provider does not match the runtime")
		return
	}
	if len(snapshot.Windows) > maxUsageLimitsWindows {
		writeError(w, http.StatusBadRequest, "too many usage windows")
		return
	}
	if snapshot.Windows == nil {
		snapshot.Windows = []agent.RateLimitWindow{}
	}
	if snapshot.CheckedAt.IsZero() {
		snapshot.CheckedAt = time.Now().UTC()
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode usage limits")
		return
	}
	if err := h.Queries.SetAgentRuntimeUsageLimits(r.Context(), db.SetAgentRuntimeUsageLimitsParams{
		ID:          rt.ID,
		UsageLimits: encoded,
	}); err != nil {
		slog.Warn("store runtime usage limits failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to store usage limits")
		return
	}
	h.publish(protocol.EventDaemonRegister, uuidToString(rt.WorkspaceID), "system", "", map[string]any{
		"action":     "usage_limits",
		"runtime_id": uuidToString(rt.ID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// RefreshRuntimeUsageLimits asks the runtime's daemon to check plan limits
// now. Delivery is best-effort; the daemon's own timer stays the fallback.
func (h *Handler) RefreshRuntimeUsageLimits(w http.ResponseWriter, r *http.Request) {
	rt, _, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeAPI, chi.URLParam(r, "runtimeId"))
	if !ok {
		return
	}
	if rt.Status != "online" {
		writeError(w, http.StatusServiceUnavailable, "runtime is offline")
		return
	}
	h.requestDaemonPendingWork(uuidToString(rt.ID), protocol.PendingWorkKindUsageLimits)
	w.WriteHeader(http.StatusAccepted)
}
