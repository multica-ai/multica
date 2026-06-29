package agentvault

import "strings"

// SpawnEnv is the per-agent environment the broker may inject at claim time.
//
// FIR-2210 removed the agent-side forward-proxy transport (HTTPS_PROXY relay),
// so the broker now injects nothing — PrepareSpawnEnv returns nil. The type is
// retained as the claim-hook seam that the connections-backed credential path
// (FIR-2166 server-side connection proxy) builds on.
type SpawnEnv map[string]string

// AllowedVaults returns the distinct vault names from an access list, in input
// order, skipping blanks. Used to scope a per-agent grant to exactly the boxes
// the admin granted.
func AllowedVaults(access []Access) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, a := range access {
		v := strings.TrimSpace(a.Vault)
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
