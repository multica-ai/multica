"use client";

import type { ReactNode } from "react";
import { Archive, ArrowDown, Undo2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import type { InboxBarAction } from "@multica/core/inbox/auto-archive";
import type { KeptInboxRow } from "@multica/core/inbox/open-row";
import { useActorName } from "@multica/core/workspace/hooks";
import type { InboxItem } from "@multica/core/types";
import { useLocale, useT } from "../../i18n";
import { ActionShortcutKeycaps } from "../../common/shortcut-keycaps";
import type { IssueAgentState } from "../../issues/surface/use-issue-agent-state";
import { useStatusLabel } from "../../issues/utils/status-label";
import { formatSnoozeTime } from "./inbox-snooze-time";
import { getInboxDisplayTitle, isQuotaNotice } from "./inbox-display";
import { InboxBar, InboxBarKeycaps, InboxBarSeparator, InboxBarStatus } from "./inbox-bar";
import { useInboxUndoShortcut } from "./use-inbox-shortcut";

/**
 * How a kept row (`keepOpenInboxRow`) says why it is settled: `line` in its
 * place in the list, `status` in the open notification's floating group.
 */
export function useKeptInboxRowText(wsId: string) {
  const { t } = useT("inbox");
  const { t: tIssues } = useT("issues");
  const locale = useLocale();
  const { categoryOf } = useIssueStatuses(wsId);
  const statusLabelOf = useStatusLabel(wsId);
  const { getActorName } = useActorName();
  // Finishing the issue is what archives its notifications on its own, so the
  // status says why where it can.
  const finished = (item: InboxItem) => {
    const status = item.issue_status;
    if (!status) return null;
    const category = categoryOf(status);
    return category === "done" || category === "closed" ? statusLabelOf(status) : null;
  };
  const agentLabel = (agents: IssueAgentState) => {
    if (!agents || agents.agentIds.length === 0) return null;
    if (agents.agentIds.length === 1) {
      return tIssues(
        ($) => (agents.running ? $.agent_live.is_working : $.agent_live.is_queued),
        { name: getActorName("agent", agents.agentIds[0] ?? "") },
      );
    }
    return tIssues(
      ($) =>
        agents.running
          ? $.agent_activity.hover_header
          : $.agent_activity.hover_header_queued,
      { count: agents.agentIds.length },
    );
  };
  const backAt = (item: InboxItem) =>
    item.snoozed_until
      ? t(($) => $.snooze.back_at, { time: formatSnoozeTime(new Date(item.snoozed_until), new Date(), locale) })
      : t(($) => $.kept.archived);
  const did = (action: InboxBarAction) => {
    switch (action.type) {
      case "status":
        return t(($) => $.kept.changed_status, { status: statusLabelOf(action.status) });
      case "assignee":
        return action.assignee
          ? t(($) => $.kept.reassigned, { name: getActorName(action.assignee.type, action.assignee.id) })
          : t(($) => $.kept.unassigned);
      case "quick_action":
        return t(($) => $.kept.ran_quick_action, { name: action.name });
    }
  };
  return {
    isFinished: (item: InboxItem) => finished(item) !== null,
    agentLabel,
    line: (kept: KeptInboxRow, agents?: IssueAgentState) => {
      if (kept.reason === "filtered") return t(($) => $.kept.filtered);
      const why =
        kept.reason === "snoozed"
          ? backAt(kept.item)
          : (finished(kept.item) ?? agentLabel(agents ?? null) ?? t(($) => $.kept.archived));
      return `${why} · ${t(($) => $.kept.leaves)}`;
    },
    /**
     * The floating group's word for a kept row, what undoing it is called, and
     * what the undo's tooltip adds: why it was archived, when known, and that
     * it comes back on new activity. `cause` is the group action that archived
     * it, `agentTriggered` that an agent started on it (both from
     * `useAutoArchiveCause`).
     */
    status: (kept: KeptInboxRow, cause?: InboxBarAction | null, agentTriggered = false) => {
      if (kept.reason === "snoozed") {
        return { label: backAt(kept.item), hint: undefined, undo: t(($) => $.toasts.undo) };
      }
      const why =
        (cause && kept.reason === "archived" ? did(cause) : null) ??
        finished(kept.item) ??
        (agentTriggered ? t(($) => $.kept.agent_triggered) : null);
      const returns = t(($) => $.kept.returns_on_activity);
      return {
        label: t(($) => $.kept.archived),
        hint: why ? `${why} · ${returns}` : returns,
        undo: t(($) => $.kept.undo_archive),
      };
    },
  };
}

/**
 * What the open notification's group offers once it has left the Inbox on its
 * own (its issue reached Done, another window archived it) and stays on
 * screen until the user moves on: the issue's own actions when it was
 * archived, so the next change needs no trip to its properties, the way to
 * undo it (Z), the next notification, then `more`.
 *
 * What happened is not a word of its own between the buttons: the undo names
 * it ("Undo archive"), its tooltip says why, and a screen reader hears both
 * from the group's status.
 */
export function InboxKeptActions({
  label,
  hint,
  undoLabel,
  issueActions,
  onUndo,
  next,
  onNext,
  more,
}: {
  /** What happened, in a word ("Archived"); `hint` says the rest. */
  label: string;
  hint?: string;
  /** Names what the undo takes back ("Undo archive"). */
  undoLabel: string;
  issueActions?: ReactNode;
  onUndo?: () => void;
  /** The notification `onNext` opens; without one there is nothing to move on to. */
  next: InboxItem | null;
  onNext: () => void;
  more?: ReactNode;
}) {
  useInboxUndoShortcut(onUndo ?? null);
  const note = hint ?? label;

  return (
    <InboxBar settled>
      {issueActions}
      {issueActions && <InboxBarSeparator />}
      {onUndo ? (
        <>
          <span className="sr-only">{label}</span>
          {hint && <span className="sr-only">{hint}</span>}
          <TooltipHint
            label={
              <span className="flex flex-col">
                <span>{undoLabel}</span>
                <span className="text-muted-foreground">{note}</span>
              </span>
            }
            shortcut={<ActionShortcutKeycaps action="undoInboxAction" />}
            side="top"
          >
            <Button size="sm" variant="ghost" aria-label={undoLabel} className="shrink-0 text-muted-foreground" onClick={onUndo}>
              <Undo2 />
              <span className="@max-[32rem]/inbox-bar:hidden">{undoLabel}</span>
              <InboxBarKeycaps action="undoInboxAction" />
            </Button>
          </TooltipHint>
        </>
      ) : (
        <InboxBarStatus icon={<Archive />} label={label} hint={hint} />
      )}
      {next && <InboxNextButton next={next} onNext={onNext} />}
      {more}
    </InboxBar>
  );
}

/** Opens the next notification, named in its tooltip; E does the same. */
export function InboxNextButton({ next, onNext }: { next: InboxItem; onNext: () => void }) {
  const { t } = useT("inbox");
  const title = isQuotaNotice(next.type) ? t(($) => $.types[next.type]) : getInboxDisplayTitle(next);
  return (
    <TooltipHint
      label={t(($) => $.kept.next_tooltip, { title })}
      shortcut={<ActionShortcutKeycaps action="archiveInboxItem" />}
      side="top"
    >
      <Button size="sm" className="shrink-0" onClick={onNext}>
        {t(($) => $.kept.next)}
        <ArrowDown />
        <InboxBarKeycaps action="archiveInboxItem" />
      </Button>
    </TooltipHint>
  );
}
