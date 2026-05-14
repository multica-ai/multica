// CEREBRO: TypeScript module augmentation. Add fields to upstream interfaces
// here instead of editing the upstream file — keeps the upstream sync clean
// while still surfacing cerebro-only fields with full type safety on every
// consumer (apps/web, apps/desktop, packages/views, packages/cerebro-*).

import type {} from "@multica/core/types/agent";

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
  }
}

// JEH-1284/1290: Agent tool grants — W3 registry + W8 UI.
// AgentTool is a cerebro-only concept (agent_tool_grant table).
export interface AgentTool {
  name: string;
  description: string;
  enabled: boolean;
  config: Record<string, unknown>;
}

export {};
