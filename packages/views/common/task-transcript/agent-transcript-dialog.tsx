"use client";

import { useState, useRef, useCallback, useEffect, useMemo } from "react";
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
  Monitor,
  Cloud,
  Cpu,
  Filter,
  Folder,
  ArrowDownNarrowWide,
  ArrowUpNarrowWide,
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { copyText } from "@multica/ui/lib/clipboard";
import { Dialog, DialogContent, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@multica/ui/components/ui/collapsible";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuSeparator,
  DropdownMenuCheckboxItem,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { ActorAvatar } from "../actor-avatar";
import { api } from "@multica/core/api";
import {
  useTranscriptViewStore,
  type TranscriptFilterKey,
  type TranscriptSortDirection,
} from "@multica/core/agents/stores";
import type { AgentTask, Agent, AgentRuntime } from "@multica/core/types/agent";
import { redactSecrets } from "./redact";
import type { TimelineItem } from "./build-timeline";
import { useT } from "../../i18n";

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

function getEventLabel(item: TimelineItem): string {
  switch (item.type) {
    case "text":
      return "Agent";
    case "thinking":
      return "Thinking";
    case "tool_use":
      return item.tool ?? "Tool";
    case "tool_result":
      return item.tool ? `${item.tool}` : "Result";
    case "error":
      return "Error";
    default:
      return "Event";
  }
}

function getItemFilterKey(item: TimelineItem): TranscriptFilterKey {
  return item.tool && (item.type === "tool_use" || item.type === "tool_result")
    ? `tool:${item.tool}`
    : item.type;
}

function getEventSummary(item: TimelineItem): string {
  switch (item.type) {
    case "text":
      return item.content?.split("\n").find((l) => l.trim().length > 0) ?? "";
    case "thinking":
      return item.content?.slice(0, 200) ?? "";
    case "tool_use": {
      if (!item.input) return "";
      const inp = item.input as Record<string, string>;
      if (inp.query) return inp.query;
      if (inp.file_path) return shortenPath(inp.file_path);
      if (inp.path) return shortenPath(inp.path);
      if (inp.pattern) return inp.pattern;
      if (inp.description) return String(inp.description);
      if (inp.command) {
        const cmd = String(inp.command);
        return cmd.length > 120 ? cmd.slice(0, 120) + "..." : cmd;
      }
      if (inp.prompt) {
        const p = String(inp.prompt);
        return p.length > 120 ? p.slice(0, 120) + "..." : p;
      }
      if (inp.skill) return String(inp.skill);
      for (const v of Object.values(inp)) {
        if (typeof v === "string" && v.length > 0 && v.length < 120) return v;
      }
      return "";
    }
    case "tool_result":
      return item.output?.slice(0, 200) ?? "";
    case "error":
      return item.content ?? "";
    default:
      return "";
  }
}

function hasEventDetail(item: TimelineItem): boolean {
  return (
    (item.type === "tool_use" && !!item.input && Object.keys(item.input).length > 0) ||
    (item.type === "tool_result" && !!item.output && item.output.length > 0) ||
    (item.type === "thinking" && !!item.content && item.content.length > 0) ||
    (item.type === "text" && !!item.content && item.content.length > 0) ||
    (item.type === "error" && !!item.content && item.content.length > 0)
  );
}

function shortenPath(p: string): string {
  const parts = p.split("/");
  if (parts.length <= 3) return p;
  return ".../" + parts.slice(-2).join("/");
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

// ─── Main dialog ────────────────────────────────────────────────────────────

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
  const [sessionFilterKeys, setSessionFilterKeys] = useState<TranscriptFilterKey[]>([]);
  const [expandedSeqs, setExpandedSeqs] = useState<Set<number>>(() => new Set());
  const sortDirection = useTranscriptViewStore((s) => s.sortDirection);
  const setSortDirection = useTranscriptViewStore((s) => s.setSortDirection);
  const preserveFilters = useTranscriptViewStore((s) => s.preserveFilters);
  const setPreserveFilters = useTranscriptViewStore((s) => s.setPreserveFilters);
  const persistedFilterKeys = useTranscriptViewStore((s) => s.selectedFilterKeys);
  const setPersistedFilterKeys = useTranscriptViewStore((s) => s.setSelectedFilterKeys);
  const togglePersistedFilterKey = useTranscriptViewStore((s) => s.toggleFilterKey);
  const clearPersistedFilterKeys = useTranscriptViewStore((s) => s.clearFilterKeys);
  const defaultExpanded = useTranscriptViewStore((s) => s.defaultExpanded);
  const setDefaultExpanded = useTranscriptViewStore((s) => s.setDefaultExpanded);
  const eventRefs = useRef<Map<number, HTMLDivElement>>(new Map());
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const autoExpandedSeqsRef = useRef<Set<number>>(new Set());
  const initializedTaskRef = useRef<string | null>(null);
  const previousDefaultExpandedRef = useRef(defaultExpanded);
  const selectedFilterKeys = preserveFilters ? persistedFilterKeys : sessionFilterKeys;

  // Derive filter options from each item:
  //   tool_use / tool_result → filter value = tool, display = "tool:Bash"
  //   other types → display from getEventLabel
  const filterOptions = useMemo(() => {
    const options = new Map<string, string>();
    for (const item of items) {
      const key = getItemFilterKey(item);
      if (item.tool && (item.type === "tool_use" || item.type === "tool_result")) {
        if (!options.has(key)) options.set(key, key);
      } else {
        if (!options.has(key)) {
          options.set(key, getEventLabel(item));
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

  const detailSeqs = useMemo(
    () => displayItems.filter(hasEventDetail).map((item) => item.seq),
    [displayItems],
  );

  const allVisibleDetailsExpanded =
    detailSeqs.length > 0 && detailSeqs.every((seq) => expandedSeqs.has(seq));

  useEffect(() => {
    const switchedDefaultOn =
      defaultExpanded && previousDefaultExpandedRef.current !== defaultExpanded;
    previousDefaultExpandedRef.current = defaultExpanded;

    if (initializedTaskRef.current !== task.id || switchedDefaultOn) {
      initializedTaskRef.current = task.id;
      autoExpandedSeqsRef.current = new Set(defaultExpanded ? detailSeqs : []);
      setExpandedSeqs(defaultExpanded ? new Set(detailSeqs) : new Set());
      return;
    }

    if (!defaultExpanded) return;

    const unseen = detailSeqs.filter((seq) => !autoExpandedSeqsRef.current.has(seq));
    if (unseen.length === 0) return;

    for (const seq of unseen) {
      autoExpandedSeqsRef.current.add(seq);
    }
    setExpandedSeqs((prev) => new Set([...prev, ...unseen]));
  }, [task.id, defaultExpanded, detailSeqs]);

  // Toggling direction is a manual user action; jump the scroll container back
  // to the top so the newest end of the timeline (per the chosen direction) is
  // immediately visible. Avoids stranding the user mid-scroll on the wrong end.
  const handleSortDirectionChange = useCallback(
    (dir: typeof sortDirection) => {
      if (dir === sortDirection) return;
      setSortDirection(dir);
      scrollContainerRef.current?.scrollTo({ top: 0 });
    },
    [sortDirection, setSortDirection],
  );

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

  const handleSegmentClick = useCallback((seq: number) => {
    setSelectedSeq(seq);
    eventRefs.current.get(seq)?.scrollIntoView({ behavior: "smooth", block: "center" });
  }, []);

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
    const text = displayItems
      .map((item) => {
        const label = getEventLabel(item);
        const summary = getEventSummary(item);
        return `[${label}] ${summary}`;
      })
      .join("\n");
    void copyText(text).then((ok) => {
      if (!ok) return;
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }, [displayItems]);

  const toggleSessionFilterKey = useCallback((key: TranscriptFilterKey) => {
    setSessionFilterKeys((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return Array.from(next);
    });
  }, []);

  const clearFilters = useCallback(() => {
    if (preserveFilters) {
      clearPersistedFilterKeys();
      return;
    }
    setSessionFilterKeys([]);
  }, [clearPersistedFilterKeys, preserveFilters]);

  const toggleFilterKey = useCallback(
    (key: TranscriptFilterKey) => {
      if (preserveFilters) {
        togglePersistedFilterKey(key);
        return;
      }
      toggleSessionFilterKey(key);
    },
    [preserveFilters, togglePersistedFilterKey, toggleSessionFilterKey],
  );

  const handlePreserveFiltersChange = useCallback(
    (next: boolean) => {
      if (next) {
        setPersistedFilterKeys(sessionFilterKeys);
      } else {
        setSessionFilterKeys(persistedFilterKeys);
      }
      setPreserveFilters(next);
    },
    [persistedFilterKeys, sessionFilterKeys, setPersistedFilterKeys, setPreserveFilters],
  );

  const handleToggleVisibleExpanded = useCallback(() => {
    for (const seq of detailSeqs) {
      autoExpandedSeqsRef.current.add(seq);
    }
    setExpandedSeqs((prev) => {
      if (allVisibleDetailsExpanded) {
        const next = new Set(prev);
        for (const seq of detailSeqs) {
          next.delete(seq);
        }
        return next;
      }
      return new Set([...prev, ...detailSeqs]);
    });
  }, [allVisibleDetailsExpanded, detailSeqs]);

  const handleRowExpandedChange = useCallback((seq: number, expanded: boolean) => {
    autoExpandedSeqsRef.current.add(seq);
    setExpandedSeqs((prev) => {
      const next = new Set(prev);
      if (expanded) next.add(seq);
      else next.delete(seq);
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

  const toolCount = items.filter((i) => i.type === "tool_use").length;
  const copyTranscriptLabel = copied
    ? t(($) => $.transcript.copied)
    : activeFilterKeys.length > 0
      ? t(($) => $.transcript.copy_filtered)
      : t(($) => $.transcript.copy_all);

  // Status display
  const statusBadge = isLive ? (
    <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-info/15 px-2 py-0.5 text-xs font-medium text-info">
      <Loader2 className="h-3 w-3 animate-spin" />
      {t(($) => $.transcript.status_running)}
    </span>
  ) : task.status === "completed" ? (
    <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-success/15 px-2 py-0.5 text-xs font-medium text-success">
      <CheckCircle2 className="h-3 w-3" />
      {t(($) => $.transcript.status_completed)}
    </span>
  ) : task.status === "failed" ? (
    <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-destructive/15 px-2 py-0.5 text-xs font-medium text-destructive">
      <XCircle className="h-3 w-3" />
      {t(($) => $.transcript.status_failed)}
    </span>
  ) : (
    <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground capitalize">
      {task.status}
    </span>
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="!max-w-4xl !w-[calc(100vw-4rem)] !max-h-[calc(100vh-4rem)] !h-[calc(100vh-4rem)] flex flex-col !p-0 !gap-0 overflow-hidden"
        showCloseButton={false}
      >
        <DialogTitle className="sr-only">{t(($) => $.transcript.dialog_title)}</DialogTitle>

        {/* ── Header ─────────────────────────────────────────────── */}
        <div className="border-b px-4 py-3 shrink-0 space-y-2">
          {/* Top row: agent name, status, actions */}
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
            <div className="flex min-w-0 items-center gap-2">
              {task.agent_id ? (
                <ActorAvatar actorType="agent" actorId={task.agent_id} size={24} />
              ) : (
                <div className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-info/10 text-info">
                  <Bot className="h-3.5 w-3.5" />
                </div>
              )}
              <span className="truncate font-medium text-sm">{agentName}</span>
            </div>

            {statusBadge}

            <div className="flex w-full max-w-full flex-wrap items-center justify-end gap-1 sm:ml-auto sm:w-auto">
              {detailSeqs.length > 0 && (
                <button
                  type="button"
                  onClick={handleToggleVisibleExpanded}
                  aria-label={
                    allVisibleDetailsExpanded
                      ? t(($) => $.transcript.collapse_visible)
                      : t(($) => $.transcript.expand_visible)
                  }
                  className="flex shrink-0 items-center gap-1 rounded px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                >
                  <ChevronRight
                    className={cn(
                      "h-3 w-3 transition-transform",
                      !allVisibleDetailsExpanded && "rotate-90",
                    )}
                  />
                  <span className="hidden sm:inline">
                    {allVisibleDetailsExpanded
                      ? t(($) => $.transcript.collapse_visible)
                      : t(($) => $.transcript.expand_visible)}
                  </span>
                </button>
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
                    aria-label={t(($) => $.transcript.filter)}
                    className={cn(
                      "flex shrink-0 items-center gap-1 rounded px-2 py-1 text-xs transition-colors",
                      activeFilterKeys.length > 0
                        ? "text-blue-600 dark:text-blue-400 bg-blue-500/10 hover:bg-blue-500/20"
                        : "text-muted-foreground hover:text-foreground hover:bg-accent",
                    )}
                  >
                    <Filter className="h-3 w-3" />
                    <span className="hidden sm:inline">{t(($) => $.transcript.filter)}</span>
                    {activeFilterKeys.length > 0 && (
                      <span className="ml-0.5 rounded-full bg-blue-500/20 px-1.5 py-0 text-[10px] font-medium">
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
                    <DropdownMenuSeparator />
                    <DropdownMenuCheckboxItem
                      checked={preserveFilters}
                      onCheckedChange={(checked) => handlePreserveFiltersChange(checked === true)}
                    >
                      {t(($) => $.transcript.preserve_filters)}
                    </DropdownMenuCheckboxItem>
                    <DropdownMenuCheckboxItem
                      checked={defaultExpanded}
                      onCheckedChange={(checked) => setDefaultExpanded(checked === true)}
                    >
                      {t(($) => $.transcript.default_expanded)}
                    </DropdownMenuCheckboxItem>
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
              <button
                type="button"
                onClick={handleCopyAll}
                aria-label={copyTranscriptLabel}
                className="flex shrink-0 items-center gap-1 rounded px-2 py-1 text-xs text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
              >
                {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
                <span className="hidden sm:inline">{copyTranscriptLabel}</span>
              </button>
              <button
                type="button"
                onClick={() => onOpenChange(false)}
                className="flex shrink-0 items-center justify-center rounded p-1 text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
          </div>

          {/* Metadata chips row */}
          <div className="flex items-center gap-2 flex-wrap text-xs">
            {/* Runtime provider */}
            {runtimeInfo?.provider && (
              <MetadataChip icon={<Cpu className="h-3 w-3" />}>
                {formatProvider(runtimeInfo.provider)}
              </MetadataChip>
            )}

            {/* Runtime environment */}
            {runtimeInfo && (
              <MetadataChip
                icon={runtimeInfo.runtime_mode === "cloud" ? <Cloud className="h-3 w-3" /> : <Monitor className="h-3 w-3" />}
              >
                {runtimeInfo.name}
                <span className="text-muted-foreground/60 ml-0.5">({runtimeInfo.runtime_mode})</span>
              </MetadataChip>
            )}

            {/* Agent type / description */}
            {agentInfo?.description && (
              <MetadataChip icon={<Bot className="h-3 w-3" />}>
                {agentInfo.description.length > 40 ? agentInfo.description.slice(0, 40) + "..." : agentInfo.description}
              </MetadataChip>
            )}

            {/* Duration */}
            {duration && (
              <MetadataChip icon={<Clock className="h-3 w-3" />}>
                {duration}
              </MetadataChip>
            )}

            {/* Event counts */}
            {toolCount > 0 && (
              <MetadataChip>{t(($) => $.transcript.tool_calls, { count: toolCount })}</MetadataChip>
            )}
            <MetadataChip>
              {activeFilterKeys.length > 0
                ? t(($) => $.transcript.events_filtered, { shown: filteredItems.length, total: items.length })
                : t(($) => $.transcript.events, { count: items.length })}
            </MetadataChip>

            {/* Working directory — server-derived display path. Falls back to
                nothing when older backends omit the field rather than rendering
                `work_dir` raw and leaking the user's home directory. The
                absolute `task.work_dir` deliberately never reaches the DOM
                anywhere — only `relative_work_dir` is safe to render / put in
                title / copy to clipboard, because the server has already
                stripped $HOME and the username out of it. The button
                truncates because real workdir paths are routinely long
                enough to push every other chip off the row. */}
            {task.relative_work_dir && (
              <button
                type="button"
                onClick={handleCopyWorkdir}
                title={task.relative_work_dir}
                className="inline-flex max-w-[16rem] items-center gap-1 rounded-md border bg-muted/50 px-2 py-0.5 text-[11px] text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              >
                {copiedWorkdir ? (
                  <Check className="h-3 w-3 shrink-0 text-emerald-500" />
                ) : (
                  <Folder className="h-3 w-3 shrink-0" />
                )}
                <span className="truncate font-mono">{task.relative_work_dir}</span>
              </button>
            )}

            {/* Created time */}
            {task.created_at && (
              <MetadataChip>
                {new Date(task.created_at).toLocaleString(undefined, {
                  month: "short",
                  day: "numeric",
                  hour: "2-digit",
                  minute: "2-digit",
                })}
              </MetadataChip>
            )}
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
        <div
          ref={scrollContainerRef}
          className="flex-1 overflow-y-auto min-h-0"
        >
          {displayItems.length === 0 ? (
            <div className="flex items-center justify-center h-full text-sm text-muted-foreground">
              {isLive ? (
                <div className="flex items-center gap-2">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  {t(($) => $.transcript.waiting_events)}
                </div>
              ) : (
                t(($) => $.transcript.no_data)
              )}
            </div>
          ) : (
            <div className="divide-y">
              {displayItems.map((item) => (
                <TranscriptEventRow
                  key={item.seq}
                  ref={(el) => {
                    if (el) eventRefs.current.set(item.seq, el);
                    else eventRefs.current.delete(item.seq);
                  }}
                  item={item}
                  isSelected={selectedSeq === item.seq}
                  expanded={expandedSeqs.has(item.seq)}
                  onExpandedChange={(expanded) => handleRowExpandedChange(item.seq, expanded)}
                />
              ))}
            </div>
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

function SortDirectionToggle({ value, onChange, labels }: SortDirectionToggleProps) {
  return (
    <div
      role="group"
      aria-label={labels.ariaLabel}
      className="inline-flex shrink-0 items-center rounded border bg-muted/40 p-0.5 text-xs"
    >
      <button
        type="button"
        aria-pressed={value === "chronological"}
        title={labels.chronological}
        onClick={() => onChange("chronological")}
        className={cn(
          "flex items-center gap-1 rounded px-1.5 py-0.5 transition-colors",
          value === "chronological"
            ? "bg-background text-foreground shadow-sm"
            : "text-muted-foreground hover:text-foreground",
        )}
      >
        <ArrowDownNarrowWide className="h-3 w-3" />
        <span className="hidden sm:inline">{labels.chronological}</span>
      </button>
      <button
        type="button"
        aria-pressed={value === "newest_first"}
        title={labels.newestFirst}
        onClick={() => onChange("newest_first")}
        className={cn(
          "flex items-center gap-1 rounded px-1.5 py-0.5 transition-colors",
          value === "newest_first"
            ? "bg-background text-foreground shadow-sm"
            : "text-muted-foreground hover:text-foreground",
        )}
      >
        <ArrowUpNarrowWide className="h-3 w-3" />
        <span className="hidden sm:inline">{labels.newestFirst}</span>
      </button>
    </div>
  );
}

// ─── Metadata chip ──────────────────────────────────────────────────────────

function MetadataChip({ icon, children }: { icon?: React.ReactNode; children: React.ReactNode }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-md border bg-muted/50 px-2 py-0.5 text-[11px] text-muted-foreground">
      {icon}
      {children}
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
            title={`${getEventLabel(items[seg.startIdx]!)}${seg.count > 1 ? ` (+${seg.count - 1} more)` : ""}`}
          >
            <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-1 hidden group-hover:block z-10 pointer-events-none">
              <div className="rounded bg-popover border px-2 py-1 text-[10px] text-popover-foreground shadow-md whitespace-nowrap">
                {getEventLabel(items[seg.startIdx]!)}
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
  ref,
  item,
  isSelected,
  expanded,
  onExpandedChange,
}: TranscriptEventRowProps & { ref?: React.Ref<HTMLDivElement> }) => {
  const color = getEventColor(item);
  const label = getEventLabel(item);
  const summary = getEventSummary(item);
  const date = useMemo(
    () => (item.created_at ? new Date(item.created_at) : null),
    [item.created_at],
  );

  const hasDetail = hasEventDetail(item);

  return (
    <div
      ref={ref}
      className={cn(
        "group transition-colors",
        isSelected && "bg-accent/50",
      )}
    >
      <Collapsible open={expanded} onOpenChange={onExpandedChange}>
        <div className="flex items-start gap-2 px-4 py-2">
          {/* Type label badge */}
          <span
            className={cn(
              "inline-flex items-center shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium mt-0.5 min-w-[60px] justify-center",
              colorClasses[color].label,
            )}
          >
            {item.type === "thinking" && <Brain className="h-3 w-3 mr-1 shrink-0" />}
            {item.type === "error" && <AlertCircle className="h-3 w-3 mr-1 shrink-0" />}
            {label}
          </span>

          {/* Summary */}
          <CollapsibleTrigger
            className={cn(
              "flex-1 text-left text-xs min-w-0 py-0.5 transition-colors",
              hasDetail ? "cursor-pointer hover:text-foreground" : "cursor-default",
              item.type === "error" ? "text-destructive" : "text-muted-foreground",
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
              <span className="truncate">{summary || "(empty)"}</span>
            </div>
          </CollapsibleTrigger>

          {/* Seq number / index */}
          <span className="shrink-0 text-[10px] text-muted-foreground/50 tabular-nums mt-1">
            #{item.seq}
          </span>

          {/* Timestamp */}
          {date && (
            <span className="shrink-0 text-[10px] text-muted-foreground/50 tabular-nums mt-1" title={date.toLocaleString()}>
              {date.toLocaleTimeString(undefined, {
                hour: "2-digit",
                minute: "2-digit",
                second: "2-digit",
              })}
            </span>
          )}
        </div>

        {/* Expanded detail */}
        {hasDetail && (
          <CollapsibleContent>
            <div className="px-4 pb-3">
              <div className="ml-[72px] rounded bg-muted/40 border">
                <EventDetailContent item={item} />
              </div>
            </div>
          </CollapsibleContent>
        )}
      </Collapsible>
    </div>
  );
};

// ─── Event detail content ───────────────────────────────────────────────────

function EventDetailContent({ item }: { item: TimelineItem }) {
  switch (item.type) {
    case "tool_use":
      return (
        <pre className="max-h-60 overflow-auto p-3 text-[11px] text-muted-foreground whitespace-pre-wrap break-all">
          {item.input ? redactSecrets(JSON.stringify(item.input, null, 2)) : ""}
        </pre>
      );
    case "tool_result":
      return (
        <pre className="max-h-60 overflow-auto p-3 text-[11px] text-muted-foreground whitespace-pre-wrap break-all">
          {item.output
            ? item.output.length > 4000
              ? redactSecrets(item.output.slice(0, 4000)) + "\n... (truncated)"
              : redactSecrets(item.output)
            : ""}
        </pre>
      );
    case "thinking":
      return (
        <pre className="max-h-60 overflow-auto p-3 text-[11px] text-muted-foreground whitespace-pre-wrap break-words">
          {item.content ?? ""}
        </pre>
      );
    case "text":
      return (
        <pre className="max-h-60 overflow-auto p-3 text-[11px] text-muted-foreground whitespace-pre-wrap break-words">
          {item.content ?? ""}
        </pre>
      );
    case "error":
      return (
        <pre className="max-h-60 overflow-auto p-3 text-[11px] text-destructive whitespace-pre-wrap break-words">
          {item.content ?? ""}
        </pre>
      );
    default:
      return null;
  }
}
