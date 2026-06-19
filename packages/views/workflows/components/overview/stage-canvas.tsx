import type { WorkflowStage } from "@multica/core/types";

export interface StageCanvasProps {
  stages: WorkflowStage[];
  selectedStageId: string | null;
  onStageSelect: (stageId: string) => void;
}

export function StageCanvas(_props: StageCanvasProps) {
  return null;
}
