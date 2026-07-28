"use client";

import type { ComponentType, ReactNode } from "react";
import { Wrench } from "lucide-react";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { ToolPolicyTabs } from "@multica/cerebro-tool-policy/views";

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
}: {
  agent: Agent;
  canEdit?: boolean;
}) {
  const wsId = useWorkspaceId();

  // The Agent page authors the same Agent layer that Settings, Roles,
  // Capabilities and call-time enforcement resolve. The former simplified
  // table and runtime override card were separate presentations of the same
  // data and could explain the result differently, so this is now the only
  // Agent permission surface.
  return (
    <ToolPolicyTabs
      wsId={wsId}
      view="agent"
      subjectId={agent.id}
      runtimeId={agent.runtime_id}
      userId={agent.owner_id}
    />
  );
}
