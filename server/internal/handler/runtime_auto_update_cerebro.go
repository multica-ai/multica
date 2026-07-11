package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/forkdist"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (h *Handler) GetLatestRuntimeVersion(w http.ResponseWriter, r *http.Request) {
	version, err := forkdist.LatestVersion(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "latest runtime version unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": version})
}

func shouldScheduleRuntimeUpdate(rt db.AgentRuntime, latest string) bool {
	if !rt.CliVersion.Valid || !forkdist.IsNewerVersion(latest, rt.CliVersion.String) {
		return false
	}
	var metadata struct {
		LaunchedBy string `json:"launched_by"`
	}
	_ = json.Unmarshal(rt.Metadata, &metadata)
	return !strings.EqualFold(strings.TrimSpace(metadata.LaunchedBy), "desktop")
}

func (h *Handler) maybeScheduleRuntimeUpdate(ctx context.Context, rt db.AgentRuntime) {
	latest, err := forkdist.LatestVersion(ctx)
	if err != nil || !shouldScheduleRuntimeUpdate(rt, latest) {
		return
	}
	runtimeID := uuidToString(rt.ID)
	hasPending, err := h.UpdateStore.HasPending(ctx, runtimeID)
	if err != nil || hasPending {
		return
	}
	if _, err := h.UpdateStore.Create(ctx, runtimeID, latest); err != nil {
		slog.Debug("runtime auto-update scheduling skipped", "runtime_id", runtimeID, "error", err)
	}
}
