"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useReducedMotion } from "@multica/core/appearance";
import { cn } from "@multica/ui/lib/utils";
import {
  AlarmClock,
  Archive,
  Check,
  Filter,
  Inbox,
  Undo2,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { MulticaBleedMark } from "@multica/ui/components/common/multica-graphics";
import {
  undoArchiveInboxBatch,
  undoUnsubscribeArchiveInbox,
  useUnsnoozeInbox,
} from "@multica/core/inbox/mutations";
import { inboxKeys } from "@multica/core/inbox/queries";
import {
  useInboxSelectionStore,
  type InboxTab,
  type LastInboxCleared,
} from "@multica/core/inbox/selection-store";
import { ActionShortcutKeycaps } from "../../common/shortcut-keycaps";
import { useLocale, useT } from "../../i18n";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "../../navigation";
import { inboxTabHref } from "./inbox-view";
import { formatSnoozeTime } from "./inbox-snooze-time";
import { useInboxUndoShortcut } from "./use-inbox-shortcut";
import { useSnoozedInboxRows } from "./use-inbox-tab-rows";
import { useRestoreInbox } from "./use-leave-inbox-row";

/**
 * The empty detail canvas of `/inbox`:
 * - Prompt to pick a row when the Work panel's tab still lists any.
 * - Quiet empty states for the Snoozed and Archived tabs.
 * - Filtered-empty state when active filters hid remaining notifications
 *   (never confused with true Inbox Zero).
 * - Calm Inbox Zero state with MulticaBleedMark, persistent inline undo for
 *   the action that just cleared the Inbox, and snoozed summary when any
 *   issues are waiting to return.
 */
export function InboxEmptyState({
  wsId,
  tab,
  visibleCount,
  totalActiveCount,
  hasActiveFilters,
  inboxZeroRises,
  isCompact = false,
  onClearFiltersAndContinue,
}: {
  wsId: string;
  tab: InboxTab;
  /** Rows the tab lists. */
  visibleCount: number;
  /** Total active (unfiltered, unarchived) notifications in the Inbox. */
  totalActiveCount: number;
  hasActiveFilters: boolean;
  inboxZeroRises: boolean;
  isCompact?: boolean;
  onClearFiltersAndContinue: () => void;
}) {
  const { t } = useT("inbox");
  const locale = useLocale();
  const qc = useQueryClient();
  const { push } = useNavigation();
  const paths = useWorkspacePaths();
  const restoreInbox = useRestoreInbox();
  const { mutateAsync: unsnoozeAsync } = useUnsnoozeInbox();

  const lastCleared = useInboxSelectionStore((s) => s.lastClearedByWorkspace[wsId] ?? null);
  const clearLastCleared = useInboxSelectionStore((s) => s.clearLastCleared);

  const isLive = tab === "inbox";
  const isFilteredEmpty = isLive && hasActiveFilters && visibleCount === 0 && totalActiveCount > 0;
  const isInboxZero = isLive && visibleCount === 0 && !isFilteredEmpty;

  const { rows: activeSnoozed } = useSnoozedInboxRows(wsId, isInboxZero);

  const [undoing, setUndoing] = useState(false);

  /** Resolves whether the undo went through; a failure is toasted. */
  const requestUndo = (entry: LastInboxCleared): Promise<boolean> => {
    const failed = (fallback: string) => (err: unknown) => {
      toast.error(err instanceof Error && err.message ? err.message : fallback);
      return false;
    };
    switch (entry.kind) {
      case "archived":
        return restoreInbox(entry.itemId, { quiet: true });
      case "snoozed":
        return unsnoozeAsync({ id: entry.itemId, read: entry.wasRead }).then(
          () => true,
          failed(t(($) => $.snooze.unsnooze_failed)),
        );
      case "unsubscribed":
        return undoUnsubscribeArchiveInbox(qc, wsId, { id: entry.itemId, issueId: entry.issueId }).then(
          () => true,
          failed(t(($) => $.errors.undo_unsubscribe_failed)),
        );
      case "batch_archived":
        return undoArchiveInboxBatch(qc, wsId, entry.itemIds).then(
          () => true,
          failed(t(($) => $.errors.unarchive_failed)),
        );
    }
  };

  // The restored row must be in the Inbox list before its link opens: the
  // Inbox page drops a `?issue=` selection it cannot find there. The write
  // refreshes the list without waiting, and that refresh starts first, so this
  // one (cancel, then refetch) is the last and the one awaited.
  const refreshInboxList = async () => {
    await qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
    await qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
  };

  const handleUndoCleared = async (entry: LastInboxCleared) => {
    if (undoing || !canUndoCleared(entry)) return;
    setUndoing(true);
    const undone = await requestUndo(entry);
    if (undone) await refreshInboxList();
    setUndoing(false);
    // A failed undo keeps the entry, so it can be tried again.
    if (!undone) return;

    const store = useInboxSelectionStore.getState();
    clearLastCleared(wsId, entry.key ?? undefined);
    if (entry.kind === "batch_archived") {
      if (entry.key) store.returnRow(wsId, entry.key);
      return;
    }
    store.returnRow(wsId, entry.key);
    store.recordTransition(wsId, entry.key, "prev");
    push?.(entry.href);
  };

  const clearedSummary = (entry: LastInboxCleared): string => {
    if (entry.kind === "archived") return t(($) => $.zero.just_archived, { label: entry.label });
    if (entry.kind === "snoozed") {
      return t(($) => $.zero.just_snoozed, {
        label: entry.label,
        time: formatSnoozeTime(new Date(entry.until), new Date(), locale),
      });
    }
    if (entry.kind === "unsubscribed") return t(($) => $.zero.just_unsubscribed, { label: entry.label });
    return t(($) => $.zero.just_archived_many, { count: entry.count });
  };

  const canUndoCleared = (entry: LastInboxCleared) => entry.kind !== "unsubscribed" || entry.undoable;

  // Z undoes what the inline undo shows, once its toast is gone.
  const showsUndo = isLive && visibleCount === 0 && !!lastCleared && canUndoCleared(lastCleared);
  useInboxUndoShortcut(showsUndo && lastCleared ? () => void handleUndoCleared(lastCleared) : null);
  const undoKeycaps = !isCompact && <ActionShortcutKeycaps action="undoInboxAction" />;
  const reduceMotion = useReducedMotion();

  // Desktop prompt when active notifications exist in the Work panel.
  if (visibleCount > 0) {
    return (
      <div className="relative flex min-h-0 flex-1 flex-col items-center justify-center overflow-hidden text-muted-foreground">
        <Inbox className="mb-3 h-10 w-10 text-faint-foreground" />
        <p className="relative text-body">{t(($) => $.detail.select_prompt)}</p>
      </div>
    );
  }

  if (tab === "snoozed") {
    return (
      <div className="relative flex min-h-0 flex-1 flex-col items-center justify-center overflow-hidden px-6 text-center text-muted-foreground">
        <AlarmClock className="mb-3 h-10 w-10 text-faint-foreground" />
        <p className="relative text-body">{t(($) => $.snooze.view_empty)}</p>
        <p className="relative mt-1 max-w-sm text-label text-faint-foreground">
          {t(($) => $.snooze.view_empty_description)}
        </p>
      </div>
    );
  }

  // Archived view empty state stays quiet and distinct from Inbox Zero.
  if (tab === "archived") {
    return (
      <div className="relative flex min-h-0 flex-1 flex-col items-center justify-center overflow-hidden text-muted-foreground">
        <Archive className="mb-3 h-10 w-10 text-faint-foreground" />
        <p className="relative text-body">{t(($) => $.list.archived_empty)}</p>
      </div>
    );
  }

  // Filtered-empty state: active filters hid other notifications in the Inbox.
  if (isFilteredEmpty) {
    return (
      <div className="relative flex min-h-0 flex-1 flex-col items-center justify-center overflow-hidden px-6 text-center">
        <div className="-mt-8 flex max-w-md flex-col items-center">
          <div className="mb-4 flex size-11 items-center justify-center rounded-xl bg-muted text-muted-foreground">
            <Filter className="size-5" />
          </div>
          <h2 className="text-title font-semibold text-foreground">{t(($) => $.zero.filtered_title)}</h2>
          <p className="mt-1.5 text-label text-muted-foreground">
            {t(($) => $.zero.filtered_remaining, { count: totalActiveCount })}
          </p>
          <div className="mt-6 flex flex-wrap items-center justify-center gap-2">
            <Button size="sm" onClick={onClearFiltersAndContinue}>
              <span>{t(($) => $.zero.clear_and_continue)}</span>
              <span className="tabular-nums opacity-75">{totalActiveCount}</span>
            </Button>
            {lastCleared && canUndoCleared(lastCleared) && (
              <Button size="sm" variant="outline" disabled={undoing} onClick={() => void handleUndoCleared(lastCleared)}>
                <Undo2 />
                {lastCleared.kind === "batch_archived"
                  ? t(($) => $.toasts.undo)
                  : t(($) => $.zero.undo_item, { label: lastCleared.label })}
                {undoKeycaps}
              </Button>
            )}
          </div>
        </div>
      </div>
    );
  }

  // True Inbox Zero.
  const rises = inboxZeroRises || lastCleared !== null;
  const bleedKey = lastCleared ? `rise-${lastCleared.clearedAt}` : inboxZeroRises ? "rise" : "rest";
  const earliestSnooze = activeSnoozed[0] ? new Date(activeSnoozed[0].snoozed_until) : null;

  return (
    <div className="relative flex min-h-0 flex-1 flex-col items-center justify-center overflow-hidden px-6 text-center">
      <MulticaBleedMark
        key={bleedKey}
        enterDelay={rises ? 0.2 : undefined}
        className="pointer-events-none absolute -right-[14%] -bottom-[26%] w-[min(560px,58%)] text-muted"
      />
      <div
        key={`card-${bleedKey}`}
        className={cn(
          "relative z-10 -mt-8 flex max-w-md flex-col items-center",
          !reduceMotion && (rises ? "animate-inbox-enter-next" : "animate-inbox-enter-fade"),
        )}
      >
        <div
          aria-hidden
          className="mb-4 flex size-11 items-center justify-center rounded-xl border border-surface-border bg-surface text-success shadow-[var(--surface-shadow)]"
        >
          <Check className="size-5" />
        </div>
        <h2 className="text-title font-semibold text-foreground">{t(($) => $.zero.cleared_title)}</h2>
        {lastCleared && (
          <p className="mt-1.5 flex flex-wrap items-center justify-center gap-1.5 text-label text-muted-foreground">
            <span>{clearedSummary(lastCleared)}</span>
            {canUndoCleared(lastCleared) && (
              <>
                <span aria-hidden className="text-faint-foreground">·</span>
                <button
                  type="button"
                  disabled={undoing}
                  onClick={() => void handleUndoCleared(lastCleared)}
                  className="-mx-1.5 inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-caption font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring disabled:pointer-events-none disabled:opacity-50"
                >
                  <Undo2 className="size-3" />
                  {t(($) => $.toasts.undo)}
                  {undoKeycaps}
                </button>
              </>
            )}
          </p>
        )}

        {activeSnoozed.length > 0 && earliestSnooze && (
          <button
            type="button"
            onClick={() => push?.(inboxTabHref(paths, "snoozed"))}
            className="mt-2 inline-flex items-center gap-1.5 rounded-md px-1.5 py-0.5 text-caption text-faint-foreground transition-colors hover:text-muted-foreground focus-visible:outline-2 focus-visible:outline-ring"
          >
            <AlarmClock className="size-3.5 shrink-0" />
            <span>
              {t(($) => $.zero.snoozed_summary, {
                count: activeSnoozed.length,
                time: formatSnoozeTime(earliestSnooze, new Date(), locale),
              })}
            </span>
          </button>
        )}
      </div>
    </div>
  );
}
