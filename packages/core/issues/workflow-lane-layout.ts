export const MIN_WORKFLOW_LANE_HEIGHT = 240;
export const MAX_WORKFLOW_LANE_HEIGHT = 1200;

export function clampWorkflowLaneHeight(height: number): number {
  return Math.round(Math.max(MIN_WORKFLOW_LANE_HEIGHT, Math.min(MAX_WORKFLOW_LANE_HEIGHT, height)));
}
