package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Model list request store
// ---------------------------------------------------------------------------
//
// The server cannot call the daemon directly (the daemon is behind the user's
// NAT and only polls the server). So "list models for this runtime" uses a
// pending-request pattern: a frontend POST creates a pending request, the
// daemon pops it on the next heartbeat, executes locally, and reports the
// result back.
//
// The store is the cross-cutting state for that flow. It MUST stay coherent
// across API replicas — POST, heartbeat and poll can each land on a different
// node, and they all need to see the same request lifecycle. The single-node
// in-memory implementation is fine for self-hosted dev; multi-node deploys
// (Multica Cloud) MUST use the Redis-backed implementation, otherwise the
// pending request is invisible to whichever replica receives the next call
// and the picker shows "No models available" (regression: see issue
// review on multica-ai/multica#2009).

// ModelListStatus represents the lifecycle of a model list request.
type ModelListStatus string

const (
	ModelListPending   ModelListStatus = "pending"
	ModelListRunning   ModelListStatus = "running"
	ModelListCompleted ModelListStatus = "completed"
	ModelListFailed    ModelListStatus = "failed"
	ModelListTimeout   ModelListStatus = "timeout"
)

// ModelListRequest represents a pending or completed model list request.
// Supported is false when the provider ignores per-agent model
// selection entirely — agent.ModelSelectionSupported is the single
// source of truth, and it names qwenpaw and mcode. Hermes is NOT in
// that set: it honours opts.Model through the ACP `session/set_model`
// RPC, so do not "align" this list by adding it (MUL-6606 — an earlier
// version of this comment said hermes, which would have cost Hermes its
// model picker outright). The UI uses this to disable its dropdown
// rather than silently accepting a value the backend will drop.
//
// RunStartedAt is set when PopPending claims the request. It is
// `json:"-"` because it's a server-side bookkeeping field — the UI only
// needs Status / UpdatedAt to drive the polling loop.
type ModelListRequest struct {
	ID        string          `json:"id"`
	RuntimeID string          `json:"runtime_id"`
	Status    ModelListStatus `json:"status"`
	Models    []ModelEntry    `json:"models,omitempty"`
	// UnavailableModels is advisory display copy, never a source of pickable
	// values. Older daemons omit it and older clients ignore it.
	UnavailableModels []UnavailableModelEntry `json:"unavailable_models,omitempty"`
	Supported         bool                    `json:"supported"`
	Error             string                  `json:"error,omitempty"`
	CreatedAt         time.Time               `json:"created_at"`
	UpdatedAt         time.Time               `json:"updated_at"`
	RunStartedAt      *time.Time              `json:"-"`
	// Cached marks a response answered from the server-side catalog cache
	// instead of a live daemon round trip (MUL-5444). Purely informational —
	// Status is already "completed" and Models is already populated, so a client
	// that ignores this field behaves exactly as before. CachedAt carries the
	// snapshot's capture time for clients that want to surface freshness.
	// Neither field is ever persisted in the request store; they only exist on
	// the synthetic cache-hit response.
	Cached   bool       `json:"cached,omitempty"`
	CachedAt *time.Time `json:"cached_at,omitempty"`
}

// ModelEntry mirrors agent.Model for the wire. `Default` tags the
// model the runtime advertises as its preferred pick (e.g. Claude
// Code's shipped default, or hermes' currentModelId) so the UI can
// badge it — don't drop it when marshalling.
//
// `Thinking` carries the per-model reasoning-effort catalog discovered
// by the daemon for runtimes that support it (claude, codex — see
// MUL-2339). nil means "no picker for this model"; the UI hides the
// thinking_level selector. Older daemons (pre-2026-05) won't send this
// field, which is fine: the UI hides the selector and the agent runs
// with the runtime default.
//
// Every entry here is selectable. Models the runtime named but will not run are
// carried separately in UnavailableModelEntry — see the invariant there.
type ModelEntry struct {
	ID                                  string             `json:"id"`
	Label                               string             `json:"label"`
	Provider                            string             `json:"provider,omitempty"`
	Default                             bool               `json:"default,omitempty"`
	Thinking                            *ModelThinking     `json:"thinking,omitempty"`
	ServiceTiers                        []ModelServiceTier `json:"service_tiers,omitempty"`
	SupportsExplicitStandardServiceTier bool               `json:"supports_explicit_standard_service_tier,omitempty"`
}

