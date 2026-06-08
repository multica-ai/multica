package execenv

// cerebroWakeupBrief returns the Agent Wakeup subsection injected into the
// "Available Commands" block of the agent brief. Kept in its own
// cerebro-prefixed sibling file so the upstream file (runtime_config.go) only
// needs a single marked call-site rather than a multi-line inline patch.
func cerebroWakeupBrief() string { // CEREBRO-PATCH(cerebro-wakeup-brief): TECH-3013 new sibling file — Agent Wakeup section for Available Commands
	return `### Agent Wakeup (self-scheduling)

Schedule a future wakeup so you are re-invoked on an issue without burning an active run.

- ` + "`multica wakeup create --agent <agent-uuid> --issue <issue-uuid> --prompt \"...\" --at <RFC3339>`" + ` — one-shot at a specific UTC time.
- ` + "`multica wakeup create --agent <agent-uuid> --issue <issue-uuid> --prompt \"...\" --on-issue-status <sub-uuid> --watch-status done`" + ` — fires when another issue reaches the given status.
- ` + "`multica wakeup create --agent <agent-uuid> --issue <issue-uuid> --prompt \"...\" --on-github-ci <issue-uuid>`" + ` — fires when a linked GitHub CI run updates.
- ` + "`multica wakeup list [--state pending|dispatched] [--output json]`" + ` — list your wakeups.
- ` + "`multica wakeup cancel <id>`" + ` — cancel a pending wakeup.

` + "`--agent`" + ` defaults to the ` + "`MULTICA_AGENT_ID`" + ` environment variable (already set in every agent session — omit unless waking a different agent).

**Also available as MCP tools:** ` + "`schedule_wakeup`" + `, ` + "`list_wakeups`" + `, ` + "`cancel_wakeup`" + ` — prefer MCP when available (no shell required).

**IMPORTANT:** use these commands/tools — do NOT modify the heartbeat autopilot description as a workaround. That approach is deprecated.

`
}
