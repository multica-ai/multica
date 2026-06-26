"use client";

// Agent Office (FIR-1775) — the "Context" detail tab mounted on the agent
// settings page. Mirrors the cerebro-agent-tools tab-extension pattern: a
// factory returns an AgentDetailTabExtension that agent-overview-pane spreads
// into its tab list and renders by id.

import type { ComponentType, ReactNode } from "react";
import { GitBranch } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { AgentContextVersionsPanel } from "./components/agent-context-versions-panel";
import { AgentContextChangeRequestQueue } from "./components/agent-context-change-request-queue";
import { AgentContextProposeDialog } from "./components/agent-context-propose-dialog";

export interface AgentDetailTabExtension {
  id: string;
  labelKey: "context";
  icon: ComponentType<{ className?: string }>;
  render: (context: {
    agent: Agent;
    runtimes: AgentRuntime[];
    canEdit: boolean;
  }) => ReactNode;
}

export function createAgentContextTabs(): AgentDetailTabExtension[] {
  return [
    {
      id: "context",
      labelKey: "context",
      icon: GitBranch,
      render: ({ agent, canEdit }) => (
        <CerebroAgentContextTab agent={agent} canEdit={canEdit} />
      ),
    },
  ];
}

export function CerebroAgentContextTab({
  agent,
  canEdit = true,
}: {
  agent: Agent;
  canEdit?: boolean;
}) {
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  const isOwner = !!(userId && agent.owner_id && agent.owner_id === userId);
  const canManage = canEdit || isOwner;

  return (
    <div className="space-y-5 p-4 md:p-6">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold">Agent Office</h3>
          <p className="mt-0.5 max-w-prose text-xs text-muted-foreground">
            This agent&apos;s instructions and configuration are versioned and
            reviewable. Propose a change, review pending proposals, compare
            versions, and roll back — so the harness stays auditable instead of
            drifting.
          </p>
        </div>
        <AgentContextProposeDialog agent={agent} />
      </div>

      <div className="my-1 h-px bg-border" />

      <div className="space-y-5">
        <AgentContextChangeRequestQueue
          agent={agent}
          wsId={wsId}
          members={members}
          canReview={canManage}
        />
        <AgentContextVersionsPanel
          agent={agent}
          wsId={wsId}
          members={members}
          canManage={canManage}
        />
      </div>
    </div>
  );
}
