package daemon

// effective_tools_cerebro.go (FIR-2312) carries the per-agent non-CLI tool set
// (MCP tools + connection tools) from the claim response into the execenv
// context, where cerebroToolsBrief renders it into the agent brief's
// `## Available Commands` block. The server resolved and filtered these from the
// tool-policy chain at claim time (see handler/daemon_effective_tools_cerebro.go)
// — the daemon only maps the wire shape to the execenv shape.

import "github.com/multica-ai/multica/server/internal/daemon/execenv"

// TaskToolBriefEntry is the wire shape of one resolved non-CLI tool in the claim
// response. It mirrors handler.AgentTaskToolEntry and execenv.ToolBriefEntry.
type TaskToolBriefEntry struct {
	Family      string `json:"family"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Verdict     string `json:"verdict,omitempty"`
}

// effectiveToolsForEnv converts the claim-response tool entries into the execenv
// brief shape. Returns nil when there are none, so the brief renders no section.
func effectiveToolsForEnv(entries []TaskToolBriefEntry) []execenv.ToolBriefEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]execenv.ToolBriefEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, execenv.ToolBriefEntry{
			Family:      e.Family,
			Name:        e.Name,
			Description: e.Description,
			Verdict:     e.Verdict,
		})
	}
	return out
}
