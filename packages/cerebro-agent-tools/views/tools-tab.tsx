"use client";

import type { ComponentType, ReactNode } from "react";
import { Wrench } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { api } from "@multica/core/api";
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
