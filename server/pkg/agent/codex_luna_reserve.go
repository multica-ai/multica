package agent

import (
	"context"
	"encoding/json"
	"strings"
)

// Luna Reserve is a separately metered ChatGPT allowance Codex offers once the
// ordinary allowance is used up. Codex has no app-server action that "accepts"
// it: the TUI's "Continue with Luna Reserve" only appears after its automatic
// fallback has already moved the thread onto the reserve model
// (codex-rs/tui/src/app/backend_banner_fallback.rs, rust-v0.159.0). The adapter
// mirrors that fallback for one turn: when a turn fails with
// usageLimitExceeded and account/rateLimits/read reports the luna_reserve
// banner, it retries the same thread with turn/start.model set to the reserve
// model.
//
// It deliberately never acts on the banner's CTAs (Upgrade /
// open_pricing_dialog, Reset usage / reset_usage) and never calls
// account/rateLimitResetCredit/consume or any other purchase or reset method.
const (
	// codexLunaReserveModel matches LUNA_RESERVE_MODEL in
	// codex-rs/tui/src/model_catalog.rs.
	codexLunaReserveModel = "gpt-reserve"
	// codexLunaReserveBanner matches LUNA_RESERVE_BANNER in
	// codex-rs/tui/src/backend_banners.rs.
	codexLunaReserveBanner = "luna_reserve"
	// codexUsageLimitExceeded is the codexErrorInfo value Codex attaches to
	// a turn that failed because the ordinary allowance is used up.
	codexUsageLimitExceeded = "usageLimitExceeded"

	codexLunaReserveContinuePrompt = "The previous turn stopped because the Codex usage limit was reached. " +
		"Continue the task from where it stopped. Do not repeat actions that already completed."
)

// codexRateLimitsReadResult is the subset of the account/rateLimits/read
// response the reserve decision needs.
type codexRateLimitsReadResult struct {
	OrdinaryUsageAllowed *bool                             `json:"ordinaryUsageAllowed"`
	RateLimitsByLimitID  map[string]codexRateLimitSnapshot `json:"rateLimitsByLimitId"`
	RateLimitUpsell      *codexRateLimitUpsell             `json:"rateLimitUpsell"`
}

type codexRateLimitSnapshot struct {
	LimitName            *string               `json:"limitName"`
	Primary              *codexRateLimitWindow `json:"primary"`
	Secondary            *codexRateLimitWindow `json:"secondary"`
	RateLimitReachedType *string               `json:"rateLimitReachedType"`
}

type codexRateLimitWindow struct {
	UsedPercent float64 `json:"usedPercent"`
}

type codexRateLimitUpsell struct {
	BannerType       string  `json:"banner_type"`
	BlockedModelSlug *string `json:"blocked_model_slug"`
}

func (s codexRateLimitSnapshot) exhausted() bool {
	if s.RateLimitReachedType != nil && *s.RateLimitReachedType != "" {
		return true
	}
	for _, w := range []*codexRateLimitWindow{s.Primary, s.Secondary} {
		if w != nil && w.UsedPercent >= 100 {
			return true
		}
	}
	return false
}

// isCodexLunaReserveModel reports whether model names the reserve model,
// ignoring case and surrounding whitespace.
func isCodexLunaReserveModel(model string) bool {
	return strings.EqualFold(strings.TrimSpace(model), codexLunaReserveModel)
}

// codexLunaReserveAvailable decides from an account/rateLimits/read response
// whether a turn running on currentModel may continue on Luna Reserve. It
// returns a short reason for the log either way.
func codexLunaReserveAvailable(raw json.RawMessage, currentModel string) (bool, string) {
	if isCodexLunaReserveModel(currentModel) {
		return false, "already on luna reserve"
	}
	var res codexRateLimitsReadResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return false, "unreadable rate limits response"
	}
	if res.OrdinaryUsageAllowed == nil || *res.OrdinaryUsageAllowed {
		return false, "ordinary usage not reported as exhausted"
	}
	if res.RateLimitUpsell == nil || res.RateLimitUpsell.BannerType != codexLunaReserveBanner {
		return false, "luna reserve not offered"
	}
	if blocked := res.RateLimitUpsell.BlockedModelSlug; blocked != nil && *blocked != "" && *blocked != currentModel {
		return false, "luna reserve offered for a different model"
	}
	for _, snap := range res.RateLimitsByLimitID {
		if snap.LimitName != nil && isCodexLunaReserveModel(*snap.LimitName) && snap.exhausted() {
			return false, "luna reserve used up"
		}
	}
	return true, "luna reserve offered"
}

// checkCodexLunaReserve reads the account's rate limits the way the TUI does
// (supportsLunaReserve opts into the reserve banner) and applies
// codexLunaReserveAvailable.
func (c *codexClient) checkCodexLunaReserve(ctx context.Context, currentModel string) (bool, string) {
	if isCodexLunaReserveModel(currentModel) {
		return false, "already on luna reserve"
	}
	raw, err := c.request(ctx, "account/rateLimits/read", map[string]any{"supportsLunaReserve": true})
	if err != nil {
		return false, "rate limits read failed: " + err.Error()
	}
	return codexLunaReserveAvailable(raw, currentModel)
}

// codexLunaReserveTurnInput is the input for the reserve retry. A turn that
// failed before any tool ran is replayed as-is; otherwise the thread already
// holds the partial work, so the agent is told to continue instead of redoing
// it.
func codexLunaReserveTurnInput(original []map[string]any, toolActivity bool) []map[string]any {
	if !toolActivity {
		return original
	}
	return []map[string]any{{"type": "text", "text": codexLunaReserveContinuePrompt}}
}

// codexErrorInfoCode extracts the codexErrorInfo tag from an app-server
// TurnError. Simple variants serialize as a bare string
// ("usageLimitExceeded"); variants with data serialize as a single-key object.
func codexErrorInfoCode(turnError map[string]any) string {
	switch v := turnError["codexErrorInfo"].(type) {
	case string:
		return v
	case map[string]any:
		for k := range v {
			return k
		}
	}
	return ""
}
