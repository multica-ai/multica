// FIR-4012 — the workspace capability digest.
//
// The watcher used to emit one inbox card per agent ("New capabilities: <agent>"),
// which meant a workspace with twenty agents produced twenty cards a night and
// none of them could be answered. This file holds the pure half of the
// replacement: fold every agent's scan into ONE list keyed by capability, so a
// reader sees "this capability, these agents, this action" instead of walking
// agent by agent.
//
// Two deliberate narrowings live here:
//
//  1. Only capabilities NO permission sanctions reach the digest — unmapped (no
//     policy row exists at all) and blocked (a deny row exists and the agent used
//     the tool anyway). An "allow" or "ask" row means the capability is already
//     governed; it is not a finding. This is the "kun ting der ikke findes
//     permissions til" rule.
//  2. unmapped and blocked stay SEPARATE rows even for the same tool. They are
//     different failures — one is an ungoverned capability, the other is a rule
//     that is not holding — and they need different actions, so collapsing them
//     would hide the more serious one behind the more common one.
//
// Everything here is pure so the sweeper and its tests share one definition and
// the fan-in is unit-testable without a database.
package driftwatch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// digestTableLimit caps how many rows the issue table renders. Anything beyond
// it is reported as an explicit "and N more" line — a silently truncated table
// reads as "this is everything" when it is not.
const digestTableLimit = 50

// digestAgentsPerRow caps how many agent names one table cell lists before it
// collapses to "+N more". Keeps cells readable on mobile (repo CLAUDE.md: keep
// cell content under ~60 characters).
const digestAgentsPerRow = 4

// digestSignatureMarker is an HTML comment embedded in the issue description so
// the next sweep can tell whether the finding set actually changed. Storing the
// fingerprint in the body avoids a schema change and survives edits to the
// prose around it.
const digestSignatureMarker = "cerebro-capability-digest-signature"

// DigestAgent is one agent that used a capability its policy does not sanction.
type DigestAgent struct {
	ID   string
	Name string
	Uses int64
	// New reports that this scan is the first time this agent has been seen
	// using the capability at all.
	New bool
}

// DigestRow is one (capability, status) finding folded across every agent that
// hit it — the unit the issue table renders as a single line.
type DigestRow struct {
	Key    string
	Name   string
	Status string // statusUnmapped | statusBlocked
	Uses   int64  // summed across Agents
	New    bool   // true when at least one agent hit it for the first time
	Agents []DigestAgent
}

// agentFindings is one agent's contribution to the digest: the agent's identity
// plus the capabilities of its scan that no permission sanctions.
type agentFindings struct {
	AgentID   string
	AgentName string
	Caps      []observedCapability
}

// unpermittedCapabilities keeps only the capabilities that no permission
// sanctions. Runs over the FULL observed set of the scan window, not just the
// newly discovered ones: an ungoverned capability that nobody actioned last
// night is still ungoverned tonight, and dropping it from the digest because it
// is no longer "new" would quietly retire the finding without anyone deciding
// anything.
func unpermittedCapabilities(observed []observedCapability) []observedCapability {
	out := make([]observedCapability, 0, len(observed))
	for _, cap := range observed {
		if cap.Status == statusUnmapped || cap.Status == statusBlocked {
			out = append(out, cap)
		}
	}
	return out
}

// buildDigest folds every agent's findings into one row per (capability,
// status). Rows sort blocked-first (a deny that is not holding outranks an
// ungoverned tool), then by total uses descending, then by name — so the order
// is stable across runs and the signature below is order-insensitive in
// practice as well as by construction.
func buildDigest(findings []agentFindings) []DigestRow {
	type rowKey struct{ key, status string }
	index := map[rowKey]*DigestRow{}
	order := []rowKey{}

	for _, f := range findings {
		for _, cap := range f.Caps {
			rk := rowKey{key: cap.Key, status: cap.Status}
			row, ok := index[rk]
			if !ok {
				row = &DigestRow{Key: cap.Key, Name: cap.Name, Status: cap.Status}
				index[rk] = row
				order = append(order, rk)
			}
			row.Uses += cap.Uses
			row.New = row.New || cap.IsNew
			row.Agents = append(row.Agents, DigestAgent{
				ID:   f.AgentID,
				Name: f.AgentName,
				Uses: cap.Uses,
				New:  cap.IsNew,
			})
		}
	}

	out := make([]DigestRow, 0, len(order))
	for _, rk := range order {
		row := index[rk]
		sort.Slice(row.Agents, func(i, j int) bool {
			if row.Agents[i].Uses != row.Agents[j].Uses {
				return row.Agents[i].Uses > row.Agents[j].Uses
			}
			return row.Agents[i].Name < row.Agents[j].Name
		})
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Status == statusBlocked) != (out[j].Status == statusBlocked) {
			return out[i].Status == statusBlocked
		}
		if out[i].Uses != out[j].Uses {
			return out[i].Uses > out[j].Uses
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// digestSignature fingerprints a finding set so the sweeper re-notifies only
// when the set changes, instead of every night. Built from the sorted
// "<status>:<capability>:<agent-ids>" triples — a new agent hitting an existing
// capability is a change, a repeat of the same agents is not.
func digestSignature(rows []DigestRow) string {
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		ids := make([]string, 0, len(row.Agents))
		for _, a := range row.Agents {
			ids = append(ids, a.ID)
		}
		sort.Strings(ids)
		parts = append(parts, row.Status+":"+row.Key+":"+strings.Join(ids, "+"))
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, ",")))
	return hex.EncodeToString(sum[:])
}

