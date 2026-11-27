package handler

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func sampleCatalog() []ModelEntry {
	return []ModelEntry{
		{ID: "claude-sonnet-4-6", Label: "Claude Sonnet 4.6", Provider: "anthropic", Default: true},
		{ID: "claude-opus-5", Label: "Claude Opus 5", Provider: "anthropic"},
	}
}

// TestInMemoryModelCatalogCache_RoundTrip is the happy path behind the
// stale-while-revalidate fast path (MUL-5444): a completed discovery is
// remembered so the next picker open answers without a daemon round trip.
func TestInMemoryModelCatalogCache_RoundTrip(t *testing.T) {
	ctx := context.Background()
	cache := NewInMemoryModelCatalogCache()

	if got, err := cache.Get(ctx, "rt-1"); err != nil || got != nil {
		t.Fatalf("cold cache should miss: got=%+v err=%v", got, err)
	}
	if err := cache.Put(ctx, "rt-1", sampleCatalog(), nil, true); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := cache.Get(ctx, "rt-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected a cached snapshot")
	}
	if len(got.Models) != 2 || got.Models[0].ID != "claude-sonnet-4-6" {
		t.Fatalf("unexpected models: %+v", got.Models)
	}
	if !got.Models[0].Default {
		t.Error("the default badge must survive the cache round trip")
	}
	if !got.Supported {
		t.Error("supported flag lost")
	}
	if got.Age(time.Now()) < 0 {
		t.Error("snapshot age must not be negative")
	}
	// Other runtimes keep their own entry — a catalog is per machine + per CLI
	// install, never shareable across runtimes.
	if other, err := cache.Get(ctx, "rt-2"); err != nil || other != nil {
		t.Fatalf("unrelated runtime should miss: got=%+v err=%v", other, err)
	}
}

// TestInMemoryModelCatalogCache_ReturnsIndependentCopies stops a caller that
// mutates the response (e.g. filtering the list) from corrupting the shared
// cache for every later reader.
func TestInMemoryModelCatalogCache_ReturnsIndependentCopies(t *testing.T) {
	ctx := context.Background()
	cache := NewInMemoryModelCatalogCache()
	if err := cache.Put(ctx, "rt-1", sampleCatalog(), nil, true); err != nil {
		t.Fatalf("put: %v", err)
	}

	first, err := cache.Get(ctx, "rt-1")
	if err != nil || first == nil {
		t.Fatalf("get: %+v %v", first, err)
	}
	first.Models[0].ID = "mutated"

	second, err := cache.Get(ctx, "rt-1")
	if err != nil || second == nil {
		t.Fatalf("get: %+v %v", second, err)
	}
	if second.Models[0].ID != "claude-sonnet-4-6" {
		t.Fatalf("cache was corrupted by a caller mutation: %+v", second.Models[0])
	}
}

// TestInMemoryModelCatalogCache_IsolatesNestedFields extends the copy guarantee
// to everything a ModelEntry points at. A shallow slice copy would leave the
// *ModelThinking, its level slice, and ServiceTiers aliasing the cached objects,
// which also made this backend behave differently from the Redis one (JSON
// round-trip always yields an independent value).
func TestInMemoryModelCatalogCache_IsolatesNestedFields(t *testing.T) {
	ctx := context.Background()
	cache := NewInMemoryModelCatalogCache()

	source := []ModelEntry{{
		ID:    "gpt-5.6-sol",
		Label: "GPT-5.6-Sol",
		Thinking: &ModelThinking{
			DefaultLevel:    "low",
			SupportedLevels: []ThinkingLevel{{Value: "low", Label: "Low"}},
		},
		ServiceTiers: []ModelServiceTier{{ID: "fast", Name: "Fast"}},
	}}
	if err := cache.Put(ctx, "rt-1", source, nil, true); err != nil {
		t.Fatalf("put: %v", err)
	}

	// Mutating the caller's own slice after Put must not reach the cache.
	source[0].Thinking.DefaultLevel = "mutated-by-writer"
	source[0].Thinking.SupportedLevels[0].Label = "mutated-by-writer"
	source[0].ServiceTiers[0].Name = "mutated-by-writer"

	first, err := cache.Get(ctx, "rt-1")
	if err != nil || first == nil {
		t.Fatalf("get: %+v %v", first, err)
	}
	if first.Models[0].Thinking.DefaultLevel != "low" ||
		first.Models[0].Thinking.SupportedLevels[0].Label != "Low" ||
		first.Models[0].ServiceTiers[0].Name != "Fast" {
		t.Fatalf("writer mutation leaked into the cache: %+v", first.Models[0])
	}

	// Mutating a returned snapshot must not reach the cache either.
	first.Models[0].Thinking.DefaultLevel = "mutated-by-reader"
	first.Models[0].Thinking.SupportedLevels[0].Value = "mutated-by-reader"
	first.Models[0].ServiceTiers[0].ID = "mutated-by-reader"

	second, err := cache.Get(ctx, "rt-1")
	if err != nil || second == nil {
		t.Fatalf("get: %+v %v", second, err)
	}
	if second.Models[0].Thinking == first.Models[0].Thinking {
		t.Error("thinking pointer is shared between snapshots")
	}
	if second.Models[0].Thinking.DefaultLevel != "low" ||
		second.Models[0].Thinking.SupportedLevels[0].Value != "low" ||
		second.Models[0].ServiceTiers[0].ID != "fast" {
		t.Fatalf("reader mutation leaked into the cache: %+v", second.Models[0])
	}
}

