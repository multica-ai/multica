"use client";

// FIR-3212 — Setup effective prompt. This panel is deliberately evidence-led:
// it summarizes the latest immutable prompt snapshot captured at run claim
// time. It never reconstructs a prompt from the current form, because that
// could differ from what a runtime actually received.

import { useQuery } from "@tanstack/react-query";
import { FileText } from "lucide-react";
import type { Agent } from "@multica/core/types";
import {
  getAgentPromptSnapshot,
  listAgentPromptSnapshots,
} from "@multica/cerebro-agent-prompt";
import { Badge } from "@multica/ui/components/ui/badge";

const listKey = (agentId: string) =>
  ["cerebro", "agent-setup-prompt-snapshots", agentId] as const;
const detailKey = (agentId: string, taskId: string) =>
  ["cerebro", "agent-setup-prompt-snapshot", agentId, taskId] as const;

function formatBytes(bytes: number): string {
  return `${bytes.toLocaleString("en-US")} bytes`;
}

function layerLabel(name: string): string {
  const label = name
    .split("_")
    .filter(Boolean)
    .join(" ");
  return label
    ? `${label[0]?.toUpperCase()}${label.slice(1)}`
    : "Recorded layer";
}

export function AgentContextEffectivePromptPanel({ agent }: { agent: Agent }) {
  const listQuery = useQuery({
    queryKey: listKey(agent.id),
    queryFn: () => listAgentPromptSnapshots(agent.id),
    enabled: !!agent.id,
    retry: false,
  });

  const latest = listQuery.data?.[0] ?? null;
  const detailQuery = useQuery({
    queryKey: detailKey(agent.id, latest?.task_id ?? ""),
    queryFn: () => getAgentPromptSnapshot(agent.id, latest?.task_id ?? ""),
    enabled: !!agent.id && !!latest?.task_id,
    retry: false,
  });

  const snapshot = detailQuery.data ?? null;
  const preTaskLayers =
    snapshot?.layers.filter((layer) => layer.name !== "task_prompt") ?? [];
  const preTaskBytes = preTaskLayers.reduce(
    (total, layer) => total + layer.byte_size,
    0,
  );

  return (
    <div className="rounded-lg border p-4">
      <div className="flex flex-wrap items-center gap-2">
        <FileText className="size-4 text-muted-foreground" />
        <span className="text-sm font-medium">Effective prompt</span>
        {snapshot?.provider ? (
          <Badge variant="secondary">
            {snapshot.provider} {snapshot.runtime_version}
          </Badge>
        ) : null}
        {snapshot?.agent_context_version ? (
          <Badge variant="outline">{snapshot.agent_context_version}</Badge>
        ) : null}
      </div>
      <p className="mt-1 text-xs text-muted-foreground">
        What this agent actually read before its task, captured from the latest
        run — not rebuilt from the fields on this page.
      </p>

      {(listQuery.isLoading || detailQuery.isLoading) && (
        <p className="mt-3 text-xs text-muted-foreground">
          Loading recorded prompt…
        </p>
      )}

      {(listQuery.isError || detailQuery.isError) && (
        <p className="mt-3 text-xs text-muted-foreground">
          The latest recorded prompt is unavailable right now.
        </p>
      )}

      {!listQuery.isLoading && !listQuery.isError && !latest && (
        <p className="mt-3 text-xs text-muted-foreground">
          No recorded prompt for this agent yet. Run the agent once to capture
          what its engine actually reads; no estimate is shown in its place.
        </p>
      )}

      {snapshot && !detailQuery.isLoading && !detailQuery.isError && (
        <div className="mt-4 space-y-3">
          <div>
            <div className="text-2xl font-semibold tabular-nums">
              {formatBytes(preTaskBytes)} before the task
            </div>
            <div className="mt-0.5 text-xs text-muted-foreground">
              Captured from the latest run. Byte sizes are exact; token counts
              are not estimated.
            </div>
          </div>

          <div className="space-y-1.5">
            {preTaskLayers.map((layer) => (
              <div
                key={layer.name}
                className="flex items-center justify-between gap-3 border-t pt-2 text-xs"
              >
                <div className="min-w-0">
                  <div className="font-medium">{layerLabel(layer.name)}</div>
                  <div className="truncate text-muted-foreground">
                    {layer.delivery === "system_prompt"
                      ? "Native system prompt"
                      : layer.delivery === "workdir_file"
                        ? "Instruction file in the working directory"
                        : "Recorded delivery"}
                  </div>
                </div>
                <span className="shrink-0 font-mono tabular-nums">
                  {formatBytes(layer.byte_size)}
                </span>
              </div>
            ))}
          </div>

          {preTaskLayers.length === 0 && (
            <p className="text-xs text-muted-foreground">
              This run contained a task prompt but no recorded pre-task prompt.
            </p>
          )}
        </div>
      )}
    </div>
  );
}
