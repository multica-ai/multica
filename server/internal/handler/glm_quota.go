package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// GLM (Zhipu) Coding Plan quota monitor: a server-side, account-level
// collector for the provider balance behind the claude/hermes GLM runtimes.
//
// Unlike the per-runtime plan-quota collectors (kimi local Server API,
// antigravity probe), the GLM allowance is a property of the API key's
// account, not of any runtime: several runtimes share one Coding Plan key.
// It is therefore collected once here — not per daemon — and surfaced at a
// fixed spot on the runtimes page instead of per-runtime rows.
//
// Data source (official usage-query plugin contract, personal Coding Plan
// only): GET {base}/api/monitor/usage/quota/limit with the raw API key in
// Authorization. The endpoint answers HTTP 200 even for business failures,
// so the body's code/success fields are the real health check. Response
// shapes seen in the wild:
//   - Older plans: limits[] with TIME_LIMIT (monthly tool/MCP credits:
//     usage=allowance, currentValue=used, remaining, percentage=used%,
//     usageDetails per tool) and TOKENS_LIMIT (the 5-hour token window:
//     percentage only).
//   - Newer plans: several TOKENS_LIMIT and/or CREDIT_LIMIT windows side by
//     side (5-hour + weekly pools); the type alone no longer identifies the
//     period. Each limit carries unit/number (3=hours, 5=months, 6=weeks);
//     they are converted to window_minutes so the frontend can label each
//     window by its real period.
// All are passed through as typed windows; the frontend labels them.
//
// Every failure is fail-soft: a failed round keeps the previous snapshot
// (the UI ages it out) and records last_error for the status endpoint.

const (
	glmQuotaDefaultBaseURL  = "https://open.bigmodel.cn"
	glmQuotaDefaultInterval = 5 * time.Minute
	glmQuotaRequestTimeout  = 15 * time.Second
	glmQuotaPath            = "/api/monitor/usage/quota/limit"
	// Stale after a day without a successful poll — mirrors the runtime
	// plan-quota aging so both surfaces degrade the same way.
	glmQuotaStaleAfter = 24 * time.Hour
)

// GlmQuotaWindow is one quota pool from the provider response. Pointer
// fields keep "not reported" distinct from a real zero.
type GlmQuotaWindow struct {
	Type string `json:"type"`
	// WindowMinutes is the window's period in minutes derived from the
	// provider's unit/number pair (5h=300, weekly=10080); 0 when the pair
	// is absent or unrecognized — callers fall back to the type label.
	WindowMinutes int      `json:"window_minutes,omitempty"`
	UsedPercent   *float64 `json:"used_percent,omitempty"`
	Usage         *float64 `json:"usage,omitempty"`
	CurrentValue  *float64 `json:"current_value,omitempty"`
	Remaining     *float64 `json:"remaining,omitempty"`
	ResetsAt      *int64   `json:"resets_at,omitempty"`
}

// GlmQuotaSnapshot is one successful poll result.
type GlmQuotaSnapshot struct {
	Level      string           `json:"level,omitempty"`
	Windows    []GlmQuotaWindow `json:"windows"`
	ObservedAt int64            `json:"observed_at"`
}

// GlmQuotaStatus is the API payload: enabled=false when no key is
// configured, quota omitted when no successful poll ever happened.
// AnchorDevice optionally names the machine whose row should carry the GLM
// chip on the runtimes page — the balance is account-level, but operators
// typically burn it on one device, and the chip must sit visually with that
// machine's other quota pills instead of a detached card.
type GlmQuotaStatus struct {
	Enabled      bool              `json:"enabled"`
	Quota        *GlmQuotaSnapshot `json:"quota,omitempty"`
	Stale        bool              `json:"stale,omitempty"`
	LastError    string            `json:"last_error,omitempty"`
	AnchorDevice string            `json:"anchor_device,omitempty"`
}

// glmQuotaRaw mirrors the provider wire shape. Only the fields we surface
// are typed; usageDetails is deliberately dropped (per-tool usage is not
// displayed and keeps the payload small).
type glmQuotaRaw struct {
	Code    int   `json:"code"`
	Success *bool `json:"success"`
	Data    struct {
		Limits []struct {
			Type         string   `json:"type"`
			Unit         *float64 `json:"unit"`
			Number       *float64 `json:"number"`
			Usage        *float64 `json:"usage"`
			CurrentValue *float64 `json:"currentValue"`
			Remaining    *float64 `json:"remaining"`
			Percentage   *float64 `json:"percentage"`
			NextResetMs  *float64 `json:"nextResetTime"`
		} `json:"limits"`
		Level string `json:"level"`
	} `json:"data"`
}

// glmQuotaUnit* are the provider's time-unit codes in limits[].unit. The
// period is unit × number, EXCEPT weeks: plans disagree on what number means
// (1 or 7 have both been observed for the same weekly window), so a week is
// classified by unit alone and pinned to 10080 minutes (OL-1205).
const (
	glmQuotaUnitHours  = 3.0
	glmQuotaUnitMonths = 5.0
	glmQuotaUnitWeeks  = 6.0
)

