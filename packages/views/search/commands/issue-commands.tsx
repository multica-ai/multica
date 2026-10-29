"use client";

import { useMemo } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  BellOff,
  BellRing,
  CalendarClock,
  CircleDashed,
  Copy,
  FolderInput,
  Link2,
  ListChevronsDownUp,
  ListChevronsUpDown,
  Network,
  Pin,
  PinOff,
  RotateCcw,
  SignalHigh,
  Square,
  SquarePlus,
  Tag,
  Trash2,
  UserPlus,
  Zap,
} from "@multica/ui/icons";
import { dispatchReasonCode } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  issueSubscribersOptions,
  issueTasksOptions,
  issueTimelineOptions,
} from "@multica/core/issues/queries";
import { useRetryIssueRun, useToggleIssueSubscriber } from "@multica/core/issues/mutations";
import { useCommentCollapseStore, useResolvedExpandStore } from "@multica/core/issues/stores";
import { quickActionListOptions } from "@multica/core/quick-actions";
import type { AgentTask, Issue } from "@multica/core/types";
import { copyText } from "@multica/ui/lib/clipboard";
import { useIssueActions } from "../../issues/actions/use-issue-actions";
import { resolvedThreadRootIds, rootCommentIds } from "../../issues/components/thread-utils";
import { canRetryRun } from "../../issues/components/retry-run-button";
import { useT } from "../../i18n";
import { usePalette } from "../palette-context";
import type { IssueTarget, PaletteCommand, PaletteCommandGroup } from "../types";
import { ContextHeading } from "./context-heading";

const ACTIVE_TASK_STATUSES = new Set<AgentTask["status"]>([
  "queued",
  "dispatched",
  "waiting_local_directory",
  "deferred",
  "running",
]);

