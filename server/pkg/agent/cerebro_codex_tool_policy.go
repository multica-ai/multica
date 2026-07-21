package agent

import (
	"context"
	"encoding/json"
)

// handleCerebroToolPolicyDecision is the fork-owned Codex approval adapter.
// The upstream client keeps only the two-line seam that routes command and
// patch approvals here.
func (c *codexClient) handleCerebroToolPolicyDecision(id int, tool string, raw json.RawMessage) {
	var params map[string]any
	_ = json.Unmarshal(raw, &params)
	if c.cfg.ToolPolicy == nil {
		c.respond(id, map[string]any{"decision": "decline"})
		return
	}
	ctx := c.requestContext
	if ctx == nil {
		ctx = context.Background()
	}
	allowed, reason := c.cfg.ToolPolicy(ctx, tool, params)
	if allowed {
		c.respond(id, map[string]any{"decision": "accept"})
		return
	}
	c.cfg.Logger.Warn("codex: built-in blocked by Multica tool policy", "tool", tool, "reason", reason)
	c.respond(id, map[string]any{"decision": "decline"})
}