// TestInMemoryModelCatalogCache_SkipsUncacheableResults pins the same rule the
// daemon's own discovery cache uses: an empty catalog is a transient failure
// (CLI not logged in, timeout), and caching it would pin the picker empty for
// the whole serve window.
func TestInMemoryModelCatalogCache_SkipsUncacheableResults(t *testing.T) {
	ctx := context.Background()
	cache := NewInMemoryModelCatalogCache()

	if err := cache.Put(ctx, "rt-empty", nil, nil, true); err != nil {
		t.Fatalf("put empty: %v", err)
	}
	if got, _ := cache.Get(ctx, "rt-empty"); got != nil {
		t.Fatalf("empty catalog must not be cached: %+v", got)
	}

	if err := cache.Put(ctx, "rt-unsupported", sampleCatalog(), nil, false); err != nil {
		t.Fatalf("put unsupported: %v", err)
	}
	if got, _ := cache.Get(ctx, "rt-unsupported"); got != nil {
		t.Fatalf("unsupported runtime must not be cached: %+v", got)
	}

	if err := cache.Put(ctx, "", sampleCatalog(), nil, true); err != nil {
		t.Fatalf("put empty runtime id: %v", err)
	}
	if got, _ := cache.Get(ctx, ""); got != nil {
		t.Fatalf("empty runtime id must not be cached: %+v", got)
	}
}

