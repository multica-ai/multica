"use client";

import { useLayoutEffect, useMemo, useRef, useState } from "react";
import type { WorkflowStage, WorkflowNode, Agent, WorkflowEdge } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../../i18n";
import { PluginCard } from "./plugin-card";
import { CriticBadge } from "./critic-badge";
import { ArrowRight, GitBranch, Sparkles } from "lucide-react";

export interface StageSwimlaneProps {
  stage: WorkflowStage;
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
  agentLookup: Map<string, Agent | null>;
  pluginLookup: Map<string, BuiltinPlugin | null>;
  onCardClick: (nodeId: string, focus: "worker" | "critic") => void;
  selectedCard?: { nodeId: string; focus: "worker" | "critic" } | null;
}

const STAGE_STYLES = [
  {
    shell: "border-slate-400/55 bg-slate-100/80 border-l-[6px] border-l-slate-500",
    header: "bg-slate-200/80 border-b-slate-400/45",
    title: "text-slate-800",
    body: "bg-slate-50/80",
    flow: "text-slate-400",
  },
  {
    shell: "border-stone-400/55 bg-stone-100/80 border-l-[6px] border-l-stone-500",
    header: "bg-stone-200/80 border-b-stone-400/45",
    title: "text-stone-800",
    body: "bg-stone-50/80",
    flow: "text-stone-400",
  },
  {
    shell: "border-blue-400/50 bg-blue-50/80 border-l-[6px] border-l-blue-500",
    header: "bg-blue-100/80 border-b-blue-300/60",
    title: "text-blue-900",
    body: "bg-blue-50/60",
    flow: "text-blue-400",
  },
  {
    shell: "border-rose-300/55 bg-rose-50/70 border-l-[6px] border-l-rose-400",
    header: "bg-rose-100/70 border-b-rose-300/55",
    title: "text-rose-900",
    body: "bg-rose-50/55",
    flow: "text-rose-300",
  },
  {
    shell: "border-violet-300/55 bg-violet-50/75 border-l-[6px] border-l-violet-400",
    header: "bg-violet-100/75 border-b-violet-300/55",
    title: "text-violet-900",
    body: "bg-violet-50/60",
    flow: "text-violet-400",
  },
  {
    shell: "border-amber-300/60 bg-amber-50/75 border-l-[6px] border-l-amber-400",
    header: "bg-amber-100/70 border-b-amber-300/55",
    title: "text-amber-900",
    body: "bg-amber-50/60",
    flow: "text-amber-400",
  },
 ] as const;

const DEFAULT_STAGE_STYLE = STAGE_STYLES[0];

type StageEntry = {
  node: WorkflowNode;
};

type RowPosition = {
  top: number;
};

