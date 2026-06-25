"use client";

import { useState, useMemo, useLayoutEffect, useRef, useCallback, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  workflowOverviewOptions,
  workflowStagesOptions,
  workflowNodesOptions,
  workflowEdgesOptions,
} from "@multica/core/workflows/queries";
import { agentListOptions, builtinPluginListOptions } from "@multica/core/workspace/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { useWorkflowViewStore } from "@multica/core/workflows/stores/view-store";
import { useNavigation } from "../../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { PageHeader } from "../../../layout/page-header";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { AlertCircle, ArrowLeft, PanelsTopLeft } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { useT } from "../../../i18n";
import { StageLane } from "./stage-lane";
import { PanoramaSvgOverlay } from "./panorama-svg-overlay";
import {
  ArchitectureDetailPanel,
  type ArchitectureDetailPanelData,
} from "./architecture-detail-panel";
import type { Agent } from "@multica/core/types";
import type { BuiltinPlugin } from "@multica/core/api/schemas";

export interface WorkflowPanoramaPageProps {
  workflowId: string;
  viewToggle?: ReactNode;
}

type PanoramaSelection = {
  nodeId: string;
  focus: "worker" | "critic";
};

// Stage transition gradient lookup (6-color cycle -> all pairwise transitions)
const STAGE_TRANSITION_GRADIENTS = [
  "bg-gradient-to-b from-slate-50/40 to-stone-50/40",
  "bg-gradient-to-b from-stone-50/40 to-blue-50/35",
  "bg-gradient-to-b from-blue-50/35 to-rose-50/35",
  "bg-gradient-to-b from-rose-50/35 to-violet-50/35",
  "bg-gradient-to-b from-violet-50/35 to-amber-50/35",
  "bg-gradient-to-b from-amber-50/35 to-slate-50/40",
] as const;

function PanoramaSkeleton() {
  return (
    <div className="flex flex-col gap-4 p-3" data-testid="panorama-skeleton">
      <Skeleton className="h-8 w-64" />
      {Array.from({ length: 3 }).map((_, i) => (
        <Skeleton key={i} className="h-24 w-full" />
      ))}
    </div>
  );
}

