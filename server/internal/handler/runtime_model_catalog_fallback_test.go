package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// reportModelList drives the real ReportModelListResult endpoint as a daemon
// would, including chi URL params and daemon auth, and returns the response
// recorder. Going through the handler (rather than decoding into a struct the
// test declares itself) is what makes these tests catch a wrong JSON tag or a
// mis-wired cache branch in production code.
func reportModelList(t *testing.T, requestID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	req := newDaemonTokenRequest(
		http.MethodPost,
		"/api/daemon/runtimes/"+testRuntimeID+"/models/"+requestID+"/result",
		body, testWorkspaceID, "daemon-mul5549",
	)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("runtimeId", testRuntimeID)
	rctx.URLParams.Add("requestId", requestID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	testHandler.ReportModelListResult(w, req)
	return w
}

// withModelListStores swaps in fresh model-list stores for one test and
// restores the originals afterwards, so cases cannot leak state into each other
// or into the rest of the suite.
func withModelListStores(t *testing.T) ModelCatalogCache {
	t.Helper()
	origStore, origCache := testHandler.ModelListStore, testHandler.ModelCatalogCache
	cache := NewInMemoryModelCatalogCache()
	testHandler.ModelListStore = NewInMemoryModelListStore()
	testHandler.ModelCatalogCache = cache
	t.Cleanup(func() {
		testHandler.ModelListStore, testHandler.ModelCatalogCache = origStore, origCache
	})
	return cache
}

// TestReportModelListResult_FallbackDoesNotPoisonCache is the MUL-5549
// regression, driven end to end through the handler.
//
// A runtime discovers its real catalog, then hits a transient discovery failure
// and the daemon reports the static stand-in. Before the fix that stand-in was
// indistinguishable from a real catalog: non-empty and `supported`, so it was
// stored as last-known-good and served for the full 24h window — pinning one
// blip as the answer for a day. It must now leave the cached catalog untouched.
func TestReportModelListResult_FallbackDoesNotPoisonCache(t *testing.T) {
	ctx := context.Background()
	cache := withModelListStores(t)

	// 1. A real discovery lands and is cached.
	real, err := testHandler.ModelListStore.Create(ctx, testRuntimeID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if w := reportModelList(t, real.ID, map[string]any{
		"status":    "completed",
		"supported": true,
		"models": []map[string]any{
			{"id": "hy3", "label": "Hy3", "default": true},
			{"id": "glm-5.2", "label": "GLM 5.2"},
		},
	}); w.Code != http.StatusOK {
		t.Fatalf("report real catalog: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	snapshot, err := cache.Get(ctx, testRuntimeID)
	if err != nil || snapshot == nil {
		t.Fatalf("expected the real catalog to be cached: snapshot=%v err=%v", snapshot, err)
	}
	if len(snapshot.Models) != 2 || snapshot.Models[0].ID != "hy3" {
		t.Fatalf("unexpected cached catalog: %+v", snapshot.Models)
	}

	// 2. Discovery fails; the daemon reports the static stand-in.
	fallback, err := testHandler.ModelListStore.Create(ctx, testRuntimeID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if w := reportModelList(t, fallback.ID, map[string]any{
		"status":    "completed",
		"supported": true,
		"fallback":  true,
		"models": []map[string]any{
			{"id": "claude-sonnet-4.6", "label": "Claude Sonnet 4.6", "default": true},
			{"id": "gpt-5.5", "label": "GPT 5.5"},
		},
	}); w.Code != http.StatusOK {
		t.Fatalf("report fallback: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// The stand-in is still returned to whoever asked for THIS request...
	stored, err := testHandler.ModelListStore.Get(ctx, fallback.ID)
	if err != nil || stored == nil {
		t.Fatalf("expected the fallback report to be stored: %v %v", stored, err)
	}
	if len(stored.Models) != 2 {
		t.Errorf("the fallback list must still reach the picker, got %+v", stored.Models)
	}

	// ...but it must not have replaced or evicted the real catalog.
	snapshot, err = cache.Get(ctx, testRuntimeID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if snapshot == nil {
		t.Fatal("a fallback report must not evict the last known good catalog")
	}
	if len(snapshot.Models) != 2 || snapshot.Models[0].ID != "hy3" {
		t.Errorf("cached catalog was overwritten by the stand-in: %+v", snapshot.Models)
	}
}

// TestReportModelListResult_OmittedFallbackKeepsOldDaemonBehaviour pins
// backward compatibility: a daemon that predates the `fallback` field omits it,
// which must decode to false so its reports still warm the cache exactly as
// they did before. Reported through the real handler so a renamed JSON tag
// would surface here.
func TestReportModelListResult_OmittedFallbackKeepsOldDaemonBehaviour(t *testing.T) {
	ctx := context.Background()
	cache := withModelListStores(t)

	req, err := testHandler.ModelListStore.Create(ctx, testRuntimeID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if w := reportModelList(t, req.ID, map[string]any{
		"status":    "completed",
		"supported": true,
		"models":    []map[string]any{{"id": "kimi-k3-1", "label": "Kimi K3 1"}},
	}); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	snapshot, err := cache.Get(ctx, testRuntimeID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if snapshot == nil || len(snapshot.Models) != 1 || snapshot.Models[0].ID != "kimi-k3-1" {
		t.Fatalf("an older daemon's report must still be cached: %+v", snapshot)
	}
}

// TestReportModelListResult_EmptyCatalogStillDropsSnapshot guards the case the
// fallback flag must NOT swallow: an authoritative empty result really does say
// the runtime no longer advertises what we cached, so the snapshot goes.
func TestReportModelListResult_EmptyCatalogStillDropsSnapshot(t *testing.T) {
	ctx := context.Background()
	cache := withModelListStores(t)

	seeded, err := testHandler.ModelListStore.Create(ctx, testRuntimeID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if w := reportModelList(t, seeded.ID, map[string]any{
		"status":    "completed",
		"supported": true,
		"models":    []map[string]any{{"id": "hy3", "label": "Hy3"}},
	}); w.Code != http.StatusOK {
		t.Fatalf("seed: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	empty, err := testHandler.ModelListStore.Create(ctx, testRuntimeID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if w := reportModelList(t, empty.ID, map[string]any{
		"status":    "completed",
		"supported": true,
		"models":    []map[string]any{},
	}); w.Code != http.StatusOK {
		t.Fatalf("empty report: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	snapshot, err := cache.Get(ctx, testRuntimeID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if snapshot != nil {
		t.Errorf("an authoritative empty catalog must drop the snapshot, still have %+v", snapshot.Models)
	}
}

// TestModelCatalogCacheDecision pins the three-way branch ReportModelListResult
// takes on a completed report.
//
// The fallback row is the MUL-5549 regression. A static stand-in is non-empty
// and `supported`, so it used to be indistinguishable from a real catalog and
// got stored as last-known-good for the full 24h serve window. It must now be
// Keep: not stored, but also not allowed to evict a real catalog we already
// hold — a stand-in is no evidence about what the runtime supports.
func TestModelCatalogCacheDecision(t *testing.T) {
	for _, tc := range []struct {
		name      string
		models    []ModelEntry
		supported bool
		fallback  bool
		want      modelCatalogCacheAction
	}{
		{
			name:      "real non-empty catalog is stored",
			models:    sampleCatalog(),
			supported: true,
			want:      modelCatalogCacheStore,
		},
		{
			name:      "fallback catalog leaves the cache alone",
			models:    sampleCatalog(),
			supported: true,
			fallback:  true,
			want:      modelCatalogCacheKeep,
		},
		{
			name:      "authoritatively empty catalog drops the snapshot",
			models:    nil,
			supported: true,
			want:      modelCatalogCacheDrop,
		},
		{
			name:      "runtime that ignores model selection drops the snapshot",
			models:    sampleCatalog(),
			supported: false,
			want:      modelCatalogCacheDrop,
		},
		{
			name:      "an empty fallback still only keeps",
			models:    nil,
			supported: true,
			fallback:  true,
			want:      modelCatalogCacheKeep,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := modelCatalogCacheDecision(tc.models, tc.supported, tc.fallback)
			if got != tc.want {
				t.Errorf("modelCatalogCacheDecision = %v, want %v", got, tc.want)
			}
		})
	}
}