// UnavailableModelEntry is a model the runtime named but will not run on that
// host — Claude Code reports one needing a newer CLI this way. `Reason` is the
// runtime's own upgrade hint, shown so the gap reads as "your CLI is behind"
// rather than "Multica does not support this model" (MUL-6961).
//
// INVARIANT: these never appear in `models`. A client that does not know this
// field is an installed desktop build predating it, and such a client must not
// be able to render one as a selectable model and persist an id its CLI
// rejects. Keeping the lists disjoint is what makes that true without the old
// client having to do anything.
type UnavailableModelEntry struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Reason string `json:"reason,omitempty"`
}

type ModelServiceTier struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ModelThinking is the wire shape for the per-model thinking catalog.
// Mirrors agent.ModelThinking so the daemon's report passes through
// without remapping.
type ModelThinking struct {
	SupportedLevels []ThinkingLevel `json:"supported_levels"`
	DefaultLevel    string          `json:"default_level,omitempty"`
}

// ThinkingLevel is the wire shape for a single entry in a model's
// reasoning-effort catalog. `Value` is the literal token the daemon
// passes to the CLI; `Label` is the human-readable display string;
// `Description` is optional helper copy (Codex's debug-models output
// includes one per level).
type ThinkingLevel struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

const (
	// modelListPendingTimeout bounds how long a pending request can sit in
	// the store before the UI is told "daemon didn't pick this up".
	modelListPendingTimeout = 30 * time.Second
	// modelListRunningTimeout bounds how long a claimed (running) request
	// can stay claimed before the UI is told "daemon picked this up but
	// never reported a result". This matters when the heartbeat response
	// carrying `pending_model_list` is lost in transit (e.g. HTTP client
	// timeout after PopPending already mutated store state): without this
	// transition the UI would keep polling a record that is stuck in
	// `running` until retention sweeps it.
	modelListRunningTimeout = 60 * time.Second
	// modelListStoreRetention bounds how long any stored request lives in
	// the backing store. The Redis backend uses it as a TTL; the in-memory
	// backend GCs on Create. The window is deliberately wider than the
	// running/pending timeouts so terminal records are still readable when
	// the UI's last poll arrives.
	modelListStoreRetention = 2 * time.Minute
)

// ModelListStore is the contract every backend (in-memory single-node,
// Redis multi-node) must satisfy. Methods take a context so the Redis
// implementation can honour the heartbeat-side timeout that gates a
// slow shared store from stalling the rest of the heartbeat.
type ModelListStore interface {
	Create(ctx context.Context, runtimeID string) (*ModelListRequest, error)
	Get(ctx context.Context, id string) (*ModelListRequest, error)
	// HasPending is a cheap read-only probe used by the heartbeat hot path
	// to gate the side-effecting PopPending. A spurious "true" is fine —
	// PopPending handles "queue empty after probe" by returning nil.
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*ModelListRequest, error)
	Complete(ctx context.Context, id string, models []ModelEntry, unavailable []UnavailableModelEntry, supported bool) error
	Fail(ctx context.Context, id string, errMsg string) error
}

// applyModelListTimeout transitions a request to ModelListTimeout when it has
// been stuck in a non-terminal state past its threshold. Returns true when
// the record was modified so callers can persist the change. The pending
// threshold catches "daemon never picked this up"; the running threshold
// catches "daemon picked it up but the result report was lost" — without
// the running escape, only retention sweep ends the polling loop.
func applyModelListTimeout(req *ModelListRequest, now time.Time) bool {
	switch req.Status {
	case ModelListPending:
		if now.Sub(req.CreatedAt) > modelListPendingTimeout {
			req.Status = ModelListTimeout
			req.Error = "daemon did not respond within 30 seconds"
			req.UpdatedAt = now
			return true
		}
	case ModelListRunning:
		if req.RunStartedAt != nil && now.Sub(*req.RunStartedAt) > modelListRunningTimeout {
			req.Status = ModelListTimeout
			req.Error = "daemon did not finish within 60 seconds"
			req.UpdatedAt = now
			return true
		}
	}
	return false
}