// TestInMemoryModelCatalogCache_ExpiresAndInvalidates bounds how stale an
// answer the fast path can serve, and proves an explicit drop works.
func TestInMemoryModelCatalogCache_ExpiresAndInvalidates(t *testing.T) {
	ctx := context.Background()
	cache := NewInMemoryModelCatalogCache()
	cache.retainFor = 20 * time.Millisecond

	if err := cache.Put(ctx, "rt-1", sampleCatalog(), nil, true); err != nil {
		t.Fatalf("put: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if got, err := cache.Get(ctx, "rt-1"); err != nil || got != nil {
		t.Fatalf("expected expiry past the serve window: got=%+v err=%v", got, err)
	}

	cache.retainFor = modelCatalogServeWindow
	if err := cache.Put(ctx, "rt-1", sampleCatalog(), nil, true); err != nil {
		t.Fatalf("re-put: %v", err)
	}
	if err := cache.Invalidate(ctx, "rt-1"); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	if got, _ := cache.Get(ctx, "rt-1"); got != nil {
		t.Fatalf("expected a miss after Invalidate: %+v", got)
	}
}

// TestModelCatalogServeWindow_ServesDayOldSnapshotAndRevalidates pins the
// day-scale serve window (MUL-5444). Nothing keeps a snapshot warm in the
// background and the browser's own react-query cache dies with the tab, so a
// minutes-scale window turned every first-open-of-the-day into a cold daemon
// round trip — the multi-second wait this cache exists to remove. Freshness is
// not traded away for it: a snapshot past modelCatalogRevalidateAfter is still
// served, but serving it queues the refresh that makes the next open correct.
func TestModelCatalogServeWindow_ServesDayOldSnapshotAndRevalidates(t *testing.T) {
	ctx := context.Background()

	if modelCatalogRevalidateAfter >= modelCatalogServeWindow {
		t.Fatalf("revalidate threshold %s must stay well below the serve window %s, otherwise nothing refreshes",
			modelCatalogRevalidateAfter, modelCatalogServeWindow)
	}
	if modelCatalogServeWindow < 12*time.Hour {
		t.Fatalf("serve window %s is not day-scale; agent CLIs are upgraded on a scale of days, so a shorter window only makes the first open of each day slow",
			modelCatalogServeWindow)
	}

	cache := NewInMemoryModelCatalogCache()
	store := NewInMemoryModelListStore()
	rec := &pendingWorkRecorder{}
	h := &Handler{ModelCatalogCache: cache, ModelListStore: store, DaemonPendingWork: rec}

	seed := func(runtimeID string, age time.Duration) {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		cache.entries[runtimeID] = ModelCatalogSnapshot{
			RuntimeID: runtimeID,
			Models:    sampleCatalog(),
			Supported: true,
			StoredAt:  time.Now().Add(-age),
		}
	}

	for _, tc := range []struct {
		name   string
		age    time.Duration
		served bool
	}{
		{name: "hours old is still served", age: 2 * time.Hour, served: true},
		{name: "just inside the window is served", age: modelCatalogServeWindow - time.Minute, served: true},
		{name: "past the window is a miss", age: modelCatalogServeWindow + time.Minute, served: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtimeID := "rt-" + tc.name
			seed(runtimeID, tc.age)
			got := h.cachedModelCatalog(ctx, runtimeID)
			if tc.served && got == nil {
				t.Fatalf("snapshot aged %s should still answer without waiting for the daemon", tc.age)
			}
			if !tc.served && got != nil {
				t.Fatalf("snapshot aged %s is past the serve window and must not be served: %+v", tc.age, got)
			}
		})
	}

	// Serving an aged snapshot must still queue the refresh — the whole reason a
	// long window is safe.
	seed("rt-revalidate", 2*time.Hour)
	served := h.cachedModelCatalog(ctx, "rt-revalidate")
	if served == nil {
		t.Fatal("expected the aged snapshot to be served")
	}
	if age := served.Age(time.Now()); age < modelCatalogRevalidateAfter {
		t.Fatalf("test fixture is younger than the revalidate threshold (%s < %s)", age, modelCatalogRevalidateAfter)
	}
	h.revalidateModelCatalog(ctx, "rt-revalidate")
	pending, err := store.HasPending(ctx, "rt-revalidate")
	if err != nil {
		t.Fatalf("has pending: %v", err)
	}
	if !pending {
		t.Fatal("serving an aged snapshot must enqueue a background refresh")
	}
	if rec.count() != 1 {
		t.Fatalf("expected exactly 1 pending-work hint, got %d", rec.count())
	}
}

// TestInitiateListModels_ForceSkipsCatalogCache pins the contract behind the
// picker's Refresh action: a normal open stays instant on a warm catalog, while
// force=true creates a request the daemon must answer instead of returning the
// same cached snapshot again.
func TestInitiateListModels_ForceSkipsCatalogCache(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := dbfx.Runtime(t, "Force model refresh runtime")
	cache := NewInMemoryModelCatalogCache()
	unavailable := []UnavailableModelEntry{{
		ID:     "cc-update-required-1",
		Label:  "Fable 5.1 (disabled)",
		Reason: "Update Claude Code",
	}}
	if err := cache.Put(context.Background(), runtimeID, sampleCatalog(), unavailable, true); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}
	store := NewInMemoryModelListStore()
	recorder := &pendingWorkRecorder{}
	h := *testHandler
	h.ModelCatalogCache = cache
	h.ModelListStore = store
	h.DaemonPendingWork = recorder

	request := func(query string) *http.Request {
		return withURLParam(
			newRequest(http.MethodPost, "/api/runtimes/"+runtimeID+"/models"+query, nil),
			"runtimeId",
			runtimeID,
		)
	}

	var cached ModelListRequest
	testutil.Call(t, h.InitiateListModels, request("")).Want(http.StatusOK).JSON(&cached)
	if cached.Status != ModelListCompleted || !cached.Cached {
		t.Fatalf("normal open = %+v, want completed cache hit", cached)
	}
	if len(cached.UnavailableModels) != 1 || cached.UnavailableModels[0].ID != unavailable[0].ID {
		t.Fatalf("normal cache hit lost unavailable models: %+v", cached.UnavailableModels)
	}
	if recorder.count() != 0 {
		t.Fatalf("normal cache hit queued %d daemon requests, want 0", recorder.count())
	}

	var forced ModelListRequest
	testutil.Call(t, h.InitiateListModels, request("?force=true")).Want(http.StatusOK).JSON(&forced)
	if forced.Status != ModelListPending || forced.Cached {
		t.Fatalf("forced refresh = %+v, want pending live request", forced)
	}
	if recorder.count() != 1 {
		t.Fatalf("forced refresh queued %d daemon requests, want 1", recorder.count())
	}
}

