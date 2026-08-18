"use client";

import { useState, useRef, useCallback, useEffect, useLayoutEffect, useMemo, forwardRef } from "react";
import { Virtuoso, type VirtuosoHandle, type Components } from "react-virtuoso";
import {
  Bot,
  Brain,
  CircleAlert,
  CheckCircle2,
  XCircle,
  X,
  Loader2,
  Clock,
  Copy,
  Check,
  ChevronRight,
  Filter,
  FilePen,
  FileText,
  Search,
  Terminal,
  Wrench,
  MoreHorizontal,
  ArrowDownNarrowWide,
  ArrowUpNarrowWide,
  Info,
  Coins,
  GitBranch,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuSeparator,
  DropdownMenuCheckboxItem,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { ActorAvatar } from "../actor-avatar";
import { AttributionBadge } from "../../issues/components/attribution-badge";
import { cancelReasonLabel } from "../../agents/components/tabs/task-failure";
import { RichContent } from "../../rich-content";
import { api } from "@multica/core/api";
import {
  useTranscriptViewStore,
  type TranscriptFilterKey,
  type TranscriptSortDirection,
} from "@multica/core/agents/stores";
import type { AgentTask, Agent, AgentRuntime } from "@multica/core/types/agent";
import { runtimeDisplayName, providerDisplayName } from "@multica/core/runtimes";
import { useCustomPricingStore } from "@multica/core/runtimes/custom-pricing-store";
import { redactSecrets } from "./redact";
import {
  createNewestFirstFollow,
  FOLLOW_EDGE_THRESHOLD,
  LINE_SCROLL_PX,
} from "./transcript-follow";
import type { TimelineItem } from "./build-timeline";
import {
  buildLanes,
  buildSteps,
  groupSteps,
  isCallStep,
  isGroupRow,
  rowCalls,
  shouldShowTimeline,
  toolKindTotals,
  type TraceCallStep,
  type TraceGroupRow,
  type TraceMessageStep,
  type TraceRow,
  type TraceStep,
} from "./build-steps";
import { buildRunOutcome } from "./run-outcome";
import { RunTimeline } from "./run-timeline";
import {
  base64ByteLength,
  readImageResult,
  traceEventCopyText,
  traceEventDetail,
  traceEventLabel,
  traceEventSummary,
  traceToolArgSummary,
} from "./trace-event-presenter";
import type { TraceSummaryLabels } from "./trace-event-presenter";
import {
  DiffDetailSurface,
  FileWriteSurface,
  PatchDetailSurface,
  ToolDetailSurface,
} from "./detail-surfaces";
import { languageForPath } from "./diff-highlight";
import { useT } from "../../i18n";
import {
  formatTokens,
  formatUsd,
  summarizeTaskUsage,
} from "../../runtimes/utils";
import "../../editor/styles/code.css";
import "./task-transcript.css";

// The run surface, read in four layers: what happened (header), what it
// produced (outcome), where the time went (timeline), and the evidence (steps).
// Only one layer is expanded at a time — the inspector opens on demand rather
// than splitting the surface in two before the reader has asked anything.

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

// ─── Helpers ────────────────────────────────────────────────────────────────

function formatDuration(start: string, end: string): string {
  const ms = new Date(end).getTime() - new Date(start).getTime();
  return formatElapsedMs(ms);
}

function formatElapsedMs(ms: number): string {
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const secs = seconds % 60;
  return `${minutes}m ${secs}s`;
}

/** A step's own duration, in the compact form the right column carries. */
function formatStepDuration(ms: number): string {
  if (ms < 1000) return `${(ms / 1000).toFixed(1)}s`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(ms < 10_000 ? 1 : 0)}s`;
  const minutes = Math.floor(ms / 60_000);
  const seconds = Math.round((ms % 60_000) / 1000);
  return `${minutes}m ${String(seconds).padStart(2, "0")}s`;
}

/** Offset from the run's start, which is what a step's position means. */
function formatOffset(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes < 60) return `+${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
  const hours = Math.floor(minutes / 60);
  return `+${hours}:${String(minutes % 60).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
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

function timeMs(iso: string | undefined): number | undefined {
  if (!iso) return undefined;
  const ms = new Date(iso).getTime();
  return Number.isFinite(ms) ? ms : undefined;
}

function stepFilterKey(step: TraceStep): TranscriptFilterKey {
  return step.kind === "call" ? `tool:${step.tool}` : step.kind;
}

/** Lowercased haystack for the in-run search. Outputs are clipped: a match
 *  past 4KB of command output is not one anybody is scanning for. */
function stepHaystack(step: TraceStep): string {
  if (step.kind !== "call") return (step.item.content ?? "").toLowerCase();
  const input = step.call?.input ? JSON.stringify(step.call.input) : "";
  return `${step.tool} ${input} ${(step.result?.output ?? "").slice(0, 4000)}`.toLowerCase();
}

function StepIcon({ step, className }: { step: TraceStep; className?: string }) {
  if (!isCallStep(step)) {
    if (step.kind === "thinking") return <Brain className={className} />;
    if (step.kind === "error") return <CircleAlert className={className} />;
    return <Bot className={className} />;
  }

  // Tool names differ per provider, so the input's shape picks the glyph.
  const input = step.call?.input;
  if (input) {
    if (typeof input.command === "string") return <Terminal className={className} />;
    if (typeof input.old_string === "string" || typeof input.content === "string" || input.changes)
      return <FilePen className={className} />;
    if (typeof input.query === "string" || typeof input.pattern === "string")
      return <Search className={className} />;
    if (typeof input.file_path === "string" || typeof input.path === "string")
      return <FileText className={className} />;
  }
  return <Wrench className={className} />;
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
  const valueClass = cn("min-w-0 select-text break-all text-foreground", mono && "font-mono");
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
const LIST_COMPONENTS: Components<TraceRow> = { List: VirtuosoList };
const COPY_FEEDBACK_DURATION_MS = 2000;

function useCopyFeedback() {
  const [copied, setCopied] = useState(false);
  const resetTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      if (resetTimerRef.current !== null) {
        clearTimeout(resetTimerRef.current);
        resetTimerRef.current = null;
      }
    };
  }, []);

  const showCopied = useCallback(() => {
    if (!mountedRef.current) return;
    setCopied(true);
    if (resetTimerRef.current !== null) clearTimeout(resetTimerRef.current);
    resetTimerRef.current = setTimeout(() => {
      resetTimerRef.current = null;
      if (mountedRef.current) setCopied(false);
    }, COPY_FEEDBACK_DURATION_MS);
  }, []);

  return [copied, showCopied] as const;
}

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
  const [expandedGroups, setExpandedGroups] = useState<Set<number>>(() => new Set());
  const [query, setQuery] = useState("");
  const [elapsed, setElapsed] = useState("");
  const [copied, showCopied] = useCopyFeedback();
  const [copiedWorkdir, showCopiedWorkdir] = useCopyFeedback();
  const [copiedBranch, showCopiedBranch] = useCopyFeedback();
  const [agentInfo, setAgentInfo] = useState<Agent | null>(null);
  const [runtimeInfo, setRuntimeInfo] = useState<AgentRuntime | null>(null);
  const sortDirection = useTranscriptViewStore((s) => s.sortDirection);
  const setSortDirection = useTranscriptViewStore((s) => s.setSortDirection);
  // Filters always persist across opens — a facet a run doesn't have simply
  // no-ops (see activeFilterKeys), so there is no reason to make persistence a
  // user-facing toggle.
  const selectedFilterKeys = useTranscriptViewStore((s) => s.selectedFilterKeys);
  const toggleFilterKey = useTranscriptViewStore((s) => s.toggleFilterKey);
  const clearFilters = useTranscriptViewStore((s) => s.clearFilterKeys);
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
        // row controls are not scroll intent.
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

  // A different run is a different reading position: drop the selection and
  // any group the reader had opened.
  useEffect(() => {
    setSelectedSeq(null);
    setExpandedGroups(new Set());
  }, [task.id]);

  // ── Derived model ────────────────────────────────────────────────────────

  // One step per tool call, with its result folded in — see build-steps.ts for
  // why the pairing is positional.
  const steps = useMemo(() => buildSteps(items), [items]);

  const filterOptions = useMemo(() => {
    const options = new Map<string, string>();
    for (const step of steps) {
      const key = stepFilterKey(step);
      if (options.has(key)) continue;
      options.set(
        key,
        step.kind === "call"
          ? key
          : traceEventLabel({ type: step.item.type, tool: step.item.tool }),
      );
    }
    return Array.from(options.entries()).sort((a, b) => a[1].localeCompare(b[1]));
  }, [steps]);

  const filterOptionKeys = useMemo(
    () => new Set(filterOptions.map(([value]) => value)),
    [filterOptions],
  );

  const activeFilterKeys = useMemo(
    () => selectedFilterKeys.filter((key) => filterOptionKeys.has(key)),
    [selectedFilterKeys, filterOptionKeys],
  );

  const activeFilterSet = useMemo(() => new Set(activeFilterKeys), [activeFilterKeys]);

  const trimmedQuery = query.trim().toLowerCase();

  const filteredSteps = useMemo(() => {
    if (activeFilterSet.size === 0 && trimmedQuery.length === 0) return steps;
    return steps.filter((step) => {
      if (activeFilterSet.size > 0 && !activeFilterSet.has(stepFilterKey(step))) return false;
      if (trimmedQuery.length > 0 && !stepHaystack(step).includes(trimmedQuery)) return false;
      return true;
    });
  }, [steps, activeFilterSet, trimmedQuery]);

  // Grouping runs on what the reader is looking at: filtering breaks adjacency,
  // and a group that spans a hidden step would be a lie about what ran.
  const rows = useMemo(() => groupSteps(filteredSteps), [filteredSteps]);

  // Apply user-chosen sort direction. Reverse is a pure presentation concern —
  // the underlying steps (and their seq numbers) are untouched.
  const displayRows = useMemo(
    () => (sortDirection === "newest_first" ? [...rows].reverse() : rows),
    [rows, sortDirection],
  );

  const runStart = task.started_at ?? task.dispatched_at ?? steps[0]?.startedAt;
  const runStartMs = timeMs(runStart);
  const lastStamp = useMemo(() => {
    for (let i = steps.length - 1; i >= 0; i--) {
      const step = steps[i]!;
      const stamp = step.kind === "call" ? (step.endedAt ?? step.startedAt) : step.startedAt;
      if (stamp) return stamp;
    }
    return undefined;
  }, [steps]);
  const runEnd = task.completed_at ?? lastStamp;

  const lanes = useMemo(() => buildLanes(steps, runStart, runEnd), [steps, runStart, runEnd]);
  // A short run's timeline says less than the durations already on each row,
  // so it does not render at all.
  const showTimeline = shouldShowTimeline(steps, lanes);
  const toolKinds = useMemo(() => toolKindTotals(steps), [steps]);
  const outcome = useMemo(() => buildRunOutcome(steps), [steps]);

  const selectedStep = useMemo(() => {
    if (selectedSeq === null) return null;
    return steps.find((step) => step.seq === selectedSeq) ?? null;
  }, [steps, selectedSeq]);

  const selectedOffsetMs =
    selectedStep && runStartMs !== undefined
      ? Math.max(0, (timeMs(selectedStep.startedAt) ?? runStartMs) - runStartMs)
      : undefined;

  // Keyed on the run having produced nothing at all, not on the filtered view
  // being empty — a filter that hides every step is not a runtime limitation.
  const isAntigravityLiveEmpty =
    isLive && steps.length === 0 && runtimeInfo?.provider === "antigravity";

  // Newest-first shows live events as PREPENDS, and Virtuoso items opt out of
  // native scroll anchoring (`overflow-anchor: none`), so without compensation
  // every 500ms flush shifts the reading position. Virtuoso's contract: a
  // decrease of firstItemIndex by N anchors the viewport across an N-item
  // prepend, and the value must never increase within an instance — counting
  // down from a large base satisfies that while the list only grows. Sort,
  // filter, or task changes can shrink the list, so `listEpoch` remounts the
  // instance (fresh at top) instead of letting firstItemIndex climb.
  const firstItemIndex =
    sortDirection === "newest_first" ? 1_000_000 - displayRows.length : 0;
  const listEpoch = `${task.id}:${sortDirection}:${activeFilterKeys.join(",")}:${trimmedQuery}`;

  const handleSortDirectionChange = useCallback(
    (dir: TranscriptSortDirection) => {
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
  // is the authoritative pin; this effect just shortens the first-paint gap.
  const displayCount = displayRows.length;
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

  const scrollToStep = useCallback(
    (seq: number) => {
      const index = displayRows.findIndex((row) => row.seq === seq || rowCalls(row).some((c) => c.seq === seq));
      if (index < 0) return;
      // Explicit navigation away from the live end unlatches the newest-first
      // follow — otherwise the scroll-event enforcement would pin the viewport
      // straight back to the top. Scrolling back re-engages it as usual.
      if (index > 0) followCtl.disengage();
      virtuosoRef.current?.scrollToIndex({ index, align: "center", behavior: "smooth" });
    },
    [displayRows, followCtl],
  );

  // Clicking the timeline lands on the first step at or after that offset.
  const handleSeek = useCallback(
    (offsetMs: number) => {
      if (runStartMs === undefined) return;
      const target = runStartMs + offsetMs;
      let best: TraceStep | null = null;
      for (const step of steps) {
        const at = timeMs(step.startedAt);
        if (at === undefined || at < target) continue;
        if (!best || at < (timeMs(best.startedAt) ?? Infinity)) best = step;
      }
      if (!best) return;
      setSelectedSeq(best.seq);
      scrollToStep(best.seq);
    },
    [steps, runStartMs, scrollToStep],
  );

  const handleCopyWorkdir = useCallback(() => {
    if (!task.relative_work_dir) return;
    void copyText(task.relative_work_dir).then((ok) => {
      if (!ok) return;
      showCopiedWorkdir();
    });
  }, [task.relative_work_dir, showCopiedWorkdir]);

  // Worktree-mode runs deliver a branch instead of edits in the working copy,
  // so copying the name is the fastest path to `git diff <branch>`.
  const handleCopyBranch = useCallback(() => {
    if (!task.branch_name) return;
    void copyText(task.branch_name).then((ok) => {
      if (!ok) return;
      showCopiedBranch();
    });
  }, [task.branch_name, showCopiedBranch]);

  const handleCopyAll = useCallback(() => {
    // Copy the full body of each event (not the truncated row summary), with
    // the same secret redaction the detail view applies. A step copies as its
    // call followed by its result, which is the order it happened in.
    const events: TimelineItem[] = [];
    for (const row of displayRows) {
      if (row.kind === "group") {
        for (const call of row.steps) {
          if (call.call) events.push(call.call);
          if (call.result) events.push(call.result);
        }
        continue;
      }
      if (row.kind === "call") {
        if (row.call) events.push(row.call);
        if (row.result) events.push(row.result);
        continue;
      }
      events.push(row.item);
    }
    const text = events.map((event) => redactSecrets(traceEventCopyText(event))).join("\n\n");
    void copyText(text).then((ok) => {
      if (!ok) return;
      showCopied();
    });
  }, [displayRows, showCopied]);

  const handleToggleGroup = useCallback((seq: number) => {
    setExpandedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(seq)) next.delete(seq);
      else next.add(seq);
      return next;
    });
  }, []);

  const duration =
    task.started_at && task.completed_at
      ? formatDuration(task.started_at, task.completed_at)
      : isLive
        ? elapsed
        : null;

  const copyTranscriptLabel = copied
    ? t(($) => $.transcript.copied)
    : activeFilterKeys.length > 0 || trimmedQuery.length > 0
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
      case "cancelled": {
        // A server-cancelled run (worktree claim gate, preserved-work
        // delivery) carries a persisted reason the user must act on; surface
        // it on the badge instead of a bare "Cancelled". User-initiated
        // cancels have no reason and keep the plain label.
        const cancelReason = cancelReasonLabel(task);
        return (
          <span className={cn(base, "bg-muted text-muted-foreground")} title={task.error ?? undefined}>
            <XCircle className="h-3 w-3" />
            {cancelReason
              ? `${t(($) => $.transcript.status_cancelled)} · ${cancelReason}`
              : t(($) => $.transcript.status_cancelled)}
          </span>
        );
      }
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
  // up front than the runtime/provider diagnostics, which live in the ⓘ popover.
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
  const providerLabel = runtimeInfo?.provider ? transcriptProviderLabel(runtimeInfo.provider) : null;
  const createdLabel = task.created_at ? formatRunTime(task.created_at) : null;
  const startedLabel = task.started_at ? formatRunTime(task.started_at) : null;
  const completedLabel = task.completed_at ? formatRunTime(task.completed_at) : null;
  const hasTriggeredBy = !!task.attribution?.initiator;
  // This run's own spend. Present on transcripts opened from the issue
  // execution log (the endpoint that hydrates usage); absent elsewhere, where
  // the chip and the usage rows below simply don't render.
  //
  // `summarizeTaskUsage` prices through the custom-rate store, which it reads
  // imperatively — subscribing here is what makes a saved rate change reach
  // this figure, same as on the other usage surfaces.
  useCustomPricingStore((s) => s.pricings);
  const usage = summarizeTaskUsage(task.usage);
  const hasRunDetails =
    !!runtimeInfo ||
    !!task.relative_work_dir ||
    !!task.branch_name ||
    !!task.error ||
    !!createdLabel ||
    !!startedLabel ||
    !!completedLabel ||
    !!usage;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="!max-w-5xl !w-[calc(100vw-4rem)] !max-h-[calc(100vh-4rem)] !h-[calc(100vh-4rem)] flex flex-col !p-0 !gap-0 overflow-hidden"
        showCloseButton={false}
      >
        <DialogTitle className="sr-only">{t(($) => $.transcript.dialog_title)}</DialogTitle>

        {/* ── Header: outcome, identity, spend ─────────────────────────
            Everything a viewer needs BEFORE reading: how it ended, who ran
            it, why it exists, how long it took and what it cost. Diagnostics
            stay in the ⓘ popover. */}
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
              {duration && (
                <>
                  <FactDot />
                  <span className="shrink-0 tabular-nums">
                    {t(($) => $.transcript.fact_took, { duration })}
                  </span>
                </>
              )}
            </div>

            {/* What this run cost, in the header of the run you are reading —
                so "why was this one expensive" is answerable without going
                back to the list. The split lives one click away in the ⓘ
                popover. */}
            {usage && (
              <span
                className="flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-micro tabular-nums"
                title={t(($) => $.transcript.usage_chip_title)}
              >
                <Coins aria-hidden="true" className="h-3 w-3 text-muted-foreground" />
                <span className="font-medium">{formatTokens(usage.tokens)}</span>
                <span className="text-faint-foreground">·</span>
                <span className="text-muted-foreground">{formatUsd(usage.cost)}</span>
              </span>
            )}

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
                      {task.branch_name && (
                        <RunDetailRow
                          label={t(($) => $.transcript.details_branch)}
                          value={task.branch_name}
                          mono
                          onCopy={handleCopyBranch}
                          copied={copiedBranch}
                          copyTitle={t(($) => $.transcript.copy_branch)}
                        />
                      )}
                      {/* The full persisted error, for failed AND
                          server-cancelled runs — this is where "which
                          worktree holds my preserved work" and "which
                          machine needs upgrading" are actually readable. */}
                      {task.error && (
                        <RunDetailRow
                          label={t(($) => $.transcript.details_reason)}
                          value={task.error}
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
                      {usage && (
                        <>
                          <div className="my-2 h-px bg-border" />
                          <RunDetailRow
                            label={t(($) => $.transcript.details_input)}
                            value={formatTokens(usage.input)}
                          />
                          <RunDetailRow
                            label={t(($) => $.transcript.details_output)}
                            value={formatTokens(usage.output)}
                          />
                          {usage.cacheRead > 0 && (
                            <RunDetailRow
                              label={t(($) => $.transcript.details_cache_read)}
                              value={formatTokens(usage.cacheRead)}
                            />
                          )}
                          {usage.cacheWrite > 0 && (
                            <RunDetailRow
                              label={t(($) => $.transcript.details_cache_write)}
                              value={formatTokens(usage.cacheWrite)}
                            />
                          )}
                          <RunDetailRow
                            label={t(($) => $.transcript.details_cost)}
                            value={formatUsd(usage.cost)}
                          />
                        </>
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

        {/* ── What the run produced ──────────────────────────────────── */}
        <RunOutcomeRow outcome={outcome} branch={task.branch_name} />

        {/* ── Where the time went ────────────────────────────────────── */}
        {showTimeline && lanes && (
          <RunTimeline
            lanes={lanes}
            toolKinds={toolKinds}
            selectedOffsetMs={selectedOffsetMs}
            onSeek={handleSeek}
          />
        )}

        {/* ── Optional header slot (e.g. webhook payload preview) ── */}
        {headerSlot && (
          <div className="border-b px-4 py-3 shrink-0 bg-muted/20">
            {headerSlot}
          </div>
        )}

        {/* ── List toolbar: search left, the two menus right ── */}
        <div className="flex items-center gap-2 border-b px-4 py-1.5 shrink-0">
          <label className="flex h-7 min-w-0 max-w-xs flex-1 items-center gap-1.5 rounded-md bg-muted px-2 text-caption">
            <Search aria-hidden className="h-3 w-3 shrink-0 text-faint-foreground" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t(($) => $.transcript.search_placeholder)}
              aria-label={t(($) => $.transcript.search_placeholder)}
              className="min-w-0 flex-1 bg-transparent text-caption outline-none placeholder:text-faint-foreground"
            />
          </label>
          <span className="ml-auto shrink-0 whitespace-nowrap text-caption text-muted-foreground">
            {activeFilterKeys.length > 0 || trimmedQuery.length > 0
              ? t(($) => $.transcript.steps_filtered, {
                  shown: filteredSteps.length,
                  total: steps.length,
                })
              : t(($) => $.transcript.steps_count, { count: steps.length })}
          </span>
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
          {/* Sort and copy are the run-level actions nobody needs on the way
              in, so they live behind one overflow control instead of two
              permanent buttons. */}
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t(($) => $.transcript.more_actions)}
                  className="text-muted-foreground"
                />
              }
            >
              <MoreHorizontal className="h-3.5 w-3.5" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
              <DropdownMenuItem
                onClick={() =>
                  handleSortDirectionChange(
                    sortDirection === "chronological" ? "newest_first" : "chronological",
                  )
                }
              >
                {sortDirection === "chronological" ? (
                  <ArrowUpNarrowWide className="h-3.5 w-3.5" />
                ) : (
                  <ArrowDownNarrowWide className="h-3.5 w-3.5" />
                )}
                {sortDirection === "chronological"
                  ? t(($) => $.transcript.sort_newest_first)
                  : t(($) => $.transcript.sort_chronological)}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={handleCopyAll}>
                {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
                {copyTranscriptLabel}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>

        {/* ── Steps, and the inspector when one is selected ───────────── */}
        <div className="flex min-h-0 flex-1">
          <div className="flex min-w-0 flex-1 flex-col">
            {displayRows.length === 0 ? (
              <div className="flex h-full items-center justify-center text-body text-muted-foreground">
                {isAntigravityLiveEmpty ? (
                  <div className="flex max-w-md items-center gap-2 px-4 text-center">
                    <Clock className="h-4 w-4 shrink-0" />
                    {t(($) => $.transcript.antigravity_live_unavailable)}
                  </div>
                ) : isLive && steps.length === 0 ? (
                  <div className="flex items-center gap-2">
                    <Loader2 className="h-4 w-4 animate-spin" />
                    {t(($) => $.transcript.waiting_events)}
                  </div>
                ) : steps.length === 0 ? (
                  t(($) => $.transcript.no_data)
                ) : (
                  t(($) => $.transcript.no_matches)
                )}
              </div>
            ) : (
              // Virtualized so a multi-thousand-step run mounts a bounded
              // number of DOM rows (#5733).
              <Virtuoso
                key={listEpoch}
                ref={virtuosoRef}
                style={{ height: "100%" }}
                data={displayRows}
                firstItemIndex={firstItemIndex}
                // Open a live chronological transcript pinned to the newest
                // step (#5921); the per-listEpoch remount re-applies this after
                // task / sort / filter changes.
                initialTopMostItemIndex={
                  isLive && sortDirection !== "newest_first"
                    ? { index: "LAST", align: "end" }
                    : 0
                }
                followOutput={(atBottom) =>
                  isLive && sortDirection !== "newest_first" && atBottom ? "auto" : false
                }
                atBottomThreshold={FOLLOW_EDGE_THRESHOLD}
                atTopThreshold={FOLLOW_EDGE_THRESHOLD}
                atTopStateChange={(atTop) => followCtl.onAtTopChange(atTop)}
                scrollerRef={handleScrollerRef}
                computeItemKey={(_, row) => row.seq}
                components={LIST_COMPONENTS}
                itemContent={(_, row) => (
                  <TranscriptRow
                    row={row}
                    agentName={agentName || agentInfo?.name || ""}
                    agentId={task.agent_id}
                    runStartMs={runStartMs}
                    isLive={isLive}
                    selectedSeq={selectedSeq}
                    expanded={row.kind === "group" && expandedGroups.has(row.seq)}
                    onToggleGroup={handleToggleGroup}
                    onSelect={setSelectedSeq}
                  />
                )}
              />
            )}
          </div>
          {selectedStep && (
            <StepInspector
              step={selectedStep}
              runStartMs={runStartMs}
              onClose={() => setSelectedSeq(null)}
            />
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

// Provider slugs this view names differently from the runtime list. The daemon
// has no display-name override for Claude, so the shared formatter answers
// "Claude" — but a run's diagnostics have always named the tool "Claude Code",
// and `claude-code` is a legacy provider value still present on older tasks
// (title-casing it alone would read as "Claude-code"). Every other provider
// defers to the shared formatter so this row cannot drift from the runtime
// list the way it did before (#5260).
const TRANSCRIPT_PROVIDER_LABELS: Record<string, string> = {
  claude: "Claude Code",
  "claude-code": "Claude Code",
};

function transcriptProviderLabel(provider: string): string {
  return TRANSCRIPT_PROVIDER_LABELS[provider.toLowerCase()] ?? providerDisplayName(provider);
}

// ─── Facts line separator ───────────────────────────────────────────────────

function FactDot() {
  return (
    <span aria-hidden className="text-faint-foreground">
      ·
    </span>
  );
}

// ─── Outcome row ────────────────────────────────────────────────────────────

/**
 * What the run produced, above the evidence for it. For most readers this is
 * the whole answer — "it touched these files and ran these commands" settles
 * "can I trust this" without opening a single step.
 *
 * Renders nothing when the run produced nothing nameable; a row of zeroes
 * would read as "it did nothing" rather than "we have nothing to summarize".
 */
function RunOutcomeRow({
  outcome,
  branch,
}: {
  outcome: ReturnType<typeof buildRunOutcome>;
  branch?: string | null;
}) {
  const { t } = useT("agents");
  if (!outcome) return null;

  const chip = "inline-flex shrink-0 items-center gap-1.5 rounded-md border bg-surface px-2 py-1 text-caption";
  return (
    <div className="flex shrink-0 items-center gap-2 overflow-hidden border-b bg-muted/40 px-4 py-1.5">
      <span className="shrink-0 text-micro text-faint-foreground">
        {t(($) => $.transcript.outcome_label)}
      </span>
      {outcome.paths.length > 0 && (
        <span className={chip} title={outcome.paths.join("\n")}>
          <FilePen aria-hidden className="h-3 w-3 text-muted-foreground" />
          <span className="font-medium">
            {t(($) => $.transcript.outcome_files, { count: outcome.paths.length })}
          </span>
          {outcome.addedLines > 0 && (
            <span className="tabular-nums text-success">+{outcome.addedLines}</span>
          )}
          {outcome.removedLines > 0 && (
            <span className="tabular-nums text-destructive">−{outcome.removedLines}</span>
          )}
        </span>
      )}
      {outcome.commandCount > 0 && (
        <span className={chip}>
          <Terminal aria-hidden className="h-3 w-3 text-muted-foreground" />
          <span className="font-medium">
            {t(($) => $.transcript.outcome_commands, { count: outcome.commandCount })}
          </span>
        </span>
      )}
      {branch && (
        <span className={cn(chip, "min-w-0")}>
          <GitBranch aria-hidden className="h-3 w-3 shrink-0 text-muted-foreground" />
          <span className="truncate font-mono text-micro">{branch}</span>
        </span>
      )}
    </div>
  );
}

// ─── Rows ───────────────────────────────────────────────────────────────────

interface TranscriptRowProps {
  row: TraceRow;
  agentName: string;
  agentId?: string;
  runStartMs?: number;
  isLive: boolean;
  selectedSeq: number | null;
  expanded: boolean;
  onToggleGroup: (seq: number) => void;
  onSelect: (seq: number) => void;
}

function TranscriptRow(props: TranscriptRowProps) {
  const { row } = props;
  if (isGroupRow(row)) return <GroupRow {...props} row={row} />;
  if (!isCallStep(row) && row.kind === "text") return <ProseRow {...props} row={row} />;
  return <StepRow {...props} row={row} />;
}

/** The offset column: where in the run this happened. */
function OffsetCell({ startedAt, runStartMs }: { startedAt?: string; runStartMs?: number }) {
  const at = timeMs(startedAt);
  const label = at !== undefined && runStartMs !== undefined ? formatOffset(at - runStartMs) : "";
  return (
    <span
      className="w-11 shrink-0 pt-0.5 text-right font-mono text-micro tabular-nums text-faint-foreground"
      title={startedAt ? new Date(startedAt).toLocaleString() : undefined}
    >
      {label}
    </span>
  );
}

function DurationCell({ ms, pending }: { ms?: number; pending?: boolean }) {
  const { t } = useT("agents");
  if (pending) {
    return (
      <span className="shrink-0 pt-0.5 text-micro text-info" title={t(($) => $.transcript.step_running)}>
        <Loader2 aria-hidden className="h-3 w-3 animate-spin" />
        <span className="sr-only">{t(($) => $.transcript.step_running)}</span>
      </span>
    );
  }
  if (ms === undefined) return <span className="w-12 shrink-0" />;
  return (
    <span className="w-12 shrink-0 pt-0.5 text-right font-mono text-micro tabular-nums text-faint-foreground">
      {formatStepDuration(ms)}
    </span>
  );
}

/**
 * The agent's own words, at body size and in reading measure.
 *
 * This is the layer inversion the redesign turns on: the report used to look
 * exactly like a `Read`, so the one thing worth reading was the hardest thing
 * to find. Prose is content and stays open; tool calls are evidence and fold.
 */
function ProseRow({
  row,
  agentName,
  agentId,
  runStartMs,
}: TranscriptRowProps & { row: TraceMessageStep }) {
  return (
    <div className="group flex items-start gap-2 px-4 py-2.5">
      <OffsetCell startedAt={row.startedAt} runStartMs={runStartMs} />
      <span aria-hidden className="mt-1 w-0.5 self-stretch rounded-full bg-success" />
      <div className="mt-0.5 shrink-0">
        {agentId ? (
          <ActorAvatar actorType="agent" actorId={agentId} size="sm" />
        ) : (
          <div className="flex h-5 w-5 items-center justify-center rounded-full bg-info/10 text-info">
            <Bot className="h-3 w-3" />
          </div>
        )}
      </div>
      <div className="min-w-0 flex-1 pr-2">
        {agentName && (
          <div className="mb-0.5 text-caption font-semibold">{agentName}</div>
        )}
        <RichContent
          content={row.item.content ?? ""}
          density="compact"
          className="transcript-prose max-w-[62rem]"
        />
      </div>
    </div>
  );
}

/** One call, one thinking block, or one error: a line, with its detail one
 *  click away in the inspector. */
function StepRow({
  row,
  runStartMs,
  isLive,
  selectedSeq,
  onSelect,
}: TranscriptRowProps & { row: TraceStep }) {
  const { t } = useT("agents");
  const summaryLabels = useMemo<TraceSummaryLabels>(
    () => ({
      morePaths: (path, extraCount) =>
        t(($) => $.transcript.patch_summary_more, { path, extra: extraCount }),
    }),
    [t],
  );

  const call = isCallStep(row) ? row : null;
  const label = call
    ? call.tool || t(($) => $.transcript.kind_tool)
    : row.kind === "thinking"
      ? t(($) => $.transcript.kind_thinking)
      : t(($) => $.transcript.kind_error);
  const summary = call
    ? callSummary(call, summaryLabels)
    : firstLineOf((row as TraceMessageStep).item.content);
  const pending = call !== null && isLive && !call.result;
  const selected = selectedSeq === row.seq;

  return (
    <button
      type="button"
      onClick={() => onSelect(row.seq)}
      aria-current={selected ? "true" : undefined}
      className={cn(
        "group flex w-full items-start gap-2 px-4 py-1.5 text-left transition-colors",
        selected ? "bg-brand/8" : "hover:bg-accent/40",
        row.kind === "error" && "bg-destructive/5",
      )}
    >
      <OffsetCell startedAt={row.startedAt} runStartMs={runStartMs} />
      <span
        aria-hidden
        className={cn(
          "mt-0.5 w-0.5 self-stretch rounded-full",
          selected ? "bg-brand" : row.kind === "error" ? "bg-destructive" : "bg-border",
        )}
      />
      <StepIcon
        step={row}
        className={cn(
          "mt-1 h-3 w-3 shrink-0",
          row.kind === "error" ? "text-destructive" : "text-muted-foreground",
        )}
      />
      <span className="flex min-w-0 flex-1 items-baseline gap-1.5">
        <span
          className={cn(
            "shrink-0 text-caption font-medium",
            row.kind === "error" ? "text-destructive" : "text-foreground",
          )}
        >
          {label}
        </span>
        <span
          className={cn(
            "truncate text-micro text-muted-foreground",
            call && "font-mono",
          )}
        >
          {summary}
        </span>
      </span>
      <DurationCell ms={call?.durationMs} pending={pending} />
    </button>
  );
}

/** Consecutive same-tool calls, folded to one line until asked. */
function GroupRow({
  row,
  runStartMs,
  selectedSeq,
  expanded,
  onToggleGroup,
  onSelect,
}: TranscriptRowProps & { row: TraceGroupRow }) {
  const { t } = useT("agents");
  const summaryLabels = useMemo<TraceSummaryLabels>(
    () => ({
      morePaths: (path, extraCount) =>
        t(($) => $.transcript.patch_summary_more, { path, extra: extraCount }),
    }),
    [t],
  );

  return (
    <div className={cn(selectedSeq !== null && row.steps.some((s) => s.seq === selectedSeq) && "bg-brand/5")}>
      <button
        type="button"
        onClick={() => onToggleGroup(row.seq)}
        aria-expanded={expanded}
        className="group flex w-full items-start gap-2 px-4 py-1.5 text-left transition-colors hover:bg-accent/40"
      >
        <OffsetCell startedAt={row.startedAt} runStartMs={runStartMs} />
        <span aria-hidden className="mt-0.5 w-0.5 self-stretch rounded-full bg-border" />
        <ChevronRight
          aria-hidden
          className={cn(
            "mt-1 h-3 w-3 shrink-0 text-faint-foreground transition-transform",
            expanded && "rotate-90",
          )}
        />
        <span className="flex min-w-0 flex-1 items-baseline gap-1.5">
          <span className="shrink-0 text-caption font-medium">{row.tool}</span>
          {/* Name the first thing it touched, not just how many: "Read · 4
              calls" hides the one detail that makes a folded group scannable. */}
          <span className="truncate font-mono text-micro text-muted-foreground">
            {callSummary(row.steps[0]!, summaryLabels)}
          </span>
          <span className="shrink-0 text-micro text-faint-foreground">
            {t(($) => $.transcript.group_calls, { count: row.steps.length })}
          </span>
        </span>
        <DurationCell ms={row.durationMs} />
      </button>
      {expanded && (
        <div className="ml-[4.4rem] border-l pl-3">
          {row.steps.map((step) => (
            <button
              key={step.seq}
              type="button"
              onClick={() => onSelect(step.seq)}
              className={cn(
                "flex w-full items-baseline gap-2 rounded px-2 py-1 text-left text-micro transition-colors",
                selectedSeq === step.seq ? "bg-brand/10" : "hover:bg-accent/40",
              )}
            >
              <span className="truncate font-mono text-muted-foreground">
                {callSummary(step, summaryLabels) || step.tool}
              </span>
              <span className="ml-auto shrink-0 font-mono tabular-nums text-faint-foreground">
                {step.durationMs === undefined ? "" : formatStepDuration(step.durationMs)}
              </span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

/**
 * The one line a call is worth in the list: what it was asked to do.
 *
 * Deliberately the call's argument, never the result — the result row is where
 * base64 screenshots and 8KB command dumps used to land in the middle of the
 * list. Only a call we never saw falls back to its output.
 */
function callSummary(step: TraceCallStep, labels: TraceSummaryLabels): string {
  if (step.call) {
    const summary = traceToolArgSummary(step.call.input, labels);
    if (summary) return summary;
  }
  if (!step.result) return "";
  if (readImageResult(step.result.output)) return "";
  return traceEventSummary({ type: "tool_result", output: step.result.output });
}

function firstLineOf(value: string | undefined): string {
  return value?.split("\n").find((line) => line.trim().length > 0) ?? "";
}

// ─── Inspector ──────────────────────────────────────────────────────────────

/**
 * The evidence for one step, opened on demand.
 *
 * Input and result are stacked rather than tabbed: at this width both fit, and
 * a tab would hide half the evidence behind a click to save nothing.
 */
function StepInspector({
  step,
  runStartMs,
  onClose,
}: {
  step: TraceStep;
  runStartMs?: number;
  onClose: () => void;
}) {
  const { t } = useT("agents");
  const [copied, showCopied] = useCopyFeedback();

  const at = timeMs(step.startedAt);
  const offset = at !== undefined && runStartMs !== undefined ? formatOffset(at - runStartMs) : null;

  const call = isCallStep(step) ? step : null;
  const message = call ? null : (step as TraceMessageStep);

  const handleCopy = useCallback(() => {
    const parts: string[] = [];
    if (call) {
      if (call.call) parts.push(traceEventCopyText(call.call));
      if (call.result) parts.push(traceEventCopyText(call.result));
    } else if (message) {
      parts.push(traceEventCopyText(message.item));
    }
    void copyText(redactSecrets(parts.join("\n\n"))).then((ok) => {
      if (!ok) return;
      showCopied();
    });
  }, [call, message, showCopied]);

  const title =
    call
      ? call.tool || t(($) => $.transcript.kind_tool)
      : step.kind === "thinking"
        ? t(($) => $.transcript.kind_thinking)
        : t(($) => $.transcript.kind_error);

  return (
    <aside className="flex w-[26rem] shrink-0 flex-col border-l bg-muted/25">
      <div className="flex items-center gap-2 border-b px-3 py-2">
        <StepIcon step={step} className="h-4 w-4 shrink-0 text-muted-foreground" />
        <span className="shrink-0 text-label font-semibold">{title}</span>
        <span className="flex min-w-0 flex-1 items-center gap-1.5 text-micro text-muted-foreground">
          {offset && <span className="font-mono tabular-nums">{offset}</span>}
          {call?.durationMs !== undefined && (
            <>
              <FactDot />
              <span className="font-mono tabular-nums">{formatStepDuration(call.durationMs)}</span>
            </>
          )}
        </span>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={handleCopy}
          aria-label={t(($) => $.transcript.copy_step)}
          title={t(($) => $.transcript.copy_step)}
          className="shrink-0 text-muted-foreground"
        >
          {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onClose}
          aria-label={t(($) => $.transcript.close_step)}
          className="shrink-0 text-muted-foreground"
        >
          <X className="h-3.5 w-3.5" />
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {call ? (
          <>
            {call.call && (
              <InspectorSection label={t(($) => $.transcript.step_input)}>
                <StepBody item={call.call} />
              </InspectorSection>
            )}
            {call.result && (
              <InspectorSection label={t(($) => $.transcript.step_result)}>
                <StepBody item={call.result} />
              </InspectorSection>
            )}
          </>
        ) : (
          <InspectorSection label={title}>
            <ToolDetailSurface text={redactSecrets(message?.item.content ?? "")} />
          </InspectorSection>
        )}
      </div>
    </aside>
  );
}

function InspectorSection({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <section className="border-b last:border-b-0">
      <h3 className="px-3 pt-2 text-micro font-medium uppercase tracking-wide text-faint-foreground">
        {label}
      </h3>
      <div className="px-1 pb-2">{children}</div>
    </section>
  );
}

/** One payload, rendered as what it is. */
function StepBody({ item }: { item: TimelineItem }) {
  const { t } = useT("agents");
  const detail = useMemo(() => traceEventDetail(item), [item]);
  const image = useMemo(() => readImageResult(item.output), [item.output]);

  // A screenshot is a picture, not a 200KB base64 string in a <pre>.
  if (image) {
    return (
      <figure className="px-2 py-1">
        <img
          src={`data:${image.mediaType};base64,${image.base64}`}
          alt={t(($) => $.transcript.image_result)}
          className="max-h-80 w-full rounded-md border object-contain"
        />
        <figcaption className="pt-1 text-micro text-faint-foreground">
          {t(($) => $.transcript.image_result)} · {formatBytes(base64ByteLength(image.base64))}
        </figcaption>
      </figure>
    );
  }

  switch (detail.kind) {
    case "diff":
      return <DiffDetailSurface lines={detail.lines} path={detail.path} />;
    case "patch":
      return <PatchDetailSurface files={detail.files} truncated={detail.truncated} />;
    case "file":
      return (
        <FileWriteSurface text={detail.text} lineCount={detail.lineCount} path={detail.path} />
      );
    default: {
      const text = detail.text;
      const clipped =
        text.length > 8000 ? `${redactSecrets(text.slice(0, 8000))}\n... (truncated)` : redactSecrets(text);
      const path = item.type === "tool_use" ? readPathFromInput(item.input) : undefined;
      return <ToolDetailSurface text={clipped} language={path ? languageForPath(path) : undefined} />;
    }
  }
}

function readPathFromInput(input: Record<string, unknown> | undefined): string | undefined {
  if (!input) return undefined;
  const path = input.file_path ?? input.path;
  return typeof path === "string" ? path : undefined;
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
