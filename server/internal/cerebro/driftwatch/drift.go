// Package driftwatch is the TECH-3738 Bid C capability-drift watcher: a periodic
// sweeper that compares what each agent ACTUALLY used (observed access, Bid B)
// against what its declared policy allows (Bid A), and alerts the workspace's
// owners/admins when an agent shows drift — a tool it used that the policy does
// not sanction (blocked or unmapped).
//
// The drift verdicts here mirror the card's observed-access logic
// (server/internal/handler/agent_capabilities_card_cerebro.go) on purpose: the
// watcher must flag exactly what a human sees flagged on the card. The pure
// comparison lives here so both the sweeper and its tests use one definition,
// and so it never claims more than the data proves — observed *secret* use is
// not tracked anywhere, so the watcher only ever reasons about tools.
package driftwatch

import (
	"sort"
	"strings"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	cerebrotoolpolicy "github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
)

// observedWindowDays is the look-back window for observed tool usage. Matches
// the capabilities card (Bid B) so the watcher and the card agree on "recent".
const observedWindowDays = 30

// Drift status verdicts — the subset of the card's observed-vs-declared verdicts
// that count as drift. A tool used despite a deny is "blocked"; a tool used with
// no policy row on record is "unmapped" (we cannot account for it). Both are
// genuine drift; allowed/needs-approval use is not.
const (
	statusBlocked  = "blocked"
	statusUnmapped = "unmapped"
)

// DriftTool is one tool an agent used that its declared policy does not sanction.
type DriftTool struct {
	Name       string
	Uses       int64
	Permission string // "deny" for blocked, "" for unmapped
	Status     string // blocked | unmapped
}

// permissionLookup indexes the declared policy rows by tool name so an observed
// tool ("Bash") matches its effective permission whether the row carries the
// display title ("Bash") or the key ("bash"). Lower-cased on both sides.
func permissionLookup(rows []cerebrotoolpolicy.TableRow) map[string]string {
	perm := make(map[string]string, len(rows)*2)
	for _, row := range rows {
		setting := string(row.Effective.Setting)
		if setting == "" {
			continue
		}
		if row.Title != "" {
			perm[strings.ToLower(row.Title)] = setting
		}
		if row.ToolKey != "" {
			perm[strings.ToLower(row.ToolKey)] = setting
		}
	}
	return perm
}

// driftStatus reports whether an observed tool's declared permission is drift,
// and which kind. Mirrors observedToolStatus on the capabilities card.
func driftStatus(permission string, hasRow bool) (status string, drift bool) {
	if !hasRow {
		return statusUnmapped, true
	}
	switch permission {
	case "allow", "ask":
		return "", false
	case "deny":
		return statusBlocked, true
	default:
		// Unknown/empty effective setting on a present row — cannot account for
		// it, treat as unmapped drift rather than silently passing.
		return statusUnmapped, true
	}
}

// computeDrift returns the observed tools that drift from the declared policy,
// sorted by use count (most-used first) then name for stable output/dedup. Pure
// — unit-tested without a DB.
func computeDrift(usage []cerebrodb.ListAgentObservedToolUsageRow, permByName map[string]string) []DriftTool {
	out := []DriftTool{}
	for _, u := range usage {
		perm, hasRow := permByName[strings.ToLower(u.Tool)]
		status, drift := driftStatus(perm, hasRow)
		if !drift {
			continue
		}
		out = append(out, DriftTool{Name: u.Tool, Uses: u.Uses, Permission: perm, Status: status})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Uses != out[j].Uses {
			return out[i].Uses > out[j].Uses
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// driftSignature is a stable fingerprint of a drift set, used to dedup alerts:
// the watcher re-alerts only when the set of drifting tools changes, not on
// every tick. Built from the sorted "<status>:<name>" pairs.
func driftSignature(drift []DriftTool) string {
	parts := make([]string, 0, len(drift))
	for _, d := range drift {
		parts = append(parts, d.Status+":"+d.Name)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// driftToolNames returns just the tool names, for the alert body.
func driftToolNames(drift []DriftTool) []string {
	names := make([]string, 0, len(drift))
	for _, d := range drift {
		names = append(names, d.Name)
	}
	return names
}