// InMemoryModelListStore is the single-node implementation. Adequate for
// self-hosted dev and the test suite, but unsafe in multi-node deploys
// (each replica gets its own map and the pending request is invisible to
// every replica that didn't receive the POST).
type InMemoryModelListStore struct {
	mu       sync.Mutex
	requests map[string]*ModelListRequest
}

func NewInMemoryModelListStore() *InMemoryModelListStore {
	return &InMemoryModelListStore{requests: make(map[string]*ModelListRequest)}
}

func (s *InMemoryModelListStore) Create(_ context.Context, runtimeID string) (*ModelListRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Garbage-collect stale entries so the map can't grow unbounded.
	for id, req := range s.requests {
		if time.Since(req.CreatedAt) > modelListStoreRetention {
			delete(s.requests, id)
		}
	}

	now := time.Now()
	req := &ModelListRequest{
		ID:        randomID(),
		RuntimeID: runtimeID,
		Status:    ModelListPending,
		// Default to true; the daemon overrides this in the report
		// for providers that don't support per-agent model selection.
		Supported: true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.requests[req.ID] = req
	return req, nil
}

func (s *InMemoryModelListStore) Get(_ context.Context, id string) (*ModelListRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	applyModelListTimeout(req, time.Now())
	return req, nil
}

func (s *InMemoryModelListStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, req := range s.requests {
		applyModelListTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == ModelListPending {
			return true, nil
		}
	}
	return false, nil
}

func (s *InMemoryModelListStore) PopPending(_ context.Context, runtimeID string) (*ModelListRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var oldest *ModelListRequest
	now := time.Now()
	for _, req := range s.requests {
		applyModelListTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == ModelListPending {
			if oldest == nil || req.CreatedAt.Before(oldest.CreatedAt) {
				oldest = req
			}
		}
	}
	if oldest != nil {
		oldest.Status = ModelListRunning
		startedAt := now
		oldest.RunStartedAt = &startedAt
		oldest.UpdatedAt = now
	}
	return oldest, nil
}

func (s *InMemoryModelListStore) Complete(_ context.Context, id string, models []ModelEntry, unavailable []UnavailableModelEntry, supported bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = ModelListCompleted
		req.Models = models
		req.UnavailableModels = unavailable
		req.Supported = supported
		req.UpdatedAt = time.Now()
	}
	return nil
}

func (s *InMemoryModelListStore) Fail(_ context.Context, id string, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if req, ok := s.requests[id]; ok {
		req.Status = ModelListFailed
		req.Error = errMsg
		req.UpdatedAt = time.Now()
	}
	return nil
}

func modelListRequestTerminal(status ModelListStatus) bool {
	return status == ModelListCompleted || status == ModelListFailed || status == ModelListTimeout
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// InitiateListModels answers a "list this runtime's models" request.
//
// Fast path: a cached catalog younger than modelCatalogServeWindow is returned
// as an already-completed request, so the picker renders immediately instead of
// waiting for the daemon (stale-while-revalidate). Serving a snapshot older than
// modelCatalogRevalidateAfter also enqueues a background refresh, which nobody
// polls — its only job is to warm the cache for the next open.
//
// Slow path: enqueue a pending request the daemon claims on its next heartbeat,
// and push a wakeup hint so "next heartbeat" is now rather than up to one
// HeartbeatInterval away.
func (h *Handler) InitiateListModels(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, _, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeAPI, runtimeID)
	if !ok {
		return
	}
	if rt.Status != "online" {
		writeError(w, http.StatusServiceUnavailable, "runtime is offline")
		return
	}
	resolvedRuntimeID := uuidToString(rt.ID)

	if cached := h.cachedModelCatalog(r.Context(), resolvedRuntimeID); cached != nil {
		age := cached.Age(time.Now())
		if age >= modelCatalogRevalidateAfter {
			h.revalidateModelCatalog(r.Context(), resolvedRuntimeID)
		}
		storedAt := cached.StoredAt
		writeJSON(w, http.StatusOK, &ModelListRequest{
			// Synthetic ID: no store record backs a cache hit. Clients only poll
			// GET /models/{id} while status is pending/running, which this
			// response never is.
			ID:                randomID(),
			RuntimeID:         resolvedRuntimeID,
			Status:            ModelListCompleted,
			Models:            cached.Models,
			UnavailableModels: cached.UnavailableModels,
			Supported:         cached.Supported,
			CreatedAt:         storedAt,
			UpdatedAt:         storedAt,
			Cached:            true,
			CachedAt:          &storedAt,
		})
		return
	}

	req, err := h.ModelListStore.Create(r.Context(), resolvedRuntimeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue model list request: "+err.Error())
		return
	}
	h.requestDaemonPendingWork(resolvedRuntimeID, protocol.PendingWorkKindModelList)
	writeJSON(w, http.StatusOK, req)
}

