import type { IssueStatus } from "@multica/core/types";

/**
 * Display names the 7 built-in statuses are seeded with (MUL-4809). A built-in
 * still carrying its seeded name should render with the LOCALIZED label, so
 * Chinese / Japanese / Korean users keep seeing translated statuses. Once an
 * admin renames a built-in, that rename is the source of truth and wins over the
 * translation — otherwise the settings page and the pickers would disagree.
 *
 * Custom statuses have no locale bundle by construction (their names are free
 * text), so they always render verbatim.
 */
const SEEDED_BUILTIN_NAMES: Record<string, string> = {
  backlog: "Backlog",
  todo: "Todo",
  in_progress: "In Progress",
  in_review: "In Review",
  blocked: "Blocked",
  done: "Done",
  cancelled: "Cancelled",
};

/**
 * The locale key to render a catalog status with, or null when the status's own
 * name should be shown verbatim. Callers own the `t` call so this stays free of
 * the i18n types.
 */
export function localizableStatusKey(
  systemKey: string | null | undefined,
  name: string,
): IssueStatus | null {
  if (!systemKey) return null;
  return SEEDED_BUILTIN_NAMES[systemKey] === name ? (systemKey as IssueStatus) : null;
}