// recommendedAction is the third column: what the reader should actually do
// about this row. autoPermission reports whether the workspace has the
// auto-permission flag on, because that changes the unmapped advice from
// "nothing governs this yet" to "a rule now exists, flip it to block".
func recommendedAction(row DigestRow, autoPermission bool) string {
	if row.Status == statusBlocked {
		return "Blocked by rule but used anyway — check enforcement"
	}
	if autoPermission {
		return "Rule created as Allow — reply *block* to deny it"
	}
	return "No rule exists — reply *allow* or *block*"
}

// formatDigestAgents renders the agents cell, capped so a capability used by
// every agent in the workspace does not produce an unreadable line.
func formatDigestAgents(agents []DigestAgent) string {
	names := make([]string, 0, len(agents))
	seen := map[string]bool{}
	for _, a := range agents {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			name = "Unnamed agent"
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) <= digestAgentsPerRow {
		return strings.Join(names, ", ")
	}
	rest := len(names) - digestAgentsPerRow
	return strings.Join(names[:digestAgentsPerRow], ", ") + fmt.Sprintf(" +%d more", rest)
}

// digestTitle is the issue title. The count is in the title so the issue list
// shows at a glance whether the number is moving.
func digestTitle(rows []DigestRow) string {
	return fmt.Sprintf("Agent capabilities without a permission rule (%d)", len(rows))
}

// renderDigestBody builds the issue description: one table with the three
// columns the request asks for (capability, agents, recommended action), plus
// the use count so a reader can tell a one-off from a habit.
//
// window is rendered as plain UTC timestamps — this body is machine-written and
// read by admins across timezones, so an unambiguous absolute time beats a
// localized one.
func renderDigestBody(rows []DigestRow, windowStart, windowEnd string, autoPermission bool) string {
	var b strings.Builder

	b.WriteString("## Capabilities agents used without a permission rule\n\n")
	b.WriteString(fmt.Sprintf(
		"%d capabilit%s across the workspace, observed between `%s` and `%s`.\n\n",
		len(rows), plural2(len(rows)), windowStart, windowEnd,
	))
	b.WriteString("Only capabilities that **no rule sanctions** are listed: either no rule exists (unmapped), or a rule denies it and the agent used it anyway (blocked). Capabilities already set to allow or ask are governed and are left out.\n\n")

	b.WriteString("| Capability | Status | Agents | Uses | Recommended action |\n")
	b.WriteString("|---|---|---|---|---|\n")

	shown := rows
	if len(shown) > digestTableLimit {
		shown = shown[:digestTableLimit]
	}
	for _, row := range shown {
		name := row.Name
		if row.New {
			name += " ⁿᵉʷ"
		}
		b.WriteString(fmt.Sprintf(
			"| `%s` | %s | %s | %d | %s |\n",
			name, row.Status, formatDigestAgents(row.Agents), row.Uses, recommendedAction(row, autoPermission),
		))
	}
	if len(rows) > len(shown) {
		b.WriteString(fmt.Sprintf("\n_And %d more not shown in this table._\n", len(rows)-len(shown)))
	}

	b.WriteString("\n`ⁿᵉʷ` marks a capability seen for the first time in this scan.\n")

	b.WriteString("\n### How to act\n\n")
	b.WriteString("Reply on this issue with the capability and what should happen — for example `block Bash for Mia` or `allow WebFetch`. ")
	b.WriteString("You can also set the rule directly on the agent's Capabilities tab.\n")
	if autoPermission {
		b.WriteString("\nEvery unmapped capability above already has a rule created for it, set to **allow** — the capability is now steerable without changing what the agent could do. Set it to **deny** to block it.\n")
	} else {
		b.WriteString("\nNo rules were created automatically. Turn on **Auto-create capability rules** in Cerebro features to have every unmapped capability get a rule the moment it is discovered.\n")
	}
	return b.String()
}

// embedDigestSignature appends the fingerprint marker to a rendered body.
func embedDigestSignature(body, signature string) string {
	return body + fmt.Sprintf("\n<!-- %s: %s -->\n", digestSignatureMarker, signature)
}

// extractDigestSignature reads the fingerprint back out of an existing issue
// description. Returns "" when the marker is absent (an issue created before
// this marker existed, or a human-rewritten body) — which makes the caller fall
// back to "treat as changed", i.e. notify. Failing towards notifying is the
// safe direction for a security signal.
func extractDigestSignature(body string) string {
	prefix := "<!-- " + digestSignatureMarker + ": "
	start := strings.Index(body, prefix)
	if start < 0 {
		return ""
	}
	rest := body[start+len(prefix):]
	end := strings.Index(rest, " -->")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

// unmappedCapabilityKeys returns the capability keys of one agent's findings
// that have no policy row at all — the set the auto-permission writer creates
// rules for. Blocked capabilities are excluded: a rule already exists for them,
// and overwriting a deliberate deny with an allow would be the opposite of what
// the reader asked for.
func unmappedCapabilityKeys(caps []observedCapability) []string {
	keys := make([]string, 0, len(caps))
	seen := map[string]bool{}
	for _, cap := range caps {
		if cap.Status != statusUnmapped || cap.Key == "" || seen[cap.Key] {
			continue
		}
		seen[cap.Key] = true
		keys = append(keys, cap.Key)
	}
	sort.Strings(keys)
	return keys
}

func plural2(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
