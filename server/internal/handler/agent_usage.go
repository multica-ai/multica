package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

var errBrokerUnavailable = errors.New("claude broker returned non-2xx")

// planUsageWindow is one rate-limit window: percent consumed (0-100) and when
// it rolls over. Mirrors the broker's UsageWindow shape.
type planUsageWindow struct {
	Utilization float64   `json:"utilization"`
	ResetsAt    time.Time `json:"resets_at"`
}

// planLimitsResponse is the API contract consumed by the sidebar widget.
// Available is false when no OAuth broker is configured or it can't be
// reached, so the UI hides the widget instead of rendering empty bars.
type planLimitsResponse struct {
	Available      bool             `json:"available"`
	FiveHour       *planUsageWindow `json:"five_hour,omitempty"`
	SevenDay       *planUsageWindow `json:"seven_day,omitempty"`
	SevenDayOpus   *planUsageWindow `json:"seven_day_opus,omitempty"`
	SevenDaySonnet *planUsageWindow `json:"seven_day_sonnet,omitempty"`
	FetchedAt      *time.Time       `json:"fetched_at,omitempty"`
}

// brokerUsageSnapshot is the broker's /usage payload. Kept local so the
// handler package doesn't depend on the broker command package.
type brokerUsageSnapshot struct {
	FiveHour       *planUsageWindow `json:"five_hour"`
	SevenDay       *planUsageWindow `json:"seven_day"`
	SevenDayOpus   *planUsageWindow `json:"seven_day_opus"`
	SevenDaySonnet *planUsageWindow `json:"seven_day_sonnet"`
	FetchedAt      time.Time        `json:"fetched_at"`
}

var planLimitsHTTPClient = &http.Client{Timeout: 5 * time.Second}

// GetAgentPlanLimits proxies the OAuth broker's plan-usage snapshot. It always
// returns 200: a missing/unreachable broker yields {"available": false} rather
// than an error, so the client can treat "no data" as "hide the widget".
func (h *Handler) GetAgentPlanLimits(w http.ResponseWriter, r *http.Request) {
	base := h.cfg.ClaudeBrokerURL
	if base == "" {
		writeJSON(w, http.StatusOK, planLimitsResponse{Available: false})
		return
	}
	snap, err := fetchBrokerUsage(r.Context(), base)
	if err != nil {
		// Broker down or still warming up — degrade quietly.
		writeJSON(w, http.StatusOK, planLimitsResponse{Available: false})
		return
	}
	fetchedAt := snap.FetchedAt
	writeJSON(w, http.StatusOK, planLimitsResponse{
		Available:      true,
		FiveHour:       snap.FiveHour,
		SevenDay:       snap.SevenDay,
		SevenDayOpus:   snap.SevenDayOpus,
		SevenDaySonnet: snap.SevenDaySonnet,
		FetchedAt:      &fetchedAt,
	})
}

func fetchBrokerUsage(ctx context.Context, base string) (*brokerUsageSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/usage", nil)
	if err != nil {
		return nil, err
	}
	resp, err := planLimitsHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errBrokerUnavailable
	}
	var snap brokerUsageSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}
