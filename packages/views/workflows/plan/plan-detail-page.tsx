"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { planApi, workflowApi } from "@multica/core/api/workflows";
import { WorkflowCanvas } from "../canvas/workflow-canvas";
import { Button } from "@multica/ui/components/ui/button";

interface PlanDetailPageProps {
  planId: string;
}

export function PlanDetailPage({ planId }: PlanDetailPageProps) {
  const qc = useQueryClient();

  const { data: plan, isLoading: planLoading } = useQuery({
    queryKey: ["plan", planId],
    queryFn: () => planApi.get(planId),
  });

  const workflowId = plan?.workflow_id;
  const { data: nodes = [], isLoading: nodesLoading } = useQuery({
    queryKey: ["workflow-nodes", workflowId],
    queryFn: () => workflowApi.listNodes(workflowId!),
    enabled: !!workflowId,
  });
  const { data: edges = [] } = useQuery({
    queryKey: ["workflow-edges", workflowId],
    queryFn: () => workflowApi.listEdges(workflowId!),
    enabled: !!workflowId,
  });

  const confirmMutation = useMutation({
    mutationFn: () => workflowApi.confirm(workflowId!),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["workflow-nodes", workflowId] });
    },
  });

  const updateNodeMutation = useMutation({
    mutationFn: ({ nodeId, data }: { nodeId: string; data: Record<string, unknown> }) =>
      workflowApi.updateNode(workflowId!, nodeId, data as Parameters<typeof workflowApi.updateNode>[2]),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["workflow-nodes", workflowId] });
    },
  });

  const createEdgeMutation = useMutation({
    mutationFn: ({ sourceId, targetId }: { sourceId: string; targetId: string }) =>
      workflowApi.createEdge(workflowId!, { source_node_id: sourceId, target_node_id: targetId }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["workflow-edges", workflowId] });
    },
  });

  if (planLoading || !workflowId) {
    return <div className="p-6">Loading...</div>;
  }
  if (!plan) {
    return <div className="p-6">Plan not found</div>;
  }

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="flex items-center justify-between p-4 border-b">
        <div>
          <h1 className="text-lg font-semibold">{plan.title}</h1>
          <span className="text-sm text-muted-foreground capitalize">{plan.status}</span>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => {
            // TODO: Open agent picker modal for node creation
            // For Phase 1, create node with placeholder agent_id
            workflowApi.createNode(workflowId, {
              agent_id: "00000000-0000-0000-0000-000000000000", // placeholder
              title: "New Node",
              prompt: "",
              position_x: 100 + Math.random() * 200,
              position_y: 100 + Math.random() * 200,
            }).then(() => qc.invalidateQueries({ queryKey: ["workflow-nodes", workflowId] }));
          }}>
            + Add Node
          </Button>
          {plan.status === "draft" && (
            <Button onClick={() => confirmMutation.mutate()}>
              Confirm & Run
            </Button>
          )}
        </div>
      </div>

      {/* Canvas */}
      <div className="flex-1 min-h-0">
        <WorkflowCanvas
          workflowId={workflowId}
          initialNodes={nodes}
          initialEdges={edges}
          onNodeUpdate={(nodeId, data) =>
            updateNodeMutation.mutate({ nodeId, data })
          }
          onEdgeCreate={(sourceId, targetId) =>
            createEdgeMutation.mutate({ sourceId, targetId })
          }
        />
      </div>
    </div>
  );
}
