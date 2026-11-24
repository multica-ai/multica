"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlarmClock, Clock, RotateCcw, X } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Button } from "@multica/ui/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@multica/ui/components/ui/collapsible";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { cn } from "@multica/ui/lib/utils";
import { chatWakeupsOptions } from "@multica/core/chat/queries";
import { useCancelChatWakeup, useRetryChatWakeup } from "@multica/core/chat/mutations";
import type { ChatMessage, ChatWakeup } from "@multica/core/types";
import { useLocale, useT } from "../../i18n";
import { CHAT_COLUMN, CHAT_GUTTER } from "./chat-column";
import { WakeupTimingEditor } from "./chat-wakeup-timing-editor";

const EMPTY: ChatWakeup[] = [];
const MINUTE = 60_000;

/** A conversation's wakeups; empty for a new chat or an older server. */
export function useChatWakeups(sessionId: string | null | undefined): ChatWakeup[] {
  const { data } = useQuery(chatWakeupsOptions(sessionId ?? ""));
  return sessionId ? data ?? EMPTY : EMPTY;
}

/** Re-renders on a coarse clock so countdowns stay roughly current. */
function useNow(intervalMs = 30_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}

function relativeTime(locale: string, targetIso: string | null | undefined, now: number): string {
  const target = targetIso ? Date.parse(targetIso) : NaN;
  if (Number.isNaN(target)) return "";
  const diff = target - now;
  const abs = Math.abs(diff);
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  if (abs < 60 * MINUTE) return rtf.format(Math.round(diff / MINUTE), "minute");
  if (abs < 48 * 60 * MINUTE) return rtf.format(Math.round(diff / (60 * MINUTE)), "hour");
  return rtf.format(Math.round(diff / (24 * 60 * MINUTE)), "day");
}

/** Under 30 minutes, or the last fifth of its window, a wait is running out. */
export function isWakeupRunningOut(w: ChatWakeup, now: number): boolean {
  if (w.status !== "active" || w.kind === "at") return false;
  const expires = Date.parse(w.expires_at);
  const created = Date.parse(w.created_at);
  if (Number.isNaN(expires)) return false;
  const left = expires - now;
  if (left < 30 * MINUTE) return true;
  const total = expires - created;
  return total > 0 && left / total < 0.2;
}

export type WakeupReportState = "waiting" | "running" | "failed";

/**
 * How the report turn of a wakeup that came back is going. Null once it
 * reported, or when the wakeup has not come back.
 */
export function wakeupReportState(w: ChatWakeup): WakeupReportState | null {
  if ((w.status !== "fired" && w.status !== "expired") || !w.last_task_id) return null;
  switch (w.last_task_status) {
    case undefined:
    case "":
    case "completed":
      return null;
    case "running":
      return "running";
    case "failed":
    case "cancelled":
      return "failed";
    default:
      return "waiting";
  }
}

function activeFirst(wakeups: ChatWakeup[]): ChatWakeup[] {
  return wakeups
    .filter((w) => w.status === "active")
    .sort((a, b) => Date.parse(a.expires_at) - Date.parse(b.expires_at));
}

function useWakeupText() {
  const { t } = useT("chat");
  const locale = useLocale();
  return {
    t,
    status: (w: ChatWakeup) => {
      const report = wakeupReportState(w);
      return report ? t(($) => $.wakeup.report[report]) : t(($) => $.wakeup.status[w.status]);
    },
    /** Why a wakeup ended without coming back. */
    failure: (w: ChatWakeup) => {
      if (w.status !== "failed") return "";
      switch (w.end_reason) {
        case "permission_revoked":
        case "access_lost":
        case "dispatch_failed":
          return t(($) => $.wakeup.failure[w.end_reason as "permission_revoked" | "access_lost" | "dispatch_failed"]);
      }
      return w.last_error ?? "";
    },
    kind: (w: ChatWakeup) => t(($) => $.wakeup.kind[w.kind]),
    /** The one time a reader needs: when it fires, or when it gives up. */
    timing: (w: ChatWakeup, now: number) => {
      if (w.status !== "active") return "";
      if (w.kind === "at") {
        return t(($) => $.wakeup.reminder_at, { relative: relativeTime(locale, w.fire_at, now) });
      }
      return t(($) => $.wakeup.ends, { relative: relativeTime(locale, w.expires_at, now) });
    },
    nextCheck: (w: ChatWakeup, now: number) =>
      w.kind === "check" && w.status === "active"
        ? t(($) => $.wakeup.next_check, { relative: relativeTime(locale, w.next_eval_at, now) })
        : "",
  };
}