/** Commands for the issue on screen (`issue`) or a list selection (`issues`). */
export function useIssueCommandGroup(
  target: IssueTarget | null,
  onDeletedFallbackPath?: string,
): PaletteCommandGroup | null {
  const { t } = useT("search");
  const { t: tIssues } = useT("issues");
  const wsId = useWorkspaceId();
  const palette = usePalette();
  const queryClient = useQueryClient();
  const userId = useAuthStore((s) => s.user?.id ?? null);

  const single: Issue | null = target?.kind === "single" ? target.issue : null;
  const singleId = single?.id ?? "";
  const actions = useIssueActions(single);

  const { data: quickActions = [] } = useQuery({
    ...quickActionListOptions(wsId),
    enabled: !!single,
  });
  const activeQuickActions = useMemo(
    () => quickActions.filter((action) => action.status === "active"),
    [quickActions],
  );
  const { data: tasks = [] } = useQuery({ ...issueTasksOptions(singleId), enabled: !!single });
  const { data: subscribers = [] } = useQuery({
    ...issueSubscribersOptions(singleId),
    enabled: !!single,
  });
  const toggleSubscriber = useToggleIssueSubscriber(singleId);
  const retryRun = useRetryIssueRun(singleId);

  return useMemo(() => {
    if (!target) return null;
    const isSingle = target.kind === "single";
    const crumb = isSingle
      ? target.issue.identifier
      : t(($) => $.context.selected_issues, { count: target.issues.length });

    const commands: PaletteCommand[] = [
      {
        id: "issue-status",
        label: t(($) => $.issue.change_status),
        icon: CircleDashed,
        keywords: ["status", "state", "状态"],
        page: { type: "issue-status", target },
        crumb,
        primary: true,
      },
      {
        id: "issue-assignee",
        label: t(($) => $.issue.assign_to),
        icon: UserPlus,
        keywords: ["assign", "assignee", "owner", "agent", "指派", "负责人"],
        page: { type: "issue-assignee", target },
        crumb,
        primary: true,
      },
      {
        id: "issue-priority",
        label: t(($) => $.issue.set_priority),
        icon: SignalHigh,
        keywords: ["priority", "urgent", "优先级"],
        page: { type: "issue-priority", target },
        crumb,
        primary: true,
      },
    ];

    if (single) {
      if (activeQuickActions.length > 0) {
        commands.push({
          id: "issue-quick-action",
          label: t(($) => $.issue.run_quick_action),
          icon: Zap,
          keywords: ["quick", "action", "run", "快捷"],
          hint: t(($) => $.issue.quick_actions_available, { count: activeQuickActions.length }),
          page: { type: "issue-quick-action", issue: single },
          crumb,
          primary: true,
        });
      }
      commands.push({
        id: "issue-copy-link",
        label: t(($) => $.commands.copy_issue_link),
        icon: Link2,
        keywords: ["copy", "link", "share", "url", single.identifier.toLowerCase()],
        run: () => void actions.copyLink(),
        primary: true,
      });
    }

    commands.push(
      {
        id: "issue-labels",
        label: t(($) => $.issue.add_label),
        icon: Tag,
        keywords: ["label", "tag", "标签"],
        page: { type: "issue-labels", target },
        crumb,
      },
      {
        id: "issue-due-date",
        label: t(($) => $.issue.set_due_date),
        icon: CalendarClock,
        keywords: ["due", "date", "deadline", "截止"],
        page: { type: "issue-due-date", target },
        crumb,
      },
      {
        id: "issue-project",
        label: t(($) => $.issue.move_to_project),
        icon: FolderInput,
        keywords: ["project", "move", "项目"],
        page: { type: "issue-project", target },
        crumb,
      },
    );

    if (single) {
      commands.push({
        id: "issue-parent",
        label: t(($) => $.issue.set_parent),
        icon: Network,
        keywords: ["parent", "sub", "父任务"],
        run: actions.openSetParent,
      });

      const sorted = [...tasks].sort((a, b) => b.created_at.localeCompare(a.created_at));
      const activeTask = sorted.find((task) => ACTIVE_TASK_STATUSES.has(task.status));
      const lastTask = sorted[0];
      if (activeTask) {
        commands.push({
          id: "issue-cancel-run",
          label: t(($) => $.issue.cancel_run),
          icon: Square,
          keywords: ["cancel", "stop", "terminate", "run", "取消", "停止", "运行"],
          run: () =>
            palette.openHostDialog({
              type: "cancel-run",
              issueId: single.id,
              taskId: activeTask.id,
              running:
                activeTask.status === "running" ||
                activeTask.status === "dispatched" ||
                activeTask.status === "waiting_local_directory",
            }),
        });
      } else if (lastTask && canRetryRun(lastTask)) {
        commands.push({
          id: "issue-retry-run",
          label: t(($) => $.issue.retry_run),
          icon: RotateCcw,
          keywords: ["retry", "rerun", "again", "run", "重试", "运行"],
          run: () => {
            retryRun.mutateAsync(lastTask.id).then(
              () => toast.success(t(($) => $.toast.run_retried)),
              (e: unknown) =>
                toast.error(
                  dispatchReasonCode(e) === "invocation_not_allowed"
                    ? tIssues(($) => $.execution_log.retry_blocked)
                    : e instanceof Error
                      ? e.message
                      : tIssues(($) => $.execution_log.retry_failed),
                ),
            );
          },
        });
      }

      const subscribed = !!userId && subscribers.some(
        (s) => s.user_type === "member" && s.user_id === userId,
      );
      commands.push(
        {
          id: "issue-create-sub-issue",
          label: t(($) => $.issue.create_sub_issue),
          icon: SquarePlus,
          keywords: ["sub", "child", "new", "子任务"],
          run: actions.openCreateSubIssue,
        },
        {
          id: "issue-subscribe",
          label: subscribed ? t(($) => $.issue.unsubscribe) : t(($) => $.issue.subscribe),
          icon: subscribed ? BellOff : BellRing,
          keywords: ["subscribe", "watch", "notify", "订阅"],
          run: () => {
            if (!userId) return;
            toggleSubscriber.mutateAsync({ userId, userType: "member", subscribed }).then(
              () =>
                toast.success(
                  subscribed ? t(($) => $.toast.unsubscribed) : t(($) => $.toast.subscribed),
                ),
              (err: unknown) =>
                toast.error(err instanceof Error && err.message ? err.message : t(($) => $.toast.update_failed)),
            );
          },
        },
        {
          id: "issue-pin",
          label: actions.isPinned
            ? tIssues(($) => $.actions.unpin_from_sidebar)
            : tIssues(($) => $.actions.pin_to_sidebar),
          icon: actions.isPinned ? PinOff : Pin,
          keywords: ["pin", "sidebar", "固定"],
          run: actions.togglePin,
        },
        {
          id: "issue-copy-identifier",
          label: t(($) => $.commands.copy_identifier, { identifier: single.identifier }),
          icon: Copy,
          keywords: ["copy", "id", "identifier", single.identifier.toLowerCase()],
          run: () => {
            void copyText(single.identifier).then((ok) => {
              if (ok) {
                toast.success(t(($) => $.toast.copied_identifier, { identifier: single.identifier }));
              }
            });
          },
        },
        {
          id: "issue-fold-comments",
          label: t(($) => $.commands.fold_all_comments),
          icon: ListChevronsDownUp,
          keywords: ["fold", "collapse", "comments", "收起", "折叠", "评论"],
          run: () => {
            // The timeline is already cached whenever the issue page has
            // rendered; ensureQueryData only fetches on a cold cache. If it
            // still can't load, no comments are on screen — dropping the
            // action matches the visible state.
            void queryClient
              .ensureQueryData(issueTimelineOptions(single.id))
              .then((entries) => {
                useCommentCollapseStore.getState().collapseAll(single.id, rootCommentIds(entries));
                useResolvedExpandStore.getState().collapseAll(single.id);
              })
              .catch(() => {});
          },
        },
        {
          id: "issue-unfold-comments",
          label: t(($) => $.commands.unfold_all_comments),
          icon: ListChevronsUpDown,
          keywords: ["unfold", "expand", "comments", "展开", "评论"],
          run: () => {
            void queryClient
              .ensureQueryData(issueTimelineOptions(single.id))
              .then((entries) => {
                useCommentCollapseStore.getState().expandAll(single.id);
                useResolvedExpandStore
                  .getState()
                  .expandAll(single.id, resolvedThreadRootIds(entries));
              })
              .catch(() => {});
          },
        },
        {
          id: "issue-delete",
          label: t(($) => $.issue.delete),
          icon: Trash2,
          keywords: ["delete", "remove", "删除"],
          destructive: true,
          run: () => actions.openDeleteConfirm({ onDeletedFallbackPath }),
        },
      );
    } else if (target.kind === "batch") {
      commands.push(
        {
          id: "issue-parent",
          label: t(($) => $.issue.set_parent),
          icon: Network,
          keywords: ["parent", "sub", "父任务"],
          page: { type: "issue-parent", target },
          crumb,
        },
        {
          id: "issue-delete",
          label: t(($) => $.issue.delete_selected, { count: target.issues.length }),
          icon: Trash2,
          keywords: ["delete", "remove", "删除"],
          destructive: true,
          run: () =>
            palette.openHostDialog({
              type: "batch-delete",
              issues: target.issues,
              clearSelection: target.clearSelection,
            }),
        },
      );
    }

    return {
      id: isSingle ? "context-issue" : "context-issues",
      heading: isSingle ? (
        <ContextHeading
          label={t(($) => $.context.current_issue)}
          subject={target.issue.identifier}
          detail={target.issue.title}
        />
      ) : (
        <ContextHeading label={crumb} />
      ),
      commands,
    };
  }, [
    actions,
    activeQuickActions.length,
    onDeletedFallbackPath,
    palette,
    queryClient,
    retryRun,
    single,
    subscribers,
    t,
    tIssues,
    target,
    tasks,
    toggleSubscriber,
    userId,
  ]);
}