// glmQuotaWindowMinutes converts a limit's unit/number pair into the window's
// period in minutes: unit=3 hours×number (5h window → 300), unit=6 → a fixed
// week (10080), unit=5 → a month-like window with no fixed minute count. 0
// means "no recognizable period" — the frontend falls back to the type label
// instead of mislabeling the window.
func glmQuotaWindowMinutes(unit, number *float64) int {
	if unit == nil || *unit <= 0 {
		return 0
	}
	switch *unit {
	case glmQuotaUnitWeeks:
		return 7 * 24 * 60
	case glmQuotaUnitHours:
		if number == nil || *number <= 0 {
			return 0
		}
		return int(*number * 60)
	case glmQuotaUnitMonths:
		// TIME_LIMIT monthly pools: labeled by type, not by duration.
		return 0
	default:
		return 0
	}
}

// parseGlmQuotaBody converts one provider response body into a snapshot.
// observedAt is injected so tests can pin time.
func parseGlmQuotaBody(body []byte, observedAt time.Time) (*GlmQuotaSnapshot, error) {
	var raw glmQuotaRaw
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("glm quota: decode body: %w", err)
	}
	// HTTP 200 with a business failure is this endpoint's error shape —
	// checking the transport code alone would surface garbage as data.
	if raw.Code != 200 || (raw.Success != nil && !*raw.Success) {
		return nil, fmt.Errorf("glm quota: provider rejected the request (code=%d)", raw.Code)
	}
	snap := &GlmQuotaSnapshot{Level: raw.Data.Level, ObservedAt: observedAt.Unix()}
	for _, lim := range raw.Data.Limits {
		w := GlmQuotaWindow{
			Type:         lim.Type,
			WindowMinutes: glmQuotaWindowMinutes(lim.Unit, lim.Number),
			UsedPercent:  lim.Percentage,
			Usage:        lim.Usage,
			CurrentValue: lim.CurrentValue,
			Remaining:    lim.Remaining,
		}
		if lim.NextResetMs != nil {
			reset := int64(*lim.NextResetMs) / 1000
			w.ResetsAt = &reset
		}
		snap.Windows = append(snap.Windows, w)
	}
	if len(snap.Windows) == 0 {
		return nil, fmt.Errorf("glm quota: response carried no limit windows")
	}
	return snap, nil
}

// GlmQuotaMonitor polls the provider and holds the latest snapshot.
type GlmQuotaMonitor struct {
	apiKey       string
	baseURL      string
	interval     time.Duration
	anchorDevice string
	client       *http.Client

	mu        sync.RWMutex
	snapshot  *GlmQuotaSnapshot
	lastError string
}

// NewGlmQuotaMonitorFromEnv returns nil when GLM_QUOTA_API_KEY is unset —
// the feature is off and the endpoint answers enabled=false.
func NewGlmQuotaMonitorFromEnv() *GlmQuotaMonitor {
	key := os.Getenv("GLM_QUOTA_API_KEY")
	if key == "" {
		return nil
	}
	interval := glmQuotaDefaultInterval
	if v := os.Getenv("GLM_QUOTA_POLL_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			interval = d
		}
	}
	base := os.Getenv("GLM_QUOTA_BASE_URL")
	if base == "" {
		base = glmQuotaDefaultBaseURL
	}
	return &GlmQuotaMonitor{
		apiKey:       key,
		baseURL:      base,
		interval:     interval,
		anchorDevice: os.Getenv("GLM_QUOTA_ANCHOR_DEVICE"),
		client:       &http.Client{Timeout: glmQuotaRequestTimeout},
	}
}

// Start launches the poll loop; the first round runs immediately so a
// freshly booted server serves data without waiting a full interval.
func (m *GlmQuotaMonitor) Start(ctx context.Context) {
	go func() {
		m.poll(ctx)
		t := time.NewTicker(m.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.poll(ctx)
			}
		}
	}()
}

// poll runs one round; any failure keeps the previous snapshot.
func (m *GlmQuotaMonitor) poll(ctx context.Context) {
	snap, err := m.fetch(ctx)
	m.mu.Lock()
	if err != nil {
		m.lastError = err.Error()
	} else {
		m.snapshot = snap
		m.lastError = ""
	}
	m.mu.Unlock()
}

func (m *GlmQuotaMonitor) fetch(ctx context.Context) (*GlmQuotaSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.baseURL+glmQuotaPath, nil)
	if err != nil {
		return nil, err
	}
	// The provider expects the raw key (official plugin sends it verbatim).
	req.Header.Set("Authorization", m.apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("glm quota: request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("glm quota: http %d", resp.StatusCode)
	}
	body := make([]byte, 0, 8192)
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		body = append(body, buf[:n]...)
		if err != nil || len(body) > 1<<20 {
			break
		}
	}
	return parseGlmQuotaBody(body, time.Now())
}

// Status returns the API view of the monitor. enabled reports only whether
// polling is active; quota is nil until the first successful round.
func (m *GlmQuotaMonitor) Status() GlmQuotaStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := GlmQuotaStatus{Enabled: true, Quota: m.snapshot, LastError: m.lastError, AnchorDevice: m.anchorDevice}
	if m.snapshot != nil && time.Since(time.Unix(m.snapshot.ObservedAt, 0)) > glmQuotaStaleAfter {
		st.Stale = true
	}
	return st
}

// GetGlmQuota serves GET /api/glm-quota (session-authed). A nil monitor
// (no key configured) still answers, with enabled=false, so the frontend
// can distinguish "not configured" from "configured but never succeeded".
func (h *Handler) GetGlmQuota(w http.ResponseWriter, r *http.Request) {
	if h.GlmQuota == nil {
		writeJSON(w, http.StatusOK, GlmQuotaStatus{Enabled: false})
		return
	}
	writeJSON(w, http.StatusOK, h.GlmQuota.Status())
}