function useCancel(sessionId: string) {
  const { t } = useT("chat");
  const cancel = useCancelChatWakeup();
  return (wakeupId: string) =>
    cancel.mutate(
      { sessionId, wakeupId },
      { onError: () => toast.error(t(($) => $.wakeup.cancel_failed)) },
    );
}

function useRetry(sessionId: string) {
  const { t } = useT("chat");
  const retry = useRetryChatWakeup();
  return {
    pending: retry.isPending,
    run: (wakeupId: string) =>
      retry.mutate(
        { sessionId, wakeupId },
        { onError: () => toast.error(t(($) => $.wakeup.retry_failed)) },
      ),
  };
}

function RetryButton({ wakeup, sessionId }: { wakeup: ChatWakeup; sessionId: string }) {
  const { t } = useT("chat");
  const retry = useRetry(sessionId);
  return (
    <Button
      variant="ghost"
      size="xs"
      className="shrink-0"
      disabled={retry.pending}
      onClick={() => retry.run(wakeup.id)}
    >
      <RotateCcw aria-hidden />
      {t(($) => $.wakeup.retry)}
    </Button>
  );
}

function WakeupRow({
  wakeup,
  sessionId,
  now,
  editing = false,
  onEditingChange,
}: {
  wakeup: ChatWakeup;
  sessionId: string;
  now: number;
  editing?: boolean;
  onEditingChange?: (editing: boolean) => void;
}) {
  const text = useWakeupText();
  const cancel = useCancel(sessionId);
  const urgent = isWakeupRunningOut(wakeup, now);
  const active = wakeup.status === "active";
  const report = wakeupReportState(wakeup);
  const failure = text.failure(wakeup);
  const details = [text.kind(wakeup), text.timing(wakeup, now), text.nextCheck(wakeup, now)].filter(Boolean);
  const isEditing = editing && active;
  return (
    <Collapsible
      data-slot="chat-wakeup-row"
      open={isEditing}
      onOpenChange={(open) => onEditingChange?.(open)}
      className={cn("rounded-md px-2 py-1.5 transition-colors", isEditing && "bg-surface-hover")}
    >
      <div className="flex min-w-0 items-start gap-2">
        <AlarmClock
          aria-hidden
          className={cn("mt-0.5 size-3.5 shrink-0", active ? "text-brand" : "text-faint-foreground")}
        />
        <div className="min-w-0 flex-1">
          <div className={cn("truncate text-body", !active && "text-muted-foreground")}>{wakeup.title}</div>
          <div
            className={cn(
              "text-caption text-muted-foreground",
              urgent && "text-warning",
              report === "failed" && "text-destructive",
            )}
          >
            {active ? details.join(" · ") : text.status(wakeup)}
          </div>
          {wakeup.last_check_note && active && (
            <div className="truncate text-caption text-muted-foreground">
              {text.t(($) => $.wakeup.last_check, { note: wakeup.last_check_note })}
            </div>
          )}
          {failure && <div className="truncate text-caption text-destructive">{failure}</div>}
        </div>
        {report === "failed" && <RetryButton wakeup={wakeup} sessionId={sessionId} />}
        {active && onEditingChange && (
          <CollapsibleTrigger
            render={
              <IconButton
                variant="ghost"
                size="icon-xs"
                className="shrink-0 text-muted-foreground aria-expanded:bg-surface-selected aria-expanded:text-foreground"
                label={text.t(($) => $.wakeup.change_timing)}
              />
            }
          >
            <Clock aria-hidden />
          </CollapsibleTrigger>
        )}
        {active && (
          <IconButton
            variant="ghost"
            size="icon-xs"
            className="shrink-0 text-muted-foreground"
            label={text.t(($) => $.wakeup.cancel)}
            onClick={() => cancel(wakeup.id)}
          >
            <X aria-hidden />
          </IconButton>
        )}
      </div>
      {active && onEditingChange && (
        <CollapsibleContent>
          <WakeupTimingEditor wakeup={wakeup} sessionId={sessionId} onClose={() => onEditingChange(false)} />
        </CollapsibleContent>
      )}
    </Collapsible>
  );
}

