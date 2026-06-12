package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Broker wraps the runtime state served to clients: the most recently loaded
// TokenState (cached for fast reads from /access_token), a ready flag flipped
// true once we've successfully loaded state at least once, and the Refresher
// + store used for synchronous refresh-on-demand.
type Broker struct {
	refresher *Refresher
	store     *SecretStore
	logger    *slog.Logger

	mu     sync.RWMutex
	cached *TokenState

	usageMu sync.RWMutex
	usage   *UsageSnapshot

	ready atomic.Bool
}

func NewBroker(refresher *Refresher, store *SecretStore, logger *slog.Logger) *Broker {
	return &Broker{refresher: refresher, store: store, logger: logger}
}

// Reload loads state from the Secret into the cache without touching the
// refresh leader gate. Called once at startup so the broker can serve
// /access_token immediately even before leader election settles.
//
// Also mirrors the current access_token into the dedicated access-token
// Secret that worker Job pods read via secretKeyRef. If the mirror Secret
// is the only consumer (the apiKeyHelper HTTP path is deprecated), missing
// it at controller-dispatch time would cause worker pods to fail to start.
func (b *Broker) Reload(ctx context.Context) error {
	state, err := b.store.Load(ctx)
	if err != nil {
		return err
	}
	b.setCached(state)
	b.ready.Store(true)
	if err := b.store.MirrorAccessToken(ctx, state.AccessToken); err != nil {
		// Non-fatal — log but stay up. /access_token still serves; operators
		// can investigate via the broker logs. If the controller can't bind
		// the env var, worker pods will fail until this is resolved.
		b.logger.Warn("mirror access_token on Reload failed", "error", err)
	}
	return nil
}

// RunRefreshLoop ticks at `interval`. Each tick calls RefreshIfNeeded; the
// leader gate makes non-leader pods silently no-op until they win the lease.
func (b *Broker) RunRefreshLoop(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.tickRefresh(ctx)
		}
	}
}

func (b *Broker) tickRefresh(ctx context.Context) {
	start := time.Now()
	refreshed, state, err := b.refresher.RefreshIfNeeded(ctx)
	refreshDuration.Observe(time.Since(start).Seconds())

	switch {
	case err == nil && refreshed:
		refreshTotal.WithLabelValues(outcomeOk).Inc()
		b.setCached(state)
		if err := b.store.MirrorAccessToken(ctx, state.AccessToken); err != nil {
			b.logger.Warn("mirror access_token after refresh failed", "error", err)
		}
		b.logger.Info("refresh ok", "expires_at", state.ExpiresAt)
	case err == nil && !refreshed:
		refreshTotal.WithLabelValues(outcomeSkipped).Inc()
	case errors.Is(err, ErrNotLeader):
		refreshTotal.WithLabelValues(outcomeSkipped).Inc()
		refreshFailures.WithLabelValues("not_leader").Inc()
	default:
		refreshTotal.WithLabelValues(outcomeError).Inc()
		var perm *PermanentError
		var transient *TransientError
		switch {
		case errors.As(err, &perm):
			refreshFailures.WithLabelValues("permanent").Inc()
			b.logger.Error("refresh failed (permanent)", "error", err)
		case errors.As(err, &transient):
			refreshFailures.WithLabelValues("transient").Inc()
			b.logger.Warn("refresh failed (transient)", "error", err)
		default:
			refreshFailures.WithLabelValues("other").Inc()
			b.logger.Warn("refresh failed", "error", err)
		}
	}
}

func (b *Broker) setCached(state *TokenState) {
	b.mu.Lock()
	b.cached = state
	b.mu.Unlock()
	if !state.ExpiresAt.IsZero() {
		accessTokenExpiresAt.Set(float64(state.ExpiresAt.Unix()))
	}
}

func (b *Broker) cachedSnapshot() *TokenState {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.cached == nil {
		return nil
	}
	cp := *b.cached
	return &cp
}

// ----------- HTTP handlers ----------------------------------------------

// NewAdminMux returns the cluster-reachable handlers: /access_token + probes.
// /refresh is NOT here — see NewOpsMux below for the loopback-only listener.
func NewAdminMux(broker *Broker) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", broker.healthHandler)
	mux.HandleFunc("/readyz", broker.readyHandler)
	mux.HandleFunc("/access_token", broker.accessTokenHandler)
	mux.HandleFunc("/usage", broker.usageHandler)
	return mux
}

// NewOpsMux is the loopback-only handler set. Bound to 127.0.0.1 by main,
// so pod-network traffic can't reach it.
func NewOpsMux(broker *Broker) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/refresh", broker.refreshHandler)
	return mux
}

func (b *Broker) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (b *Broker) readyHandler(w http.ResponseWriter, r *http.Request) {
	if !b.ready.Load() {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}

// accessTokenHandler returns the cached bearer token. If the cached token is
// within RefreshPad of expiry, it triggers a synchronous refresh first so
// callers always get a non-expiring token (subject to the leader gate and
// network conditions). If the cached token is already past expiry and no
// fresh one is available, it returns 503.
func (b *Broker) accessTokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Best-effort sync refresh. Errors are logged but don't fail the request
	// if we still have a non-expired cached token.
	b.tickRefresh(r.Context())

	state := b.cachedSnapshot()
	if state == nil || state.AccessToken == "" {
		accessTokenRequestsTotal.WithLabelValues(outcomeError).Inc()
		http.Error(w, "no token available", http.StatusServiceUnavailable)
		return
	}
	if !state.ExpiresAt.IsZero() && time.Now().After(state.ExpiresAt) {
		// Expired and no refresh available — serve as stale so callers see the
		// 503 they need to surface to operators.
		accessTokenRequestsTotal.WithLabelValues(outcomeStale).Inc()
		http.Error(w, "cached token expired and refresh unavailable", http.StatusServiceUnavailable)
		return
	}
	accessTokenRequestsTotal.WithLabelValues(outcomeOk).Inc()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, state.AccessToken)
}

// refreshHandler forces a refresh regardless of expiry. Loopback-only —
// reachable only via kubectl exec on the broker pod. Operator endpoint.
func (b *Broker) refreshHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Bypass the "still fresh" early-return by zeroing the cached expiry
	// briefly via a forced load+refresh path: just call the OAuth client
	// directly through the refresher's components.
	state, err := b.store.Load(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !b.refresher.leader.IsLeader() {
		http.Error(w, "not the leader; refresh routed to leader pod required", http.StatusServiceUnavailable)
		return
	}
	res, err := b.refresher.oauth.Refresh(r.Context(), state.RefreshToken)
	if err != nil {
		var perm *PermanentError
		status := http.StatusBadGateway
		if errors.As(err, &perm) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	newState := &TokenState{
		AccessToken:  res.AccessToken,
		RefreshToken: res.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(res.ExpiresIn) * time.Second),
	}
	if newState.RefreshToken == "" {
		newState.RefreshToken = state.RefreshToken
	}
	if err := b.store.Store(r.Context(), newState); err != nil {
		http.Error(w, "persist: "+err.Error(), http.StatusInternalServerError)
		return
	}
	b.setCached(newState)
	if err := b.store.MirrorAccessToken(r.Context(), newState.AccessToken); err != nil {
		b.logger.Warn("mirror access_token after manual refresh failed", "error", err)
	}
	refreshTotal.WithLabelValues(outcomeOk).Inc()
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, "refreshed; expires_at=%s\n", newState.ExpiresAt.Format(time.RFC3339))
}
