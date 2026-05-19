// CEREBRO: TypeScript module augmentation. Add fields to upstream interfaces
// here instead of editing the upstream file — keeps the upstream sync clean
// while still surfacing cerebro-only fields with full type safety on every
// consumer (apps/web, apps/desktop, packages/views, packages/cerebro-*).

import type {} from "@multica/core/types/agent";
import type {} from "@multica/core/types/autopilot";

// JEH-848 runtime pause/unpause fields on the upstream RuntimeDevice
// interface. Server adds the columns via 9016_cerebro_runtime_pause and
// surfaces them on AgentRuntimeResponse via the runtime-pause-response
// patch. All three fields are optional on the wire — older clients
// (and the upstream API contract) won't include them.
declare module "@multica/core/types/agent" {
  interface RuntimeDevice {
    /** Non-null when the runtime is paused. ISO timestamp set by the server. */
    paused_at?: string | null;
    /** Scheduled auto-unpause timestamp. Null = stay paused until manual unpause. */
    unpause_at?: string | null;
    /** Short slug for telemetry/UI: 'rate_limit', 'manual', 'maintenance', ... */
    pause_reason?: string | null;
    /**
     * JEH-999: the cerebro_account the daemon currently authenticates as.
     * Populated by JEH-997 daemon heartbeat; older daemons (and the upstream
     * API contract) omit it. RuntimeAccountsCard treats null/missing as
     * "konto ukendt — daemon har ikke rapporteret endnu".
     */
    current_account_id?: string | null;
    /**
     * 9031 (runtime-level MCP defaults): JSON document of shape
     * {"mcpServers": {...}} that the daemon merges with each agent's
     * mcp_config at task-claim time. Null/missing = no runtime-level
     * defaults (daemon falls back to agent.mcp_config alone).
     */
    tools_config?: unknown | null;
  }
}

// JEH-1310: per-autopilot model override. Server adds the column via
// 9029_cerebro_autopilot_model and surfaces it on AutopilotResponse. Field is
// nullable end-to-end ("" === null === fall back to the agent default).
declare module "@multica/core/types/autopilot" {
  interface Autopilot {
    model?: string | null;
  }
  interface CreateAutopilotRequest {
    model?: string;
  }
  interface UpdateAutopilotRequest {
    model?: string | null;
  }
}

// JEH-1284/1290: Agent tool grants — W3 registry + W8 UI.
// AgentTool is a cerebro-only concept (agent_tool_grant table).
export interface AgentTool {
  name: string;
  description: string;
  status: "implemented" | "newly_implemented" | "explicitly_excluded";
  enabled: boolean;
  config: Record<string, unknown>;
}

// JEH-1710: Runtime-level unified tool inventory. One row per (runtime, tool)
// regardless of whether the tool comes from the cloud gateway (`cloud`) or a
// daemon-discovered MCP server (`mcp`, with `mcp_server_name` filled). The UI
// blends both sources in one table — the source is a badge, not a section.
export interface RuntimeTool {
  id: string;
  runtime_id: string;
  name: string;
  source: string; // "cloud" | "mcp" | future values — switch on with a default branch.
  mcp_server_name: string;
  description: string;
  enabled: boolean;
  last_scanned_at: string | null;
}

export interface RuntimeToolGroupGrant {
  runtime_id: string;
  tool_name: string;
  group_id: string;
  group_name: string;
  granted_at: string;
}

export interface RuntimeToolUserGrant {
  runtime_id: string;
  tool_name: string;
  user_id: string;
  user_name: string;
  user_email: string;
  user_avatar_url: string;
  granted_at: string;
}

export interface RuntimeToolGrants {
  group_grants: RuntimeToolGroupGrant[];
  user_grants: RuntimeToolUserGrant[];
}

// JEH-1710 (bid 4): per-agent override on top of the runtime-level tool grant.
// One row per (agent_id, tool_name) with `enabled` forcing the effective state
// either on or off — the absence of a row means the agent inherits the runtime
// default. The override never widens beyond a runtime that has the tool off
// for everyone; it expresses the agent-specific exception inside the rules the
// runtime already permits.
export interface AgentToolOverride {
  agent_id: string;
  tool_name: string;
  enabled: boolean;
  updated_at: string;
}

export {};
