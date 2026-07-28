// Firtal-owned OpenCode plugin: the mandatory before-call tool-policy gate.
//
// OpenCode has no provider-native hook file the daemon can write, so this
// plugin is its tool-policy adapter. `tool.execute.before` fires before every
// tool OpenCode runs — built-in and MCP alike — and throwing from it aborts the
// call, which is the same completeness Claude's PreToolUse hook gives.
//
// The plugin resolves each call against the daemon's loopback tool-policy IPC,
// exactly like the Pi harness. The daemon proxies to the server with its own
// credential, so no token ever enters the agent process.
//
// Every failure path denies: no daemon port, a non-OK response, or a transport
// error. An un-resolvable call must never become an allowed call — this gate is
// the only thing standing between an OpenCode agent and an unenforced run.

const policyPort = process.env.MULTICA_DAEMON_PORT || "";

const DENY_PREFIX = "Blocked by Multica tool policy";

async function resolvePolicy(toolName, args) {
  if (!policyPort) {
    return { blocked: true, reason: "Multica tool policy is unavailable (failing closed)" };
  }
  try {
    const response = await fetch(`http://127.0.0.1:${policyPort}/tool-policy/resolve`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        workspace_id: process.env.MULTICA_WORKSPACE_ID || "",
        agent_id: process.env.MULTICA_AGENT_ID || "",
        // The server rejects a resolve without a task id (400) and the daemon
        // turns that into a fail-closed deny, so omitting it would block every
        // call rather than gate it. The daemon injects MULTICA_TASK_ID into
        // every agent spawn.
        task_id: process.env.MULTICA_TASK_ID || "",
        tool_name: toolName,
        resource_pattern: "",
        args: args || {},
        stage: "enforce",
      }),
    });
    if (!response.ok) {
      return { blocked: true, reason: `tool policy returned HTTP ${response.status} (failing closed)` };
    }
    const decision = await response.json();
    if (!decision.allowed) {
      return { blocked: true, reason: decision.reason || DENY_PREFIX };
    }
    return { blocked: false };
  } catch (error) {
    return { blocked: true, reason: `tool policy unreachable: ${error?.message || error} (failing closed)` };
  }
}

export const MulticaToolPolicy = async () => ({
  "tool.execute.before": async (input, output) => {
    const decision = await resolvePolicy(input?.tool || "unknown", output?.args);
    if (decision.blocked) {
      throw new Error(`${DENY_PREFIX}: ${input?.tool || "unknown"} (${decision.reason})`);
    }
  },
});

export default MulticaToolPolicy;
