package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/forkdist"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// autoUpdateCooldown bounds how often the server may auto-schedule a CLI update
// for one runtime. There used to be no bound at all: maybeScheduleRuntimeUpdate
// asked only "is one already queued", so a runtime whose reported version never
// reaches the release line got a fresh order on EVERY heartbeat — 15s apart,
// per runtime. On the Sliplane runtime container (16 runtimes behind one
// daemon) that produced 22k deferred updates and 945 rejected GitHub calls in a
// single day: the release lookup is unauthenticated and GitHub caps those at 60
// per hour per IP, so the loop burned its own quota, every attempt then failed
// with 403, the reported version stayed put, and the failure scheduled the next
// attempt (FIR-4369).
//
// An hour is deliberately coarse. An update needs an idle runtime anyway, so a
// busy one retrying in an hour rather than 15 seconds loses nothing, and a
// runtime that cannot update at all now costs one request per hour instead of
// 240.
const autoUpdateCooldown = time.Hour

// autoUpdateGate holds the next time each runtime may have an update
// auto-scheduled. It is process-local rather than part of UpdateStore because
// the cooldown guards only this cerebro scheduler, and UpdateStore is upstream.
// With several API replicas each keeps its own gate, so the worst case is one
// order per replica per hour — still bounded, still far under the GitHub quota.
var autoUpdateGate = struct {
	sync.Mutex
	nextAllowed map[string]time.Time
}{nextAllowed: make(map[string]time.Time)}

// reserveAutoUpdateSlot reports whether an update may be auto-scheduled for
// this runtime now, claiming the next cooldown window when it may. Reserving at
// schedule time rather than on failure covers every way an update can fail to
// land: the daemon defers it, the download is rejected, or the daemon dies and
// never reports at all.
func reserveAutoUpdateSlot(runtimeID string, now time.Time) bool {
	autoUpdateGate.Lock()
	defer autoUpdateGate.Unlock()
	if next, ok := autoUpdateGate.nextAllowed[runtimeID]; ok && now.Before(next) {
		return false
	}
	autoUpdateGate.nextAllowed[runtimeID] = now.Add(autoUpdateCooldown)
	return true
}

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
	if !reserveAutoUpdateSlot(runtimeID, time.Now()) {
		return
	}
	if _, err := h.UpdateStore.Create(ctx, runtimeID, latest); err != nil {
		slog.Debug("runtime auto-update scheduling skipped", "runtime_id", runtimeID, "error", err)
	}
}
