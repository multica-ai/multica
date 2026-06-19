import type { WorkflowNode, WorkflowEdge } from "@multica/core/types";

export interface StageNodeDagProps {
  stageId: string;
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
  onNodeSelect: (nodeId: string) => void;
}

export function StageNodeDag(_props: StageNodeDagProps) {
  return null;
}
