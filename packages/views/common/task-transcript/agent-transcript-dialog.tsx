"use client";

import { useState, useRef, useCallback, useEffect, useLayoutEffect, useMemo, forwardRef } from "react";
import { Virtuoso, type VirtuosoHandle, type Components } from "react-virtuoso";
import {
  Bot,
  ChevronRight,
  Brain,
  AlertCircle,
  CheckCircle2,
  XCircle,
  X,
  Loader2,
  Clock,
  Copy,
  Check,
  Filter,
  ArrowDownNarrowWide,
  ArrowUpNarrowWide,
  ListCollapse,
  Info,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@multica/ui/components/ui/collapsible";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuSeparator,
  DropdownMenuCheckboxItem,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { ActorAvatar } from "../actor-avatar";
import { AttributionBadge } from "../../issues/components/attribution-badge";
import { RichContent } from "../../rich-content";
import { api } from "@multica/core/api";
import {
  useTranscriptViewStore,
  type TranscriptDetailDensity,
  type TranscriptFilterKey,
  type TranscriptSortDirection,
} from "@multica/core/agents/stores";
import type { AgentTask, Agent, AgentRuntime } from "@multica/core/types/agent";
import { runtimeDisplayName } from "@multica/core/runtimes";
import { redactSecrets } from "./redact";
import {
  createNewestFirstFollow,
  FOLLOW_EDGE_THRESHOLD,
  LINE_SCROLL_PX,
} from "./transcript-follow";
import type { TimelineItem } from "./build-timeline";
import {
  traceEventCopyText,
  traceEventDefaultExpanded,
  traceEventDetail,
  traceEventHasDetail,
  traceEventKind,
  traceEventLabel,
  traceEventSummary,
  traceEventSummaryIsMono,
} from "./trace-event-presenter";
import type { TraceDiffLine, TracePatchFile, TraceSummaryLabels } from "./trace-event-presenter";
import { highlightBlock, highlightToLines, languageForPath } from "./diff-highlight";
import { useT } from "../../i18n";
import "../../editor/styles/code.css";
import "./task-transcript.css";

interface AgentTranscriptDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  task: AgentTask;
  items: TimelineItem[];
  agentName: string;
  isLive?: boolean;
  /**
   * Optional content rendered between the header chips and the event list.
   * Used by autopilot run rows to surface the inbound webhook trigger
   * payload so it's visible regardless of whether the agent echoes it.
   * The dialog stays generic — slot content is the caller's concern.
   */
  headerSlot?: React.ReactNode;
}

// ─── Color mapping for timeline segments ────────────────────────────────────

type EventColor = "agent" | "thinking" | "tool" | "result" | "error";

function getEventColor(item: TimelineItem): EventColor {
  switch (item.type) {
    case "text":
      return "agent";
    case "thinking":
      return "thinking";
    case "tool_use":
      return "tool";
    case "tool_result":
      return "result";
    case "error":
      return "error";
    default:
      return "result";
  }
}

const colorClasses: Record<EventColor, { bg: string; bgActive: string; label: string }> = {
  agent: { bg: "bg-emerald-400/60", bgActive: "bg-emerald-500", label: "bg-emerald-500" },
  thinking: { bg: "bg-violet-400/60", bgActive: "bg-violet-500", label: "bg-violet-500/20 text-violet-700 dark:text-violet-300" },
  tool: { bg: "bg-blue-400/60", bgActive: "bg-blue-500", label: "bg-blue-500/20 text-blue-700 dark:text-blue-300" },
  result: { bg: "bg-slate-300/60 dark:bg-slate-600/60", bgActive: "bg-slate-400 dark:bg-slate-500", label: "bg-muted text-muted-foreground" },
  error: { bg: "bg-red-400/60", bgActive: "bg-red-500", label: "bg-red-500/20 text-red-700 dark:text-red-300" },
};

// ─── Helpers ────────────────────────────────────────────────────────────────
// Presentation rules (kind/label/summary/default expansion) live in the pure
// trace-event-presenter module; only view-plumbing helpers remain here.

function getItemFilterKey(item: TimelineItem): TranscriptFilterKey {
  return item.tool && (item.type === "tool_use" || item.type === "tool_result")
    ? `tool:${item.tool}`
    : item.type;
}

function formatDuration(start: string, end: string): string {
  const ms = new Date(end).getTime() - new Date(start).getTime();
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const secs = seconds % 60;
  return `${minutes}m ${secs}s`;
}

function formatElapsedMs(ms: number): string {
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const secs = seconds % 60;
  return `${minutes}m ${secs}s`;
}

function formatRunTime(iso: string): string {
  return new Date(iso).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

// ─── Run detail row (ⓘ popover) ─────────────────────────────────────────────
// One labeled fact in the diagnostic popover. When `onCopy` is given the whole
// row is a copy button (used for the workdir path).
function RunDetailRow({
  label,
  value,
  mono,
  onCopy,
  copied,
  copyTitle,
}: {
  label: string;
  value: string;
  mono?: boolean;
  onCopy?: () => void;
  copied?: boolean;
  copyTitle?: string;
}) {
  const valueClass = cn("min-w-0 select-text break-all text-foreground/80", mono && "font-mono");
  if (onCopy) {
    return (
      <button
        type="button"
        onClick={onCopy}
        title={copyTitle}
        className="group -mx-1 grid w-[calc(100%+0.5rem)] grid-cols-[4.5rem_minmax(0,1fr)] items-start gap-3 rounded px-1 py-0.5 text-left transition-colors hover:bg-accent/60"
      >
        <span className="text-muted-foreground">{label}</span>
        <span className="flex min-w-0 items-start gap-1.5">
          <span className={cn(valueClass, "flex-1")}>{value}</span>
          {copied ? (
            <Check className="mt-0.5 h-3 w-3 shrink-0 text-success" />
          ) : (
            <Copy className="mt-0.5 h-3 w-3 shrink-0 opacity-0 transition-opacity group-hover:opacity-100" />
          )}
        </span>
      </button>
    );
  }
  return (
    <div className="grid grid-cols-[4.5rem_minmax(0,1fr)] items-start gap-3">
      <span className="text-muted-foreground">{label}</span>
      <span className={valueClass}>{value}</span>
    </div>
  );
}

// ─── Main dialog ────────────────────────────────────────────────────────────

// Virtuoso mounts rows as direct children of its List element; carry the
// divider styling the plain list container used to provide. Defined at module
// scope — an inline `components` object would remount the list every render.
const VirtuosoList = forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  function VirtuosoList(props, ref) {
    return <div ref={ref} {...props} className="divide-y" />;
  },
);
const LIST_COMPONENTS: Components<TimelineItem> = { List: VirtuosoList };