// cachedModelCatalog returns a usable cached catalog, or nil when the cache is
// absent, cold, or unreadable. Cache problems must never fail the request — the
// caller just falls back to the daemon round trip.
func (h *Handler) cachedModelCatalog(ctx context.Context, runtimeID string) *ModelCatalogSnapshot {
	if h.ModelCatalogCache == nil {
		return nil
	}
	snapshot, err := h.ModelCatalogCache.Get(ctx, runtimeID)
	if err != nil {
		slog.Warn("model catalog cache read failed", "error", err, "runtime_id", runtimeID)
		return nil
	}
	// fallback=false: a stored snapshot is by construction a real discovery
	// result — fallback catalogs never enter the cache.
	if snapshot == nil || !cacheableModelCatalog(snapshot.Models, snapshot.Supported, false) {
		return nil
	}
	return snapshot
}

// revalidateModelCatalog enqueues a background discovery request whose result
// only updates the cache. No client polls it; the store's own timeout sweeps the
// record if the daemon never answers.
//
// Stampede control: skip when a request for this runtime is already queued. A
// running request that has already been claimed is not visible to HasPending, so
// two opens in the same second can enqueue two refreshes — bounded, cheap
// (the daemon memoizes discovery for 60s), and strictly better than the
// alternative of never refreshing.
func (h *Handler) revalidateModelCatalog(ctx context.Context, runtimeID string) {
	if h.ModelListStore == nil {
		return
	}
	pending, err := h.ModelListStore.HasPending(ctx, runtimeID)
	if err != nil {
		slog.Debug("model catalog revalidate probe failed", "error", err, "runtime_id", runtimeID)
		return
	}
	if pending {
		return
	}
	if _, err := h.ModelListStore.Create(ctx, runtimeID); err != nil {
		slog.Debug("model catalog revalidate enqueue failed", "error", err, "runtime_id", runtimeID)
		return
	}
	h.requestDaemonPendingWork(runtimeID, protocol.PendingWorkKindModelList)
}

// requestDaemonPendingWork nudges the daemon to heartbeat now. Best-effort by
// design: the daemon's scheduled heartbeat remains the correctness path, so a
// missing notifier or an offline daemon only costs latency. Prefers the relay
// notifier (reaches the API node holding the daemon's socket) and falls back to
// the local hub, which is the whole cluster in single-node deployments.
func (h *Handler) requestDaemonPendingWork(runtimeID, kind string) {
	if runtimeID == "" {
		return
	}
	if h.DaemonPendingWork != nil {
		h.DaemonPendingWork.NotifyPendingWork(runtimeID, kind)
		return
	}
	if h.DaemonHub != nil {
		h.DaemonHub.NotifyPendingWork(runtimeID, kind)
	}
}

