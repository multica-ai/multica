package evals

import (
	"net/http"
)

// run_now.go adds the default-OFF "Run now" endpoint (FIR-3496): it executes an
// eval for real through the merged runner engine instead of trusting a pass/fail
// result posted from outside. The handler depends only on the RunExecutor
// interface; the concrete engine-backed executor lives in the runner package
// (which already imports this one) and is wired in via WithRunExecutor only when
// the model gateway is configured. With no executor the endpoint is disabled, so
// the existing POST /{id}/runs live path is untouched until the flag is turned
// on — and turning it off again is a one-line revert.

// RunNow executes the eval for real and stores the resulting run. When no
// executor is wired (flag OFF) the endpoint reports 404 so the feature stays
// invisible until it is deliberately enabled. A setup error from the executor
// (no tasks, no usable grader, a target kind not yet wired) is a 422: the eval
// could not really run, so nothing is recorded as a pass.
func (h *Handler) RunNow(w http.ResponseWriter, r *http.Request) {
	if !h.runNowEnabled {
		writeError(w, http.StatusNotFound, "run-now is not enabled")
		return
	}
	actorID, workspaceID, actorType, ok := requestContext(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	run, err := h.store.CreateRun(r.Context(), workspaceID, actorID, id, actorType, EvalRunInput{})
	if err != nil {
		h.storeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, run)
}
