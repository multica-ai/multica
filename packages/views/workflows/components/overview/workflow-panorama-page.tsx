"use client";

import { useState, useMemo, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  workflowOverviewOptions,
  workflowStagesOptions,
  workflowNodesOptions,
  workflowEdgesOptions,
} from "@multica/core/workflows/queries";
import { agentListOptions, builtinPluginListOptions } from "@multica/core/workspace/queries";
import { useWorkflowViewStore } from "@multica/core/workflows/stores/view-store";
import { useNavigation } from "../../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { PageHeader } from "../../../layout/page-header";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { AlertCircle, ArrowLeft } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { useT } from "../../../i18n";
import { StageSwimlane } from "./stage-swimlane";
import { DataFlowArrow } from "./data-flow-arrow";
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

/** Loading skeleton for panorama view. */
function PanoramaSkeleton() {
  return (
    <div className="flex flex-col gap-4 p-6" data-testid="panorama-skeleton">
      <Skeleton className="h-8 w-64" />
      {Array.from({ length: 3 }).map((_, i) => (
        <Skeleton key={i} className="h-32 w-full rounded-lg" />
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

  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);

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

  const isLoading = workflowLoading || stagesLoading || nodesLoading;

  // ── Derived lookups ──
  const agentLookup = useMemo(() => {
    const map = new Map<string, Agent | null>();
    for (const a of agents) {
      map.set(a.id, a);
    }
    return map;
  }, [agents]);

  const pluginLookup = useMemo(() => {
    const map = new Map<string, BuiltinPlugin | null>();
    const items = pluginsData?.items ?? [];
    for (const p of items) {
      map.set(p.id, p);
    }
    return map;
  }, [pluginsData]);

  // Build detail panel data for selected node
  const selectedPanelData: ArchitectureDetailPanelData | null = useMemo(() => {
    if (!selectedNodeId) return null;
    const node = nodes.find((n) => n.id === selectedNodeId);
    if (!node) return null;

    const isCriticNode = !!node.critic_id;

    if (isCriticNode) {
      // Critic node: worker_id is the critic's agent
      const criticAgent = agentLookup.get(node.worker_id ?? "") ?? null;
      return { node, agent: null, plugin: null, criticAgent };
    }

    const agent = agentLookup.get(node.worker_id ?? "") ?? null;
    const plugin = agent?.plugin_id
      ? pluginLookup.get(agent.plugin_id) ?? null
      : null;
    const criticAgent = node.critic_id
      ? agentLookup.get(node.critic_id) ?? null
      : null;

    return { node, agent, plugin, criticAgent };
  }, [selectedNodeId, nodes, agentLookup, pluginLookup]);

  // ── Handlers ──
  const handleCardClick = (nodeId: string) => {
    setSelectedNodeId(nodeId);
  };

  const handleDetailClose = () => {
    setSelectedNodeId(null);
  };

  const handleOpenInEditor = () => {
    setViewMode("editor");
  };

  // ── Loading ──
  if (isLoading) {
    return (
      <div className="flex flex-col h-full">
        <PageHeader>
          <Skeleton className="h-4 w-48" />
        </PageHeader>
        <PanoramaSkeleton />
      </div>
    );
  }

  // ── Error ──
  if (workflowError || !workflow) {
    return (
      <div className="flex flex-col h-full">
        <PageHeader>
          <Skeleton className="h-4 w-48" />
        </PageHeader>
        <div className="flex h-full items-center justify-center p-6">
          <Alert variant="destructive" className="max-w-md">
            <AlertCircle className="h-4 w-4" />
            <AlertTitle>{t(($) => $.detail.not_found)}</AlertTitle>
            <AlertDescription className="flex flex-col gap-3">
              <p className="text-sm text-muted-foreground">
                {t(($) => $.detail.not_found)}
              </p>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => navigation.push(wsPaths.workflows())}
                >
                  <ArrowLeft className="mr-2 h-4 w-4" />
                  {t(($) => $.detail.back_to_workflows)}
                </Button>
                <Button
                  variant="default"
                  size="sm"
                  onClick={() => workflowRefetch()}
                >
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
        <div className="flex items-center gap-2 min-w-0">
          <h1 className="text-sm font-medium truncate">{workflow.title}</h1>
        </div>
        {viewToggle && <div className="flex items-center gap-1">{viewToggle}</div>}
      </PageHeader>

      <div className="flex-1 overflow-auto p-6 flex flex-col gap-4">
        {sortedStages.map((stage, idx) => (
          <div key={stage.id}>
            <StageSwimlane
              stage={stage}
              nodes={nodes}
              agentLookup={agentLookup}
              pluginLookup={pluginLookup}
              onCardClick={handleCardClick}
            />
            {idx < sortedStages.length - 1 && (
              <DataFlowArrow edges={edges} nodes={nodes} />
            )}
          </div>
        ))}

        {sortedStages.length === 0 && (
          <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
            {t(($) => $.overview.stage_canvas.empty_title)}
          </div>
        )}
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
