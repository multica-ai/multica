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
} from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
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
import { useT } from "@multica/i18n/react";
import type { AgentTask, Agent, AgentRuntime } from "@multica/core/types/agent";
import { redactSecrets } from "./redact";
import type { TimelineItem } from "./build-timeline";

interface AgentTranscriptDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  task: AgentTask;
  items: TimelineItem[];
  agentName: string;
  isLive?: boolean;
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

function getEventLabel(item: TimelineItem, t: (key: string) => string): string {
  switch (item.type) {
    case "text":
      return t("agent");
    case "thinking":
      return t("thinking");
    case "tool_use":
      return item.tool ?? t("tool");
    case "tool_result":
      return item.tool ? `${item.tool}` : t("result");
    case "error":
      return t("error");
    default:
      return t("event");
  }
}

function getEventSummary(item: TimelineItem): string {
  switch (item.type) {
    case "text":
      return item.content?.split("\n").filter(Boolean).pop() ?? "";
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
}: AgentTranscriptDialogProps) {
  const t = useT("transcript");
  const [selectedSeq, setSelectedSeq] = useState<number | null>(null);
  const [elapsed, setElapsed] = useState("");
  const [copied, setCopied] = useState(false);
  const [agentInfo, setAgentInfo] = useState<Agent | null>(null);
  const [runtimeInfo, setRuntimeInfo] = useState<AgentRuntime | null>(null);
  const [selectedTools, setSelectedTools] = useState<Set<string>>(new Set());
  const eventRefs = useRef<Map<number, HTMLDivElement>>(new Map());
  const scrollContainerRef = useRef<HTMLDivElement>(null);

  // Derive filter options from each item:
  //   tool_use / tool_result → filter value = tool, display = "tool:Bash"
  //   other types → display from getEventLabel
  const filterOptions = useMemo(() => {
    const options = new Map<string, string>();
    for (const item of items) {
      if (item.tool && (item.type === "tool_use" || item.type === "tool_result")) {
        const key = `tool:${item.tool}`;
        if (!options.has(key)) options.set(key, key);
      } else {
        const value = item.type;
        if (!options.has(value)) {
          options.set(value, getEventLabel(item, t));
        }
      }
    }
    return Array.from(options.entries()).sort((a, b) => a[1].localeCompare(b[1]));
  }, [items]);

  // Resolve filter key for each item — mirrors filterOptions derivation exactly
  const itemFilterKey = (item: TimelineItem) =>
    item.tool && (item.type === "tool_use" || item.type === "tool_result")
      ? `tool:${item.tool}`
      : item.type;

  // Strict filter
  const filteredItems = useMemo(() => {
    if (selectedTools.size === 0) return items;
    return items.filter((item) => selectedTools.has(itemFilterKey(item)));
  }, [items, selectedTools]);

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

  // Copy all events as text (uses filtered items)
  const handleCopyAll = useCallback(() => {
    const text = filteredItems
      .map((item) => {
        const label = getEventLabel(item, t);
        const summary = getEventSummary(item);
        return `[${label}] ${summary}`;
      })
      .join("\n");
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }, [filteredItems, t]);

  // Toggle tool filter
  const toggleTool = useCallback((tool: string) => {
    setSelectedTools((prev) => {
      const next = new Set(prev);
      if (next.has(tool)) next.delete(tool);
      else next.add(tool);
      return next;
    });
  }, []);

  const clearFilters = useCallback(() => {
    setSelectedTools(new Set());
  }, []);

  // Duration
  const duration =
    task.started_at && task.completed_at
      ? formatDuration(task.started_at, task.completed_at)
      : isLive
        ? elapsed
        : null;

  const toolCount = items.filter((i) => i.type === "tool_use").length;

  // Status display
  const statusBadge = isLive ? (
    <span className="inline-flex items-center gap-1 rounded-full bg-info/15 px-2 py-0.5 text-xs font-medium text-info">
      <Loader2 className="h-3 w-3 animate-spin" />
      {t("running")}
    </span>
  ) : task.status === "completed" ? (
    <span className="inline-flex items-center gap-1 rounded-full bg-success/15 px-2 py-0.5 text-xs font-medium text-success">
      <CheckCircle2 className="h-3 w-3" />
      {t("completed")}
    </span>
  ) : task.status === "failed" ? (
    <span className="inline-flex items-center gap-1 rounded-full bg-destructive/15 px-2 py-0.5 text-xs font-medium text-destructive">
      <XCircle className="h-3 w-3" />
      {t("failed")}
    </span>
  ) : (
    <span className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground capitalize">
      {task.status}
    </span>
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="!max-w-4xl !w-[calc(100vw-4rem)] !max-h-[calc(100vh-4rem)] !h-[calc(100vh-4rem)] flex flex-col !p-0 !gap-0 overflow-hidden"
        showCloseButton={false}
      >
        <DialogTitle className="sr-only">{t("dialog_title")}</DialogTitle>

        {/* ── Header ─────────────────────────────────────────────── */}
        <div className="border-b px-4 py-3 shrink-0 space-y-2">
          {/* Top row: agent name, status, actions */}
          <div className="flex items-center gap-3">
            <div className="flex items-center gap-2">
              {task.agent_id ? (
                <ActorAvatar actorType="agent" actorId={task.agent_id} size={24} />
              ) : (
                <div className="flex items-center justify-center h-6 w-6 rounded-full bg-info/10 text-info">
                  <Bot className="h-3.5 w-3.5" />
                </div>
              )}
              <span className="font-medium text-sm">{agentName}</span>
            </div>

            {statusBadge}

            <div className="ml-auto flex items-center gap-1">
              {filterOptions.length > 0 && (
                <DropdownMenu>
                  <DropdownMenuTrigger
                    className={cn(
                      "flex items-center gap-1 rounded px-2 py-1 text-xs transition-colors",
                      selectedTools.size > 0
                        ? "text-blue-600 dark:text-blue-400 bg-blue-500/10 hover:bg-blue-500/20"
                        : "text-muted-foreground hover:text-foreground hover:bg-accent",
                    )}
                  >
                    <Filter className="h-3 w-3" />
                    {t("filter")}
                    {selectedTools.size > 0 && (
                      <span className="ml-0.5 rounded-full bg-blue-500/20 px-1.5 py-0 text-[10px] font-medium">
                        {selectedTools.size}
                      </span>
                    )}
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="w-auto">
                    {filterOptions.map(([value, label]) => (
                      <DropdownMenuCheckboxItem
                        key={value}
                        checked={selectedTools.has(value)}
                        onCheckedChange={() => toggleTool(value)}
                      >
                        {label}
                      </DropdownMenuCheckboxItem>
                    ))}
                    {selectedTools.size > 0 && (
                      <>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem onClick={clearFilters} className="text-muted-foreground">
                          {t("clear_filters")}
                        </DropdownMenuItem>
                      </>
                    )}
                  </DropdownMenuContent>
                </DropdownMenu>
              )}
              <button
                onClick={handleCopyAll}
                className="flex items-center gap-1 rounded px-2 py-1 text-xs text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
              >
                {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
                {copied ? t("copied") : selectedTools.size > 0 ? t("copy_filtered") : t("copy_all")}
              </button>
              <button
                onClick={() => onOpenChange(false)}
                className="flex items-center justify-center rounded p-1 text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
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
              <MetadataChip>{t("tool_calls", { count: toolCount })}</MetadataChip>
            )}
            <MetadataChip>
              {selectedTools.size > 0 ? t("events_filtered", { filtered: String(filteredItems.length), total: String(items.length) }) : t("events", { count: items.length })}
            </MetadataChip>

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
        {filteredItems.length > 0 && (
          <div className="border-b px-4 py-2.5 shrink-0">
            <TimelineBar
              items={filteredItems}
              selectedSeq={selectedSeq}
              onSegmentClick={handleSegmentClick}
              t={t}
            />
          </div>
        )}

        {/* ── Event list ─────────────────────────────────────────── */}
        <div
          ref={scrollContainerRef}
          className="flex-1 overflow-y-auto min-h-0"
        >
          {filteredItems.length === 0 ? (
            <div className="flex items-center justify-center h-full text-sm text-muted-foreground">
              {isLive ? (
                <div className="flex items-center gap-2">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  {t("waiting_for_events")}
                </div>
              ) : (
                t("no_data")
              )}
            </div>
          ) : (
            <div className="divide-y">
              {filteredItems.map((item) => (
                <TranscriptEventRow
                  key={item.seq}
                  ref={(el) => {
                    if (el) eventRefs.current.set(item.seq, el);
                    else eventRefs.current.delete(item.seq);
                  }}
                  item={item}
                  isSelected={selectedSeq === item.seq}
                />
              ))}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
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
  t,
}: {
  items: TimelineItem[];
  selectedSeq: number | null;
  onSegmentClick: (seq: number) => void;
  t: (key: string, params?: Record<string, string | number>) => string;
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
    <div className="flex gap-0.5 h-5 rounded overflow-hidden" role="navigation" aria-label={t("aria_timeline")}>
      {segments.map((seg) => {
        const isSelected = selectedSeq !== null && items.slice(seg.startIdx, seg.endIdx + 1).some((i) => i.seq === selectedSeq);
        const color = colorClasses[seg.color];
        const widthPercent = (seg.count / items.length) * 100;

        return (
          <button
            key={seg.startIdx}
            className={cn(
              "h-full transition-all duration-150 hover:opacity-80 relative group",
              isSelected ? color.bgActive : color.bg,
              "min-w-[4px]",
            )}
            style={{ width: `${Math.max(widthPercent, 0.5)}%` }}
            onClick={() => onSegmentClick(items[seg.startIdx]!.seq)}
            title={`${getEventLabel(items[seg.startIdx]!, t)}${seg.count > 1 ? ` ${t("segment_more", { count: seg.count - 1 })}` : ""}`}
          >
            <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-1 hidden group-hover:block z-10 pointer-events-none">
              <div className="rounded bg-popover border px-2 py-1 text-[10px] text-popover-foreground shadow-md whitespace-nowrap">
                {getEventLabel(items[seg.startIdx]!, t)}
                {seg.count > 1 && <span className="text-muted-foreground ml-1">{t("more_count", { count: seg.count - 1 })}</span>}
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
}

const TranscriptEventRow = ({
  ref,
  item,
  isSelected,
}: TranscriptEventRowProps & { ref?: React.Ref<HTMLDivElement> }) => {
  const t = useT("transcript");
  const [expanded, setExpanded] = useState(false);
  const color = getEventColor(item);
  const label = getEventLabel(item, t);
  const summary = getEventSummary(item);

  const hasDetail =
    (item.type === "tool_use" && item.input && Object.keys(item.input).length > 0) ||
    (item.type === "tool_result" && item.output && item.output.length > 0) ||
    (item.type === "thinking" && item.content && item.content.length > 0) ||
    (item.type === "text" && item.content && item.content.split("\n").length > 1) ||
    (item.type === "error" && item.content && item.content.length > 0);

  return (
    <div
      ref={ref}
      className={cn(
        "group transition-colors",
        isSelected && "bg-accent/50",
      )}
    >
      <Collapsible open={expanded} onOpenChange={setExpanded}>
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
              <span className="truncate">{summary || t("empty")}</span>
            </div>
          </CollapsibleTrigger>

          {/* Seq number / index */}
          <span className="shrink-0 text-[10px] text-muted-foreground/50 tabular-nums mt-1">
            #{item.seq}
          </span>
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
  const t = useT("transcript");
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
              ? redactSecrets(item.output.slice(0, 4000)) + "\n" + t("truncated")
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