// GetModelListRequest returns the status of a model list request.
func (h *Handler) GetModelListRequest(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, _, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeModelPoll, runtimeID)
	if !ok {
		return
	}

	requestID := chi.URLParam(r, "requestId")

	req, err := h.ModelListStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load request: "+err.Error())
		return
	}
	if req == nil || req.RuntimeID != uuidToString(rt.ID) {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// ReportModelListResult receives the list result from the daemon.
func (h *Handler) ReportModelListResult(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")

	if _, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID); !ok {
		return
	}

	requestID := chi.URLParam(r, "requestId")

	// Fetch first so we can ignore stale reports for already-terminal
	// requests (e.g. the heartbeat response that triggered the daemon
	// run was a retry, and the original report already landed).
	existing, err := h.ModelListStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load request: "+err.Error())
		return
	}
	if existing == nil || existing.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if modelListRequestTerminal(existing.Status) {
		slog.Debug("ignoring stale model list report", "runtime_id", runtimeID, "request_id", requestID, "status", existing.Status)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	var body struct {
		Status string       `json:"status"` // "completed" or "failed"
		Models []ModelEntry `json:"models"`
		// UnavailableModels is what the runtime named but will not run. Older
		// daemons omit it; absent simply means no advisory rows to show.
		UnavailableModels []UnavailableModelEntry `json:"unavailable_models"`
		Supported         *bool                   `json:"supported"`
		Error             string                  `json:"error"`
		// Fallback marks a completed report whose models are a static
		// stand-in the provider substituted after discovery failed, not the
		// runtime's real catalog. Older daemons omit it; absent means "this
		// daemon cannot tell us", which stays the pre-MUL-5549 behaviour.
		Fallback bool `json:"fallback"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if body.Status == "completed" {
		// Older daemons may omit `supported`; default to true to keep
		// the UI usable while they haven't been redeployed yet.
		supported := true
		if body.Supported != nil {
			supported = *body.Supported
		}
		if err := h.ModelListStore.Complete(r.Context(), requestID, body.Models, body.UnavailableModels, supported); err != nil {
			// Surface the store failure as 5xx so the daemon can retry instead
			// of swallowing the report (leaves the request stuck in running
			// until the server-side timeout, which is exactly the "looks OK
			// but nothing happens" class of bug we're trying to avoid).
			slog.Error("ModelListStore Complete failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist completion")
			return
		}
		// Warm the catalog cache so the next picker open renders instantly
		// (MUL-5444). A cache write failure is not the daemon's problem — the
		// report itself succeeded — so it is logged, not surfaced.
		//
		// A completed-but-uncacheable result (empty catalog, or a runtime that
		// does not honour per-agent model selection) is the freshest truth we
		// have: drop any older snapshot instead of letting the fast path keep
		// serving a catalog the runtime no longer advertises. Failed reports
		// deliberately leave the cache alone — serving the last known good list
		// through a transient discovery failure is the point of the cache.
		//
		// A fallback report is a failed report wearing a completed label: the
		// models are a static stand-in, so they are neither fresh truth to
		// store nor grounds to discard a real catalog we already hold. Treat it
		// like a failure and leave the cache untouched (MUL-5549).
		if h.ModelCatalogCache != nil {
			switch modelCatalogCacheDecision(body.Models, supported, body.Fallback) {
			case modelCatalogCacheStore:
				if err := h.ModelCatalogCache.Put(r.Context(), runtimeID, body.Models, body.UnavailableModels, supported); err != nil {
					slog.Warn("model catalog cache write failed", "error", err, "runtime_id", runtimeID)
				}
			case modelCatalogCacheDrop:
				if err := h.ModelCatalogCache.Invalidate(r.Context(), runtimeID); err != nil {
					slog.Warn("model catalog cache invalidate failed", "error", err, "runtime_id", runtimeID)
				}
			case modelCatalogCacheKeep:
				slog.Debug("model discovery reported a fallback catalog; leaving cached catalog untouched",
					"runtime_id", runtimeID, "count", len(body.Models))
			}
		}
	} else {
		if err := h.ModelListStore.Fail(r.Context(), requestID, body.Error); err != nil {
			slog.Error("ModelListStore Fail failed", "error", err, "request_id", requestID)
			writeError(w, http.StatusInternalServerError, "failed to persist failure")
			return
		}
	}

	slog.Debug("model list report", "runtime_id", runtimeID, "request_id", requestID, "status", body.Status, "count", len(body.Models))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