// failingModelCatalogCache reports a backend error on every read.
type failingModelCatalogCache struct{}

func (failingModelCatalogCache) Get(context.Context, string) (*ModelCatalogSnapshot, error) {
	return nil, errors.New("redis down")
}
func (failingModelCatalogCache) Put(context.Context, string, []ModelEntry, []UnavailableModelEntry, bool) error {
	return nil
}
func (failingModelCatalogCache) Invalidate(context.Context, string) error { return nil }

// TestCachedModelCatalog_DegradesToMiss proves the cache can never fail a
// request: a nil cache, a backend error, or a snapshot that is no longer
// serveable all fall back to the normal daemon round trip.
func TestCachedModelCatalog_DegradesToMiss(t *testing.T) {
	ctx := context.Background()

	if got := (&Handler{}).cachedModelCatalog(ctx, "rt-1"); got != nil {
		t.Fatalf("nil cache should miss, got %+v", got)
	}

	h := &Handler{ModelCatalogCache: failingModelCatalogCache{}}
	if got := h.cachedModelCatalog(ctx, "rt-1"); got != nil {
		t.Fatalf("failing cache should miss, got %+v", got)
	}

	// A snapshot that somehow holds an unusable catalog is treated as a miss
	// rather than answering the picker with an empty list.
	inmem := NewInMemoryModelCatalogCache()
	inmem.entries["rt-1"] = ModelCatalogSnapshot{RuntimeID: "rt-1", Supported: true, StoredAt: time.Now()}
	h = &Handler{ModelCatalogCache: inmem}
	if got := h.cachedModelCatalog(ctx, "rt-1"); got != nil {
		t.Fatalf("empty cached catalog should miss, got %+v", got)
	}
}

// pendingWorkRecorder records the runtime-scoped hints a handler pushes.
type pendingWorkRecorder struct {
	mu    sync.Mutex
	hints []string
}

func (r *pendingWorkRecorder) NotifyPendingWork(runtimeID, kind string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hints = append(r.hints, runtimeID+":"+kind)
}

func (r *pendingWorkRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.hints)
}

// TestRevalidateModelCatalog_EnqueuesAndHints covers the "revalidate" half of
// stale-while-revalidate: serving a stale snapshot must queue a refresh AND
// push the wakeup hint, so the next open is both fast and fresh.
func TestRevalidateModelCatalog_EnqueuesAndHints(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryModelListStore()
	rec := &pendingWorkRecorder{}
	h := &Handler{ModelListStore: store, DaemonPendingWork: rec}

	h.revalidateModelCatalog(ctx, "rt-1")

	pending, err := store.HasPending(ctx, "rt-1")
	if err != nil {
		t.Fatalf("has pending: %v", err)
	}
	if !pending {
		t.Fatal("expected a background refresh request to be enqueued")
	}
	if rec.count() != 1 {
		t.Fatalf("expected exactly 1 pending-work hint, got %d", rec.count())
	}

	// Stampede control: a second open while the first refresh is still queued
	// must not pile on another request.
	h.revalidateModelCatalog(ctx, "rt-1")
	if rec.count() != 1 {
		t.Fatalf("expected the queued refresh to suppress a second hint, got %d", rec.count())
	}
}

