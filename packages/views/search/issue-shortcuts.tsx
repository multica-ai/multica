"use client";

import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { useCommandBarContextStore } from "@multica/core/command-bar";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useIsGuest } from "@multica/core/permissions";
import { quickActionListOptions } from "@multica/core/quick-actions";
import { getShortcut, shortcutMatchesEvent, type ShortcutActionId } from "@multica/core/shortcuts";
import { isPageShortcutEvent } from "../common/page-shortcut";
import { composerFocusFor } from "../issues/components/composer-focus";
import { useT } from "../i18n";
import { issueTargetFromContexts } from "./issue-target";
import { stripEllipsis } from "./matching";
import { openPalettePage } from "./palette-launch";
import type { IssueTarget, PalettePage } from "./types";

type PickerPageType = Exclude<Extract<PalettePage, { target: IssueTarget }>["type"], "issue-parent">;

const PICKER_PAGES = {
  setIssueStatus: "issue-status",
  assignIssue: "issue-assignee",
  setIssuePriority: "issue-priority",
  setIssueLabels: "issue-labels",
  setIssueDueDate: "issue-due-date",
  moveIssueToProject: "issue-project",
} as const satisfies Partial<Record<ShortcutActionId, PickerPageType>>;

type PickerVerb = keyof typeof PICKER_PAGES;
type IssueVerb = PickerVerb | "runIssueQuickAction" | "replyToIssue";

const ISSUE_VERBS: readonly IssueVerb[] = [
  ...(Object.keys(PICKER_PAGES) as PickerVerb[]),
  "runIssueQuickAction",
  "replyToIssue",
];

/**
 * The issue verbs (S, A, P, ...) on the issue in front of the user: each opens
 * the ⌘K page its command opens, already on that issue or selection, and R
 * focuses the open issue's composer. A verb with nothing to act on leaves the
 * key alone.
 *
 * Listens on `window`, after every document-level handler, so a key that
 * means something more specific where it is pressed (a board peek's H / L)
 * keeps that meaning.
 */
export function IssueShortcuts() {
  const { t } = useT("search");
  // Mounted with the closed palette, before the workspace may have resolved.
  const wsId = useCurrentWorkspace()?.id ?? "";
  // A guest (MUL-9420) only comments: every field verb stays unclaimed.
  const guest = useIsGuest(wsId);
  const { data: quickActions = [] } = useQuery({
    ...quickActionListOptions(wsId),
    enabled: wsId !== "" && !guest,
  });
  const hasQuickActions = quickActions.some((action) => action.status === "active");

  const actionFor = (verb: IssueVerb, target: IssueTarget): (() => void) | null => {
    const single = target.kind === "single" ? target.issue : null;
    if (verb === "replyToIssue") return single ? composerFocusFor(single.id) : null;
    if (guest || wsId === "") return null;
    const crumb =
      target.kind === "single"
        ? target.issue.identifier
        : t(($) => $.context.selected_issues, { count: target.issues.length });
    if (verb === "runIssueQuickAction") {
      if (!single || !hasQuickActions) return null;
      return () =>
        openPalettePage({
          page: { type: "issue-quick-action", issue: single },
          crumbs: [crumb, stripEllipsis(t(($) => $.issue.run_quick_action))],
        });
    }
    const labels: Record<PickerVerb, string> = {
      setIssueStatus: t(($) => $.issue.change_status),
      assignIssue: t(($) => $.issue.assign_to),
      setIssuePriority: t(($) => $.issue.set_priority),
      setIssueLabels: t(($) => $.issue.add_label),
      setIssueDueDate: t(($) => $.issue.set_due_date),
      moveIssueToProject: t(($) => $.issue.move_to_project),
    };
    return () =>
      openPalettePage({
        page: { type: PICKER_PAGES[verb], target },
        crumbs: [crumb, stripEllipsis(labels[verb])],
      });
  };
  const actionForRef = useRef(actionFor);
  actionForRef.current = actionFor;

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!isPageShortcutEvent(event)) return;
      const verb = ISSUE_VERBS.find((id) => shortcutMatchesEvent(getShortcut(id), event));
      if (!verb) return;
      const contexts = useCommandBarContextStore.getState().entries.map((entry) => entry.context);
      const target = issueTargetFromContexts(contexts);
      const run = target ? actionForRef.current(verb, target) : null;
      if (!run) return;
      event.preventDefault();
      run();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  return null;
}
