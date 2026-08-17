package transportretry

import (
	"encoding/json"
	"os"
	"strings"
)

// Policy describes a provider-agnostic transport-layer retry ladder.
type Policy struct {
	ID              string
	Providers       []string
	MatchError      func(err string) bool
	MaxExtraAttempts int
	DelaysMs        []int
	SessionStrategy []SessionRetryMode
	Enabled         bool
}

// Config is the layered transport-retry configuration (defaults overridden by file/env).
type Config struct {
	Enabled            bool
	Policies           []Policy
	MaxRetryWallClockS int
}

// PolicyOverride is the JSON shape for workspace/runtime/agent overrides.
type PolicyOverride struct {
	ID               string             `json:"id"`
	Enabled          *bool              `json:"enabled,omitempty"`
	MaxExtraAttempts *int               `json:"max_extra_attempts,omitempty"`
	DelaysMs         []int              `json:"delays_ms,omitempty"`
	SessionStrategy  []SessionRetryMode `json:"session_strategy,omitempty"`
}

// FileConfig is the JSON document for transport_retry settings.
type FileConfig struct {
	Enabled            *bool            `json:"enabled,omitempty"`
	MaxRetryWallClockS *int             `json:"max_retry_wall_clock_s,omitempty"`
	Policies           []PolicyOverride `json:"policies,omitempty"`
}

func matchCursorWritableIterable(err string) bool {
	lower := strings.ToLower(err)
	if strings.Contains(lower, "writableiterable is closed") {
		return true
	}
	if strings.Contains(lower, "retriableerror") && strings.Contains(lower, "result_seen=false") {
		return true
	}
	return false
}

func matchStreamConnectionClosed(err string) bool {
	lower := strings.ToLower(err)
	return strings.Contains(lower, "connection closed") || strings.Contains(lower, "mid-response")
}

// DefaultPolicies returns built-in transport retry policies.
func DefaultPolicies() []Policy {
	return []Policy{
		{
			ID:               "cursor_writable_iterable",
			Providers:        []string{"cursor"},
			MatchError:       matchCursorWritableIterable,
			MaxExtraAttempts: 2,
			DelaysMs: []int{0, 0, 5000},
			SessionStrategy: []SessionRetryMode{
				SessionRetrySame,
				SessionRetrySame,
				SessionRetryFresh,
			},
			Enabled: true,
		},
		{
			ID:               "stream_connection_closed",
			Providers:        []string{"claude", "cursor"},
			MatchError:       matchStreamConnectionClosed,
			MaxExtraAttempts: 1,
			DelaysMs:         []int{0, 0},
			SessionStrategy: []SessionRetryMode{
				SessionRetrySame,
				SessionRetrySame,
			},
			Enabled: true,
		},
	}
}

// DefaultConfig returns enabled defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:  true,
		Policies: DefaultPolicies(),
	}
}

// LoadConfig merges built-in defaults with optional JSON from MULTICA_TRANSPORT_RETRY_CONFIG.
func LoadConfig() Config {
	cfg := DefaultConfig()
	raw := strings.TrimSpace(os.Getenv("MULTICA_TRANSPORT_RETRY_CONFIG"))
	if raw == "" {
		return cfg
	}
	return MergeConfigJSON(cfg, raw)
}

// MergeConfigJSON overlays a JSON transport-retry document onto cfg.
func MergeConfigJSON(cfg Config, raw string) Config {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return cfg
	}
	var file FileConfig
	if err := json.Unmarshal([]byte(raw), &file); err != nil {
		return cfg
	}
	if file.Enabled != nil {
		cfg.Enabled = *file.Enabled
	}
	if file.MaxRetryWallClockS != nil {
		cfg.MaxRetryWallClockS = *file.MaxRetryWallClockS
	}
	if len(file.Policies) > 0 {
		applyPolicyOverrides(&cfg, file.Policies)
	}
	return cfg
}

// ResolveConfig layers daemon env defaults with an optional per-agent override
// from custom_env (MULTICA_TRANSPORT_RETRY_CONFIG wins over the process env).
func ResolveConfig(agentCustomEnv map[string]string) Config {
	cfg := LoadConfig()
	if agentCustomEnv != nil {
		if raw := strings.TrimSpace(agentCustomEnv["MULTICA_TRANSPORT_RETRY_CONFIG"]); raw != "" {
			cfg = MergeConfigJSON(cfg, raw)
		}
	}
	return cfg
}

func applyPolicyOverrides(cfg *Config, overrides []PolicyOverride) {
	byID := make(map[string]int, len(cfg.Policies))
	for i, p := range cfg.Policies {
		byID[p.ID] = i
	}
	for _, o := range overrides {
		if o.ID == "" {
			continue
		}
		idx, ok := byID[o.ID]
		if !ok {
			continue
		}
		p := &cfg.Policies[idx]
		if o.Enabled != nil {
			p.Enabled = *o.Enabled
		}
		if o.MaxExtraAttempts != nil {
			p.MaxExtraAttempts = *o.MaxExtraAttempts
		}
		if len(o.DelaysMs) > 0 {
			p.DelaysMs = append([]int(nil), o.DelaysMs...)
		}
		if len(o.SessionStrategy) > 0 {
			p.SessionStrategy = append([]SessionRetryMode(nil), o.SessionStrategy...)
		}
	}
}

func policyMatchesProvider(policy Policy, provider string) bool {
	for _, p := range policy.Providers {
		if p == "*" || p == provider {
			return true
		}
	}
	return false
}

func findPolicy(cfg Config, provider string, errText string) (Policy, bool) {
	for _, p := range cfg.Policies {
		if !p.Enabled || p.MatchError == nil {
			continue
		}
		if !policyMatchesProvider(p, provider) {
			continue
		}
		if p.MatchError(errText) {
			return p, true
		}
	}
	return Policy{}, false
}

// totalLaunches is the maximum backend launches for a policy. SessionStrategy
// entries describe the session mode for each retry after a failure; the
// initial launch is not listed, so budget is 1 + len(SessionStrategy).
func totalLaunches(policy Policy) int {
	n := len(policy.SessionStrategy)
	if n == 0 {
		return 1 + policy.MaxExtraAttempts
	}
	return 1 + n
}

func delayForLaunch(policy Policy, launchIndex int) int {
	if launchIndex < len(policy.DelaysMs) {
		return policy.DelaysMs[launchIndex]
	}
	if len(policy.DelaysMs) > 0 {
		return policy.DelaysMs[len(policy.DelaysMs)-1]
	}
	return 0
}

func sessionModeForLaunch(policy Policy, launchIndex int) SessionRetryMode {
	if launchIndex < len(policy.SessionStrategy) {
		return policy.SessionStrategy[launchIndex]
	}
	if len(policy.SessionStrategy) > 0 {
		return policy.SessionStrategy[len(policy.SessionStrategy)-1]
	}
	return SessionRetryCold
}
