"use client";

// One virtualized Execution Log dialog for the full Run lifecycle (MUL-5122).
// While active it renders the WS-backed task-message cache; when the Run becomes
// terminal it adopts bounded server pages without changing the dialog or its
// view state. Long history therefore never enters the DOM all at once.
//
// Rows use one scan rhythm in both modes: fixed kind badge, one-line summary,
// and inline #seq / time. Full evidence is shown only in an expanded detail
// panel; Agent text uses compact RichContent Markdown.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  keepPreviousData,
  useInfiniteQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { Virtuoso, type Components, type VirtuosoHandle } from "react-virtuoso";
import {
  AlertCircle,
  ArrowDownNarrowWide,
  ArrowUpNarrowWide,
  Bot,
  Brain,
  Check,
  CheckCircle2,
  ChevronRight,
  Copy,
  Cpu,
  Folder,
  Info,
  Loader2,
  X,
  XCircle,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { api } from "@multica/core/api";
import {
  executionLogCopyAllOptions,
  executionLogPageOptions,
  flattenExecutionLogPages,
  isTaskMessageTaskId,
  type ExecutionLogFilters,
} from "@multica/core/chat/queries";
import {
  useTranscriptViewStore,
  type TranscriptFilterKey,
} from "@multica/core/agents/stores";
import type { AgentRuntime, AgentTask } from "@multica/core/types/agent";
import type { TaskMessagePayload } from "@multica/core/types/events";
import { redactSecrets } from "./redact";
import {
  decodeToolResultOutput,
  traceEventCopyText,
  traceEventHasDetail,
  traceEventKind,
  traceEventLabel,
  traceEventSummary,
} from "./trace-event-presenter";
import { AttributionBadge } from "../../issues/components/attribution-badge";
import { useT } from "../../i18n";
import { RichContent } from "../../rich-content";
import { ActorAvatar } from "../actor-avatar";

const TOOL_KEY_PREFIX = "tool:";
const OUTPUT_DETAIL_CAP = 4000;
const EMPTY_TASK_MESSAGES: TaskMessagePayload[] = [];

/** Stable identity for a loaded event, used as the shared expand-set key. Seq is
 *  unique within a Run; the rest disambiguate defensively if seq ever repeats. */
function execEventKey(m: TaskMessagePayload): string {
  return `${m.seq}|${m.created_at ?? ""}|${m.type}|${m.tool ?? ""}`;
}

function executionLogCopyBlock(message: TaskMessagePayload): string {
  const header = [
    `#${message.seq}`,
    message.created_at,
    `[${traceEventLabel(message)}]`,
  ]
    .filter(Boolean)
    .join(" · ");
  const body = redactSecrets(traceEventCopyText(message)).trim();
  return body ? `${header}\n${body}` : header;
}

function formatRuntimeProvider(provider: string): string {
  const known: Record<string, string> = {
    antigravity: "Antigravity",
    claude: "Claude",
    codex: "Codex",
    cursor: "Cursor",
    hermes: "Hermes",
    openclaw: "OpenClaw",
    opencode: "OpenCode",
  };
  return known[provider.toLowerCase()] ?? provider;
}

/** A human label for a type facet key ("text" → "Agent", etc.). */
function typeFacetLabel(key: string): string {
  return traceEventLabel({ type: key });
}

interface ExecutionLogFacetChip {
  key: string;
  label: string;
  count: number;
}

function buildLocalFacetChips(messages: TaskMessagePayload[]): ExecutionLogFacetChip[] {
  const typeCounts = new Map<string, number>();
  const toolCounts = new Map<string, number>();
  for (const message of messages) {
    typeCounts.set(message.type, (typeCounts.get(message.type) ?? 0) + 1);
    if (message.tool) {
      toolCounts.set(message.tool, (toolCounts.get(message.tool) ?? 0) + 1);
    }
  }
  const byCountThenKey = (a: [string, number], b: [string, number]) =>
    b[1] - a[1] || a[0].localeCompare(b[0]);
  return [
    ...Array.from(typeCounts.entries())
      .sort(byCountThenKey)
      .map(([key, count]) => ({ key, label: typeFacetLabel(key), count })),
    ...Array.from(toolCounts.entries())
      .sort(byCountThenKey)
      .map(([key, count]) => ({ key: `${TOOL_KEY_PREFIX}${key}`, label: key, count })),
  ];
}

function matchesExecutionLogFilters(
  message: TaskMessagePayload,
  filters: ExecutionLogFilters | undefined,
): boolean {
  if (!filters) return true;
  return (
    (filters.types?.includes(message.type) ?? false) ||
    (!!message.tool && (filters.tools?.includes(message.tool) ?? false))
  );
}

// ─── Fixed kind colors ───────────────────────────────────────────────────────
//
// The timeline segments + legend dots are the ONE place this dialog uses fixed
// palette colors instead of design tokens so a given event kind remains
// visually stable across live and terminal data sources.

type ExecEventColor = "agent" | "thinking" | "tool" | "result" | "error";

function execEventColor(message: TaskMessagePayload): ExecEventColor {
  switch (traceEventKind(message)) {
    case "agent":
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

const EXEC_COLOR_CLASSES: Record<
  ExecEventColor,
  { bar: string; barActive: string; label: string }
> = {
  agent: {
    bar: "bg-emerald-400/60",
    barActive: "bg-emerald-500",
    label: "bg-emerald-500 text-white",
  },
  thinking: {
    bar: "bg-violet-400/60",
    barActive: "bg-violet-500",
    label: "bg-violet-500/20 text-violet-700 dark:text-violet-300",
  },
  tool: {
    bar: "bg-blue-400/60",
    barActive: "bg-blue-500",
    label: "bg-blue-500/20 text-blue-700 dark:text-blue-300",
  },
  result: {
    bar: "bg-slate-300/60 dark:bg-slate-600/60",
    barActive: "bg-slate-400 dark:bg-slate-500",
    label: "bg-muted text-muted-foreground",
  },
  error: {
    bar: "bg-red-400/60",
    barActive: "bg-red-500",
    label: "bg-red-500/20 text-red-700 dark:text-red-300",
  },
};

const EXEC_COLOR_ORDER: ExecEventColor[] = ["agent", "thinking", "tool", "result", "error"];

// Legend labels intentionally reuse the presenter's provider-native kind
// vocabulary (traceEventLabel emits "Agent"/"Thinking"/… untranslated by design)
// so the legend and the row kind labels can never disagree.
const EXEC_KIND_LABEL: Record<ExecEventColor, string> = {
  agent: "Agent",
  thinking: "Thinking",
  tool: "Tool",
  result: "Result",
  error: "Error",
};

/** Wall-clock duration between two ISO timestamps, e.g. "1m 0s". */
function formatDuration(start: string, end: string): string {
  const seconds = Math.max(
    0,
    Math.floor((new Date(end).getTime() - new Date(start).getTime()) / 1000),
  );
  if (seconds < 60) return `${seconds}s`;
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
}

/** Short local date-time for the run-detail rows. */
function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function LiveRunDuration({ start }: { start: string }) {
  const { t } = useT("issues");
  const [duration, setDuration] = useState(() =>
    formatDuration(start, new Date().toISOString()),
  );

  useEffect(() => {
    const update = () => setDuration(formatDuration(start, new Date().toISOString()));
    update();
    const timer = setInterval(update, 1000);
    return () => clearInterval(timer);
  }, [start]);

  return <>{t(($) => $.execution_log.run_duration, { duration })}</>;
}

// ─── Virtuoso loading-edge slot (loading-older / older-page error) ───────────
//
// Module-scope + context-driven, matching chat-message-list: an inline
// components prop rebuilds the slot type every render and remounts its subtree,
// which is exactly the churn MUL-3960 fixed. Per-render data reaches the slot
// through Virtuoso's `context` prop instead. In chronological order older events
// load at the START (Header slot); in newest-first order they load at the END
// (Footer slot) — the content is identical, only the slot differs.

interface LogListContext {
  isFetchingEarlier: boolean;
  earlierError: boolean;
  onRetryEarlier: () => void;
}

function LogListEdge({ context }: { context?: LogListContext }) {
  const { t } = useT("issues");
  if (!context) return null;
  if (context.earlierError) {
    return (
      <div className="flex items-center justify-center gap-2 px-4 py-2 text-xs text-muted-foreground">
        <span>{t(($) => $.execution_log.earlier_error)}</span>
        <button
          type="button"
          onClick={context.onRetryEarlier}
          className="rounded px-1.5 py-0.5 text-foreground underline underline-offset-2 hover:bg-accent"
        >
          {t(($) => $.execution_log.retry)}
        </button>
      </div>
    );
  }
  if (context.isFetchingEarlier) {
    return (
      <div className="flex items-center justify-center gap-2 px-4 py-2 text-xs text-muted-foreground">
        <Loader2 className="h-3 w-3 animate-spin" />
        {t(($) => $.execution_log.loading_earlier)}
      </div>
    );
  }
  return null;
}

const HEADER_COMPONENTS: Components<TaskMessagePayload, LogListContext> = {
  Header: LogListEdge,
};
const FOOTER_COMPONENTS: Components<TaskMessagePayload, LogListContext> = {
  Footer: LogListEdge,
};

// ─── Main dialog ─────────────────────────────────────────────────────────────

interface ExecutionLogDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  task: AgentTask;
  agentName: string;
  actorType?: string;
  actorId?: string;
  isLive?: boolean;
  /**
   * Defined only when this dialog session began while the Run was active.
   * The array is updated by the shared WS cache and remains a transition
   * fallback until the first terminal page arrives.
   */
  liveMessages?: TaskMessagePayload[];
  /**
   * Optional content rendered between the header and the event list, e.g. an
   * autopilot webhook payload preview.
   */
  headerSlot?: React.ReactNode;
}

export function ExecutionLogDialog({
  open,
  onOpenChange,
  task,
  agentName,
  actorType = "agent",
  actorId,
  isLive = false,
  liveMessages,
  headerSlot,
}: ExecutionLogDialogProps) {
  const { t } = useT("issues");
  const queryClient = useQueryClient();
  const [scrollEl, setScrollEl] = useState<HTMLDivElement | null>(null);
  const [copied, setCopied] = useState(false);
  const [copiedWorkdir, setCopiedWorkdir] = useState(false);
  const [isCopying, setIsCopying] = useState(false);
  const [facetScope, setFacetScope] = useState<{
    taskId: string;
    keys: string[];
  } | null>(null);
  // Shared expand set owned by the dialog so a bulk action can expand/collapse
  // every loaded row at once. Holds `execEventKey` values; a row is open iff its
  // key is present.
  const [expandedKeys, setExpandedKeys] = useState<Set<string>>(() => new Set());
  // Segment click highlights the target row briefly, then clears.
  const [highlightedKey, setHighlightedKey] = useState<string | null>(null);
  const [runtimeInfo, setRuntimeInfo] = useState<AgentRuntime | null>(null);

  const virtuosoRef = useRef<VirtuosoHandle>(null);
  const highlightTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const copiedTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const copiedWorkdirTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const autoAppliedKeysRef = useRef<Set<string>>(new Set());
  const initializedTaskRef = useRef<string | null>(null);
  const previousDefaultExpandedRef = useRef(false);

  const sortDirection = useTranscriptViewStore((s) => s.sortDirection);
  const setSortDirection = useTranscriptViewStore((s) => s.setSortDirection);
  const selectedKeys = useTranscriptViewStore((s) => s.selectedFilterKeys);
  const togglePersistedFilterKey = useTranscriptViewStore((s) => s.toggleFilterKey);
  const clearPersistedFilterKeys = useTranscriptViewStore((s) => s.clearFilterKeys);
  const defaultExpanded = useTranscriptViewStore((s) => s.defaultExpanded);
  const setDefaultExpanded = useTranscriptViewStore((s) => s.setDefaultExpanded);
  const chronological = sortDirection === "chronological";

  // Filters persist by default. Resolve them against this Run's complete facet
  // list before applying them, so a tool/type remembered from another Run
  // cannot create an invisible, impossible-to-toggle empty filter.
  const applicableSelectedKeys = useMemo(() => {
    if (facetScope?.taskId !== task.id) return [];
    const available = new Set(facetScope.keys);
    return selectedKeys.filter((key) => available.has(key));
  }, [facetScope, selectedKeys, task.id]);

  // Selected chip keys are in the presenter's traceEventFilterKey format
  // ("error" or "tool:Bash"); split them back into the API's type/tool arrays.
  const filters = useMemo<ExecutionLogFilters | undefined>(() => {
    if (applicableSelectedKeys.length === 0) return undefined;
    const types: string[] = [];
    const tools: string[] = [];
    for (const key of applicableSelectedKeys) {
      if (key.startsWith(TOOL_KEY_PREFIX)) tools.push(key.slice(TOOL_KEY_PREFIX.length));
      else types.push(key);
    }
    return {
      types: types.length > 0 ? types : undefined,
      tools: tools.length > 0 ? tools : undefined,
    };
  }, [applicableSelectedKeys]);

  const pageOptions = executionLogPageOptions(task.id, filters, 50);
  const {
    data: pagedData,
    isLoading: isPagedLoading,
    isError: isPagedError,
    refetch,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    isPlaceholderData,
    fetchNextPage,
  } = useInfiniteQuery({
    ...pageOptions,
    enabled: !isLive && isTaskMessageTaskId(task.id),
    // Facet buttons stay available while a new combination loads, so users can
    // select several filters consecutively without the controls disappearing.
    placeholderData: keepPreviousData,
  });

  const pages = pagedData?.pages ?? [];
  const first = pages[0];
  const pagedMessages = useMemo(
    () => flattenExecutionLogPages(pagedData?.pages),
    [pagedData?.pages],
  );
  const liveAllMessages = liveMessages ?? EMPTY_TASK_MESSAGES;
  // On a running→terminal transition, retain the live cache until the first
  // bounded page is ready. The component and all view state stay mounted.
  const usingLiveSource = liveMessages !== undefined && (isLive || pagedData === undefined);
  const liveFilteredMessages = useMemo(
    () => liveAllMessages.filter((message) => matchesExecutionLogFilters(message, filters)),
    [filters, liveAllMessages],
  );
  const messages = usingLiveSource ? liveFilteredMessages : pagedMessages;

  // Newest-first is a pure presentation reverse; seq numbers and detail are
  // untouched, so filters/copy keep working against the same data.
  const orderedMessages = useMemo(
    () => (chronological ? messages : [...messages].reverse()),
    [messages, chronological],
  );

  // Chronological: older pages prepend at the START, so firstItemIndex counts
  // down by their length to keep already-rendered rows anchored across a
  // prepend. Newest-first: older pages append at the END and never shift the
  // existing indices, so a fixed 0 anchor is correct.
  const olderCount = pages.slice(1).reduce((sum, page) => sum + page.messages.length, 0);
  const firstItemIndex = chronological
    ? messages.length > 0
      ? 1_000_000 - olderCount
      : 0
    : 0;

  // Totals and facets are full-Run context. Live mode derives them from the
  // complete cache; terminal mode takes the server-owned values.
  const rawTotal = usingLiveSource ? liveAllMessages.length : (first?.raw_total ?? 0);
  const matchedTotal = usingLiveSource
    ? liveFilteredMessages.length
    : (first?.matched_total ?? 0);

  const filterActive = applicableSelectedKeys.length > 0;

  const pagedChips = useMemo<ExecutionLogFacetChip[]>(
    () => [
      ...(first?.type_facets ?? []).map((f) => ({
        key: f.key,
        label: typeFacetLabel(f.key),
        count: f.count,
      })),
      ...(first?.tool_facets ?? []).map((f) => ({
        key: `${TOOL_KEY_PREFIX}${f.key}`,
        label: f.key,
        count: f.count,
      })),
    ],
    [first],
  );
  const liveChips = useMemo(() => buildLocalFacetChips(liveAllMessages), [liveAllMessages]);
  const chips = usingLiveSource ? liveChips : pagedChips;
  const facetsReady = usingLiveSource || (!isPlaceholderData && first !== undefined);

  useEffect(() => {
    if (!facetsReady) return;
    const keys = chips.map((chip) => chip.key).sort();
    setFacetScope((prev) => {
      if (
        prev?.taskId === task.id &&
        prev.keys.length === keys.length &&
        prev.keys.every((key, index) => key === keys[index])
      ) {
        return prev;
      }
      return { taskId: task.id, keys };
    });
  }, [chips, facetsReady, task.id]);

  const toggleKey = useCallback(
    (key: TranscriptFilterKey) => {
      togglePersistedFilterKey(key);
    },
    [togglePersistedFilterKey],
  );

  const clearFilters = useCallback(() => {
    clearPersistedFilterKeys();
  }, [clearPersistedFilterKeys]);

  // Bulk expand is bounded to LOADED rows with detail — never the unfetched
  // history (which could be tens of thousands of events).
  const expandableKeys = useMemo(
    () => messages.filter((m) => traceEventHasDetail(m)).map(execEventKey),
    [messages],
  );
  const toggleExpanded = useCallback(
    (key: string) => {
      autoAppliedKeysRef.current.add(key);
      const next = new Set(expandedKeys);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      setExpandedKeys(next);
      setDefaultExpanded(
        expandableKeys.length > 0 && expandableKeys.every((candidate) => next.has(candidate)),
      );
    },
    [expandedKeys, expandableKeys, setDefaultExpanded],
  );

  const handleBulkExpand = useCallback(() => {
    const nextExpanded = !defaultExpanded;
    for (const key of expandableKeys) autoAppliedKeysRef.current.add(key);
    setExpandedKeys((prev) => {
      if (!nextExpanded) return new Set();
      return new Set([...prev, ...expandableKeys]);
    });
    setDefaultExpanded(nextExpanded);
  }, [defaultExpanded, expandableKeys, setDefaultExpanded]);

  useEffect(() => {
    const switchedOn =
      defaultExpanded && previousDefaultExpandedRef.current !== defaultExpanded;
    previousDefaultExpandedRef.current = defaultExpanded;

    if (initializedTaskRef.current !== task.id || switchedOn) {
      initializedTaskRef.current = task.id;
      autoAppliedKeysRef.current = new Set(defaultExpanded ? expandableKeys : []);
      setExpandedKeys(defaultExpanded ? new Set(expandableKeys) : new Set());
      return;
    }

    if (!defaultExpanded) return;
    const unseen = expandableKeys.filter((key) => !autoAppliedKeysRef.current.has(key));
    if (unseen.length === 0) return;
    for (const key of unseen) autoAppliedKeysRef.current.add(key);
    setExpandedKeys((prev) => new Set([...prev, ...unseen]));
  }, [defaultExpanded, expandableKeys, task.id]);

  const handleRetryEarlier = useCallback(() => {
    void fetchNextPage();
  }, [fetchNextPage]);

  const handleEdgeReached = useCallback(() => {
    if (hasNextPage && !isFetchingNextPage) void fetchNextPage();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

  // Copy is an explicit full-Run action. Live mode copies the complete cache;
  // terminal mode walks every unfiltered page. Neither depends on active facets.
  const handleCopyAll = useCallback(async () => {
    if (isCopying) return;
    setIsCopying(true);
    setCopied(false);
    try {
      let allMessages: TaskMessagePayload[];
      if (usingLiveSource) {
        allMessages = liveAllMessages;
      } else {
        allMessages = await queryClient.fetchQuery(
          executionLogCopyAllOptions(task.id),
        );
      }

      const copyMessages = chronological ? allMessages : [...allMessages].reverse();
      const ok = await copyText(copyMessages.map(executionLogCopyBlock).join("\n\n"));
      if (!ok) return;
      setCopied(true);
      if (copiedTimerRef.current) clearTimeout(copiedTimerRef.current);
      copiedTimerRef.current = setTimeout(() => setCopied(false), 2000);
    } catch {
      setCopied(false);
    } finally {
      setIsCopying(false);
    }
  }, [
    chronological,
    isCopying,
    liveAllMessages,
    queryClient,
    task.id,
    usingLiveSource,
  ]);

  const handleCopyWorkdir = useCallback(() => {
    if (!task.relative_work_dir) return;
    void copyText(task.relative_work_dir).then((ok) => {
      if (!ok) return;
      setCopiedWorkdir(true);
      if (copiedWorkdirTimerRef.current) clearTimeout(copiedWorkdirTimerRef.current);
      copiedWorkdirTimerRef.current = setTimeout(() => setCopiedWorkdir(false), 2000);
    });
  }, [task.relative_work_dir]);

  const handleSegmentClick = useCallback(
    (index: number) => {
      const message = orderedMessages[index];
      if (!message) return;
      setHighlightedKey(execEventKey(message));
      // Virtuoso scrollToIndex takes the DATA index (position in `data`), not the
      // firstItemIndex-offset index — so the orderedMessages index is correct.
      virtuosoRef.current?.scrollToIndex({ index, align: "center" });
      if (highlightTimerRef.current) clearTimeout(highlightTimerRef.current);
      highlightTimerRef.current = setTimeout(() => setHighlightedKey(null), 1600);
    },
    [orderedMessages],
  );

  // Run-context metadata for the information popover.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    if (task.runtime_id) {
      api
        .listRuntimes()
        .then((runtimes) => {
          if (cancelled) return;
          const rt = runtimes.find((r) => r.id === task.runtime_id);
          if (rt) setRuntimeInfo(rt);
        })
        .catch(() => {});
    }
    return () => {
      cancelled = true;
    };
  }, [open, task.runtime_id]);

  useEffect(
    () => () => {
      if (highlightTimerRef.current) clearTimeout(highlightTimerRef.current);
      if (copiedTimerRef.current) clearTimeout(copiedTimerRef.current);
      if (copiedWorkdirTimerRef.current) clearTimeout(copiedWorkdirTimerRef.current);
    },
    [],
  );

  // Loaded #seq range (reduce, not Math.min(...spread), so a large loaded window
  // never overflows the call-argument limit).
  const seqRange = useMemo(() => {
    if (messages.length === 0) return null;
    let min = Infinity;
    let max = -Infinity;
    for (const m of messages) {
      if (m.seq < min) min = m.seq;
      if (m.seq > max) max = m.seq;
    }
    return { min, max };
  }, [messages]);

  const legendKinds = useMemo(() => {
    const present = new Set(messages.map(execEventColor));
    return EXEC_COLOR_ORDER.filter((c) => present.has(c));
  }, [messages]);

  // Secondary one-liner: live/terminal timing and work volume.
  const isTerminal =
    task.status === "completed" ||
    task.status === "failed" ||
    task.status === "cancelled";
  const duration =
    isTerminal && task.started_at && task.completed_at
      ? formatDuration(task.started_at, task.completed_at)
      : null;
  const liveStart = isLive ? (task.started_at ?? task.dispatched_at) : null;

  // Low-frequency debugging metadata stays in the information popover.
  // Workdir is the server-derived RELATIVE path only — the absolute work_dir
  // never renders.
  const runtimeName = runtimeInfo?.name ?? null;
  const providerLabel = runtimeInfo?.provider
    ? formatRuntimeProvider(runtimeInfo.provider)
    : null;
  const runtimeMode = runtimeInfo?.runtime_mode ?? null;
  const workdir = task.relative_work_dir || null;
  const createdLabel = task.created_at ? formatDateTime(task.created_at) : null;
  const dispatchedLabel = task.dispatched_at ? formatDateTime(task.dispatched_at) : null;
  const startedLabel = task.started_at ? formatDateTime(task.started_at) : null;
  const completedLabel =
    isTerminal && task.completed_at ? formatDateTime(task.completed_at) : null;
  const hasRunSummary = !!(
    duration ||
    liveStart ||
    rawTotal > 0 ||
    startedLabel ||
    completedLabel
  );
  const hasRunDetails = !!(
    runtimeName ||
    providerLabel ||
    runtimeMode ||
    workdir ||
    createdLabel ||
    dispatchedLabel ||
    startedLabel ||
    completedLabel
  );
  const displayActorId = actorId ?? task.agent_id;

  const listContext: LogListContext = {
    isFetchingEarlier: isFetchingNextPage,
    earlierError: isFetchNextPageError,
    onRetryEarlier: handleRetryEarlier,
  };

  const showInitialLoading = isPagedLoading && !usingLiveSource;
  const showPagedError = isPagedError && !usingLiveSource;

  let body: React.ReactNode;
  if (showInitialLoading) {
    body = <ExecutionLogSkeleton />;
  } else if (showPagedError) {
    body = (
      <div className="flex h-full flex-col items-center justify-center gap-3 px-4 text-center text-sm text-muted-foreground">
        <span>{t(($) => $.execution_log.load_error)}</span>
        <Button variant="outline" size="sm" onClick={() => void refetch()}>
          {t(($) => $.execution_log.retry)}
        </Button>
      </div>
    );
  } else if (
    isLive &&
    rawTotal === 0 &&
    runtimeInfo?.provider.toLowerCase() === "antigravity"
  ) {
    body = (
      <div className="flex h-full items-center justify-center gap-2 px-6 text-center text-sm text-muted-foreground">
        <Info className="h-4 w-4 shrink-0" />
        {t(($) => $.execution_log.antigravity_live_unavailable)}
      </div>
    );
  } else if (isLive && rawTotal === 0) {
    body = (
      <div className="flex h-full items-center justify-center gap-2 px-4 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        {t(($) => $.execution_log.waiting_events)}
      </div>
    );
  } else if (rawTotal === 0) {
    body = (
      <div className="flex h-full items-center justify-center px-4 text-sm text-muted-foreground">
        {t(($) => $.execution_log.empty)}
      </div>
    );
  } else if (messages.length === 0) {
    body = (
      <div className="flex h-full items-center justify-center px-4 text-sm text-muted-foreground">
        {t(($) => $.execution_log.no_match)}
      </div>
    );
  } else if (scrollEl) {
    body = (
      <Virtuoso<TaskMessagePayload, LogListContext>
        ref={virtuosoRef}
        // Remount when direction flips so the list lands at that direction's
        // start point (bottom for chronological, top for newest-first).
        key={sortDirection}
        customScrollParent={scrollEl}
        data={orderedMessages}
        firstItemIndex={firstItemIndex}
        initialTopMostItemIndex={chronological ? { index: "LAST", align: "end" } : 0}
        increaseViewportBy={{ top: 400, bottom: 600 }}
        startReached={chronological ? handleEdgeReached : undefined}
        endReached={chronological ? undefined : handleEdgeReached}
        context={listContext}
        components={chronological ? HEADER_COMPONENTS : FOOTER_COMPONENTS}
        itemContent={(_, message) => {
          const key = execEventKey(message);
          return (
            <ExecutionLogRow
              message={message}
              open={expandedKeys.has(key)}
              onToggle={() => toggleExpanded(key)}
              highlighted={highlightedKey === key}
            />
          );
        }}
      />
    );
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        data-testid="execution-log-dialog"
        className="!max-w-4xl !w-[calc(100vw-4rem)] !max-h-[calc(100vh-4rem)] !h-[calc(100vh-4rem)] flex flex-col !p-0 !gap-0 overflow-hidden"
        showCloseButton={false}
      >
        <DialogTitle className="sr-only">{t(($) => $.execution_log.dialog_title)}</DialogTitle>

        {/* ── Run identity ───────────────────────────────────────── */}
        <div className="shrink-0 border-b px-4 py-3">
          <div
            data-testid="execution-log-run-header"
            className="flex items-center gap-3"
          >
            <RunStatusBadge status={task.status} />

            <div className="flex min-w-0 flex-1 items-center gap-x-4 overflow-hidden">
              <div className="flex min-w-0 items-center gap-2">
                {displayActorId ? (
                  <ActorAvatar
                    actorType={actorType}
                    actorId={displayActorId}
                    size="md"
                    enableHoverCard
                  />
                ) : (
                  <div className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-info/10 text-info">
                    <Bot className="h-3.5 w-3.5" />
                  </div>
                )}
                {agentName && (
                  <span className="min-w-0 truncate text-sm font-medium">
                    {t(($) => $.execution_log.executed_by, {
                      name: agentName,
                    })}
                  </span>
                )}
              </div>
              <RunTriggerSource task={task} />
              <AttributionBadge
                attribution={task.attribution}
                variant="inline"
                className="max-w-52"
              />
            </div>

            <div className="flex shrink-0 items-center gap-0.5">
              {hasRunDetails && (
                <Popover>
                  <PopoverTrigger
                    render={
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        data-testid="execution-log-info"
                        aria-label={t(($) => $.execution_log.run_info)}
                        title={t(($) => $.execution_log.run_info)}
                        className="h-7 w-7 text-muted-foreground"
                      >
                        <Info className="h-3.5 w-3.5" />
                      </Button>
                    }
                  />
                  <PopoverContent
                    align="end"
                    className="w-96 max-w-[calc(100vw-2rem)] p-3 text-xs"
                  >
                    <div className="mb-2 font-medium text-foreground">
                      {t(($) => $.execution_log.run_info)}
                    </div>
                    <div className="space-y-1">
                      {runtimeName && (
                        <RunDetailRow
                          icon={<Cpu className="h-3 w-3" />}
                          label={t(($) => $.execution_log.details_runtime)}
                          value={runtimeName}
                        />
                      )}
                      {providerLabel && (
                        <RunDetailRow
                          label={t(($) => $.execution_log.details_provider)}
                          value={providerLabel}
                        />
                      )}
                      {runtimeMode && (
                        <RunDetailRow
                          label={t(($) => $.execution_log.details_mode)}
                          value={runtimeMode}
                        />
                      )}
                      {workdir && (
                        <CopyableRunDetailRow
                          icon={<Folder className="h-3 w-3" />}
                          label={t(($) => $.execution_log.details_workdir)}
                          value={workdir}
                          copied={copiedWorkdir}
                          onCopy={handleCopyWorkdir}
                          title={`${t(($) => $.execution_log.copy_workdir_tooltip)}\n${workdir}`}
                        />
                      )}
                      {createdLabel && (
                        <RunDetailRow
                          label={t(($) => $.execution_log.details_created)}
                          value={createdLabel}
                        />
                      )}
                      {dispatchedLabel && (
                        <RunDetailRow
                          label={t(($) => $.execution_log.details_dispatched)}
                          value={dispatchedLabel}
                        />
                      )}
                      {startedLabel && (
                        <RunDetailRow
                          label={t(($) => $.execution_log.details_started)}
                          value={startedLabel}
                        />
                      )}
                      {completedLabel && (
                        <RunDetailRow
                          label={t(($) => $.execution_log.details_completed)}
                          value={completedLabel}
                        />
                      )}
                    </div>
                  </PopoverContent>
                </Popover>
              )}
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                onClick={() => onOpenChange(false)}
                aria-label={t(($) => $.execution_log.close)}
                className="h-7 w-7 text-muted-foreground"
              >
                <X className="h-4 w-4" />
              </Button>
            </div>
          </div>
        </div>

        {/* ── Run summary + log viewing controls ────────────────── */}
        {showInitialLoading ? (
          <ExecutionLogToolbarSkeleton />
        ) : rawTotal > 0 || hasRunSummary ? (
          <div className="shrink-0 space-y-2 border-b px-4 py-2.5">
            <div
              data-testid="execution-log-toolbar"
              className="flex max-w-full items-center gap-3"
            >
              <div
                data-testid="execution-log-summary"
                className="flex min-w-0 flex-1 items-center gap-x-3 overflow-hidden whitespace-nowrap text-xs text-muted-foreground"
              >
                {(liveStart || duration) && (
                  <Badge
                    data-testid="execution-log-duration"
                    variant="secondary"
                    className="h-6 shrink-0 px-2 font-normal text-muted-foreground"
                  >
                    {liveStart ? (
                      <LiveRunDuration start={liveStart} />
                    ) : (
                      t(($) => $.execution_log.run_duration, { duration })
                    )}
                  </Badge>
                )}
                {rawTotal > 0 && (
                  <Badge
                    data-testid="execution-log-event-count"
                    variant="secondary"
                    className="h-6 shrink-0 px-2 font-normal text-muted-foreground"
                  >
                    {t(($) => $.execution_log.events_count, {
                      count: rawTotal,
                    })}
                  </Badge>
                )}
                {startedLabel && (
                  <span
                    data-testid="execution-log-start-time"
                    className="inline-flex shrink-0 items-center gap-1"
                    title={startedLabel}
                  >
                    <span>{t(($) => $.execution_log.details_started)}</span>
                    <span className="tabular-nums text-foreground/70">
                      {startedLabel}
                    </span>
                  </span>
                )}
                {completedLabel && (
                  <span
                    data-testid="execution-log-end-time"
                    className="inline-flex shrink-0 items-center gap-1"
                    title={completedLabel}
                  >
                    <span>{t(($) => $.execution_log.details_completed)}</span>
                    <span className="tabular-nums text-foreground/70">
                      {completedLabel}
                    </span>
                  </span>
                )}
              </div>

              {rawTotal > 0 && (
                <div
                  data-testid="execution-log-toolbar-actions"
                  className="ml-auto flex shrink-0 items-center gap-1"
                >
                  {messages.length > 0 && (
                    <ExecutionLogToggleButton
                      pressed={chronological}
                      onClick={() =>
                        setSortDirection(
                          chronological ? "newest_first" : "chronological",
                        )
                      }
                      aria-label={
                        chronological
                          ? t(($) => $.execution_log.sort_chronological)
                          : t(($) => $.execution_log.sort_newest_first)
                      }
                      title={
                        chronological
                          ? t(($) => $.execution_log.sort_to_newest)
                          : t(($) => $.execution_log.sort_to_chronological)
                      }
                    >
                      {chronological ? (
                        <ArrowDownNarrowWide className="h-3 w-3" />
                      ) : (
                        <ArrowUpNarrowWide className="h-3 w-3" />
                      )}
                      {chronological
                        ? t(($) => $.execution_log.sort_chronological)
                        : t(($) => $.execution_log.sort_newest_first)}
                    </ExecutionLogToggleButton>
                  )}

                  {expandableKeys.length > 0 && (
                    <ExecutionLogToggleButton
                      pressed={defaultExpanded}
                      data-testid="execution-log-expand-all"
                      onClick={handleBulkExpand}
                      title={
                        defaultExpanded
                          ? t(($) => $.execution_log.collapse_scope_tooltip, {
                              n: expandableKeys.length,
                            })
                          : t(($) => $.execution_log.expand_scope_tooltip, {
                              n: expandableKeys.length,
                            })
                      }
                    >
                      <ChevronRight
                        className={cn(
                          "h-3 w-3 transition-transform",
                          defaultExpanded && "rotate-90",
                        )}
                      />
                      {t(($) => $.execution_log.expand_all)}
                    </ExecutionLogToggleButton>
                  )}

                  <Button
                    type="button"
                    variant="outline"
                    size="icon-sm"
                    data-testid="execution-log-copy-all"
                    aria-label={t(($) => $.execution_log.copy_all)}
                    title={t(($) => $.execution_log.copy_all_tooltip, {
                      n: rawTotal,
                    })}
                    disabled={isCopying}
                    onClick={() => void handleCopyAll()}
                    className="h-7 w-7 bg-transparent text-muted-foreground"
                  >
                    {isCopying ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : copied ? (
                      <Check className="h-3.5 w-3.5" />
                    ) : (
                      <Copy className="h-3.5 w-3.5" />
                    )}
                  </Button>
                </div>
              )}
            </div>

            {rawTotal > 0 && chips.length > 0 && (
              <div
                data-testid="execution-log-filter"
                className="flex max-w-full flex-wrap items-center gap-1.5"
              >
                <span className="mr-0.5 text-xs text-muted-foreground">
                  {t(($) => $.execution_log.filter)}
                </span>
                {chips.map((chip) => {
                  const active = applicableSelectedKeys.includes(chip.key);
                  return (
                    <ExecutionLogToggleButton
                      key={chip.key}
                      onClick={() => toggleKey(chip.key)}
                      pressed={active}
                      aria-label={`${chip.label} ${chip.count}`}
                    >
                      <span>{chip.label}</span>
                      <span
                        className={cn(
                          "tabular-nums",
                          active
                            ? "text-accent-foreground/70"
                            : "text-muted-foreground/60",
                        )}
                      >
                        {chip.count}
                      </span>
                    </ExecutionLogToggleButton>
                  );
                })}
                {filterActive && (
                  <button
                    type="button"
                    onClick={clearFilters}
                    className="h-7 rounded-md px-2 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                  >
                    {t(($) => $.execution_log.clear_filters)}
                  </button>
                )}
              </div>
            )}
          </div>
        ) : null}

        {/* ── Color timeline + loaded-window counts ──────────────── */}
        {showInitialLoading ? (
          <ExecutionLogTimelineSkeleton />
        ) : messages.length > 0 ? (
          <div
            data-testid="execution-log-timeline"
            className="border-b px-4 py-2.5 shrink-0 space-y-1.5"
          >
            <div className="flex items-center justify-between gap-2 text-xs text-muted-foreground">
              <div className="min-w-0 truncate">
                <span data-testid="execution-log-total">{rawTotal}</span>{" "}
                {t(($) => $.execution_log.events_label)}
                {filterActive && (
                  <>
                    {" · "}
                    {t(($) => $.execution_log.matched_count, { n: matchedTotal })}
                  </>
                )}
                {" · "}
                {t(($) => $.execution_log.loaded_count, { n: messages.length })}
              </div>
              {seqRange && (
                <span className="shrink-0 tabular-nums">
                  {`#${seqRange.min}–#${seqRange.max}`}
                </span>
              )}
            </div>

            <ExecutionTimelineBar
              messages={orderedMessages}
              highlightedKey={highlightedKey}
              onSegmentClick={handleSegmentClick}
              ariaLabel={t(($) => $.execution_log.timeline_label)}
            />

            {legendKinds.length > 0 && (
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                {legendKinds.map((kind) => (
                  <span
                    key={kind}
                    className="inline-flex items-center gap-1 text-[10px] text-muted-foreground"
                  >
                    <span
                      className={cn("h-2 w-2 rounded-full", EXEC_COLOR_CLASSES[kind].barActive)}
                    />
                    {EXEC_KIND_LABEL[kind]}
                  </span>
                ))}
              </div>
            )}
          </div>
        ) : null}

        {/* ── Optional header slot (e.g. webhook payload preview) ── */}
        {headerSlot && (
          <div className="border-b px-4 py-3 shrink-0 bg-muted/20">{headerSlot}</div>
        )}

        {/* ── Event list ─────────────────────────────────────────── */}
        <div
          ref={setScrollEl}
          data-testid="execution-log-scroll"
          className="flex-1 overflow-y-auto min-h-0"
        >
          {body}
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ─── Run status badge ────────────────────────────────────────────────────────

// Small identity-line badge for the run's terminal/active status. Uses the
// shared `status_*` copy already in the execution_log namespace + semantic
// tokens. A default branch keeps a server-added status readable.
function RunStatusBadge({ status }: { status: AgentTask["status"] }) {
  const { t } = useT("issues");
  const base = "h-6 shrink-0 px-2.5";
  switch (status) {
    case "completed":
      return (
        <Badge
          data-testid="execution-log-status"
          variant="secondary"
          className={cn(base, "bg-success/15 text-success")}
        >
          <CheckCircle2 className="h-3 w-3" />
          {t(($) => $.execution_log.status_completed)}
        </Badge>
      );
    case "failed":
      return (
        <Badge
          data-testid="execution-log-status"
          variant="destructive"
          className={base}
        >
          <XCircle className="h-3 w-3" />
          {t(($) => $.execution_log.status_failed)}
        </Badge>
      );
    case "running":
      return (
        <Badge
          data-testid="execution-log-status"
          variant="secondary"
          className={cn(base, "bg-info/15 text-info")}
        >
          <Loader2 className="h-3 w-3 animate-spin" />
          {t(($) => $.execution_log.status_running)}
        </Badge>
      );
    case "dispatched":
      return (
        <Badge
          data-testid="execution-log-status"
          variant="secondary"
          className={cn(base, "bg-info/15 text-info")}
        >
          {t(($) => $.execution_log.status_dispatched)}
        </Badge>
      );
    case "queued":
      return (
        <Badge
          data-testid="execution-log-status"
          variant="secondary"
          className={cn(base, "text-muted-foreground")}
        >
          {t(($) => $.execution_log.status_queued)}
        </Badge>
      );
    case "waiting_local_directory":
      return (
        <Badge
          data-testid="execution-log-status"
          variant="secondary"
          className={cn(base, "text-muted-foreground")}
        >
          {t(($) => $.execution_log.status_waiting_local_directory)}
        </Badge>
      );
    case "cancelled":
      return (
        <Badge
          data-testid="execution-log-status"
          variant="secondary"
          className={cn(base, "text-muted-foreground")}
        >
          {t(($) => $.execution_log.status_cancelled)}
        </Badge>
      );
    default:
      return (
        <Badge
          data-testid="execution-log-status"
          variant="secondary"
          className={cn(base, "text-muted-foreground capitalize")}
        >
          {status}
        </Badge>
      );
  }
}

function RunTriggerSource({ task }: { task: AgentTask }) {
  const { t } = useT("issues");
  let label: string;
  if (task.parent_task_id) {
    label = t(($) => $.execution_log.trigger_retry);
  } else if (task.kind === "comment" || task.trigger_comment_id) {
    label = t(($) => $.execution_log.trigger_comment);
  } else if (task.kind === "autopilot" || task.autopilot_run_id) {
    label = t(($) => $.execution_log.trigger_autopilot);
  } else if (task.kind === "chat" || task.chat_session_id) {
    label = t(($) => $.execution_log.trigger_chat);
  } else if (task.kind === "quick_create") {
    label = t(($) => $.execution_log.trigger_quick_create);
  } else if (task.kind === "direct" || task.handoff_note) {
    label = t(($) => $.execution_log.trigger_direct);
  } else {
    label = t(($) => $.execution_log.trigger_initial);
  }

  return (
    <span
      data-testid="execution-log-trigger-source"
      className="shrink-0 text-xs text-muted-foreground"
    >
      {label}
    </span>
  );
}

// ─── Run information row ─────────────────────────────────────────────────────

function RunDetailRow({
  icon,
  label,
  value,
  mono,
}: {
  icon?: React.ReactNode;
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="grid grid-cols-[5rem_minmax(0,1fr)] items-start gap-3">
      <span className="flex items-center gap-1 text-muted-foreground">
        {icon}
        {label}
      </span>
      <span
        className={cn(
          "min-w-0 select-text break-all text-left text-foreground/80",
          mono && "font-mono",
        )}
      >
        {value}
      </span>
    </div>
  );
}

function CopyableRunDetailRow({
  icon,
  label,
  value,
  copied,
  onCopy,
  title,
}: {
  icon?: React.ReactNode;
  label: string;
  value: string;
  copied: boolean;
  onCopy: () => void;
  title: string;
}) {
  return (
    <button
      type="button"
      onClick={onCopy}
      title={title}
      className="group -mx-1 grid w-[calc(100%+0.5rem)] grid-cols-[5rem_minmax(0,1fr)] items-start gap-3 rounded px-1 py-1 text-left transition-colors hover:bg-accent/60"
    >
      <span className="flex items-center gap-1 text-muted-foreground">
        {icon}
        {label}
      </span>
      <span className="flex min-w-0 items-start gap-1.5">
        <span className="min-w-0 flex-1 break-all font-mono text-foreground/80">
          {value}
        </span>
        {copied ? (
          <Check className="mt-0.5 h-3 w-3 shrink-0 text-success" />
        ) : (
          <Copy className="mt-0.5 h-3 w-3 shrink-0 opacity-0 transition-opacity group-hover:opacity-100" />
        )}
      </span>
    </button>
  );
}

// ─── Color timeline bar ──────────────────────────────────────────────────────

// One thin segment per LOADED message, in the current sort order, colored by
// kind. Represents ONLY the loaded window; there is no dimmed non-matching
// state — a filter change simply redraws for the matched+loaded set. Clicking a
// segment scrolls its row into view (data index === position in `messages`).
function ExecutionTimelineBar({
  messages,
  highlightedKey,
  onSegmentClick,
  ariaLabel,
}: {
  messages: TaskMessagePayload[];
  highlightedKey: string | null;
  onSegmentClick: (index: number) => void;
  ariaLabel: string;
}) {
  return (
    <div
      role="navigation"
      aria-label={ariaLabel}
      className="flex h-4 gap-px overflow-hidden rounded"
    >
      {messages.map((message, index) => {
        const color = execEventColor(message);
        const key = execEventKey(message);
        const active = highlightedKey === key;
        const label = `#${message.seq} ${EXEC_KIND_LABEL[color]}`;
        return (
          <button
            key={key}
            type="button"
            onClick={() => onSegmentClick(index)}
            title={label}
            aria-label={label}
            className={cn(
              "h-full min-w-0 flex-1 transition-opacity hover:opacity-80",
              active ? EXEC_COLOR_CLASSES[color].barActive : EXEC_COLOR_CLASSES[color].bar,
            )}
          />
        );
      })}
    </div>
  );
}

// ─── Toolbar state and facet controls ────────────────────────────────────────

function ExecutionLogToggleButton({
  pressed,
  onClick,
  children,
  className,
  ...props
}: {
  pressed: boolean;
  onClick: () => void;
  children: React.ReactNode;
  className?: string;
} & Omit<
  React.ComponentProps<typeof Button>,
  "aria-pressed" | "children" | "onClick" | "size" | "type" | "variant"
>) {
  return (
    <Button
      {...props}
      type="button"
      variant="ghost"
      size="sm"
      aria-pressed={pressed}
      onClick={onClick}
      className={cn(
        "h-7 gap-1 border px-2 text-xs",
        pressed
          ? "border-border bg-accent text-accent-foreground hover:bg-accent/80"
          : "border-border bg-transparent text-foreground hover:bg-muted/60",
        className,
      )}
    >
      {children}
    </Button>
  );
}

// ─── Skeleton ────────────────────────────────────────────────────────────────

function ExecutionLogToolbarSkeleton() {
  return (
    <div
      data-testid="execution-log-toolbar-skeleton"
      className="shrink-0 space-y-2 border-b px-4 py-2.5"
    >
      <div className="flex max-w-full items-center gap-3">
        <div className="flex min-w-0 items-center gap-3">
          <Skeleton className="h-6 w-20 rounded-full" />
          <Skeleton className="h-6 w-24 rounded-full" />
          <Skeleton className="h-3 w-24" />
          <Skeleton className="h-3 w-24" />
        </div>
        <div className="ml-auto flex items-center gap-1">
          <Skeleton className="h-7 w-24 rounded-md" />
          <Skeleton className="h-7 w-20 rounded-md" />
          <Skeleton className="h-7 w-7 rounded-md" />
        </div>
      </div>
      <div className="flex max-w-full items-center gap-1.5">
        <Skeleton className="mr-0.5 h-3 w-7" />
        <Skeleton className="h-7 w-16 rounded-md" />
        <Skeleton className="h-7 w-20 rounded-md" />
        <Skeleton className="h-7 w-16 rounded-md" />
        <Skeleton className="h-7 w-20 rounded-md" />
      </div>
    </div>
  );
}

function ExecutionLogTimelineSkeleton() {
  return (
    <div
      data-testid="execution-log-timeline-skeleton"
      className="shrink-0 space-y-2 border-b px-4 py-2.5"
    >
      <div className="flex items-center justify-between gap-4">
        <Skeleton className="h-3 w-40" />
        <Skeleton className="h-3 w-16" />
      </div>
      <Skeleton className="h-4 w-full rounded" />
      <div className="flex gap-3">
        <Skeleton className="h-3 w-14" />
        <Skeleton className="h-3 w-12" />
        <Skeleton className="h-3 w-16" />
      </div>
    </div>
  );
}

function ExecutionLogSkeleton() {
  const widths = [
    "w-2/3",
    "w-1/2",
    "w-3/4",
    "w-5/12",
    "w-7/12",
    "w-1/2",
    "w-4/5",
    "w-3/5",
  ];
  return (
    <div className="divide-y">
      {Array.from({ length: 8 }).map((_, i) => (
        <div
          key={i}
          data-testid="execution-log-row-skeleton"
          className="flex items-center gap-2 px-4 py-2.5"
        >
          <Skeleton className="h-5 w-[60px] shrink-0 rounded" />
          <Skeleton className="h-3 w-3 shrink-0" />
          <div className="min-w-0 flex-1">
            <Skeleton className={cn("h-3", widths[i])} />
          </div>
          <Skeleton className="h-3 w-8 shrink-0" />
          <Skeleton className="h-3 w-16 shrink-0" />
        </div>
      ))}
    </div>
  );
}

// ─── Event row ───────────────────────────────────────────────────────────────

// One row per loaded message. Rows are intentionally NOT coalesced: Virtuoso's
// firstItemIndex math requires row count to equal loaded message count. The row
// owns the established badge / summary / meta layout; the expanded detail owns
// the per-kind reading hierarchy.
function ExecutionLogRow({
  message,
  open,
  onToggle,
  highlighted,
}: {
  message: TaskMessagePayload;
  open: boolean;
  onToggle: () => void;
  highlighted?: boolean;
}) {
  const { t } = useT("issues");
  const [copied, setCopied] = useState(false);
  const copyTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (copyTimerRef.current) clearTimeout(copyTimerRef.current);
    },
    [],
  );

  const time = message.created_at
    ? new Date(message.created_at).toLocaleTimeString(undefined, {
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
      })
    : null;
  const kind = traceEventKind(message);
  const label = traceEventLabel(message);
  const color = execEventColor(message);
  const hasDetail = traceEventHasDetail(message);
  const summary = redactSecrets(traceEventSummary(message)).replace(/^#{1,6}\s+/, "");

  // Copy this single event's readable body (decoded result / input JSON / text),
  // redacted the same way it renders. Empty bodies get no copy affordance.
  const copyBody = useMemo(() => redactSecrets(traceEventCopyText(message)), [message]);
  const handleCopyRow = useCallback(() => {
    if (!copyBody) return;
    void copyText(copyBody).then((ok) => {
      if (!ok) return;
      setCopied(true);
      if (copyTimerRef.current) clearTimeout(copyTimerRef.current);
      copyTimerRef.current = setTimeout(() => setCopied(false), 1500);
    });
  }, [copyBody]);

  return (
    <div
      data-testid="execution-log-row"
      className={cn(
        "group/exec-row border-b px-4 py-2 transition-colors",
        highlighted && "bg-accent/60",
      )}
    >
      <div className="flex items-start gap-2">
        <span
          data-testid="execution-log-row-kind"
          title={label}
          className={cn(
            "mt-0.5 inline-flex min-w-[60px] max-w-28 shrink-0 items-center justify-center truncate rounded px-1.5 py-0.5 text-[11px] font-medium",
            EXEC_COLOR_CLASSES[color].label,
          )}
        >
          {kind === "thinking" && <Brain className="mr-1 h-3 w-3 shrink-0" />}
          {kind === "error" && <AlertCircle className="mr-1 h-3 w-3 shrink-0" />}
          <span className="truncate">{label}</span>
        </span>

        <div className="min-w-0 flex-1">
          <button
            type="button"
            onClick={onToggle}
            disabled={!hasDetail}
            className={cn(
              "flex w-full min-w-0 items-start gap-1.5 py-0.5 text-left text-xs transition-colors",
              hasDetail ? "cursor-pointer hover:text-foreground" : "cursor-default",
              kind === "error" ? "text-destructive" : "text-muted-foreground",
            )}
          >
            {hasDetail ? (
              <ChevronRight
                className={cn(
                  "mt-0.5 h-3 w-3 shrink-0 text-muted-foreground/50 transition-transform",
                  open && "rotate-90",
                )}
              />
            ) : (
              <span className="h-3 w-3 shrink-0" />
            )}
            <span className="truncate">{summary || "(empty)"}</span>
          </button>

          {open && hasDetail && (
            <div
              data-testid="execution-log-row-detail"
              className="mt-2 overflow-hidden rounded border bg-muted/30"
            >
              <ExecutionLogRowDetail message={message} />
            </div>
          )}
        </div>

        <div
          data-testid="execution-log-row-meta"
          className="mt-1 flex shrink-0 items-center gap-2 whitespace-nowrap text-[10px] tabular-nums text-muted-foreground/50"
        >
          {copyBody.length > 0 && (
            <button
              type="button"
              data-testid="execution-log-row-copy"
              onClick={handleCopyRow}
              aria-label={
                copied ? t(($) => $.execution_log.copied) : t(($) => $.execution_log.copy)
              }
              title={copied ? t(($) => $.execution_log.copied) : t(($) => $.execution_log.copy)}
              className={cn(
                "rounded p-0.5 opacity-0 transition-all hover:bg-accent hover:text-foreground",
                "focus-visible:opacity-100 group-hover/exec-row:opacity-100 [@media(hover:none)]:opacity-100",
              )}
            >
              {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
            </button>
          )}
          <span>#{message.seq}</span>
          {time && <span title={new Date(message.created_at!).toLocaleString()}>{time}</span>}
        </div>
      </div>
    </div>
  );
}

function ExecutionLogRowDetail({ message }: { message: TaskMessagePayload }) {
  const kind = traceEventKind(message);

  switch (kind) {
    case "agent": {
      const content = redactSecrets(message.content ?? "");
      return (
        <div className="px-3 py-2.5">
          <RichContent
            content={content || "(empty)"}
            density="compact"
            phase="settled"
            className={cn(
              "!text-[13px] text-foreground",
              "[&_h1]:!mb-1.5 [&_h1]:!mt-3 [&_h1]:!text-base",
              "[&_h2]:!mb-1.5 [&_h2]:!mt-3 [&_h2]:!text-sm",
              "[&_h3]:!mb-1 [&_h3]:!mt-2 [&_h3]:!text-[13px]",
              "[&_li]:!my-0.5 [&_li]:!leading-5 [&_p]:!my-1 [&_p]:!leading-5",
            )}
          />
        </div>
      );
    }

    case "error": {
      const content = redactSecrets(message.content ?? "");
      return (
        <p className="whitespace-pre-wrap px-3 py-2.5 text-xs leading-relaxed text-destructive">
          {content || "(empty)"}
        </p>
      );
    }

    case "thinking": {
      const full = redactSecrets(message.content ?? "");
      return (
        <p className="whitespace-pre-wrap px-3 py-2.5 text-xs italic leading-relaxed text-muted-foreground">
          {full || "(empty)"}
        </p>
      );
    }

    case "tool_use": {
      const inputJson = message.input
        ? redactSecrets(JSON.stringify(message.input, null, 2))
        : "";
      return (
        <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-words p-3 font-mono text-[11px] leading-relaxed text-muted-foreground">
          {inputJson || "(empty)"}
        </pre>
      );
    }

    case "tool_result": {
      // Decode defensively once: historical records may be double-JSON-encoded,
      // so render the decoded text (real newlines / pretty JSON), not the blob.
      const { text: decoded } = decodeToolResultOutput(message.output ?? "");
      const fullOutput =
        decoded.length > OUTPUT_DETAIL_CAP
          ? redactSecrets(decoded.slice(0, OUTPUT_DETAIL_CAP)) + "\n... (truncated)"
          : redactSecrets(decoded);
      return (
        <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-words p-3 font-mono text-[11px] leading-relaxed text-muted-foreground">
          {fullOutput || "(empty)"}
        </pre>
      );
    }

    default: {
      // Generic/unknown — label is the raw type; surface content, then output,
      // then input JSON (monospace) so nothing is silently dropped.
      const detail = redactSecrets(
        message.content ??
          message.output ??
          (message.input ? JSON.stringify(message.input, null, 2) : ""),
      );
      const mono = !message.content && !message.output && !!message.input;
      return mono ? (
        <pre className="max-h-72 overflow-auto whitespace-pre-wrap break-words p-3 font-mono text-[11px] leading-relaxed text-muted-foreground">
          {detail || "(empty)"}
        </pre>
      ) : (
        <p className="whitespace-pre-wrap px-3 py-2.5 text-xs leading-relaxed text-muted-foreground">
          {detail || "(empty)"}
        </p>
      );
    }
  }
}
