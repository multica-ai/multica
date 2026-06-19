import type { WorkflowNode, WorkflowEdge } from "@multica/core/types";

export interface NodeDetailPanelProps {
  nodeId: string;
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
  onClose: () => void;
}

export function NodeDetailPanel(_props: NodeDetailPanelProps) {
  return null;
}
