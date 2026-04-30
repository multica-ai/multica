import type { Autopilot, AutopilotExecutionMode, AutopilotTrigger } from "@multica/core/types";
import { parseCronExpression, type TriggerConfig } from "./trigger-config";

export interface AutopilotDuplicatePreset {
  initial: {
    title: string;
    description: string;
    assignee_id: string;
    execution_mode: AutopilotExecutionMode;
  };
  initialTriggerConfig?: TriggerConfig;
}

export function buildAutopilotDuplicatePreset(
  autopilot: Autopilot,
  triggers: AutopilotTrigger[],
): AutopilotDuplicatePreset {
  const primarySchedule = triggers.find(
    (trigger) => trigger.kind === "schedule" && !!trigger.cron_expression,
  );

  return {
    initial: {
      title: `${autopilot.title} (Copy)`,
      description: autopilot.description ?? "",
      assignee_id: autopilot.assignee_id,
      execution_mode: autopilot.execution_mode,
    },
    initialTriggerConfig: primarySchedule?.cron_expression
      ? parseCronExpression(primarySchedule.cron_expression, primarySchedule.timezone ?? "UTC")
      : undefined,
  };
}
