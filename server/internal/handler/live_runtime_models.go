package handler

import (
	"context"

	"github.com/multica-ai/multica/server/internal/service"
)

// LiveRuntimeModelsFromCache adapts the picker catalog cache into the
// save/resolve availability check. A nil cache is a permanent miss and fails
// open — the static Codex table must not be used as a gate.
func LiveRuntimeModelsFromCache(cache ModelCatalogCache) service.LiveRuntimeModels {
	return liveRuntimeModelsAdapter{cache: cache}
}

type liveRuntimeModelsAdapter struct {
	cache ModelCatalogCache
}

func (a liveRuntimeModelsAdapter) LiveModelIDs(ctx context.Context, runtimeID string) (map[string]struct{}, bool) {
	if a.cache == nil || runtimeID == "" {
		return nil, false
	}
	snap, err := a.cache.Get(ctx, runtimeID)
	if err != nil || snap == nil || !snap.Supported || len(snap.Models) == 0 {
		return nil, false
	}
	ids := make(map[string]struct{}, len(snap.Models))
	for _, m := range snap.Models {
		if m.ID == "" {
			continue
		}
		ids[m.ID] = struct{}{}
	}
	if len(ids) == 0 {
		return nil, false
	}
	return ids, true
}

func (h *Handler) liveRuntimeModels() service.LiveRuntimeModels {
	if h == nil {
		return nil
	}
	if h.TaskService != nil && h.TaskService.LiveRuntimeModels != nil {
		return h.TaskService.LiveRuntimeModels
	}
	return LiveRuntimeModelsFromCache(h.ModelCatalogCache)
}