export function WorkflowPanoramaPage({ workflowId, viewToggle }: WorkflowPanoramaPageProps) {
  const { t } = useT("workflows");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const navigation = useNavigation();
  const setViewMode = useWorkflowViewStore((s) => s.setViewMode);

  const [selectedCard, setSelectedCard] = useState<PanoramaSelection | null>(null);

  // ── Queries ──
  const {
    data: workflow,
    isLoading: workflowLoading,
    isError: workflowError,
    refetch: workflowRefetch,
  } = useQuery(workflowOverviewOptions(wsId, workflowId));

  const { data: stages = [], isLoading: stagesLoading } = useQuery(
    workflowStagesOptions(wsId, workflowId),
  );

  const { data: nodes = [], isLoading: nodesLoading } = useQuery(
    workflowNodesOptions(wsId, workflowId),
  );

  const { data: edges = [] } = useQuery(
    workflowEdgesOptions(wsId, workflowId),
  );

  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const { data: pluginsData } = useQuery(builtinPluginListOptions());

  const { getActorName } = useActorName();

  const isLoading = workflowLoading || stagesLoading || nodesLoading;

  // ── Derived lookups ──
  const agentLookup = useMemo(() => {
    const map = new Map<string, Agent | null>();
    for (const a of agents) map.set(a.id, a);
    return map;
  }, [agents]);

  const pluginLookup = useMemo(() => {
    const map = new Map<string, BuiltinPlugin | null>();
    const items = pluginsData?.items ?? [];
    for (const p of items) map.set(p.id, p);
    return map;
  }, [pluginsData]);

  // ── Node/critic position measurement for SVG overlay ──
  const containerRef = useRef<HTMLDivElement | null>(null);
  const nodeElementMap = useRef(new Map<string, HTMLButtonElement>());
  const criticElementMap = useRef(new Map<string, HTMLButtonElement>());
  const [nodePositions, setNodePositions] = useState(new Map<string, DOMRect>());
  const [criticPositions, setCriticPositions] = useState(new Map<string, DOMRect>());

  const measurePositions = useCallback(() => {
    const containerRect = containerRef.current?.getBoundingClientRect();
    if (!containerRect) return;

    const nextNodePos = new Map<string, DOMRect>();
    nodeElementMap.current.forEach((el, id) => {
      const rect = el.getBoundingClientRect();
      // Convert viewport-relative to container-relative coordinates
      nextNodePos.set(id, new DOMRect(
        rect.left - containerRect.left + (containerRef.current?.scrollLeft ?? 0),
        rect.top - containerRect.top + (containerRef.current?.scrollTop ?? 0),
        rect.width,
        rect.height,
      ));
    });
    setNodePositions(nextNodePos);

    const nextCriticPos = new Map<string, DOMRect>();
    criticElementMap.current.forEach((el, id) => {
      const rect = el.getBoundingClientRect();
      nextCriticPos.set(id, new DOMRect(
        rect.left - containerRect.left + (containerRef.current?.scrollLeft ?? 0),
        rect.top - containerRect.top + (containerRef.current?.scrollTop ?? 0),
        rect.width,
        rect.height,
      ));
    });
    setCriticPositions(nextCriticPos);
  }, []);

  useLayoutEffect(() => {
    measurePositions();
    const observer = new ResizeObserver(() => measurePositions());
    if (containerRef.current) observer.observe(containerRef.current);
    return () => observer.disconnect();
  }, [nodes, stages, measurePositions]);

  // ── Create callback refs for nodes and critics ──
  const nodeElementRefs = useMemo(() => {
    const map = new Map<string, (el: HTMLButtonElement | null) => void>();
    for (const node of nodes) {
      map.set(node.id, (el) => {
        if (el) nodeElementMap.current.set(node.id, el);
        else nodeElementMap.current.delete(node.id);
      });
    }
    return map;
  }, [nodes]);

  const criticElementRefs = useMemo(() => {
    const map = new Map<string, (el: HTMLButtonElement | null) => void>();
    for (const node of nodes) {
      if (node.critic_id || node.critic_api_url) {
        map.set(node.id, (el) => {
          if (el) criticElementMap.current.set(node.id, el);
          else criticElementMap.current.delete(node.id);
        });
      }
    }
    return map;
  }, [nodes]);

  // ── Build detail panel data ──
  const selectedPanelData: ArchitectureDetailPanelData | null = useMemo(() => {
    if (!selectedCard) return null;
    const node = nodes.find((n) => n.id === selectedCard.nodeId);
    if (!node) return null;

    if (selectedCard.focus === "critic") {
      const criticAgent = agentLookup.get(node.critic_id ?? "") ?? null;
      return { node, agent: null, plugin: null, criticAgent, focus: "critic" };
    }

    const agent = agentLookup.get(node.worker_id ?? "") ?? null;
    const plugin = agent?.plugin_id
      ? pluginLookup.get(agent.plugin_id) ?? null
      : null;
    const criticAgent = node.critic_id
      ? agentLookup.get(node.critic_id) ?? null
      : null;

    return { node, agent, plugin, criticAgent, focus: "worker" };
  }, [selectedCard, nodes, agentLookup, pluginLookup]);

  // ── Handlers ──
  const handleCardClick = (nodeId: string, focus: "worker" | "critic") => {
    setSelectedCard({ nodeId, focus });
  };

  const handleDetailClose = () => setSelectedCard(null);
  const handleOpenInEditor = () => setViewMode("editor");

  // ── Group nodes by stage ──
  const nodesByStage = useMemo(() => {
    const map = new Map<string, typeof nodes>();
    for (const node of nodes) {
      const sid = node.stage_id ?? "__unassigned__";
      if (!map.has(sid)) map.set(sid, []);
      map.get(sid)!.push(node);
    }
    return map;
  }, [nodes]);

  // ── Loading ──
  if (isLoading) {
    return (
      <div className="flex flex-col h-full">
        <PageHeader><Skeleton className="h-4 w-48" /></PageHeader>
        <PanoramaSkeleton />
      </div>
    );
  }

  // ── Error ──
  if (workflowError || !workflow) {
    return (
      <div className="flex flex-col h-full">
        <PageHeader><Skeleton className="h-4 w-48" /></PageHeader>
        <div className="flex h-full items-center justify-center p-6">
          <Alert variant="destructive" className="max-w-md">
            <AlertCircle className="h-4 w-4" />
            <AlertTitle>{t(($) => $.detail.not_found)}</AlertTitle>
            <AlertDescription className="flex flex-col gap-3">
              <p className="text-sm text-muted-foreground">
                {t(($) => $.detail.not_found)}
              </p>
              <div className="flex gap-2">
                <Button variant="outline" size="sm" onClick={() => navigation.push(wsPaths.workflows())}>
                  <ArrowLeft className="mr-2 h-4 w-4" />
                  {t(($) => $.detail.back_to_workflows)}
                </Button>
                <Button variant="default" size="sm" onClick={() => workflowRefetch()}>
                  {t(($) => $.overview.error_retry)}
                </Button>
              </div>
            </AlertDescription>
          </Alert>
        </div>
      </div>
    );
  }

  // ── Panorama view ──
  const sortedStages = [...stages].sort((a, b) => a.sort_order - b.sort_order);

  return (
    <div className="flex flex-col h-full">
      <PageHeader className="justify-between px-5 shrink-0">
        <div className="flex min-w-0 items-center gap-3">
          <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-xl border border-border/70 bg-muted/60 text-muted-foreground">
            <PanelsTopLeft className="h-4 w-4" strokeWidth={1.9} />
          </span>
          <div className="min-w-0">
            <div className="text-[10px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
              Workflow panorama
            </div>
            <h1 className="text-sm font-medium truncate">{workflow.title}</h1>
          </div>
        </div>
        {viewToggle && <div className="flex items-center gap-1">{viewToggle}</div>}
      </PageHeader>

      <div
        data-testid="workflow-panorama-canvas"
        className="relative flex-1 overflow-auto bg-slate-100/70 p-4"
      >
        <div
          ref={containerRef}
          data-testid="workflow-panorama-rail"
          className="relative ml-0 w-full min-w-[1320px] rounded-xl bg-white shadow-[0_14px_40px_rgba(15,23,42,0.08)]"
        >
          <PanoramaSvgOverlay
            edges={edges}
            nodes={nodes}
            stages={stages}
            nodePositions={nodePositions}
            criticPositions={criticPositions}
          />

          {sortedStages.length === 0 ? (
            <div className="flex h-64 items-center justify-center text-sm text-muted-foreground">
              {t(($) => $.overview.stage_canvas.empty_title)}
            </div>
          ) : (
            sortedStages.map((stage, idx) => {
              const currColorIdx = Math.abs(stage.sort_order) % 6;
              const gradientClass = STAGE_TRANSITION_GRADIENTS[currColorIdx] ?? STAGE_TRANSITION_GRADIENTS[0];

              return (
                <div key={stage.id}>
                  <StageLane
                    stage={stage}
                    nodeIds={nodesByStage.get(stage.id) ?? []}
                    getActorName={getActorName}
                    agentLookup={agentLookup}
                    pluginLookup={pluginLookup}
                    onCardClick={handleCardClick}
                    selectedCard={selectedCard}
                    nodeElementRefs={nodeElementRefs}
                    criticElementRefs={criticElementRefs}
                  />
                  {idx < sortedStages.length - 1 && (
                    <div
                      data-testid="stage-transition-gradient"
                      className={`relative z-0 h-2 ${gradientClass}`}
                    />
                  )}
                </div>
              );
            })
          )}
        </div>
      </div>

      {selectedPanelData && (
        <ArchitectureDetailPanel
          data={selectedPanelData}
          onClose={handleDetailClose}
          onOpenInEditor={handleOpenInEditor}
        />
      )}
    </div>
  );
}