export function StageSwimlane({
  stage,
  nodes,
  edges,
  agentLookup,
  pluginLookup,
  onCardClick,
  selectedCard = null,
}: StageSwimlaneProps) {
  const { t } = useT("workflows");
  const rootRef = useRef<HTMLElement | null>(null);
  const workerCardRefs = useRef(new Map<string, HTMLDivElement | null>());
  const [workerRowPositions, setWorkerRowPositions] = useState<Map<string, RowPosition>>(new Map());
  const hasCriticAttachment = (node: WorkflowNode) =>
    Boolean(node.critic_id || node.critic_api_url);
  const stageNodes = useMemo(
    () => nodes.filter((node) => node.stage_id === stage.id),
    [nodes, stage.id],
  );

  const stageStyle = STAGE_STYLES[Math.abs(stage.sort_order) % STAGE_STYLES.length] ?? DEFAULT_STAGE_STYLE;
  const workerCount = stageNodes.length;
  const criticCount = stageNodes.filter(hasCriticAttachment).length;

  const orderedEntries = useMemo(() => {
    const entries = stageNodes.map<StageEntry>((node) => ({
      node,
    }));
    return entries.sort((left, right) => left.node.sort_order - right.node.sort_order);
  }, [stageNodes]);

  const stageNodeIds = useMemo(
    () => new Set(stageNodes.map((node) => node.id)),
    [stageNodes],
  );

  const stageEdges = useMemo(
    () =>
      edges.filter(
        (edge) => stageNodeIds.has(edge.source_node_id) && stageNodeIds.has(edge.target_node_id),
      ),
    [edges, stageNodeIds],
  );

  const entryByNodeId = useMemo(
    () => new Map(orderedEntries.map((entry) => [entry.node.id, entry])),
    [orderedEntries],
  );

  const skipEdgeKeys = useMemo(() => {
    const keys = new Set<string>();
    for (let index = 0; index < orderedEntries.length - 1; index += 1) {
      const current = orderedEntries[index]!;
      const next = orderedEntries[index + 1]!;
      keys.add(`${current.node.id}:${next.node.id}`);
    }
    return keys;
  }, [orderedEntries]);

  const arcEdges = useMemo(
    () =>
      stageEdges.filter((edge) => {
        const sourceEntry = entryByNodeId.get(edge.source_node_id);
        const targetEntry = entryByNodeId.get(edge.target_node_id);
        if (!sourceEntry || !targetEntry) return false;
        return !skipEdgeKeys.has(`${sourceEntry.node.id}:${targetEntry.node.id}`);
      }),
    [entryByNodeId, skipEdgeKeys, stageEdges],
  );

  useLayoutEffect(() => {
    const measureRows = () => {
      if (orderedEntries.length === 0) {
        setWorkerRowPositions(new Map());
        return;
      }

      const nextPositions = new Map<string, RowPosition>();
      for (const entry of orderedEntries) {
        const element = workerCardRefs.current.get(entry.node.id);
        if (!element) continue;
        const rect = element.getBoundingClientRect();
        nextPositions.set(entry.node.id, { top: rect.top });
      }

      setWorkerRowPositions(nextPositions);
    };

    measureRows();

    if (typeof ResizeObserver === "undefined" || !rootRef.current) {
      return;
    }

    const observer = new ResizeObserver(() => {
      measureRows();
    });

    observer.observe(rootRef.current);
    return () => {
      observer.disconnect();
    };
  }, [orderedEntries]);

  const isSameRow = (currentNodeId: string, nextNodeId: string) => {
    const current = workerRowPositions.get(currentNodeId);
    const next = workerRowPositions.get(nextNodeId);
    if (!current || !next) return true;
    return Math.abs(current.top - next.top) < 4;
  };

  return (
    <section
      ref={rootRef}
      data-testid={`stage-swimlane-${stage.id}`}
      className={cn(
        "overflow-hidden rounded-2xl border shadow-[0_1px_0_rgba(15,23,42,0.04)] transition-shadow duration-200",
        stageStyle.shell,
      )}
    >
      <div className={cn("border-b px-5 py-3", stageStyle.header)}>
        <div className="flex flex-col gap-2 md:flex-row md:items-start md:justify-between">
          <div className="min-w-0">
            <div className="inline-flex items-center gap-1 rounded-full bg-white/55 px-2 py-0.5 text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
              <Sparkles className="h-3 w-3" strokeWidth={1.8} />
              Stage {stage.sort_order + 1}
            </div>
            <h3 className={cn("mt-2 text-base font-semibold tracking-tight", stageStyle.title)}>
              {stage.name}
            </h3>
            {stage.description && (
              <p className="mt-1 max-w-3xl text-sm text-muted-foreground">
                {stage.description}
              </p>
            )}
          </div>
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span className="rounded-full bg-white/55 px-2.5 py-1">
              {workerCount} plugin{workerCount === 1 ? "" : "s"}
            </span>
            {criticCount > 0 ? (
              <span className="rounded-full bg-white/55 px-2.5 py-1">
                {criticCount} critic{criticCount === 1 ? "" : "s"}
              </span>
            ) : null}
          </div>
        </div>
      </div>

      <div className={cn("px-4 py-4", stageStyle.body)}>
        {stageNodes.length === 0 ? (
          <div
            data-testid="stage-swimlane-empty"
            className="flex h-16 items-center justify-center text-xs text-muted-foreground"
          >
            {t(($) => $.overview.node_dag.empty_title)}
          </div>
        ) : (
          <div className="space-y-3">
            <div data-testid={`stage-flow-${stage.id}`}>
              <div className="flex flex-wrap items-start gap-3">
                {orderedEntries.map(({ node }, index) => {
                  const next = orderedEntries[index + 1] ?? null;
                  const agent = agentLookup.get(node.worker_id ?? "") ?? null;
                  const plugin = agent?.plugin_id
                    ? pluginLookup.get(agent.plugin_id) ?? null
                    : null;
                  const criticAgent = node.critic_id
                    ? agentLookup.get(node.critic_id) ?? null
                    : null;

                  return (
                    <div key={node.id} className="flex items-start gap-3">
                      <div
                        data-testid={`stage-node-stack-${node.id}`}
                        className="flex flex-col items-start gap-3"
                      >
                        <div ref={(element) => { workerCardRefs.current.set(node.id, element); }}>
                          <PluginCard
                            node={node}
                            agent={agent}
                            plugin={plugin}
                            onClick={onCardClick}
                            isSelected={
                              selectedCard?.nodeId === node.id && selectedCard.focus === "worker"
                            }
                          />
                        </div>
                        {hasCriticAttachment(node) ? (
                          <div
                            data-testid={`critic-attachment-${node.id}`}
                            className="ml-6 flex flex-col items-start gap-2"
                          >
                            <div
                              data-testid={`critic-attachment-connector-${node.id}`}
                              aria-hidden="true"
                              className="h-4 w-8 border-l border-b border-dashed border-[var(--warning)]/45"
                            />
                            <CriticBadge
                              node={node}
                              criticAgent={criticAgent}
                              onClick={onCardClick}
                              isSelected={
                                selectedCard?.nodeId === node.id && selectedCard.focus === "critic"
                              }
                            />
                          </div>
                        ) : null}
                      </div>
                      {next && isSameRow(node.id, next.node.id) ? (
                        <div
                          data-testid="stage-flow-connector"
                          className={cn(
                            "mt-8 flex h-10 w-10 shrink-0 items-center justify-center",
                            stageStyle.flow,
                          )}
                          aria-hidden="true"
                        >
                          <span className="flex h-8 w-8 items-center justify-center rounded-full bg-current/5">
                            <ArrowRight
                              data-testid="stage-flow-connector-icon"
                              className="h-[18px] w-[18px]"
                              strokeWidth={1.85}
                            />
                          </span>
                        </div>
                      ) : null}
                    </div>
                  );
                })}
              </div>

              {arcEdges.length > 0 ? (
                <div className="mt-3 flex flex-wrap gap-2">
                  {arcEdges.map((edge) => {
                    const sourceEntry = entryByNodeId.get(edge.source_node_id);
                    const targetEntry = entryByNodeId.get(edge.target_node_id);
                    if (!sourceEntry || !targetEntry) return null;

                    return (
                      <div
                        data-testid="stage-arc-edge-badge"
                        key={edge.id}
                        className={cn(
                          "inline-flex items-center gap-2 overflow-visible rounded-full border border-current/20 bg-white/80 px-3 py-1.5 text-[11px] shadow-sm shadow-slate-200/40",
                          stageStyle.flow,
                        )}
                      >
                        <span
                          data-testid="stage-arc-edge-icon-shell"
                          className="flex h-6 w-6 shrink-0 items-center justify-center overflow-hidden rounded-full bg-current/10"
                          aria-hidden="true"
                        >
                          <GitBranch
                            data-testid="stage-arc-edge-icon"
                            className="h-3.5 w-3.5"
                            strokeWidth={1.7}
                          />
                        </span>
                        <span className="text-slate-600">
                          {sourceEntry.node.title} -&gt; {targetEntry.node.title}
                        </span>
                      </div>
                    );
                  })}
                </div>
              ) : null}
            </div>
          </div>
        )}
      </div>
    </section>
  );
}
