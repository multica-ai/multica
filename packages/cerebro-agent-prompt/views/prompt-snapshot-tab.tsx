// FIR-3212 — the Production prompt tab on the agent detail page: the
// byte-exact prompt a run actually read, captured by the daemon at claim
// time. Everything shown here is recorded evidence — never a preview agent,
// never a reconstruction. When there is no evidence the tab says so instead
// of synthesizing one.

"use client";

import { useMemo, useState, type ComponentType, type ReactNode } from "react";
import { FileCode2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  listAgentPromptSnapshots,
  getAgentPromptSnapshot,
  type PromptSnapshot,
  type PromptSnapshotLayer,
  type PromptSnapshotRef,
} from "../api";

export interface AgentPromptSnapshotTabExtension {
  id: string;
  labelKey: "production_prompt";
  icon: ComponentType<{ className?: string }>;
  render: (context: {
    agent: Agent;
    runtimes: AgentRuntime[];
    canEdit: boolean;
  }) => ReactNode;
}

export function createAgentPromptSnapshotTabs(): AgentPromptSnapshotTabExtension[] {
  return [
    {
      id: "production_prompt",
      labelKey: "production_prompt",
      icon: FileCode2,
      render: ({ agent }) => <CerebroAgentPromptSnapshotTab agent={agent} />,
    },
  ];
}

const listKey = (agentId: string) =>
  ["cerebro", "agent-prompt-snapshots", agentId] as const;
const detailKey = (agentId: string, taskId: string) =>
  ["cerebro", "agent-prompt-snapshot", agentId, taskId] as const;

type ViewMode = "current" | "after_proposal" | "diff";

const shortSha = (sha: string) => (sha ? sha.slice(0, 8) : "—");

function formatBytes(n: number): string {
  return `${n.toLocaleString("en-US")} bytes`;
}