/** Every wakeup of the conversation; an active one can change its timing in place. */
function WakeupPanel({ wakeups, sessionId }: { wakeups: ChatWakeup[]; sessionId: string }) {
  const { t } = useT("chat");
  const now = useNow();
  const active = activeFirst(wakeups);
  const ended = wakeups.filter((w) => w.status !== "active");
  const [editingId, setEditingId] = useState<string | null>(null);
  return (
    <>
      <div className="shrink-0 px-2 py-1.5 text-caption font-medium text-muted-foreground">
        {t(($) => $.wakeup.panel_title)}
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {wakeups.length === 0 && (
          <div className="px-2 py-1.5 text-caption text-muted-foreground">{t(($) => $.wakeup.panel_empty)}</div>
        )}
        {active.map((w) => (
          <WakeupRow
            key={w.id}
            wakeup={w}
            sessionId={sessionId}
            now={now}
            editing={editingId === w.id}
            onEditingChange={(editing) => setEditingId(editing ? w.id : null)}
          />
        ))}
        {ended.length > 0 && (
          <div className="border-t border-surface-border pt-1">
            <div className="px-2 py-1 text-caption text-muted-foreground">{t(($) => $.wakeup.panel_ended)}</div>
            {ended.map((w) => <WakeupRow key={w.id} wakeup={w} sessionId={sessionId} now={now} />)}
          </div>
        )}
      </div>
    </>
  );
}

const PANEL_CLASS = "max-h-[min(28rem,var(--available-height))] w-80 gap-0 overflow-hidden p-1";

/**
 * What the agent is waiting on, just above the composer: the wait that ends
 * soonest, its deadline, and the rest behind "+N". Hidden with nothing active.
 */
export function ChatWakeupStrip({ sessionId, wakeups }: { sessionId: string | null; wakeups: ChatWakeup[] }) {
  const text = useWakeupText();
  const cancel = useCancel(sessionId ?? "");
  const now = useNow();
  const [open, setOpen] = useState(false);
  const active = activeFirst(wakeups);
  const head = active[0];
  if (!sessionId || !head) return null;
  const urgent = isWakeupRunningOut(head, now);
  const label = text.t(($) => $.wakeup.panel_title);
  return (
    <div data-slot="chat-wakeup-strip" className={cn(CHAT_GUTTER, "mb-1.5")}>
      <div
        className={cn(
          CHAT_COLUMN,
          "flex min-w-0 items-center gap-1.5 rounded-md px-2.5 py-1 text-caption ring-1",
          urgent
            ? "bg-amber-50 text-amber-900 ring-amber-200/60 dark:bg-amber-950/40 dark:text-amber-200 dark:ring-amber-900/40"
            : "bg-surface text-muted-foreground ring-surface-border",
        )}
      >
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger
            aria-label={label}
            className="flex min-w-0 flex-1 cursor-pointer items-center gap-1.5 rounded-sm py-0.5 text-left outline-none focus-visible:ring-1 focus-visible:ring-brand"
          >
            <AlarmClock aria-hidden className={cn("size-3.5 shrink-0", !urgent && "text-brand")} />
            <span className="min-w-0 truncate">
              {text.t(($) => $.wakeup.strip_waiting, { title: head.title })}
            </span>
            <span className="shrink-0">· {text.timing(head, now)}</span>
            {active.length > 1 && (
              <span className="shrink-0 rounded-sm bg-muted px-1 text-foreground">+{active.length - 1}</span>
            )}
          </PopoverTrigger>
          <PopoverContent align="start" side="top" sideOffset={8} aria-label={label} className={PANEL_CLASS}>
            <WakeupPanel wakeups={wakeups} sessionId={sessionId} />
          </PopoverContent>
        </Popover>
        <IconButton
          variant="ghost"
          size="icon-xs"
          className="shrink-0 text-current"
          label={text.t(($) => $.wakeup.cancel)}
          onClick={() => cancel(head.id)}
        >
          <X aria-hidden />
        </IconButton>
      </div>
    </div>
  );
}

