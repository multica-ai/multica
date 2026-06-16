// Package webfetchpolicy holds the per-workspace web_fetch URL policy (TECH-3522).
//
// A workspace admin chooses a mode — allow-list (only listed hosts may be
// fetched) or disallow-list (everything except listed hosts) — and a list of
// host rules. The gateway web_fetch tool resolves the effective policy on every
// call and rejects a host the policy does not permit, with an error that
// explains the active mode so the agent can tell the user why.
//
// A workspace that has never configured a policy uses Default(): an allow-list
// seeded with the legacy hardcoded hosts, so behaviour is unchanged on deploy.
package webfetchpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// Mode values.
const (
	ModeAllow    = "allow"    // only hosts matching a rule may be fetched
	ModeDisallow = "disallow" // every host may be fetched except those matching a rule
)

// FlagKey is the cerebro feature flag (registry.ts) that gates this feature.
// Default ON: when off, the gateway falls back to Default() (legacy behaviour).
const FlagKey = "cerebro_web_fetch_policy"

// DefaultHosts mirrors the legacy hardcoded allowlist in the gateway web_fetch
// tool. When a workspace has never configured a policy, this is the effective
// allow-list — so there is no regression for existing workspaces.
var DefaultHosts = []string{"firtal.com", "docs.anthropic.com"}

// Policy is the resolved, wire-facing web_fetch policy for a workspace.
type Policy struct {
	Mode  string   `json:"mode"`
	Rules []string `json:"rules"`
}

// Default returns the seeded allow-list applied when a workspace has no row.
func Default() Policy {
	return Policy{Mode: ModeAllow, Rules: append([]string{}, DefaultHosts...)}
}

// HostMatchesRule reports whether host matches a single rule. A rule is a bare
// host ("github.com") or a subdomain wildcard ("*.github.com"). A bare host
// matches the host itself and any subdomain; "*.host" matches subdomains only.
// This mirrors the legacy suffix logic so the seeded defaults behave as before.
func HostMatchesRule(host, rule string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	rule = strings.ToLower(strings.TrimSpace(rule))
	if host == "" || rule == "" {
		return false
	}
	if strings.HasPrefix(rule, "*.") {
		base := rule[2:]
		return base != "" && strings.HasSuffix(host, "."+base)
	}
	base := strings.TrimPrefix(rule, ".")
	if base == "" {
		return false
	}
	return host == base || strings.HasSuffix(host, "."+base)
}

// Allows reports whether the policy permits fetching the given host.
func (p Policy) Allows(host string) bool {
	matched := false
	for _, r := range p.Rules {
		if HostMatchesRule(host, r) {
			matched = true
			break
		}
	}
	if p.Mode == ModeDisallow {
		return !matched
	}
	return matched
}

// Normalize cleans a policy for storage: defaults an unknown mode to allow and
// trims/lowercases/dedupes rules, dropping empties.
func Normalize(mode string, rules []string) Policy {
	if mode != ModeAllow && mode != ModeDisallow {
		mode = ModeAllow
	}
	seen := make(map[string]struct{}, len(rules))
	out := make([]string, 0, len(rules))
	for _, r := range rules {
		r = strings.ToLower(strings.TrimSpace(r))
		if r == "" {
			continue
		}
		if _, dup := seen[r]; dup {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return Policy{Mode: mode, Rules: out}
}

// fromRow decodes a DB row into a Policy.
func fromRow(row cerebrodb.CerebroWebFetchPolicy) Policy {
	rules := []string{}
	if len(row.HostRules) > 0 {
		_ = json.Unmarshal(row.HostRules, &rules)
	}
	return Normalize(row.Mode, rules)
}

// Load returns the stored policy for a workspace, or Default() when none exists.
// A DB error other than "no rows" is returned so the caller can decide.
func Load(ctx context.Context, cerebro *cerebrodb.Queries, workspaceID pgtype.UUID) (Policy, error) {
	if cerebro == nil || !workspaceID.Valid {
		return Default(), nil
	}
	row, err := cerebro.GetCerebroWebFetchPolicy(ctx, workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Default(), nil
	}
	if err != nil {
		return Default(), err
	}
	return fromRow(row), nil
}

// FeatureEnabled reports whether the per-workspace policy feature is active for
// a workspace. The flag default is ON (registry.ts); a missing override row or
// any lookup failure resolves to enabled so the feature is not silently dropped.
func FeatureEnabled(ctx context.Context, cerebro *cerebrodb.Queries, workspaceID pgtype.UUID) bool {
	if cerebro == nil || !workspaceID.Valid {
		return true
	}
	rows, err := cerebro.ListCerebroWorkspaceFeatureFlags(ctx, workspaceID)
	if err != nil {
		return true
	}
	for _, row := range rows {
		if row.FlagKey == FlagKey {
			return row.Enabled
		}
	}
	return true
}

// Effective resolves the policy the gateway should enforce for a workspace.
// When the feature flag is off it returns Default() (legacy behaviour); when on
// it returns the stored policy (or Default() when none is configured).
func Effective(ctx context.Context, cerebro *cerebrodb.Queries, workspaceID pgtype.UUID) Policy {
	if !FeatureEnabled(ctx, cerebro, workspaceID) {
		return Default()
	}
	p, _ := Load(ctx, cerebro, workspaceID)
	return p
}
