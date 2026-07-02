// FIR-1794 — the per-agent Memory tab on the agent detail page. Renders the
// caller's own two switches for this agent: may READ memory, may WRITE
// memory. Both default off; a 404 (workspace flag off, or the caller lacks
// create_memory) renders as a disabled explanation instead of a broken toggle
// — enum/permission drift downgrades gracefully (API Response Compatibility).

"use client";

import type { ComponentType, ReactNode } from "react";
import { BrainCircuit } from "lucide-react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { ApiError } from "@multica/core/api";
import { Switch } from "@multica/ui/components/ui/switch";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  getAgentMemorySettings,
  setAgentMemorySettings,
  type AgentMemorySettings,
} from "../api";

export interface AgentMemoryTabExtension {
  id: string;
  labelKey: "memory";
  icon: ComponentType<{ className?: string }>;
  render: (context: {
    agent: Agent;
    runtimes: AgentRuntime[];
    canEdit: boolean;
  }) => ReactNode;
}

const memorySettingsKey = (agentId: string) =>
  ["cerebro", "agent-memory-settings", agentId] as const;

export function createAgentMemoryTabs(): AgentMemoryTabExtension[] {
  return [
    {
      id: "memory",
      labelKey: "memory",
      icon: BrainCircuit,
      render: ({ agent, canEdit }) => (
        <CerebroAgentMemoryTab agent={agent} canEdit={canEdit} />
      ),
    },
  ];
}

export function CerebroAgentMemoryTab({
  agent,
  canEdit,
}: {
  agent: Agent;
  canEdit: boolean;
}) {
  const queryClient = useQueryClient();
  const { data, isLoading, error } = useQuery({
    queryKey: memorySettingsKey(agent.id),
    queryFn: () => getAgentMemorySettings(agent.id),
    enabled: !!agent.id,
    retry: false,
  });

  const mutation = useMutation({
    mutationFn: (next: { can_read_memory: boolean; can_write_memory: boolean }) =>
      setAgentMemorySettings(agent.id, next),
    onMutate: async (next) => {
      await queryClient.cancelQueries({ queryKey: memorySettingsKey(agent.id) });
      const previous = queryClient.getQueryData<AgentMemorySettings>(
        memorySettingsKey(agent.id),
      );
      queryClient.setQueryData(memorySettingsKey(agent.id), {
        agent_id: agent.id,
        can_read_memory: next.can_read_memory,
        can_write_memory: next.can_write_memory,
      });
      return { previous };
    },
    onError: (err, _next, context) => {
      if (context?.previous) {
        queryClient.setQueryData(memorySettingsKey(agent.id), context.previous);
      }
      toast.error(
        err instanceof ApiError && err.status === 403
          ? "You don't have the create_memory permission — ask a workspace admin."
          : "Failed to update memory settings.",
      );
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: memorySettingsKey(agent.id) });
    },
  });

  if (isLoading) {
    return <Skeleton className="h-24 w-full" />;
  }

  // A 404 here means the workspace hasn't turned memory on, or a lookup
  // failed server-side — either way there is nothing to toggle. Enum/error
  // drift downgrades to an explanatory empty state, not a broken switch.
  if (error) {
    return (
      <p className="text-sm text-muted-foreground">
        Memory is not available for this agent — either your workspace admin
        has not turned on the &quot;Agent &amp; member memory&quot; feature,
        or your group has not been granted the{" "}
        <code className="font-mono">create_memory</code> permission.
      </p>
    );
  }

  const settings = data ?? {
    agent_id: agent.id,
    can_read_memory: false,
    can_write_memory: false,
  };

  const onChange = (field: "can_read_memory" | "can_write_memory") => (next: boolean) => {
    mutation.mutate({
      can_read_memory: field === "can_read_memory" ? next : settings.can_read_memory,
      can_write_memory: field === "can_write_memory" ? next : settings.can_write_memory,
    });
  };

  return (
    <div className="flex flex-col gap-5">
      <p className="text-sm text-muted-foreground">
        Turn Cognee-backed memory on for yourself and this agent. Both switches
        default off. Company memory (shared, read-only) is unaffected by these
        switches — this only controls the agent&apos;s private, relevance-scoped
        memory for you.
      </p>
      <MemoryToggleRow
        label="Read memory"
        description="This agent may recall your private memories when it answers."
        checked={settings.can_read_memory}
        disabled={!canEdit || mutation.isPending}
        onChange={onChange("can_read_memory")}
        testId="agent-memory-can-read"
      />
      <MemoryToggleRow
        label="Write memory"
        description="This agent may save new private memories for you. Requires the create_memory permission."
        checked={settings.can_write_memory}
        disabled={!canEdit || mutation.isPending}
        onChange={onChange("can_write_memory")}
        testId="agent-memory-can-write"
      />
    </div>
  );
}

function MemoryToggleRow({
  label,
  description,
  checked,
  disabled,
  onChange,
  testId,
}: {
  label: string;
  description: string;
  checked: boolean;
  disabled: boolean;
  onChange: (next: boolean) => void;
  testId: string;
}) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium" data-testid={testId}>
          {label}
        </p>
        <p className="mt-0.5 text-xs text-muted-foreground">{description}</p>
      </div>
      <Switch
        checked={checked}
        disabled={disabled}
        onCheckedChange={onChange}
        aria-label={label}
      />
    </div>
  );
}
