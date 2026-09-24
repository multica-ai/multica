"use client";

import { useCallback } from "react";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { needsMeReasons, type NeedsMeItem } from "@multica/core/home";
import { useActorName } from "@multica/core/workspace/hooks";
import { useT } from "../../i18n";
import { priorityLabel } from "../../issues/utils/priority-label";
import { useStatusLabel } from "../../issues/utils/status-label";

/** Compact localized duration: 35m, 2h, 1d. */
export function useFormatDuration() {
  const { t } = useT("home");
  return useCallback(
    (ms: number): string => {
      const minutes = Math.max(1, Math.floor(ms / 60_000));
      if (minutes < 60) return t(($) => $.duration.minutes, { count: minutes });
      const hours = Math.floor(minutes / 60);
      if (hours < 24) return t(($) => $.duration.hours, { count: hours });
      return t(($) => $.duration.days, { count: Math.floor(hours / 24) });
    },
    [t],
  );
}

/**
 * Words for one queue entry. The verb comes from the kind, the object from the
 * waiting actor's name and the issue title — nothing is parsed out of comment
 * prose, so the line can never misstate what the agent wrote.
 */
export function useNeedsMeCopy() {
  const { t } = useT("home");
  const { t: tIssues } = useT("issues");
  const wsId = useWorkspaceId();
  const statusLabel = useStatusLabel(wsId);
  const userId = useAuthStore((s) => s.user?.id);
  const { getActorName } = useActorName();
  const formatDuration = useFormatDuration();

  /** The actor waiting on the viewer, unless that actor is the viewer. */
  const waitingName = useCallback(
    (item: NeedsMeItem): string | null => {
      const actor = item.actor;
      if (!actor || actor.type === "system") return null;
      if (actor.type === "member" && actor.id === userId) return null;
      return getActorName(actor.type, actor.id);
    },
    [getActorName, userId],
  );

  const sentence = useCallback(
    (item: NeedsMeItem): string => {
      const title = item.title;
      const name = waitingName(item);
      switch (item.kind) {
        case "in_review":
          return name
            ? t(($) => $.needs_me.sentence.in_review_by, { name, title })
            : t(($) => $.needs_me.sentence.in_review, { title });
        case "blocked":
          return name
            ? t(($) => $.needs_me.sentence.blocked_by, { name, title })
            : t(($) => $.needs_me.sentence.blocked, { title });
        case "mentioned":
          return name
            ? t(($) => $.needs_me.sentence.mentioned_by, { name, title })
            : t(($) => $.needs_me.sentence.mentioned, { title });
        case "action_required":
          if (item.inbox?.type === "issue_assigned") {
            return t(($) => $.needs_me.sentence.issue_assigned, { title });
          }
          if (item.inbox?.type === "task_failed") {
            return t(($) => $.needs_me.sentence.task_failed, { title });
          }
          return t(($) => $.needs_me.sentence.action_required, { title });
      }
    },
    [t, waitingName],
  );

  /** Chip text: the existing status name for status entries, else the inbox kind. */
  const kindLabel = useCallback(
    (item: NeedsMeItem): string => {
      if (item.kind === "in_review" || item.kind === "blocked") return statusLabel(item.kind);
      if (item.kind === "mentioned") return t(($) => $.needs_me.kind.mentioned);
      return t(($) => $.needs_me.kind.action_required);
    },
    [statusLabel, t],
  );

  const actionLabel = useCallback(
    (item: NeedsMeItem): string => t(($) => $.needs_me.action[item.kind]),
    [t],
  );

  /**
   * "High · Due today · Linus stopped 1d ago" — the ranking, in words. Rows
   * that already draw the priority glyph pass `withPriority: false`.
   */
  const reasons = useCallback(
    (item: NeedsMeItem, { withPriority = true }: { withPriority?: boolean } = {}): string[] =>
      needsMeReasons(item)
        .filter((reason) => withPriority || reason.kind !== "priority")
        .map((reason) => {
        switch (reason.kind) {
          case "priority":
            return priorityLabel(reason.priority, tIssues);
          case "overdue":
            return t(($) => $.needs_me.reason.overdue);
          case "due_today":
            return t(($) => $.needs_me.reason.due_today);
          case "due_tomorrow":
            return t(($) => $.needs_me.reason.due_tomorrow);
          case "waiting": {
            const duration = formatDuration(reason.ms);
            const name = item.kind === "blocked" ? waitingName(item) : null;
            return name
              ? t(($) => $.needs_me.reason.stopped, { name, duration })
              : t(($) => $.needs_me.reason.waiting, { duration });
          }
        }
        }),
    [formatDuration, t, tIssues, waitingName],
  );

  return { sentence, kindLabel, actionLabel, reasons, waitingName };
}
