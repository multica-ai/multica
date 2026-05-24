"use client";

// CEREBRO-PATCH(agent-tools-tab): JEH-1710 — agent override UI on top of
// runtime-level tool inventory. Replaces the previous AgentTool toggle list
// (which read agent_tool_grant directly) with the runtime-default + per-agent
// override model. The body lives in @multica/cerebro-runtime/views so the
// upstream-zone footprint stays minimal — this wrapper just decides admin gate
// + resolves the runtime name shown in the inherit banner.
// Local AND cloud runtime agents are supported: both inherit from runtime-level
// tools (cloud built-ins + scanned MCP-tools) and both can be overridden.

import { useQuery } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { api } from "@multica/core/api";
import { AgentToolsCard } from "@multica/cerebro-runtime/views";

const runtimeListKey = (wsId: string) =>
  ["workspace", "agent-runtimes", wsId] as const;

export function CerebroToolsTab({
  agent,
  canEdit = true,
}: {
  agent: Agent;
  canEdit?: boolean;
}) {
  const wsId = useWorkspaceId();

  const { data: runtimes = [] } = useQuery({
    queryKey: runtimeListKey(wsId),
    queryFn: () => api.listRuntimes({ workspace_id: wsId }),
    enabled: !!wsId,
  });

  const runtime = runtimes.find((r) => r.id === agent.runtime_id);

  return (
    <AgentToolsCard
      agent={agent}
      canEdit={canEdit}
      runtimeName={runtime?.name}
    />
  );
}
