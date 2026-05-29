"use client";

import type { ComponentType, ReactNode } from "react";
import { Wrench } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { api } from "@multica/core/api";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  SimpleToolPolicyTable,
  ToolPolicyTable,
} from "@multica/cerebro-tool-policy/views";
import { AgentToolsCard } from "./components/agent-tools-card";

export interface AgentDetailTabExtension {
  id: string;
  labelKey: "tools";
  icon: ComponentType<{ className?: string }>;
  render: (context: {
    agent: Agent;
    runtimes: AgentRuntime[];
    canEdit: boolean;
  }) => ReactNode;
}

const runtimeListKey = (wsId: string) =>
  ["workspace", "agent-runtimes", wsId] as const;

export function createAgentToolsTabs(): AgentDetailTabExtension[] {
  return [
    {
      id: "tools",
      labelKey: "tools",
      icon: Wrench,
      render: ({ agent, canEdit }) => (
        <CerebroToolsTab agent={agent} canEdit={canEdit} />
      ),
    },
  ];
}

export function CerebroToolsTab({
  agent,
  canEdit = true,
}: {
  agent: Agent;
  canEdit?: boolean;
}) {
  const wsId = useWorkspaceId();
  // FIR-2230: when the unified per-tool permission table is enabled, the agent
  // Tools tab shows the Runtime › Agent › Group › User chain with one
  // Allow/Ask/Deny/Inherit control per tool and a server-resolved Effective
  // column — replacing the prior force-on/off override card. The flag defaults
  // off, so production keeps the old card until the new chain is verified live.
  const unifiedToolPolicy = useFeatureFlag("cerebro_tool_policy");
  // FIR-2358 (Decision A): the simple, user-facing table is the default surface
  // — one Allow/Ask/Block toggle per tool, grouped into four sections. It reuses
  // the same data layer and writes only the agent layer. The rich table above
  // stays the explicit power-view: when its flag is on it wins, so turning
  // cerebro_tool_policy back on restores the full Effective chain.
  const simpleToolPolicy = useFeatureFlag("cerebro_simple_tool_policy");

  const { data: runtimes = [] } = useQuery({
    queryKey: runtimeListKey(wsId),
    queryFn: () => api.listRuntimes({ workspace_id: wsId }),
    enabled: !!wsId,
  });

  const runtime = runtimes.find((r) => r.id === agent.runtime_id);

  if (unifiedToolPolicy) {
    // The agent page edits the "This agent" layer; the runtime column is shown
    // read-only as the inherited layer below. The user ceiling is bound to the
    // agent's owner — the principal whose access the agent runs under — so the
    // Effective column shows the full Runtime › Agent › Group › User chain,
    // including any "Capped by user"/"Capped by group". The owner's real group
    // memberships are expanded server-side from the user id (the table never
    // trusts a client-supplied group list).
    return (
      <ToolPolicyTable
        wsId={wsId}
        view="agent"
        subjectId={agent.id}
        runtimeId={agent.runtime_id}
        userId={agent.owner_id}
      />
    );
  }

  if (simpleToolPolicy) {
    // The simple table edits the "This agent" layer and inherits everything
    // below; the owner is bound as the user ceiling so inherited rows resolve
    // their Effective verdict server-side, same as the rich agent view.
    return (
      <SimpleToolPolicyTable
        wsId={wsId}
        agentId={agent.id}
        runtimeId={agent.runtime_id}
        userId={agent.owner_id}
      />
    );
  }

  return (
    <AgentToolsCard
      agent={agent}
      canEdit={canEdit}
      runtimeName={runtime?.name}
    />
  );
}