// TestRequestDaemonPendingWork_PrefersNotifier keeps the notifier optional: a
// handler without one must not panic (single-node deployments fall back to the
// local hub, which is nil in unit tests).
func TestRequestDaemonPendingWork_PrefersNotifier(t *testing.T) {
	rec := &pendingWorkRecorder{}
	h := &Handler{DaemonPendingWork: rec}
	h.requestDaemonPendingWork("rt-1", "model_list")
	if rec.count() != 1 {
		t.Fatalf("expected the notifier to receive the hint, got %d", rec.count())
	}

	h.requestDaemonPendingWork("", "model_list")
	if rec.count() != 1 {
		t.Fatalf("an empty runtime id must not produce a hint, got %d", rec.count())
	}

	// No notifier and no hub: still a no-op, never a panic.
	(&Handler{}).requestDaemonPendingWork("rt-1", "model_list")
}

// TestCacheableModelCatalog pins which completed discovery results are worth
// remembering. It is also the branch ReportModelListResult uses to decide
// between warming the cache and dropping a snapshot the runtime no longer
// advertises.
func TestCacheableModelCatalog(t *testing.T) {
	if !cacheableModelCatalog(sampleCatalog(), true, false) {
		t.Error("a supported, non-empty catalog must be cacheable")
	}
	if cacheableModelCatalog(sampleCatalog(), false, false) {
		t.Error("a runtime that ignores model selection must not be cached")
	}
	if cacheableModelCatalog(nil, true, false) {
		t.Error("an empty catalog is a transient failure, not a cacheable answer")
	}
	if cacheableModelCatalog([]ModelEntry{}, true, false) {
		t.Error("an empty (non-nil) catalog is not cacheable either")
	}
	// The MUL-5549 hole: a fallback catalog is non-empty and `supported`, so
	// both checks above pass it. Only the fallback flag keeps a static
	// stand-in out of the 24h serve window.
	if cacheableModelCatalog(sampleCatalog(), true, true) {
		t.Error("a fallback catalog must never be cached as the runtime's real catalog")
	}
}

// TestInMemoryModelCatalogCache_RoundTripsUnavailableModels covers the advisory
// list (MUL-6961). Without it, a warm cache hit would silently drop the greyed
// "needs a newer CLI" rows for the whole serve window, so the same runtime would
// explain the missing model on a cold open and stay silent on every open after.
func TestInMemoryModelCatalogCache_RoundTripsUnavailableModels(t *testing.T) {
	ctx := context.Background()
	cache := NewInMemoryModelCatalogCache()

	unavailable := []UnavailableModelEntry{{
		ID:     "cc-update-required-1",
		Label:  "Fable 5.1 (disabled)",
		Reason: "Update to 2.1.255+ to use Fable 5.1",
	}}
	if err := cache.Put(ctx, "rt-1", sampleCatalog(), unavailable, true); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := cache.Get(ctx, "rt-1")
	if err != nil || got == nil {
		t.Fatalf("get: got=%+v err=%v", got, err)
	}
	if len(got.UnavailableModels) != 1 {
		t.Fatalf("unavailable models lost in the cache: %+v", got.UnavailableModels)
	}
	if got.UnavailableModels[0].Reason != "Update to 2.1.255+ to use Fable 5.1" {
		t.Errorf("reason = %q, want the runtime's upgrade hint", got.UnavailableModels[0].Reason)
	}
	// The selectable list must not have absorbed it on the way through.
	for _, m := range got.Models {
		if m.ID == "cc-update-required-1" {
			t.Fatal("an unavailable model leaked into the selectable catalog")
		}
	}

	// Mutating the returned slice must not corrupt the cache, same as Models.
	got.UnavailableModels[0].Label = "mutated"
	again, err := cache.Get(ctx, "rt-1")
	if err != nil || again == nil {
		t.Fatalf("second get: got=%+v err=%v", again, err)
	}
	if again.UnavailableModels[0].Label != "Fable 5.1 (disabled)" {
		t.Errorf("cache was corrupted by a caller mutation: %q", again.UnavailableModels[0].Label)
	}
}
