package handler

// CEREBRO-PATCH(trace-config-handler): TECH-3621 fleet rollout — serves the
// agent-trace upload config (endpoint + ingest key) to authenticated fleet
// daemons so trace upload can be turned on for EVERY machine from a single
// server-side setting, instead of pasting the key into each machine's daemon
// env. Net-new fork file in the upstream-zone handler path (cerebro- naming).
//
// Trust model: this returns the ingest key, so it is mounted under the
// /api/daemon/runtimes/{runtimeId}/* group and gated by requireDaemonRuntimeAccess
// — exactly the same auth level as the sibling daemon endpoints that already
// ship secrets (e.g. claim-task returns per-task mat_ tokens). The key is read
// from the server's own environment, so it lives in one place (the backend's
// secret store), never on a laptop. The key is only ever included in the
// response when the feature is fully configured (endpoint AND key set).
//
// Turn the feature on for the whole fleet by setting MULTICA_TRACE_UPLOAD_ENDPOINT
// + MULTICA_TRACE_UPLOAD_API_KEY on the backend. With either unset, the endpoint
// reports enabled=false and returns no secret, and no daemon uploads.

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/cerebro/traceupload"
)

// traceConfigResponse is the JSON contract returned to daemons. The daemon's
// matching decoder is daemon.TraceConfigResponse — keep the json tags in sync.
type traceConfigResponse struct {
	Enabled     bool   `json:"enabled"`
	Endpoint    string `json:"endpoint"`
	APIKey      string `json:"api_key"`
	MaxAgeHours int    `json:"max_age_hours"`
}

// traceConfigFromEnv reads the backend's own trace-upload environment. The key
// and endpoint are only populated when BOTH are set (enabled) — a half-config
// never leaks the key. Pure (no request/DB) so it is unit-testable.
func traceConfigFromEnv() traceConfigResponse {
	endpoint := strings.TrimSpace(os.Getenv(traceupload.EnvEndpoint))
	apiKey := strings.TrimSpace(os.Getenv(traceupload.EnvAPIKey))
	if endpoint == "" || apiKey == "" {
		return traceConfigResponse{Enabled: false}
	}
	resp := traceConfigResponse{Enabled: true, Endpoint: endpoint, APIKey: apiKey}
	if v := strings.TrimSpace(os.Getenv(traceupload.EnvMaxAgeHours)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			resp.MaxAgeHours = n
		}
	}
	return resp
}

// GetTraceConfig handles GET /api/daemon/runtimes/{runtimeId}/trace-config. The
// runtime guard puts it at the same trust level as the other daemon runtime
// endpoints; on success it reflects the backend's trace-upload config to the
// daemon. enabled is true only when both endpoint and key are set server-side.
func (h *Handler) GetTraceConfig(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	if _, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID); !ok {
		return
	}
	writeJSON(w, http.StatusOK, traceConfigFromEnv())
}