function formatRunDate(iso: string | null | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString("en-US", {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** Human wording for how a layer physically reached the runtime. */
function deliveryLabel(delivery: string): string {
  switch (delivery) {
    case "system_prompt":
      return "native system prompt";
    case "workdir_file":
      return "instruction file in the working directory";
    case "user_prompt":
      return "delivered as user text — no native system prompt on this runtime";
    default:
      return delivery || "unknown delivery";
  }
}

export function CerebroAgentPromptSnapshotTab({ agent }: { agent: Agent }) {
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);
  const [viewMode, setViewMode] = useState<ViewMode>("current");
  const [activeLayer, setActiveLayer] = useState<string | null>(null);

  const listQuery = useQuery({
    queryKey: listKey(agent.id),
    queryFn: () => listAgentPromptSnapshots(agent.id),
    enabled: !!agent.id,
    retry: false,
  });

  const snapshots = listQuery.data ?? [];
  const effectiveTaskId = selectedTaskId ?? snapshots[0]?.task_id ?? null;

  const detailQuery = useQuery({
    queryKey: detailKey(agent.id, effectiveTaskId ?? ""),
    queryFn: () => getAgentPromptSnapshot(agent.id, effectiveTaskId ?? ""),
    enabled: !!agent.id && !!effectiveTaskId,
    retry: false,
  });

  if (listQuery.isLoading) {
    return <Skeleton className="h-48 w-full" />;
  }

  if (listQuery.isError) {
    return (
      <p className="text-sm text-muted-foreground">
        Prompt snapshots could not be loaded. The snapshot store may be
        unavailable — try again, or check that the backend is reachable.
      </p>
    );
  }

  if (snapshots.length === 0) {
    return (
      <div className="space-y-2">
        <h3 className="text-sm font-medium">Production prompt</h3>
        <p className="text-sm text-muted-foreground">
          No prompt snapshots recorded for this agent yet. The daemon captures
          the exact prompt each run reads when it claims a task; the first run
          after this feature is enabled will appear here. Nothing is
          reconstructed after the fact — only recorded evidence is shown.
        </p>
      </div>
    );
  }

  const snapshot = detailQuery.data ?? null;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h3 className="text-sm font-medium">Production prompt</h3>
          <p className="text-xs text-muted-foreground">
            What the agent actually read — byte for byte, as recorded in the
            run.
          </p>
        </div>
        <div className="flex items-center gap-1" role="group" aria-label="View">
          <Button
            size="sm"
            variant={viewMode === "current" ? "secondary" : "ghost"}
            onClick={() => setViewMode("current")}
          >
            Current
          </Button>
          <Button
            size="sm"
            variant={viewMode === "after_proposal" ? "secondary" : "ghost"}
            onClick={() => setViewMode("after_proposal")}
          >
            After proposal
          </Button>
          <Button
            size="sm"
            variant={viewMode === "diff" ? "secondary" : "ghost"}
            onClick={() => setViewMode("diff")}
          >
            Diff
          </Button>
        </div>
      </div>

      <RunSelector
        snapshots={snapshots}
        selectedTaskId={effectiveTaskId}
        onSelect={(taskId) => {
          setSelectedTaskId(taskId);
          setActiveLayer(null);
        }}
      />

      {viewMode !== "current" ? (
        <p className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">
          Preview of a pending proposal is not available yet. The prompt shown
          under Current is the recorded evidence from the run — a proposal
          preview will be composed by the runtime, never reconstructed here.
        </p>
      ) : detailQuery.isLoading ? (
        <Skeleton className="h-64 w-full" />
      ) : !snapshot ? (
        <p className="text-sm text-muted-foreground">
          This run&apos;s snapshot could not be loaded.
        </p>
      ) : (
        <SnapshotView
          snapshot={snapshot}
          activeLayer={activeLayer}
          onSelectLayer={setActiveLayer}
        />
      )}
    </div>
  );
}

function RunSelector({
  snapshots,
  selectedTaskId,
  onSelect,
}: {
  snapshots: PromptSnapshotRef[];
  selectedTaskId: string | null;
  onSelect: (taskId: string) => void;
}) {
  return (
    <div className="flex items-center gap-2">
      <label
        htmlFor="cerebro-prompt-snapshot-run"
        className="text-xs text-muted-foreground"
      >
        Run
      </label>
      <select
        id="cerebro-prompt-snapshot-run"
        className="h-8 rounded-md border bg-background px-2 text-xs"
        value={selectedTaskId ?? ""}
        onChange={(e) => onSelect(e.target.value)}
      >
        {snapshots.map((s) => (
          <option key={s.task_id} value={s.task_id}>
            {formatRunDate(s.created_at)} · {s.provider} · {s.agent_context_version || "no version"}
          </option>
        ))}
      </select>
    </div>
  );
}

function SnapshotView({
  snapshot,
  activeLayer,
  onSelectLayer,
}: {
  snapshot: PromptSnapshot;
  activeLayer: string | null;
  onSelectLayer: (name: string | null) => void;
}) {
  const layers = snapshot.layers;
  const visibleLayers = useMemo(
    () =>
      activeLayer ? layers.filter((l) => l.name === activeLayer) : layers,
    [layers, activeLayer],
  );

  // Continuous line numbering across layers, in recorded order.
  const layerLineStarts = useMemo(() => {
    const starts = new Map<string, number>();
    let line = 1;
    for (const l of layers) {
      starts.set(l.name, line);
      line += l.content_redacted.split("\n").length;
    }
    return starts;
  }, [layers]);

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
        <span>{formatRunDate(snapshot.created_at)}</span>
        <span className="font-mono">
          {snapshot.provider} {snapshot.runtime_version}
        </span>
        <Badge variant="outline">{snapshot.agent_context_version || "no version"}</Badge>
        <span>{formatBytes(snapshot.total_bytes)}</span>
        <span className="font-mono" title={snapshot.sha256_redacted}>
          sha256 {shortSha(snapshot.sha256_redacted)}
        </span>
        <Badge variant="secondary">
          Captured from the run — not reconstructed
        </Badge>
      </div>

      {snapshot.redacted && (
        <p className="text-xs text-muted-foreground">
          Secrets were redacted before storage. Original prompt sha256{" "}
          <span className="font-mono" title={snapshot.sha256_original}>
            {shortSha(snapshot.sha256_original)}
          </span>{" "}
          (computed before redaction, recorded for provenance); the view below
          hashes to{" "}
          <span className="font-mono" title={snapshot.sha256_redacted}>
            {shortSha(snapshot.sha256_redacted)}
          </span>
          .
        </p>
      )}

      <div className="flex gap-4">
        <nav className="w-56 shrink-0 space-y-1" aria-label="Layers in the file">
          <p className="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
            Layers
          </p>
          <button
            type="button"
            className={`block w-full rounded px-2 py-1 text-left text-xs ${
              activeLayer === null ? "bg-muted font-medium" : "hover:bg-muted/50"
            }`}
            onClick={() => onSelectLayer(null)}
          >
            All layers
          </button>
          {layers.map((l) => (
            <button
              key={l.name}
              type="button"
              className={`flex w-full items-center justify-between gap-2 rounded px-2 py-1 text-left text-xs ${
                activeLayer === l.name
                  ? "bg-muted font-medium"
                  : "hover:bg-muted/50"
              }`}
              onClick={() => onSelectLayer(l.name)}
            >
              <span className="truncate">{l.name}</span>
              <span className="shrink-0 text-muted-foreground">
                {l.byte_size.toLocaleString("en-US")}
              </span>
            </button>
          ))}
        </nav>

        <div className="min-w-0 flex-1 space-y-4">
          {visibleLayers.map((layer) => (
            <LayerContent
              key={layer.name}
              layer={layer}
              startLine={layerLineStarts.get(layer.name) ?? 1}
            />
          ))}
        </div>
      </div>

      <p className="border-t pt-2 text-xs text-muted-foreground">
        Config{" "}
        <span className="font-medium">
          {snapshot.agent_context_version || "(unversioned)"}
        </span>{" "}
        locks this prompt:{" "}
        <span className="font-mono">sha256 {shortSha(snapshot.sha256_redacted)}</span>.
        If any field changes, the next run gets a new sha — and a new version.
      </p>
    </div>
  );
}

function LayerContent({
  layer,
  startLine,
}: {
  layer: PromptSnapshotLayer;
  startLine: number;
}) {
  const lines = layer.content_redacted.split("\n");
  return (
    <section aria-label={layer.name} className="rounded-md border">
      <header className="flex flex-wrap items-center gap-2 border-b bg-muted/40 px-3 py-1.5">
        <span className="font-mono text-xs font-medium">{layer.name}</span>
        <span className="text-[10px] text-muted-foreground">
          {deliveryLabel(layer.delivery)}
        </span>
        <span className="ml-auto text-[10px] text-muted-foreground">
          {layer.byte_size.toLocaleString("en-US")} bytes
        </span>
      </header>
      <pre className="overflow-x-auto p-0 text-xs leading-5">
        <code>
          {lines.map((text, i) => (
            <span key={i} className="flex">
              <span className="w-12 shrink-0 select-none border-r pr-2 text-right text-muted-foreground/60">
                {startLine + i}
              </span>
              <span className="whitespace-pre-wrap pl-3">{text}</span>
            </span>
          ))}
        </code>
      </pre>
    </section>
  );
}
