package execenv

// cerebro_tools_brief.go renders the "Connections & MCP tools" subsection that
// is appended to the agent brief's `## Available Commands` block. Unlike the
// other cerebro*Brief siblings (which are static strings), this one is RENDERED
// from ctx.EffectiveTools — the per-agent set of non-CLI tools the server
// resolved from the tool-policy chain (Workspace › Runtime › Agent › Group ›
// User) at claim time. So the agent sees exactly the connections and MCP tools
// it is actually allowed to use right now: if a permission is removed the tool
// drops out of the resolved set and disappears from this section automatically.
//
// This is the single rendering chokepoint for the "agent understands every tool
// it has" pattern (FIR-2312). New tool families (future connections, MCP tools,
// platform tools) reach the brief by appearing in ctx.EffectiveTools — they do
// NOT need a new hand-written brief module. See docs/agents/agent-tool-brief.md.
//
// Kept in its own cerebro-prefixed sibling file so the upstream file
// (runtime_config.go) only needs a single marked call-site.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/connmeta"
)

// ToolBriefEntry is one non-CLI tool the agent currently has, already resolved
// to an Allow/Ask verdict server-side. Family groups entries under a heading
// (e.g. "MCP tools", "Connections"); Name is the exact tool name the agent
// calls verbatim; Description is the one-line, model-facing summary. Verdict is
// "allow" or "ask" — "ask" tools are listed but pause for human approval when
// called, mirroring how Ask works everywhere else.
type ToolBriefEntry struct {
	// CEREBRO-PATCH(connection-instructions-brief): FIR-2760 carries permission-scoped connection guidance.
	Family       string
	Name         string
	Description  string
	Verdict      string
	Connection   string
	Instructions string
}

// familyOrder fixes the heading order so the rendered section is deterministic
// (the brief must be byte-stable across identical inputs — see
// TestRuntimeBriefOmitsDynamicCurrentTime). Families not listed here sort
// alphabetically after the known ones.
var familyOrder = map[string]int{
	"MCP tools":   0,
	"Connections": 1,
}

// cerebroToolsBrief renders the dynamic tools subsection from the resolved
// entries. It returns "" when there are no entries — runtimes that do not ship
// a resolved tool set (or agents with no connections/MCP tools) get no section
// at all, exactly as today.
func cerebroToolsBrief(entries []ToolBriefEntry) string { // CEREBRO-PATCH(cerebro-tools-brief): FIR-2312 dynamic per-permission tools section for Available Commands
	// FIR-2441 (DoD 4): count connection-family tools so the first prompt can
	// say explicitly when the agent has zero of them. Platform MCP tools
	// (schedule_wakeup, …) are not connections and do not count here.
	connCount := 0
	for _, e := range entries {
		if strings.TrimSpace(e.Name) == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(e.Family), "Connections") {
			connCount++
		}
	}

	// zeroConnectionsNote turns silence into an explanation. An agent with no
	// connection tools could not otherwise tell "nothing is wired to me" (the
	// normal case) apart from "the tools or docs failed to load" (a bug). State
	// it, and point at the read-only diagnostic so the agent verifies instead of
	// guessing.
	zeroConnectionsNote := ""
	if connCount == 0 {
		zeroConnectionsNote = "**You currently have 0 connection tools.** That is the normal state unless a workspace admin has wired an API or MCP connection to you — it does not mean documentation or tools failed to load. Call `multica_connection_tools_status` (a read-only diagnostic) to confirm what, if anything, is wired to you.\n\n"
	}

	if len(entries) == 0 {
		// No non-CLI tools resolved at all. Render only the compact
		// zero-connections explanation — there is no tool list to show and no
		// argument-shape guidance to give — so the absence is explained, not
		// silent.
		return "### Connections & MCP tools (resolved live from your permissions)\n\n" + zeroConnectionsNote
	}

	// Group by family, preserving a stable family order and a stable
	// within-family order (by tool name).
	byFamily := map[string][]ToolBriefEntry{}
	for _, e := range entries {
		if strings.TrimSpace(e.Name) == "" {
			continue
		}
		fam := strings.TrimSpace(e.Family)
		if fam == "" {
			fam = "Other tools"
		}
		byFamily[fam] = append(byFamily[fam], e)
	}
	if len(byFamily) == 0 {
		return ""
	}

	fams := make([]string, 0, len(byFamily))
	for fam := range byFamily {
		fams = append(fams, fam)
	}
	sort.Slice(fams, func(i, j int) bool {
		oi, iok := familyOrder[fams[i]]
		oj, jok := familyOrder[fams[j]]
		switch {
		case iok && jok:
			return oi < oj
		case iok != jok:
			return iok // known families first
		default:
			return fams[i] < fams[j]
		}
	})

	var b strings.Builder
	b.WriteString("### Connections & MCP tools (resolved live from your permissions)\n\n")
	b.WriteString("Beyond the `multica` CLI commands above, these are the non-CLI tools you have **right now**. This list is built from your actual permissions when this task starts — if a tool is not listed, you do not have it, and if a permission is removed the tool disappears from here. The same Allow/Ask/Deny chain that decides this list also enforces every call, so the two can never disagree.\n\n")
	b.WriteString("A **connection** is an external API or MCP server a workspace admin wired into this workspace (for example a customer-service backend or a data registry). Its tools show up below and you call them like any other tool. **MCP tools** are self-describing — read their schema for exact arguments. " + connmeta.APIConnectionArgHint + " Tools marked `(ask)` pause for human approval when you call them.\n\n")

	// When the agent has MCP tools but no connection tools, state the zero
	// explicitly here too so the empty Connections family is never ambiguous.
	b.WriteString(zeroConnectionsNote)
	// CEREBRO-PATCH(connection-instructions-brief): Render each permitted connection's guidance once.
	instructions := map[string]string{}
	for _, e := range entries {
		if strings.TrimSpace(e.Connection) != "" && strings.TrimSpace(e.Instructions) != "" {
			instructions[strings.TrimSpace(e.Connection)] = strings.TrimSpace(e.Instructions)
		}
	}
	if len(instructions) > 0 {
		b.WriteString("**Connection instructions**\n")
		names := make([]string, 0, len(instructions))
		for name := range instructions {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			b.WriteString(fmt.Sprintf("- **%s**: %s\n", name, instructions[name]))
		}
		b.WriteString("\n")
	}

	for _, fam := range fams {
		list := byFamily[fam]
		sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
		b.WriteString(fmt.Sprintf("**%s**\n", fam))
		for _, e := range list {
			ask := ""
			if strings.EqualFold(strings.TrimSpace(e.Verdict), "ask") {
				ask = " _(ask)_"
			}
			desc := strings.TrimSpace(e.Description)
			if desc == "" {
				b.WriteString(fmt.Sprintf("- `%s`%s\n", e.Name, ask))
				continue
			}
			b.WriteString(fmt.Sprintf("- `%s`%s — %s\n", e.Name, ask, desc))
		}
		b.WriteString("\n")
	}

	return b.String()
}
