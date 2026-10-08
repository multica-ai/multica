import { PRIORITY_CONFIG } from "@multica/core/issues/config";
import type { IssuePriority } from "@multica/core/types";
import { useT } from "../../i18n";

/** The `t` of the issues namespace, where every priority name lives. */
export type IssuesT = ReturnType<typeof useT<"issues">>["t"];

/**
 * Names a priority VALUE. The five built-ins are localized from the key, so a
 * non-English member never reads `PRIORITY_CONFIG`'s English seed; a value
 * this client is too old to know falls back to the raw value rather than
 * rendering blank. The status counterpart is `useStatusLabel`.
 *
 * Pure rather than a hook, so the issue activity formatter — which is a plain
 * function holding a `t` it was handed — shares it with components that call
 * `useT("issues")` themselves.
 */
export function priorityLabel(priority: string, t: IssuesT): string {
  if (priority in PRIORITY_CONFIG) {
    return t(($) => $.priority[priority as IssuePriority]);
  }
  return priority;
}
