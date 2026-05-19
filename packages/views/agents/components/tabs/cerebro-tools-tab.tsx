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
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import { memberListOptions } from "@multica/core/workspace/queries";
import { AgentToolsCard } from "@multica/cerebro-runtime/views";

const runtimeListKey = (wsId: string) =>
  ["workspace", "agent-runtimes", wsId] as const;

export function CerebroToolsTab({ agent }: { agent: Agent }) {
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);

  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: !!wsId,
  });
  const { data: runtimes = [] } = useQuery({
    queryKey: runtimeListKey(wsId),
    queryFn: () => api.listRuntimes({ workspace_id: wsId }),
    enabled: !!wsId,
  });

  const currentMember = user ? members.find((m) => m.user_id === user.id) : null;
  const isAdmin = currentMember
    ? currentMember.role === "owner" || currentMember.role === "admin"
    : false;
  const runtime = runtimes.find((r) => r.id === agent.runtime_id);

  return (
    <AgentToolsCard
      agent={agent}
      canEdit={isAdmin}
      runtimeName={runtime?.name}
    />
  );
}