/** Header entry to the conversation's wakeups; hidden when it has none. */
export function ChatWakeupHeaderButton({ sessionId, wakeups }: { sessionId: string; wakeups: ChatWakeup[] }) {
  const { t } = useT("chat");
  const [open, setOpen] = useState(false);
  if (wakeups.length === 0) return null;
  const activeCount = wakeups.filter((w) => w.status === "active").length;
  const label = t(($) => $.wakeup.panel_title);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        aria-label={label}
        className="flex h-7 items-center gap-1 rounded-md px-1.5 text-caption text-muted-foreground outline-none transition-colors hover:bg-accent data-[popup-open]:bg-accent focus-visible:ring-1 focus-visible:ring-brand"
      >
        <AlarmClock aria-hidden className={cn("size-3.5", activeCount > 0 && "text-brand")} />
        {activeCount > 0 && <span>{activeCount}</span>}
      </PopoverTrigger>
      <PopoverContent align="end" side="bottom" sideOffset={6} aria-label={label} className={PANEL_CLASS}>
        <WakeupPanel wakeups={wakeups} sessionId={sessionId} />
      </PopoverContent>
    </Popover>
  );
}

/**
 * The input row of a wakeup turn: a divider saying why the agent came back,
 * in place of a message the person never wrote.
 */
export function ChatWakeupDivider({ message, wakeup }: { message: ChatMessage; wakeup: ChatWakeup | undefined }) {
  const { t } = useT("chat");
  let reason = t(($) => $.wakeup.divider.generic);
  if (wakeup) {
    const isLastTurn = wakeup.last_task_id === message.task_id;
    if (wakeup.status === "expired" && isLastTurn) reason = t(($) => $.wakeup.divider.expired);
    else reason = t(($) => $.wakeup.divider[wakeup.kind]);
  }
  return (
    <div data-slot="chat-wakeup-divider" className="flex items-center gap-2 py-1 text-caption text-muted-foreground">
      <span aria-hidden className="h-px flex-1 bg-surface-border" />
      <AlarmClock aria-hidden className="size-3.5 shrink-0 text-brand" />
      <span className="min-w-0 truncate">
        {wakeup ? `${reason} · ${wakeup.title}` : reason}
      </span>
      <span aria-hidden className="h-px flex-1 bg-surface-border" />
    </div>
  );
}

/** Under the reply that made a promise: what it will come back for, and when. */
export function ChatWakeupCards({ wakeups }: { wakeups: ChatWakeup[] }) {
  const text = useWakeupText();
  const now = useNow();
  if (wakeups.length === 0) return null;
  return (
    <div data-slot="chat-wakeup-cards" className="mt-2 flex flex-col gap-1">
      {wakeups.map((w) => {
        const active = w.status === "active";
        const urgent = isWakeupRunningOut(w, now);
        const failed = wakeupReportState(w) === "failed";
        return (
          <div
            key={w.id}
            className="flex min-w-0 items-center gap-2 rounded-md border border-surface-border bg-surface px-2.5 py-1.5 text-caption"
          >
            <AlarmClock aria-hidden className={cn("size-3.5 shrink-0", active ? "text-brand" : "text-faint-foreground")} />
            <span className="min-w-0 flex-1 truncate">
              {text.t(($) => $.wakeup.card, { title: w.title })}
            </span>
            <span
              className={cn("shrink-0 text-muted-foreground", urgent && "text-warning", failed && "text-destructive")}
            >
              {active ? text.timing(w, now) : text.status(w)}
            </span>
            {failed && <RetryButton wakeup={w} sessionId={w.chat_session_id} />}
          </div>
        );
      })}
    </div>
  );
}

/** Wakeups grouped by the turn that created them, for reply cards. */
export function useWakeupsBySourceTask(wakeups: ChatWakeup[] | undefined) {
  return useMemo(() => {
    const map = new Map<string, ChatWakeup[]>();
    for (const w of wakeups ?? EMPTY) {
      if (!w.source_task_id) continue;
      map.set(w.source_task_id, [...(map.get(w.source_task_id) ?? []), w]);
    }
    return map;
  }, [wakeups]);
}
