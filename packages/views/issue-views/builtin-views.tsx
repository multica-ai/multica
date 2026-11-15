"use client";

import { useCallback } from "react";
import { AlarmClock, Bell, Bot, CircleUser, ListTodo } from "@multica/ui/icons";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { issueViewContainerKey, useActiveIssueViewStore } from "@multica/core/issue-views/active-view-store";
import { FILTERED_BUILTIN_VIEWS } from "@multica/core/issue-views/builtin-issue-views";
import type { BuiltinViewKey } from "@multica/core/issue-views/sidebar-views";
import { issueViewTarget } from "@multica/core/issue-views/view-target";
import { useT } from "../i18n";

const WORKSPACE_SCOPE = { scope_type: "workspace" } as const;

export const BUILTIN_VIEW_ICON: Record<BuiltinViewKey, React.ReactNode> = {
  all_issues: <ListTodo className="size-4 shrink-0" />,
  my_work: <CircleUser className="size-4 shrink-0" />,
  working: <Bot className="size-4 shrink-0" />,
  subscribed: <Bell className="size-4 shrink-0" />,
  snoozed: <AlarmClock className="size-4 shrink-0" />,
};

/** The `?view=` id of the issues page's Snoozed list, which is not a filter of All issues. */
export const SNOOZED_VIEW_ID = "snoozed";

export function useBuiltinViewLabel() {
  const { t } = useT("issues");
  return useCallback((key: BuiltinViewKey) => t(($) => $.views.builtin[key]), [t]);
}

/**
 * Where a built-in view lives, and the store write that makes its page show
 * it rather than whichever saved view was left open. Every built-in is the
 * workspace issues page; all but All issues open it under their filter, and
 * Snoozed in place of the table.
 */
export function useBuiltinViewNavigation() {
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const setActive = useActiveIssueViewStore((s) => s.setActive);

  const hrefFor = useCallback(
    (key: BuiltinViewKey) =>
      key === "all_issues"
        ? p.issues()
        : key === "snoozed"
          ? `${p.issues()}?view=${SNOOZED_VIEW_ID}`
          : issueViewTarget(wsId, FILTERED_BUILTIN_VIEWS[key], p).href,
    [p, wsId],
  );

  const prepare = useCallback(
    (key: BuiltinViewKey) =>
      setActive(issueViewContainerKey(wsId, WORKSPACE_SCOPE), key === "all_issues" ? null : key),
    [setActive, wsId],
  );

  return { hrefFor, prepare };
}
