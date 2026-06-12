package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// UsagePoller periodically fetches the account's plan-usage snapshot from
// Anthropic and caches it on the broker for /usage to serve. Only the refresh
// leader polls — the usage endpoint is per-account and aggressively
// rate-limited, so fanning out across replicas would just earn 429s.
type UsagePoller struct {
	broker   *Broker
	leader   LeaderGate
	client   *UsageClient
	interval time.Duration
}

func NewUsagePoller(broker *Broker, leader LeaderGate, client *UsageClient, interval time.Duration) *UsagePoller {
	return &UsagePoller{broker: broker, leader: leader, client: client, interval: interval}
}

// Run ticks at the configured interval until ctx is cancelled. The first poll
// fires one interval in (the token cache is already warm from Reload, but
// delaying avoids racing leader election at startup).
func (p *UsagePoller) Run(ctx context.Context) {
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.tick(ctx)
		}
	}
}

func (p *UsagePoller) tick(ctx context.Context) {
	// Leader-only: keeps us to a single caller per account so we stay under
	// the endpoint's ~180s budget.
	if !p.leader.IsLeader() {
		return
	}
	state := p.broker.cachedSnapshot()
	if state == nil || state.AccessToken == "" {
		return
	}
	snap, err := p.client.Fetch(ctx, state.AccessToken)
	if err != nil {
		switch {
		case errors.Is(err, ErrUsageRateLimited):
			usagePollTotal.WithLabelValues(outcomeRateLimited).Inc()
			p.broker.logger.Warn("usage poll rate-limited; serving last snapshot")
		default:
			usagePollTotal.WithLabelValues(outcomeError).Inc()
			p.broker.logger.Warn("usage poll failed", "error", err)
		}
		return
	}
	usagePollTotal.WithLabelValues(outcomeOk).Inc()
	observeUsage(snap)
	p.broker.setUsage(snap)
}

func (b *Broker) setUsage(snap *UsageSnapshot) {
	b.usageMu.Lock()
	b.usage = snap
	b.usageMu.Unlock()
}

func (b *Broker) usageSnapshot() *UsageSnapshot {
	b.usageMu.RLock()
	defer b.usageMu.RUnlock()
	if b.usage == nil {
		return nil
	}
	cp := *b.usage
	return &cp
}

// usageHandler serves the most recently polled plan-usage snapshot as JSON.
// Returns 503 until the first successful poll so callers can distinguish
// "not ready yet" from "zero usage".
func (b *Broker) usageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snap := b.usageSnapshot()
	if snap == nil {
		usageRequestsTotal.WithLabelValues(outcomeStale).Inc()
		http.Error(w, "no usage snapshot available yet", http.StatusServiceUnavailable)
		return
	}
	usageRequestsTotal.WithLabelValues(outcomeOk).Inc()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(snap)
}
