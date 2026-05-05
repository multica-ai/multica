import type { TaskFailureReason } from "@multica/core/types";
import type { InterpolationParams } from "@multica/i18n";

type TFn = (key: string, params?: InterpolationParams) => string;

const FAILURE_KEY: Record<TaskFailureReason, string> = {
  agent_error: "issues.task_failure_agent_error",
  timeout: "issues.task_failure_timeout",
  runtime_offline: "issues.task_failure_runtime_offline",
  runtime_recovery: "issues.task_failure_runtime_recovery",
  manual: "issues.task_failure_manual",
};

export function getFailureReasonLabel(t: TFn, reason: TaskFailureReason): string {
  return t(FAILURE_KEY[reason]);
}
