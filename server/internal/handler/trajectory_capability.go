package handler

import (
	"net/http"

	"github.com/multica-ai/multica/server/internal/featureflags"
)

func (h *Handler) trajectoryEnabled(r *http.Request) bool {
	return featureflags.TrajectoryV1Enabled(r.Context(), h.FeatureFlags, ctxWorkspaceID(r.Context()), requestUserID(r))
}

// GetTrajectoryCapability tells clients whether to show the Trajectory entry
// in the current workspace.
func (h *Handler) GetTrajectoryCapability(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private,no-store")
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": h.trajectoryEnabled(r)})
}

// requireTrajectory answers 404 while the flag is off so clients that already
// opened the view return to the native list.
func (h *Handler) requireTrajectory(w http.ResponseWriter, r *http.Request) bool {
	if h.trajectoryEnabled(r) {
		return true
	}
	writeError(w, http.StatusNotFound, "trajectory is not available in this workspace")
	return false
}
