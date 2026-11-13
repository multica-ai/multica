import type { QuickAction } from "../types";

type QuickActionBody = Pick<
  QuickAction,
  "prompt" | "assignee_type" | "assignee_id" | "target_name" | "target_missing"
>;

/** Draft text only; the comment send path checks invocation permission. */
export function buildQuickActionBody(action: QuickActionBody): string | null {
  if (action.target_missing) return null;
  if (action.assignee_type === "") return action.prompt;
  if (
    (action.assignee_type !== "agent" && action.assignee_type !== "squad") ||
    !action.assignee_id ||
    !action.target_name
  ) return null;

  return `[@${action.target_name}](mention://${action.assignee_type}/${action.assignee_id})\n\n${action.prompt}`;
}