export function AgentTranscriptDialog({
  open,
  onOpenChange,
  task,
  items,
  agentName,
  isLive = false,
  headerSlot,
}: AgentTranscriptDialogProps) {
  const { t } = useT("agents");
  const [selectedSeq, setSelectedSeq] = useState<number | null>(null);
  const [elapsed, setElapsed] = useState("");
  const [copied, setCopied] = useState(false);
  const [copiedWorkdir, setCopiedWorkdir] = useState(false);
  const [agentInfo, setAgentInfo] = useState<Agent | null>(null);
  const [runtimeInfo, setRuntimeInfo] = useState<AgentRuntime | null>(null);
  // Row-level expand overrides. A row the user toggled follows the toggle; any
  // other row follows the density preference (see traceEventDefaultExpanded).
  // Switching density or task resets the overrides wholesale.
  const [rowOverrides, setRowOverrides] = useState<Map<number, boolean>>(() => new Map());
  const sortDirection = useTranscriptViewStore((s) => s.sortDirection);
  const setSortDirection = useTranscriptViewStore((s) => s.setSortDirection);
  // Filters always persist across opens — a facet a run doesn't have simply
  // no-ops (see activeFilterKeys), so there is no reason to make persistence a
  // user-facing toggle.
  const selectedFilterKeys = useTranscriptViewStore((s) => s.selectedFilterKeys);
  const toggleFilterKey = useTranscriptViewStore((s) => s.toggleFilterKey);
  const clearFilters = useTranscriptViewStore((s) => s.clearFilterKeys);
  const density = useTranscriptViewStore((s) => s.density);
  const setDensity = useTranscriptViewStore((s) => s.setDensity);
  const virtuosoRef = useRef<VirtuosoHandle>(null);
  // Newest-first live follow (#5921): all latch decisions live in the pure
  // controller (see transcript-follow.ts for the model); this component only
  // wires DOM events to it. A stable instance, never re-rendered by scroll
  // traffic.
  const followCtl = useMemo(() => createNewestFirstFollow(), []);
  const detachScrollerRef = useRef<(() => void) | null>(null);

  const handleScrollerRef = useCallback(
    (el: HTMLElement | Window | null) => {
      detachScrollerRef.current?.();
      detachScrollerRef.current = null;
      if (!(el instanceof HTMLElement)) return;
      const onWheel = (e: WheelEvent) => {
        const scale =
          e.deltaMode === 1 ? LINE_SCROLL_PX : e.deltaMode === 2 ? el.clientHeight : 1;
        followCtl.input(e.deltaY * scale);
      };
      let lastTouchY: number | null = null;
      const onTouchStart = (e: TouchEvent) => {
        lastTouchY = e.touches[0]?.clientY ?? null;
      };
      const onTouchMove = (e: TouchEvent) => {
        const y = e.touches[0]?.clientY;
        if (y === undefined) return;
        // Finger moving up scrolls the content down (away from the live end).
        if (lastTouchY !== null) followCtl.input(lastTouchY - y);
        lastTouchY = y;
      };
      const onKeyDown = (e: KeyboardEvent) => {
        // Only keys aimed at the scroller itself; Space/arrows bubbling from
        // row controls (e.g. a collapsible trigger) are not scroll intent.
        if (e.target !== el) return;
        if (e.key === "ArrowDown") followCtl.input(LINE_SCROLL_PX);
        else if (e.key === "ArrowUp") followCtl.input(-LINE_SCROLL_PX);
        else if (e.key === "PageDown" || e.key === " ") followCtl.input(el.clientHeight);
        else if (e.key === "PageUp") followCtl.input(-el.clientHeight);
        else if (e.key === "End") followCtl.disengage();
      };
      // Scrollbar drags hit the scroller element itself; clicks on row
      // content hit children (tracked too — enforcement must not fight text
      // selection autoscroll while the button is held).
      const onPointerDown = (e: MouseEvent) => {
        followCtl.pointerDown(e.target === el);
      };
      const onPointerUp = () => {
        followCtl.pointerUp();
      };
      const onScroll = () => {
        // Enforce the follow here, on the scroll event itself: Virtuoso's
        // prepend compensation lands after React effects, so an effect-timed
        // snap alone stays one flush behind. NOTE: this direct write staying
        // ahead of Virtuoso's own state relies on react-virtuoso registering
        // its scroll listener after calling scrollerRef (true in 4.18.7); if
        // that inverts, route the write through virtuosoRef.scrollTo instead.
        if (followCtl.onScroll(el.scrollTop)) el.scrollTop = 0;
      };
      el.addEventListener("wheel", onWheel, { passive: true });
      el.addEventListener("touchstart", onTouchStart, { passive: true });
      el.addEventListener("touchmove", onTouchMove, { passive: true });
      el.addEventListener("keydown", onKeyDown);
      el.addEventListener("mousedown", onPointerDown);
      window.addEventListener("mouseup", onPointerUp, { capture: true });
      el.addEventListener("scroll", onScroll, { passive: true });
      detachScrollerRef.current = () => {
        el.removeEventListener("wheel", onWheel);
        el.removeEventListener("touchstart", onTouchStart);
        el.removeEventListener("touchmove", onTouchMove);
        el.removeEventListener("keydown", onKeyDown);
        el.removeEventListener("mousedown", onPointerDown);
        window.removeEventListener("mouseup", onPointerUp, { capture: true });
        el.removeEventListener("scroll", onScroll);
        // The scroller can detach mid-drag (listEpoch remount); a stuck
        // held-mouse flag would suppress enforcement forever.
        followCtl.pointerUp();
      };
    },
    [followCtl],
  );

  useEffect(() => () => detachScrollerRef.current?.(), []);

  useEffect(() => {
    setRowOverrides(new Map());
  }, [task.id, density]);

  // Derive filter options from each item:
  //   tool_use / tool_result → filter value = tool, display = "tool:Bash"
  //   other types → display from traceEventLabel
  const filterOptions = useMemo(() => {
    const options = new Map<string, string>();
    for (const item of items) {
      const key = getItemFilterKey(item);
      if (item.tool && (item.type === "tool_use" || item.type === "tool_result")) {
        if (!options.has(key)) options.set(key, key);
      } else {
        if (!options.has(key)) {
          options.set(key, traceEventLabel(item));
        }
      }
    }
    return Array.from(options.entries()).sort((a, b) => a[1].localeCompare(b[1]));
  }, [items]);

  const filterOptionKeys = useMemo(
    () => new Set(filterOptions.map(([value]) => value)),
    [filterOptions],
  );

  const activeFilterKeys = useMemo(
    () => selectedFilterKeys.filter((key) => filterOptionKeys.has(key)),
    [selectedFilterKeys, filterOptionKeys],
  );

  const activeFilterSet = useMemo(() => new Set(activeFilterKeys), [activeFilterKeys]);

  // Strict filter
  const filteredItems = useMemo(() => {
    if (activeFilterSet.size === 0) return items;
    return items.filter((item) => activeFilterSet.has(getItemFilterKey(item)));
  }, [items, activeFilterSet]);

  // Apply user-chosen sort direction. Reverse is a pure presentation concern —
  // the underlying timeline (and its seq numbers) is untouched, so copy/filter
  // and segment navigation continue to work against the same data.
  const displayItems = useMemo(
    () => (sortDirection === "newest_first" ? [...filteredItems].reverse() : filteredItems),
    [filteredItems, sortDirection],
  );
  const isAntigravityLiveEmpty =
    isLive && displayItems.length === 0 && runtimeInfo?.provider === "antigravity";

  // Newest-first shows live events as PREPENDS, and Virtuoso items opt out of
  // native scroll anchoring (`overflow-anchor: none`), so without compensation
  // every 500ms flush shifts the reading position. Virtuoso's contract: a
  // decrease of firstItemIndex by N anchors the viewport across an N-item
  // prepend, and the value must never increase within an instance — counting
  // down from a large base satisfies that while the list only grows. Sort,
  // filter, or task changes can shrink the list, so `listEpoch` remounts the
  // instance (fresh at top) instead of letting firstItemIndex climb.
  // `scrollToIndex`/`computeItemKey` are unaffected: indices stay data-relative
  // (verified against scrollToIndexSystem — it never reads firstItemIndex).
  const firstItemIndex =
    sortDirection === "newest_first" ? 1_000_000 - displayItems.length : 0;
  const listEpoch = `${task.id}:${sortDirection}:${activeFilterKeys.join(",")}`;

  // Toggling direction is a manual user action; jump the scroll container back
  // to the top so the newest end of the timeline (per the chosen direction) is
  // immediately visible. Avoids stranding the user mid-scroll on the wrong end.
  const handleSortDirectionChange = useCallback(
    (dir: typeof sortDirection) => {
      if (dir === sortDirection) return;
      setSortDirection(dir);
      virtuosoRef.current?.scrollTo({ top: 0 });
    },
    [sortDirection, setSortDirection],
  );

  useLayoutEffect(() => {
    followCtl.setActive(isLive && sortDirection === "newest_first");
  }, [followCtl, isLive, sortDirection]);

  // A new list instance (task/sort/filter change) mounts at its live end with
  // the follow engaged — the same contract as first open.
  useLayoutEffect(() => {
    followCtl.reset();
  }, [followCtl, listEpoch]);

  // Live follow for newest-first (#5921): while the latch is engaged, every
  // flush snaps back to the newest row — prepend anchoring would otherwise
  // hold the viewport on the previous first row. Instant (not smooth): a
  // smooth animation spans multiple flushes and its in-flight position reads
  // as user displacement. The scroll-event enforcement in handleScrollerRef
  // is the authoritative pin (Virtuoso's prepend compensation lands after
  // React effects); this effect just shortens the first-paint gap.
  // Chronological live follow is handled natively by Virtuoso's followOutput
  // below; this pair is its prepend-side counterpart.
  const displayCount = displayItems.length;
  useLayoutEffect(() => {
    if (!followCtl.isFollowing()) return;
    virtuosoRef.current?.scrollToIndex({ index: 0, align: "start", behavior: "auto" });
  }, [followCtl, displayCount]);

  // Fetch agent and runtime metadata when dialog opens
  useEffect(() => {
    if (!open) return;
    let cancelled = false;

    if (task.agent_id) {
      api.getAgent(task.agent_id).then((agent) => {
        if (!cancelled) setAgentInfo(agent);
      }).catch(() => {});
    }

    if (task.runtime_id) {
      api.listRuntimes().then((runtimes) => {
        if (cancelled) return;
        const rt = runtimes.find((r) => r.id === task.runtime_id);
        if (rt) setRuntimeInfo(rt);
      }).catch(() => {});
    }

    return () => { cancelled = true; };
  }, [open, task.agent_id, task.runtime_id]);

  // Elapsed time for live tasks
  useEffect(() => {
    if (!isLive || (!task.started_at && !task.dispatched_at)) return;
    const startRef = task.started_at ?? task.dispatched_at!;
    const update = () => setElapsed(formatElapsedMs(Date.now() - new Date(startRef).getTime()));
    update();
    const interval = setInterval(update, 1000);
    return () => clearInterval(interval);
  }, [isLive, task.started_at, task.dispatched_at]);

  // Rows are virtualized, so an off-screen target is not in the DOM;
  // navigate by index instead of a node ref.
  const handleSegmentClick = useCallback(
    (seq: number) => {
      setSelectedSeq(seq);
      const index = displayItems.findIndex((item) => item.seq === seq);
      if (index < 0) return;
      // Explicit navigation away from the live end unlatches the newest-first
      // follow — otherwise the scroll-event enforcement would pin the viewport
      // straight back to the top. Scrolling back re-engages it as usual.
      if (index > 0) followCtl.disengage();
      virtuosoRef.current?.scrollToIndex({ index, align: "center", behavior: "smooth" });
    },
    [displayItems, followCtl],
  );

  // Copy all events as text. Use the displayed order so users get the same
  // sequence they see on screen — matters when sort is set to newest-first.
  const handleCopyWorkdir = useCallback(() => {
    if (!task.relative_work_dir) return;
    void copyText(task.relative_work_dir).then((ok) => {
      if (!ok) return;
      setCopiedWorkdir(true);
      setTimeout(() => setCopiedWorkdir(false), 2000);
    });
  }, [task.relative_work_dir]);

  const handleCopyAll = useCallback(() => {
    // Copy the full body of each event (not the truncated row summary), with
    // the same secret redaction the detail view applies.
    const text = displayItems
      .map((item) => redactSecrets(traceEventCopyText(item)))
      .join("\n\n");
    void copyText(text).then((ok) => {
      if (!ok) return;
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }, [displayItems]);


  const handleRowExpandedChange = useCallback((seq: number, expanded: boolean) => {
    setRowOverrides((prev) => {
      const next = new Map(prev);
      next.set(seq, expanded);
      return next;
    });
  }, []);

  // Duration
  const duration =
    task.started_at && task.completed_at
      ? formatDuration(task.started_at, task.completed_at)
      : isLive
        ? elapsed
        : null;

  const copyTranscriptLabel = copied
    ? t(($) => $.transcript.copied)
    : activeFilterKeys.length > 0
      ? t(($) => $.transcript.copy_filtered)
      : t(($) => $.transcript.copy_all);

  // Status badge — full state machine, so queued/dispatched/cancelled render as
  // proper labels instead of raw enum text.
  const effectiveStatus = isLive ? "running" : task.status;
  const statusBadge = (() => {
    const base = "inline-flex shrink-0 items-center gap-1 rounded-full px-2 py-0.5 text-caption font-medium";
    switch (effectiveStatus) {
      case "running":
        return (
          <span className={cn(base, "bg-info/15 text-info")}>
            <Loader2 className="h-3 w-3 animate-spin" />
            {t(($) => $.transcript.status_running)}
          </span>
        );
      case "completed":
        return (
          <span className={cn(base, "bg-success/15 text-success")}>
            <CheckCircle2 className="h-3 w-3" />
            {t(($) => $.transcript.status_completed)}
          </span>
        );
      case "failed":
        return (
          <span className={cn(base, "bg-destructive/15 text-destructive")}>
            <XCircle className="h-3 w-3" />
            {t(($) => $.transcript.status_failed)}
          </span>
        );
      case "cancelled":
        return (
          <span className={cn(base, "bg-muted text-muted-foreground")}>
            <XCircle className="h-3 w-3" />
            {t(($) => $.transcript.status_cancelled)}
          </span>
        );
      case "queued":
        return (
          <span className={cn(base, "bg-muted text-muted-foreground")}>
            {t(($) => $.transcript.status_queued)}
          </span>
        );
      case "dispatched":
        return (
          <span className={cn(base, "bg-info/15 text-info")}>
            {t(($) => $.transcript.status_dispatched)}
          </span>
        );
      case "waiting_local_directory":
        return (
          <span className={cn(base, "bg-muted text-muted-foreground")}>
            {t(($) => $.transcript.status_waiting)}
          </span>
        );
      default:
        return (
          <span className={cn(base, "bg-muted text-muted-foreground capitalize")}>
            {task.status}
          </span>
        );
    }
  })();

  // Trigger source: one word answering "why does this run exist" — more useful
  // up front than the runtime/provider diagnostics, which move to the ⓘ popover.
  const triggerLabel = task.parent_task_id
    ? t(($) => $.transcript.trigger_retry)
    : task.kind === "comment" || task.trigger_comment_id
      ? t(($) => $.transcript.trigger_comment)
      : task.kind === "autopilot" || task.autopilot_run_id
        ? t(($) => $.transcript.trigger_autopilot)
        : task.kind === "chat" || task.chat_session_id
          ? t(($) => $.transcript.trigger_chat)
          : task.kind === "quick_create"
            ? t(($) => $.transcript.trigger_quick_create)
            : task.kind === "direct" || task.handoff_note
              ? t(($) => $.transcript.trigger_direct)
              : t(($) => $.transcript.trigger_initial);

  // Diagnostic detail for the ⓘ popover: everything a reader needs only when
  // debugging this specific run, kept off the always-visible surface.
  const providerLabel = runtimeInfo?.provider ? formatProvider(runtimeInfo.provider) : null;
  const createdLabel = task.created_at ? formatRunTime(task.created_at) : null;
  const startedLabel = task.started_at ? formatRunTime(task.started_at) : null;
  const completedLabel = task.completed_at ? formatRunTime(task.completed_at) : null;
  // "When was this run created" — a read-before-you-read fact worth the toolbar
  // surface (the ⓘ popover keeps the full-precision created/started/completed).
  const createdShort = task.created_at
    ? new Date(task.created_at).toLocaleString(undefined, {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      })
    : null;
  const hasTriggeredBy = !!task.attribution?.initiator;
  const hasRunDetails =
    !!runtimeInfo ||
    !!task.relative_work_dir ||
    !!createdLabel ||
    !!startedLabel ||
    !!completedLabel;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="!max-w-4xl !w-[calc(100vw-4rem)] !max-h-[calc(100vh-4rem)] !h-[calc(100vh-4rem)] flex flex-col !p-0 !gap-0 overflow-hidden"
        showCloseButton={false}
      >
        <DialogTitle className="sr-only">{t(($) => $.transcript.dialog_title)}</DialogTitle>

        {/* ── Header: identity only ──────────────────────────────────
            Tier 1 — everything a viewer needs BEFORE reading: outcome
            (status anchors the left), who ran it, why it exists (trigger),
            and who's accountable. All diagnostics move to the ⓘ popover. */}
        <div className="border-b px-4 py-3 shrink-0">
          <div className="flex min-w-0 items-center gap-3">
            {statusBadge}
            {/* Primary identity: the agent that ran this. It is the one
                foreground entity — avatar + medium weight. */}
            <div className="flex min-w-0 items-center gap-2">
              {task.agent_id ? (
                <ActorAvatar actorType="agent" actorId={task.agent_id} size="sm" enableHoverCard />
              ) : (
                <div className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-info/10 text-info">
                  <Bot className="h-3 w-3" />
                </div>
              )}
              <span className="truncate font-medium text-body">
                {agentName || agentInfo?.name || ""}
              </span>
            </div>
            {/* Provenance, one muted secondary unit set apart from the agent:
                who triggered the run and how — reads as "<person> · <how>",
                not three peer entities. The person's avatar is dropped here so
                two same-size faces don't read as two agents. */}
            <div className="flex min-w-0 flex-1 items-center gap-x-1.5 overflow-hidden text-caption text-muted-foreground">
              {hasTriggeredBy && (
                <>
                  <AttributionBadge
                    attribution={task.attribution}
                    variant="inline"
                    hideAvatar
                    className="min-w-0"
                  />
                  <FactDot />
                </>
              )}
              <span className="shrink-0">{triggerLabel}</span>
            </div>

            <div className="flex shrink-0 items-center gap-0.5">
              {hasRunDetails && (
                <Popover>
                  <PopoverTrigger
                    render={
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label={t(($) => $.transcript.run_info)}
                        title={t(($) => $.transcript.run_info)}
                        className="text-muted-foreground"
                      />
                    }
                  >
                    <Info className="h-3.5 w-3.5" />
                  </PopoverTrigger>
                  <PopoverContent align="end" className="w-80 max-w-[calc(100vw-2rem)] p-3">
                    <div className="mb-2 text-caption font-medium text-foreground">
                      {t(($) => $.transcript.run_info)}
                    </div>
                    <div className="space-y-1 text-caption">
                      {runtimeInfo && (
                        <RunDetailRow
                          label={t(($) => $.transcript.details_runtime)}
                          value={runtimeDisplayName(runtimeInfo)}
                        />
                      )}
                      {providerLabel && (
                        <RunDetailRow label={t(($) => $.transcript.details_provider)} value={providerLabel} />
                      )}
                      {runtimeInfo && (
                        <RunDetailRow label={t(($) => $.transcript.details_mode)} value={runtimeInfo.runtime_mode} />
                      )}
                      {task.relative_work_dir && (
                        <RunDetailRow
                          label={t(($) => $.transcript.details_workdir)}
                          value={task.relative_work_dir}
                          mono
                          onCopy={handleCopyWorkdir}
                          copied={copiedWorkdir}
                          copyTitle={t(($) => $.transcript.copy_workdir)}
                        />
                      )}
                      {createdLabel && (
                        <RunDetailRow label={t(($) => $.transcript.details_created)} value={createdLabel} />
                      )}
                      {startedLabel && (
                        <RunDetailRow label={t(($) => $.transcript.details_started)} value={startedLabel} />
                      )}
                      {completedLabel && (
                        <RunDetailRow label={t(($) => $.transcript.details_completed)} value={completedLabel} />
                      )}
                    </div>
                  </PopoverContent>
                </Popover>
              )}
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={() => onOpenChange(false)}
                aria-label={t(($) => $.transcript.close)}
                className="text-muted-foreground"
              >
                <X className="h-4 w-4" />
              </Button>
            </div>
          </div>
        </div>

        {/* ── List toolbar: read-before-you-read summary (left) + controls
            (right). Duration + event count fill the left, so the row balances
            instead of leaving dead space. ── */}
        <div className="flex items-center gap-3 border-b px-4 py-1.5 shrink-0">
          <div className="flex min-w-0 flex-1 items-center gap-x-1.5 overflow-hidden whitespace-nowrap text-caption text-muted-foreground">
            {createdShort && (
              <>
                <span>{t(($) => $.transcript.fact_created, { time: createdShort })}</span>
                <FactDot />
              </>
            )}
            {duration && (
              <>
                <span className="tabular-nums">{t(($) => $.transcript.fact_took, { duration })}</span>
                <FactDot />
              </>
            )}
            <span>
              {activeFilterKeys.length > 0
                ? t(($) => $.transcript.events_filtered, { shown: filteredItems.length, total: items.length })
                : t(($) => $.transcript.events, { count: items.length })}
            </span>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            {items.length > 0 && (
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="sm"
                      aria-label={t(($) => $.transcript.density_label)}
                      className="text-muted-foreground"
                    />
                  }
                >
                  <ListCollapse className="h-3 w-3" />
                  <span className="hidden sm:inline">
                    {density === "smart"
                      ? t(($) => $.transcript.density_smart)
                      : density === "expanded"
                        ? t(($) => $.transcript.density_expanded)
                        : t(($) => $.transcript.density_collapsed)}
                  </span>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-64">
                  <DropdownMenuRadioGroup
                    value={density}
                    onValueChange={(value) => setDensity(value as TranscriptDetailDensity)}
                  >
                    {(
                      [
                        ["smart", t(($) => $.transcript.density_smart), t(($) => $.transcript.density_smart_desc)],
                        ["expanded", t(($) => $.transcript.density_expanded), t(($) => $.transcript.density_expanded_desc)],
                        ["collapsed", t(($) => $.transcript.density_collapsed), t(($) => $.transcript.density_collapsed_desc)],
                      ] as const
                    ).map(([value, name, description]) => (
                      <DropdownMenuRadioItem key={value} value={value} className="items-start">
                        <span className="flex min-w-0 flex-col gap-0.5">
                          <span>{name}</span>
                          <span className="text-micro leading-snug text-muted-foreground">
                            {description}
                          </span>
                        </span>
                      </DropdownMenuRadioItem>
                    ))}
                  </DropdownMenuRadioGroup>
                </DropdownMenuContent>
              </DropdownMenu>
            )}
            {items.length > 1 && (
              <SortDirectionToggle
                value={sortDirection}
                onChange={handleSortDirectionChange}
                labels={{
                  chronological: t(($) => $.transcript.sort_chronological),
                  newestFirst: t(($) => $.transcript.sort_newest_first),
                  ariaLabel: t(($) => $.transcript.sort_label),
                }}
              />
            )}
            {filterOptions.length > 0 && (
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button
                      variant={activeFilterKeys.length > 0 ? "brand" : "ghost"}
                      size="sm"
                      aria-label={t(($) => $.transcript.filter)}
                      className={activeFilterKeys.length > 0 ? undefined : "text-muted-foreground"}
                    />
                  }
                >
                  <Filter className="h-3 w-3" />
                  <span className="hidden sm:inline">{t(($) => $.transcript.filter)}</span>
                  {activeFilterKeys.length > 0 && (
                    <span className="ml-0.5 rounded-full bg-brand-foreground/20 px-1.5 py-0 text-micro font-medium tabular-nums">
                      {activeFilterKeys.length}
                    </span>
                  )}
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-auto">
                  {filterOptions.map(([value, label]) => (
                    <DropdownMenuCheckboxItem
                      key={value}
                      checked={selectedFilterKeys.includes(value)}
                      onCheckedChange={() => toggleFilterKey(value)}
                    >
                      {label}
                    </DropdownMenuCheckboxItem>
                  ))}
                  {selectedFilterKeys.length > 0 && (
                    <>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem onClick={clearFilters} className="text-muted-foreground">
                        {t(($) => $.transcript.clear_filters)}
                      </DropdownMenuItem>
                    </>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
            )}
            <Button
              variant="ghost"
              size="sm"
              onClick={handleCopyAll}
              aria-label={copyTranscriptLabel}
              className="text-muted-foreground"
            >
              {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
              <span className="hidden sm:inline">{copyTranscriptLabel}</span>
            </Button>
          </div>
        </div>

        {/* ── Timeline progress bar ─────────────────────────────── */}
        {displayItems.length > 0 && (
          <div className="border-b px-4 py-2.5 shrink-0">
            <TimelineBar
              items={displayItems}
              selectedSeq={selectedSeq}
              onSegmentClick={handleSegmentClick}
            />
          </div>
        )}

        {/* ── Optional header slot (e.g. webhook payload preview) ── */}
        {headerSlot && (
          <div className="border-b px-4 py-3 shrink-0 bg-muted/20">
            {headerSlot}
          </div>
        )}

        {/* ── Event list ─────────────────────────────────────────── */}
        <div className="flex-1 min-h-0">
          {displayItems.length === 0 ? (
            <div className="flex items-center justify-center h-full text-body text-muted-foreground">
              {isAntigravityLiveEmpty ? (
                <div className="flex max-w-md items-center gap-2 px-4 text-center">
                  <Clock className="h-4 w-4 shrink-0" />
                  {t(($) => $.transcript.antigravity_live_unavailable)}
                </div>
              ) : isLive ? (
                <div className="flex items-center gap-2">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  {t(($) => $.transcript.waiting_events)}
                </div>
              ) : (
                t(($) => $.transcript.no_data)
              )}
            </div>
          ) : (
            // Virtualized so a multi-thousand-event run mounts a bounded number
            // of DOM rows (#5733). Rows expand/collapse to variable heights;
            // Virtuoso re-measures them via ResizeObserver.
            <Virtuoso
              key={listEpoch}
              ref={virtuosoRef}
              style={{ height: "100%" }}
              data={displayItems}
              firstItemIndex={firstItemIndex}
              // Open a live chronological transcript pinned to the newest
              // event (#5921); the per-listEpoch remount re-applies this after
              // task / sort / filter changes. Completed tasks keep reading
              // from the top, and newest-first has its live end there already.
              initialTopMostItemIndex={
                isLive && sortDirection !== "newest_first"
                  ? { index: "LAST", align: "end" }
                  : 0
              }
              // Follow appended events while the reader is at the bottom;
              // scrolling up suspends the follow until they return (#5921).
              // Instant, not smooth: transcript flushes append several rows at
              // a time and a still-animating scroll makes the next evaluation
              // read "not at bottom", permanently dropping the follow.
              // Newest-first opts out — its growth is prepends, handled by the
              // scrollToIndex effect above.
              followOutput={(atBottom) =>
                isLive && sortDirection !== "newest_first" && atBottom ? "auto" : false
              }
              atBottomThreshold={FOLLOW_EDGE_THRESHOLD}
              atTopThreshold={FOLLOW_EDGE_THRESHOLD}
              // Back within the top zone (any way you got there) re-engages
              // the newest-first follow. Leaving it does NOT disengage:
              // anchored prepends leave the top zone on their own — only
              // accumulated user input may break the latch (transcript-follow.ts).
              atTopStateChange={(atTop) => followCtl.onAtTopChange(atTop)}
              scrollerRef={handleScrollerRef}
              computeItemKey={(_, item) => item.seq}
              components={LIST_COMPONENTS}
              itemContent={(_, item) => (
                <TranscriptEventRow
                  item={item}
                  isSelected={selectedSeq === item.seq}
                  expanded={
                    rowOverrides.get(item.seq) ?? traceEventDefaultExpanded(item, density)
                  }
                  onExpandedChange={(expanded) => handleRowExpandedChange(item.seq, expanded)}
                />
              )}
            />
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ─── Sort direction toggle ──────────────────────────────────────────────────

interface SortDirectionToggleProps {
  value: TranscriptSortDirection;
  onChange: (dir: TranscriptSortDirection) => void;
  labels: { chronological: string; newestFirst: string; ariaLabel: string };
}

// Sort is a two-state toggle, not a mode picker: one button showing the
// current direction that flips on click. Shares the toolbar button chassis so
// it reads as the same control family as density/filter/copy — no tab strip.
function SortDirectionToggle({ value, onChange, labels }: SortDirectionToggleProps) {
  const isChronological = value === "chronological";
  return (
    <Button
      variant="ghost"
      size="sm"
      onClick={() => onChange(isChronological ? "newest_first" : "chronological")}
      aria-label={labels.ariaLabel}
      title={labels.ariaLabel}
      className="text-muted-foreground"
    >
      {isChronological ? (
        <ArrowDownNarrowWide className="h-3 w-3" />
      ) : (
        <ArrowUpNarrowWide className="h-3 w-3" />
      )}
      <span className="hidden sm:inline">
        {isChronological ? labels.chronological : labels.newestFirst}
      </span>
    </Button>
  );
}

// ─── Facts line separator ───────────────────────────────────────────────────

function FactDot() {
  return (
    <span aria-hidden className="text-muted-foreground/40">
      ·
    </span>
  );
}

function formatProvider(provider: string): string {
  const map: Record<string, string> = {
    claude: "Claude Code",
    "claude-code": "Claude Code",
    codex: "Codex",
    pi: "Pi",
  };
  return map[provider.toLowerCase()] ?? provider;
}

// ─── Timeline bar (colored segments) ────────────────────────────────────────

function TimelineBar({
  items,
  selectedSeq,
  onSegmentClick,
}: {
  items: TimelineItem[];
  selectedSeq: number | null;
  onSegmentClick: (seq: number) => void;
}) {
  const segments: { startIdx: number; endIdx: number; color: EventColor; count: number }[] = [];
  let currentColor: EventColor | null = null;
  let currentStart = 0;

  for (let i = 0; i < items.length; i++) {
    const item = items[i]!;
    const color = getEventColor(item);
    if (color !== currentColor) {
      if (currentColor !== null) {
        segments.push({ startIdx: currentStart, endIdx: i - 1, color: currentColor, count: i - currentStart });
      }
      currentColor = color;
      currentStart = i;
    }
  }
  if (currentColor !== null) {
    segments.push({ startIdx: currentStart, endIdx: items.length - 1, color: currentColor, count: items.length - currentStart });
  }

  return (
    <div className="flex gap-0.5 h-5 rounded overflow-hidden" role="navigation" aria-label="Timeline">
      {segments.map((seg) => {
        const isSelected = selectedSeq !== null && items.slice(seg.startIdx, seg.endIdx + 1).some((i) => i.seq === selectedSeq);
        const color = colorClasses[seg.color];
        const widthPercent = (seg.count / items.length) * 100;

        return (
          <button
            type="button"
            key={seg.startIdx}
            className={cn(
              "h-full transition-all duration-150 hover:opacity-80 relative group",
              isSelected ? color.bgActive : color.bg,
              "min-w-[4px]",
            )}
            style={{ width: `${Math.max(widthPercent, 0.5)}%` }}
            onClick={() => onSegmentClick(items[seg.startIdx]!.seq)}
            title={`${traceEventLabel(items[seg.startIdx]!)}${seg.count > 1 ? ` (+${seg.count - 1} more)` : ""}`}
          >
            <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-1 hidden group-hover:block z-10 pointer-events-none">
              <div className="rounded bg-popover border px-2 py-1 text-micro text-popover-foreground shadow-md whitespace-nowrap">
                {traceEventLabel(items[seg.startIdx]!)}
                {seg.count > 1 && <span className="text-muted-foreground ml-1">+{seg.count - 1}</span>}
              </div>
            </div>
          </button>
        );
      })}
    </div>
  );
}

// ─── Transcript event row ───────────────────────────────────────────────────

interface TranscriptEventRowProps {
  item: TimelineItem;
  isSelected: boolean;
  expanded: boolean;
  onExpandedChange: (expanded: boolean) => void;
}

const TranscriptEventRow = ({
  item,
  isSelected,
  expanded,
  onExpandedChange,
}: TranscriptEventRowProps) => {
  const { t } = useT("agents");
  const kind = traceEventKind(item);
  const color = getEventColor(item);
  const label = traceEventLabel(item);
  // The presenter stays i18n-free, so the localizable phrasing is injected.
  // `extra` counts files beyond the named one, and is deliberately not called
  // `count`: i18next reads `count` as a plural selector.
  const summaryLabels = useMemo<TraceSummaryLabels>(
    () => ({
      morePaths: (path, extraCount) =>
        t(($) => $.transcript.patch_summary_more, { path, extra: extraCount }),
    }),
    [t],
  );
  const summary = traceEventSummary(item, summaryLabels);
  const date = useMemo(
    () => (item.created_at ? new Date(item.created_at) : null),
    [item.created_at],
  );

  const hasDetail = traceEventHasDetail(item);
  const detail = useMemo(() => traceEventDetail(item), [item]);
  // Prose kinds swap the one-line summary for the full body in place when
  // expanded (no box). Tool kinds keep the summary line and reveal the
  // params/output surface below it.
  const isProse = kind !== "tool_use" && kind !== "tool_result";
  const showInlineBody = isProse && hasDetail && expanded;

  return (
    <div
      className={cn(
        "group transition-colors",
        isSelected && "bg-accent/50",
        kind === "error" && "bg-destructive/5",
      )}
    >
      <Collapsible open={expanded} onOpenChange={onExpandedChange}>
        <div className="flex items-start gap-2 px-4 py-2">
          {/* Type label badge */}
          <span
            className={cn(
              "inline-flex items-center shrink-0 rounded px-1.5 py-0.5 text-micro font-medium mt-0.5 min-w-[60px] justify-center",
              colorClasses[color].label,
            )}
          >
            {item.type === "thinking" && <Brain className="h-3 w-3 mr-1 shrink-0" />}
            {item.type === "error" && <AlertCircle className="h-3 w-3 mr-1 shrink-0" />}
            {label}
          </span>

          {showInlineBody ? (
            <div className="flex flex-1 items-start gap-1.5 min-w-0">
              <CollapsibleTrigger
                aria-label={label}
                className="shrink-0 mt-0.5 cursor-pointer rounded p-0.5 text-muted-foreground/50 transition-colors hover:text-foreground"
              >
                <ChevronRight className="h-3 w-3 rotate-90 transition-transform" />
              </CollapsibleTrigger>
              <div className="flex-1 min-w-0">
                {kind === "agent" ? (
                  <RichContent
                    content={item.content ?? ""}
                    density="compact"
                    className="transcript-prose"
                  />
                ) : (
                  <div
                    className={cn(
                      "whitespace-pre-wrap break-words text-caption leading-relaxed",
                      kind === "error" ? "text-destructive" : "text-muted-foreground",
                    )}
                  >
                    {item.content ?? ""}
                  </div>
                )}
              </div>
            </div>
          ) : (
            <CollapsibleTrigger
              className={cn(
                "flex-1 text-left text-caption min-w-0 py-0.5 transition-colors",
                hasDetail ? "cursor-pointer hover:text-foreground" : "cursor-default",
                kind === "error" ? "text-destructive" : "text-muted-foreground",
              )}
              disabled={!hasDetail}
            >
              <div className="flex items-start gap-1.5">
                {hasDetail && (
                  <ChevronRight
                    className={cn(
                      "h-3 w-3 shrink-0 mt-0.5 text-muted-foreground/50 transition-transform",
                      expanded && "rotate-90",
                    )}
                  />
                )}
                <span
                  className={cn(
                    "truncate",
                    traceEventSummaryIsMono(kind) && summary && "font-mono text-micro",
                    !summary && "text-muted-foreground/60",
                  )}
                >
                  {summary || t(($) => $.transcript.no_output)}
                </span>
              </div>
            </CollapsibleTrigger>
          )}

          {/* Seq number / index */}
          <span className="shrink-0 text-micro text-muted-foreground/50 tabular-nums mt-1">
            #{item.seq}
          </span>

          {/* Timestamp */}
          {date && (
            <span className="shrink-0 text-micro text-muted-foreground/50 tabular-nums mt-1" title={date.toLocaleString()}>
              {date.toLocaleTimeString(undefined, {
                hour: "2-digit",
                minute: "2-digit",
                second: "2-digit",
              })}
            </span>
          )}
        </div>

        {/* Expanded params/output for tool kinds — a quiet, borderless surface
            aligned to the content column. */}
        {!isProse && hasDetail && (
          <CollapsibleContent>
            <div className="px-4 pb-3">
              <div className="ml-[72px] rounded-md bg-muted/40">
                {detail.kind === "diff" ? (
                  <DiffDetailSurface lines={detail.lines} path={detail.path} />
                ) : detail.kind === "patch" ? (
                  <PatchDetailSurface files={detail.files} truncated={detail.truncated} />
                ) : detail.kind === "file" ? (
                  <FileWriteSurface text={detail.text} lineCount={detail.lineCount} path={detail.path} />
                ) : (
                  <ToolDetailSurface
                    text={
                      detail.text.length > 4000
                        ? redactSecrets(detail.text.slice(0, 4000)) + "\n... (truncated)"
                        : redactSecrets(detail.text)
                    }
                  />
                )}
              </div>
            </div>
          </CollapsibleContent>
        )}
      </Collapsible>
    </div>
  );
};

// ─── Tool detail surface ────────────────────────────────────────────────────

/**
 * Long content fades out behind a "show all" affordance instead of trapping a
 * nested scrollbar inside the virtualized list.
 */
type HighlightedSides = Record<"add" | "remove" | "context", string[] | null> | null;

/**
 * Diff rows. Highlighting is looked up per side, since each side was
 * highlighted as its own block; a side that failed to highlight falls back to
 * plain text for that side only.
 */
function renderDiffRows(lines: TraceDiffLine[], highlighted: HighlightedSides) {
  const cursor: Record<"add" | "remove" | "context", number> = { add: 0, remove: 0, context: 0 };

  return lines.map((line, index) => {
    if (line.kind === "gap") {
      return (
        // Transcript events are immutable once persisted, so index is stable.
        <div key={index} className="select-none text-muted-foreground/40" aria-hidden>
          {"  ⋯"}
        </div>
      );
    }

    const kind = line.kind;
    const html = highlighted?.[kind]?.[cursor[kind]];
    cursor[kind] += 1;

    return (
      <div
        key={index}
        className={cn(
          "-mx-1 px-1",
          kind === "add" && "bg-success/10",
          kind === "remove" && "bg-destructive/10",
          // Only tint the gutter for changed rows; the code itself keeps its
          // syntax colours so a diff reads like the file it came from.
          !html && kind === "add" && "text-success",
          !html && kind === "remove" && "text-destructive",
          !html && kind === "context" && "text-muted-foreground",
        )}
      >
        <span
          aria-hidden
          className={cn(
            "select-none opacity-60",
            kind === "add" && "text-success",
            kind === "remove" && "text-destructive",
          )}
        >
          {kind === "add" ? "+" : kind === "remove" ? "-" : " "}
        </span>{" "}
        {html === undefined ? (
          redactSecrets(line.text)
        ) : (
          // lowlight output only: text is escaped by the hast serializer and
          // the sole elements are its own `hljs-*` spans.
          <span className="hljs" dangerouslySetInnerHTML={{ __html: html }} />
        )}
      </div>
    );
  });
}

/**
 * A whole-file write has no before side, so it reads as plain content with a
 * line count rather than as an all-additions diff — the `+` gutter would carry
 * no information here.
 */
function FileWriteSurface({
  text,
  lineCount,
  path,
}: {
  text: string;
  lineCount: number;
  path: string;
}) {
  return (
    <div>
      <div className="px-3 pt-2 font-mono text-micro text-success">+{lineCount}</div>
      <ToolDetailSurface text={redactSecrets(text)} language={languageForPath(path)} />
    </div>
  );
}

/**
 * A multi-file patch: one section per file, each reusing the single-file
 * surfaces. Codex's `patch_apply` routinely touches several files in one call,
 * so collapsing them into a single body would lose which change belongs where.
 */
function PatchDetailSurface({
  files,
  truncated,
}: {
  files: TracePatchFile[];
  truncated: boolean;
}) {
  const { t } = useT("agents");
  return (
    <div className="divide-y divide-border/40">
      {files.map((file, index) => (
        // Transcript events are immutable once persisted, so index is stable.
        <div key={`${file.path}:${index}`}>
          <div className="flex items-center gap-2 px-3 pt-2 font-mono text-micro">
            {file.changeKind && (
              <span
                className={cn(
                  "shrink-0 uppercase",
                  file.changeKind === "add" && "text-success",
                  file.changeKind === "delete" && "text-destructive",
                  file.changeKind === "update" && "text-muted-foreground/70",
                )}
              >
                {file.changeKind}
              </span>
            )}
            <span className="truncate text-muted-foreground">{file.path}</span>
            {file.movePath && (
              <>
                <span className="shrink-0 text-muted-foreground/50" aria-hidden>
                  →
                </span>
                <span className="truncate text-muted-foreground">{file.movePath}</span>
              </>
            )}
          </div>
          {file.body.kind === "diff" ? (
            <DiffDetailSurface lines={file.body.lines} path={file.path} />
          ) : file.body.kind === "file" ? (
            <FileWriteSurface
              text={file.body.text}
              lineCount={file.body.lineCount}
              path={file.path}
            />
          ) : (
            <div className="px-3 pb-2 pt-1 font-mono text-micro text-muted-foreground/60">
              {file.truncated
                ? t(($) => $.transcript.patch_body_truncated)
                : t(($) => $.transcript.patch_no_content)}
            </div>
          )}
        </div>
      ))}
      {truncated && (
        <div className="px-3 py-2 font-mono text-micro text-muted-foreground/60">
          {t(($) => $.transcript.patch_truncated)}
        </div>
      )}
    </div>
  );
}

/**
 * A file change reads as a diff rather than two escaped string literals. Same
 * fade/"show all" shell as the text surface so both bodies behave alike inside
 * the virtualized list.
 */
function DiffDetailSurface({ lines, path }: { lines: TraceDiffLine[]; path: string }) {
  const { t } = useT("agents");
  const [showAll, setShowAll] = useState(false);
  const isLong = lines.length > 14;
  const added = lines.filter((l) => l.kind === "add").length;
  const removed = lines.filter((l) => l.kind === "remove").length;

  // Each side is highlighted as one block so multi-line strings and comments
  // keep their grammar, then split back per line to sit in the diff gutter.
  // Runs only when the row is expanded, which is where this component mounts.
  const highlighted = useMemo(() => {
    const language = languageForPath(path);
    if (!language) return null;
    const sides: Record<"add" | "remove" | "context", string[] | null> = {
      add: null,
      remove: null,
      context: null,
    };
    for (const kind of ["add", "remove", "context"] as const) {
      const side = lines.filter((l) => l.kind === kind);
      if (side.length > 0) {
        sides[kind] = highlightToLines(side.map((l) => l.text).join("\n"), language);
      }
    }
    return sides;
  }, [lines, path]);

  return (
    <div className="relative">
      <div className="flex items-center gap-2 px-3 pt-2 font-mono text-micro text-muted-foreground/70">
        {added > 0 && <span className="text-success">+{added}</span>}
        {removed > 0 && <span className="text-destructive">-{removed}</span>}
      </div>
      <pre
        className={cn(
          "transcript-code px-3 pb-3 pt-1 font-mono text-micro whitespace-pre-wrap break-all",
          isLong && !showAll && "max-h-52 overflow-hidden",
        )}
      >
        {renderDiffRows(lines, highlighted)}
      </pre>
      {isLong && !showAll && (
        <div className="absolute inset-x-0 bottom-0 flex h-12 items-end justify-center rounded-b-md bg-gradient-to-b from-transparent to-background">
          <button
            type="button"
            onClick={() => setShowAll(true)}
            // Opaque: the gradient alone does not clear the clipped line, so a
            // transparent label lands on top of it and both become unreadable.
            className="mb-1.5 rounded border bg-background px-2 py-0.5 text-micro text-muted-foreground shadow-sm transition-colors hover:bg-accent hover:text-foreground"
          >
            {t(($) => $.transcript.show_all)}
          </button>
        </div>
      )}
    </div>
  );
}

function ToolDetailSurface({ text, language }: { text: string; language?: string }) {
  const { t } = useT("agents");
  const [showAll, setShowAll] = useState(false);
  const isLong = text.length > 1600 || text.split("\n").length > 14;
  // Only file bodies carry a language; command output and JSON stay plain.
  const html = useMemo(() => (language ? highlightBlock(text, language) : null), [text, language]);

  return (
    <div className="relative">
      <pre
        className={cn(
          "transcript-code p-3 font-mono text-micro text-muted-foreground whitespace-pre-wrap break-all",
          isLong && !showAll && "max-h-52 overflow-hidden",
        )}
      >
        {html === null ? (
          text
        ) : (
          // lowlight output only: the hast serializer escapes text and the sole
          // elements are its own `hljs-*` spans.
          <code className="hljs" dangerouslySetInnerHTML={{ __html: html }} />
        )}
      </pre>
      {isLong && !showAll && (
        <div className="absolute inset-x-0 bottom-0 flex h-12 items-end justify-center rounded-b-md bg-gradient-to-b from-transparent to-background">
          <button
            type="button"
            onClick={() => setShowAll(true)}
            // Opaque: the gradient alone does not clear the clipped line, so a
            // transparent label lands on top of it and both become unreadable.
            className="mb-1.5 rounded border bg-background px-2 py-0.5 text-micro text-muted-foreground shadow-sm transition-colors hover:bg-accent hover:text-foreground"
          >
            {t(($) => $.transcript.show_all)}
          </button>
        </div>
      )}
    </div>
  );
}

