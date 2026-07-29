"use client";

// Agent Office (FIR-1775) — the "Context" detail tab mounted on the agent
// settings page. Mirrors the cerebro-agent-tools tab-extension pattern: a
// factory returns an AgentDetailTabExtension that agent-overview-pane spreads
// into its tab list and renders by id.

import type { ComponentType, ElementType, ReactNode } from "react";
import { GitBranch } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { AgentContextVersionsPanel } from "./components/agent-context-versions-panel";
import { AgentContextChangeRequestQueue } from "./components/agent-context-change-request-queue";
import { AgentContextProposeDialog } from "./components/agent-context-propose-dialog";
import { AgentContextObservabilityPanel } from "./components/agent-context-observability-panel";
import { AgentContextRuntimeSupportPanel } from "./components/agent-context-runtime-support-panel";
import { AgentContextEffectivePromptPanel } from "./components/agent-context-effective-prompt-panel";
import { AgentContextSwapPanel } from "./components/agent-context-swap-panel";

export interface AgentInstructionsEditorProps {
  defaultValue?: string;
  onUpdate?: (markdown: string) => void;
  placeholder?: string;
  className?: string;
  debounceMs?: number;
  disableMentions?: boolean;
}

export type AgentInstructionsEditor = ElementType<AgentInstructionsEditorProps>;

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
      render: ({ agent, runtimes, canEdit }) => (
        <CerebroAgentContextTab
          agent={agent}
          runtimes={runtimes}
          canEdit={canEdit}
        />
      ),
    },
  ];
}

export function CerebroAgentContextTab({
  agent,
  runtimes = [],
  canEdit = true,
  instructionsEditor,
}: {
  agent: Agent;
  runtimes?: AgentRuntime[];
  canEdit?: boolean;
  instructionsEditor?: AgentInstructionsEditor;
}) {
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  const isOwner = !!(userId && agent.owner_id && agent.owner_id === userId);
  const canManage = canEdit || isOwner;
  const runtimeSupportOn = useFeatureFlag("cerebro_agent_setup_capabilities");

  return (
    <div className="space-y-5 p-4 md:p-6">
      <AgentContextProposeDialog
        agent={agent}
        canReview={canManage}
        controlFirst={runtimeSupportOn}
        instructionsEditor={instructionsEditor}
      />

      {runtimeSupportOn && (
        <details className="rounded-xl border bg-card">
          <summary className="cursor-pointer px-4 py-3 text-xs font-medium">
            Advanced: engine compatibility and swap checks
          </summary>
          <div className="grid gap-4 border-t p-4 xl:grid-cols-2">
            <AgentContextRuntimeSupportPanel agent={agent} />
            <AgentContextEffectivePromptPanel agent={agent} />
            <AgentContextSwapPanel agent={agent} runtimes={runtimes} />
          </div>
        </details>
      )}

      <div className="my-1 h-px bg-border" />

      <div className="space-y-5">
        <AgentContextObservabilityPanel agent={agent} />
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
