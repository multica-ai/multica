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
)

// ToolBriefEntry is one non-CLI tool the agent currently has, already resolved
// to an Allow/Ask verdict server-side. Family groups entries under a heading
// (e.g. "MCP tools", "Connections"); Name is the exact tool name the agent
// calls verbatim; Description is the one-line, model-facing summary. Verdict is
// "allow" or "ask" — "ask" tools are listed but pause for human approval when
// called, mirroring how Ask works everywhere else.
type ToolBriefEntry struct {
	Family      string
	Name        string
	Description string
	Verdict     string
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
	if len(entries) == 0 {
		return ""
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
	b.WriteString("A **connection** is an external API or MCP server a workspace admin wired into this workspace (for example a customer-service backend or a data registry). Its tools show up below and you call them like any other tool. **MCP tools** are self-describing — read their schema for exact arguments. **API connection tools** (server-side HTTP endpoints, listed under **Connections**) take a fixed argument shape: path parameters at the top level, query parameters inside a `query` object, and the request body inside `body`. Passing query parameters at the top level instead of inside `query` drops them and the call fails — so you do not need to call once and read the error first. Tools marked `(ask)` pause for human approval when you call them.\n\n")

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
