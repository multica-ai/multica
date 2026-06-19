"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  workflowOverviewOptions,
  workflowStagesOptions,
  workflowNodesOptions,
  workflowEdgesOptions,
} from "@multica/core/workflows/queries";
import { useNavigation } from "../../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { PageHeader } from "../../../layout/page-header";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import { AlertCircle, ArrowLeft } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { useT } from "../../../i18n";
import { StageCanvas } from "./stage-canvas";
import { StageNodeDag } from "./stage-node-dag";
import { NodeDetailPanel } from "./node-detail-panel";
import { StageCreateDialog } from "./stage-create-dialog";

export interface WorkflowOverviewPageProps {
  workflowId: string;
}

/** Loading skeleton for the stage card strip — 5 gray pulse placeholder cards. */
function StageCanvasSkeleton() {
  return (
    <div className="flex gap-3 overflow-hidden" data-testid="stage-canvas-skeleton">
      {Array.from({ length: 5 }).map((_, i) => (
        <Skeleton key={i} className="h-24 w-40 shrink-0 rounded-lg" />
      ))}
    </div>
  );
}

export function WorkflowOverviewPage({ workflowId }: WorkflowOverviewPageProps) {
  const { t } = useT("workflows");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const navigation = useNavigation();

  // Page-level selection state (pure browse state — useState is sufficient)
  const [selectedStageId, setSelectedStageId] = useState<string | null>(null);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [showCreateDialog, setShowCreateDialog] = useState(false);

  // Queries — these share cache keys with WorkflowDetailPage so edits sync
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

  const isLoading = workflowLoading || stagesLoading || nodesLoading;

  // ── Handlers ──

  const handleAddStage = () => {
    setShowCreateDialog(true);
  };

  const handleCloseCreateDialog = () => {
    setShowCreateDialog(false);
  };

  const handleStageSelect = (stageId: string) => {
    setSelectedStageId(stageId);
    setSelectedNodeId(null); // Clear node selection when switching stages
  };

  const handleNodeSelect = (nodeId: string) => {
    setSelectedNodeId(nodeId);
  };

  const handleNodeDetailClose = () => {
    setSelectedNodeId(null);
  };

  // ── Loading state ──

  if (isLoading) {
    return (
      <div className="flex flex-col h-full">
        <PageHeader>
          <Skeleton className="h-4 w-48" />
        </PageHeader>
        <div className="flex flex-col gap-4 p-6">
          <Skeleton className="h-8 w-64" />
          <StageCanvasSkeleton />
        </div>
      </div>
    );
  }

  // ── Error state ──

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

  // ── Data loaded ──

  return (
    <div className="flex flex-col h-full">
      <PageHeader className="justify-between px-5 shrink-0">
        <div className="flex items-center gap-2 min-w-0">
          <h1 className="text-sm font-medium truncate">{workflow.title}</h1>
        </div>
      </PageHeader>

      <div className="flex-1 overflow-auto p-6">
        {/* Stage card strip */}
        <div className="mb-6">
          <StageCanvas
            stages={stages}
            selectedStageId={selectedStageId}
            onStageSelect={handleStageSelect}
            onAddStage={handleAddStage}
          />
        </div>

        {/* Node DAG for selected stage */}
        {selectedStageId ? (
          <StageNodeDag
            stageId={selectedStageId}
            nodes={nodes}
            edges={edges}
            onNodeSelect={handleNodeSelect}
          />
        ) : (
          <div className="flex items-center justify-center h-64 text-muted-foreground text-sm">
            {t(($) => $.overview.node_dag.empty_title)}
          </div>
        )}
      </div>

      {/* Node detail panel — slide-out drawer */}
      {selectedNodeId && (
        <NodeDetailPanel
          nodeId={selectedNodeId}
          workflowId={workflowId}
          nodes={nodes}
          edges={edges}
          onClose={handleNodeDetailClose}
        />
      )}

      {/* Stage creation dialog */}
      {showCreateDialog && (
        <StageCreateDialog
          workflowId={workflowId}
          wsId={wsId}
          onClose={handleCloseCreateDialog}
        />
      )}
    </div>
  );
}
