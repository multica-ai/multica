package apps

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

func (h *Handler) Invoke(w http.ResponseWriter, r *http.Request) {
	app, ok := h.loadApp(w, r)
	if !ok {
		return
	}
	memberID, ok := requestUserID(w, r)
	if !ok {
		return
	}
	if app.Status != "published" || app.CurrentVersion == nil {
		writeError(w, http.StatusConflict, "app has no ready version")
		return
	}
	input, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 512<<10))
	if err != nil || !json.Valid(input) {
		writeError(w, http.StatusBadRequest, "input must be valid JSON")
		return
	}
	if h.runtime == nil {
		writeError(w, http.StatusBadGateway, "app runtime is unavailable")
		return
	}
	grantToken := mintInvocationGrant(h.runtimeServiceKey, invocationGrant{
		AppID: app.ID, Version: *app.CurrentVersion, WorkspaceID: app.WorkspaceID, MemberID: memberID.String(),
	}, time.Now().UTC().Add(2*time.Minute))
	envelope, err := json.Marshal(map[string]any{"input": json.RawMessage(input), "grant_token": grantToken})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to prepare app invocation")
		return
	}
	output, err := h.runtime.Invoke(r.Context(), app.ID, *app.CurrentVersion, json.RawMessage(envelope))
	if err != nil {
		writeError(w, http.StatusBadGateway, "app worker failed")
		return
	}
	_, _ = h.pool.Exec(r.Context(), `INSERT INTO cerebro_app_audit_log (workspace_id,app_id,actor_type,actor_id,action,metadata) VALUES ($1,$2,'user',$3,'app.worker.invoked',jsonb_build_object('version',$4))`, app.WorkspaceID, app.ID, memberID.String(), *app.CurrentVersion)
	writeJSON(w, http.StatusOK, output)
}
