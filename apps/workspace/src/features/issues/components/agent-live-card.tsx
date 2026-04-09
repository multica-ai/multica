"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { Bot, ChevronRight, ChevronUp, Loader2, ArrowDown, Brain, AlertCircle, Clock, CheckCircle2, XCircle, Square } from "lucide-react";
import { api } from "@/shared/api";
import { useWSEvent } from "@/features/realtime";
import type { TaskMessagePayload, TaskCompletedPayload, TaskFailedPayload, TaskCancelledPayload } from "@/shared/types/events";
import type { AgentTask } from "@/shared/types/agent";
import { cn } from "@/lib/utils";
import { toast } from "sonner";
import { ActorAvatar } from "@/components/common/actor-avatar";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { useActorName } from "@/features/workspace";
import { redactSecrets } from "../utils/redact";

// ─── Shared types & helpers ─────────────────────────────────────────────────

/** A unified timeline entry: tool calls, thinking, text, and errors in chronological order. */
interface TimelineItem {
  seq: number;
  type: "tool_use" | "tool_result" | "thinking" | "text" | "error";
  tool?: string;
  content?: string;
  input?: Record<string, unknown>;
  output?: string;
}

function formatElapsed(startedAt: string): string {
  const elapsed = Date.now() - new Date(startedAt).getTime();
  const seconds = Math.floor(elapsed / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const secs = seconds % 60;
  return `${minutes}m ${secs}s`;
}

function formatDuration(start: string, end: string): string {
  const ms = new Date(end).getTime() - new Date(start).getTime();
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const secs = seconds % 60;
  return `${minutes}m ${secs}s`;
}

function shortenPath(p: string): string {
  const parts = p.split("/");
  if (parts.length <= 3) return p;
  return ".../" + parts.slice(-2).join("/");
}

function getToolSummary(item: TimelineItem): string {
  if (!item.input) return "";
  const inp = item.input as Record<string, string>;

  // WebSearch / web search
  if (inp.query) return inp.query;
  // File operations
  if (inp.file_path) return shortenPath(inp.file_path);
  if (inp.path) return shortenPath(inp.path);
  if (inp.pattern) return inp.pattern;
  // Bash
  if (inp.description) return String(inp.description);
  if (inp.command) {
    const cmd = String(inp.command);
    return cmd.length > 100 ? cmd.slice(0, 100) + "..." : cmd;
  }
  // Agent
  if (inp.prompt) {
    const p = String(inp.prompt);
    return p.length > 100 ? p.slice(0, 100) + "..." : p;
  }
  // Skill
  if (inp.skill) return String(inp.skill);
  // Fallback: show first string value
  for (const v of Object.values(inp)) {
    if (typeof v === "string" && v.length > 0 && v.length < 120) return v;
  }
  return "";
}

/** Build a chronologically ordered timeline from raw messages. */
function buildTimeline(msgs: TaskMessagePayload[]): TimelineItem[] {
  const items: TimelineItem[] = [];
  for (const msg of msgs) {
    items.push({
      seq: msg.seq,
      type: msg.type,
      tool: msg.tool,
      content: msg.content ? redactSecrets(msg.content) : msg.content,
      input: msg.input,
      output: msg.output ? redactSecrets(msg.output) : msg.output,
    });
  }
  return items.sort((a, b) => a.seq - b.seq);
}

// ─── AgentLiveCard (real-time view) ────────────────────────────────────────

interface AgentLiveCardProps {
  issueId: string;
  agentName?: string;
  /** Scroll container ref — needed for sticky sentinel detection. */
  scrollContainerRef?: React.RefObject<HTMLDivElement | null>;
}

export function AgentLiveCard({ issueId, agentName, scrollContainerRef }: AgentLiveCardProps) {
  const { getActorName } = useActorName();
  const [activeTask, setActiveTask] = useState<AgentTask | null>(null);
  const [items, setItems] = useState<TimelineItem[]>([]);
  const [elapsed, setElapsed] = useState("");
  const [autoScroll, setAutoScroll] = useState(true);
  const [cancelling, setCancelling] = useState(false);
  const [isStuck, setIsStuck] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const sentinelRef = useRef<HTMLDivElement>(null);
  const seenSeqs = useRef(new Set<string>());

  // Check for active task on mount
  useEffect(() => {
    let cancelled = false;
    api.getActiveTaskForIssue(issueId).then(({ task }) => {
      if (!cancelled) {
        setActiveTask(task);
        if (task) {
          api.listTaskMessages(task.id).then((msgs) => {
            if (!cancelled) {
              const timeline = buildTimeline(msgs);
              setItems(timeline);
              for (const m of msgs) seenSeqs.current.add(`${m.task_id}:${m.seq}`);
            }
          }).catch(console.error);
        }
      }
    }).catch(console.error);

    return () => { cancelled = true; };
  }, [issueId]);

  // Handle real-time task messages
  useWSEvent(
    "task:message",
    useCallback((payload: unknown) => {
      const msg = payload as TaskMessagePayload;
      if (msg.issue_id !== issueId) return;
      const key = `${msg.task_id}:${msg.seq}`;
      if (seenSeqs.current.has(key)) return;
      seenSeqs.current.add(key);

      setItems((prev) => {
        const item: TimelineItem = {
          seq: msg.seq,
          type: msg.type,
          tool: msg.tool,
          content: msg.content,
          input: msg.input,
          output: msg.output,
        };
        const next = [...prev, item];
        next.sort((a, b) => a.seq - b.seq);
        return next;
      });
    }, [issueId]),
  );

  // Handle task completion/failure
  useWSEvent(
    "task:completed",
    useCallback((payload: unknown) => {
      const p = payload as TaskCompletedPayload;
      if (p.issue_id !== issueId) return;
      setActiveTask(null);
      setItems([]);
      seenSeqs.current.clear();
      setCancelling(false);
    }, [issueId]),
  );

  useWSEvent(
    "task:failed",
    useCallback((payload: unknown) => {
      const p = payload as TaskFailedPayload;
      if (p.issue_id !== issueId) return;
      setActiveTask(null);
      setItems([]);
      seenSeqs.current.clear();
      setCancelling(false);
    }, [issueId]),
  );

  useWSEvent(
    "task:cancelled",
    useCallback((payload: unknown) => {
      const p = payload as TaskCancelledPayload;
      if (p.issue_id !== issueId) return;
      setActiveTask(null);
      setItems([]);
      seenSeqs.current.clear();
      setCancelling(false);
    }, [issueId]),
  );

  // Pick up new tasks — skip if we're already showing an active task to avoid
  // replacing its timeline mid-execution (per-issue serialization in the
  // backend prevents this race, but this is a defensive safeguard).
  useWSEvent(
    "task:dispatch",
    useCallback(() => {
      if (activeTask) return;
      api.getActiveTaskForIssue(issueId).then(({ task }) => {
        if (task) {
          setActiveTask(task);
          setItems([]);
          seenSeqs.current.clear();
        }
      }).catch(console.error);
    }, [issueId, activeTask]),
  );

  // Elapsed time
  useEffect(() => {
    if (!activeTask?.started_at && !activeTask?.dispatched_at) return;
    const startRef = activeTask.started_at ?? activeTask.dispatched_at!;
    setElapsed(formatElapsed(startRef));
    const interval = setInterval(() => setElapsed(formatElapsed(startRef)), 1000);
    return () => clearInterval(interval);
  }, [activeTask?.started_at, activeTask?.dispatched_at]);

  // Sentinel pattern: detect when the card is scrolled past and becomes "stuck"
  useEffect(() => {
    const sentinel = sentinelRef.current;
    const root = scrollContainerRef?.current;
    if (!sentinel || !root || !activeTask) {
      setIsStuck(false);
      return;
    }

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]) setIsStuck(!entries[0].isIntersecting);
      },
      { root, threshold: 0, rootMargin: "-40px 0px 0px 0px" },
    );

    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [scrollContainerRef, activeTask]);

  const scrollToCard = useCallback(() => {
    sentinelRef.current?.scrollIntoView({ behavior: "smooth", block: "center" });
  }, []);

  // Auto-scroll
  useEffect(() => {
    if (autoScroll && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [items, autoScroll]);

  const handleScroll = useCallback(() => {
    if (!scrollRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = scrollRef.current;
    setAutoScroll(scrollHeight - scrollTop - clientHeight < 40);
  }, []);

  const handleCancel = useCallback(async () => {
    if (!activeTask || cancelling) return;
    setCancelling(true);
    try {
      await api.cancelTask(issueId, activeTask.id);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to cancel task");
      setCancelling(false);
    }
  }, [activeTask, issueId, cancelling]);

  if (!activeTask) return null;

  const toolCount = items.filter((i) => i.type === "tool_use").length;
  const name = (activeTask.agent_id ? getActorName("agent", activeTask.agent_id) : agentName) ?? "Agent";

  return (
    <>
      {/* Sentinel — zero-height element that IntersectionObserver watches */}
      <div ref={sentinelRef} className="mt-4 h-0 pointer-events-none" aria-hidden />

      <div
        className={cn(
          "rounded-lg border transition-all duration-200",
          isStuck
            ? "sticky top-4 z-10 shadow-md border-brand/30 bg-brand/10 backdrop-blur-md"
            : "border-info/20 bg-info/5",
        )}
      >
        {/* Header */}
        <div className="flex items-center gap-2 px-3 py-2">
          {activeTask.agent_id ? (
            <ActorAvatar actorType="agent" actorId={activeTask.agent_id} size={20} />
          ) : (
            <div className={cn(
              "flex items-center justify-center h-5 w-5 rounded-full shrink-0",
              isStuck ? "bg-brand/15 text-brand" : "bg-info/10 text-info",
            )}>
              <Bot className="h-3 w-3" />
            </div>
          )}
          <div className="flex items-center gap-1.5 text-xs font-medium min-w-0">
            <Loader2 className={cn("h-3 w-3 animate-spin shrink-0", isStuck ? "text-brand" : "text-info")} />
            <span className="truncate">{name} is working</span>
          </div>
          <span className="ml-auto text-xs text-muted-foreground tabular-nums shrink-0">{elapsed}</span>
          {!isStuck && toolCount > 0 && (
            <span className="text-xs text-muted-foreground shrink-0">
              {toolCount} tool {toolCount === 1 ? "call" : "calls"}
            </span>
          )}
          {isStuck ? (
            <button
              onClick={scrollToCard}
              className="flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-muted-foreground hover:text-foreground transition-colors shrink-0"
              title="Scroll to live card"
            >
              <ChevronUp className="h-3.5 w-3.5" />
            </button>
          ) : (
            <button
              onClick={handleCancel}
              disabled={cancelling}
              className="flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors disabled:opacity-50 shrink-0"
              title="Stop agent"
            >
              {cancelling ? (
                <Loader2 className="h-3 w-3 animate-spin" />
              ) : (
                <Square className="h-3 w-3" />
              )}
              <span>Stop</span>
            </button>
          )}
        </div>

        {/* Timeline content — collapses when stuck */}
        <div
          className={cn(
            "overflow-hidden transition-all duration-200",
            isStuck ? "max-h-0 opacity-0" : "max-h-[20rem] opacity-100",
          )}
        >
          {items.length > 0 && (
            <div
              ref={scrollRef}
              onScroll={handleScroll}
              className="relative max-h-80 overflow-y-auto border-t border-info/10 px-3 py-2 space-y-0.5"
            >
              {items.map((item, idx) => (
                <TimelineRow key={`${item.seq}-${idx}`} item={item} />
              ))}

              {!autoScroll && (
                <button
                  onClick={() => {
                    if (scrollRef.current) {
                      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
                      setAutoScroll(true);
                    }
                  }}
                  className="sticky bottom-0 left-1/2 -translate-x-1/2 flex items-center gap-1 rounded-full bg-background border px-2 py-0.5 text-xs text-muted-foreground hover:text-foreground shadow-sm"
                >
                  <ArrowDown className="h-3 w-3" />
                  Latest
                </button>
              )}
            </div>
          )}
        </div>
      </div>
    </>
  );
}

// ─── TaskRunHistory (past execution logs) ──────────────────────────────────

interface TaskRunHistoryProps {
  issueId: string;
}

export function TaskRunHistory({ issueId }: TaskRunHistoryProps) {
  const [tasks, setTasks] = useState<AgentTask[]>([]);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    api.listTasksByIssue(issueId).then(setTasks).catch(console.error);
  }, [issueId]);

  // Refresh when a task completes
  useWSEvent(
    "task:completed",
    useCallback((payload: unknown) => {
      const p = payload as TaskCompletedPayload;
      if (p.issue_id !== issueId) return;
      api.listTasksByIssue(issueId).then(setTasks).catch(console.error);
    }, [issueId]),
  );

  useWSEvent(
    "task:failed",
    useCallback((payload: unknown) => {
      const p = payload as TaskFailedPayload;
      if (p.issue_id !== issueId) return;
      api.listTasksByIssue(issueId).then(setTasks).catch(console.error);
    }, [issueId]),
  );

  // Refresh when a task is cancelled
  useWSEvent(
    "task:cancelled",
    useCallback((payload: unknown) => {
      const p = payload as TaskCancelledPayload;
      if (p.issue_id !== issueId) return;
      api.listTasksByIssue(issueId).then(setTasks).catch(console.error);
    }, [issueId]),
  );

  const completedTasks = tasks.filter((t) => t.status === "completed" || t.status === "failed" || t.status === "cancelled");
  if (completedTasks.length === 0) return null;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors py-1">
        <ChevronRight className={cn("h-3 w-3 transition-transform", open && "rotate-90")} />
        <Clock className="h-3 w-3" />
        <span>Execution history ({completedTasks.length})</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="mt-1 space-y-2">
          {completedTasks.map((task) => (
            <TaskRunEntry key={task.id} task={task} />
          ))}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

function TaskRunEntry({ task }: { task: AgentTask }) {
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<TimelineItem[] | null>(null);

  const loadMessages = useCallback(() => {
    if (items !== null) return; // already loaded
    api.listTaskMessages(task.id).then((msgs) => {
      setItems(buildTimeline(msgs));
    }).catch((e) => {
      console.error(e);
      setItems([]);
    });
  }, [task.id, items]);

  useEffect(() => {
    if (open) loadMessages();
  }, [open, loadMessages]);

  const duration = task.started_at && task.completed_at
    ? formatDuration(task.started_at, task.completed_at)
    : null;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-xs hover:bg-accent/30 transition-colors border border-transparent hover:border-border">
        <ChevronRight className={cn("h-3 w-3 shrink-0 text-muted-foreground transition-transform", open && "rotate-90")} />
        {task.status === "completed" ? (
          <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-success" />
        ) : (
          <XCircle className="h-3.5 w-3.5 shrink-0 text-destructive" />
        )}
        <span className="text-muted-foreground">
          {new Date(task.created_at).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })}
        </span>
        {duration && <span className="text-muted-foreground">{duration}</span>}
        <span className={cn("ml-auto capitalize", task.status === "completed" ? "text-success" : "text-destructive")}>
          {task.status}
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="ml-5 mt-1 max-h-64 overflow-y-auto rounded border bg-muted/30 px-3 py-2 space-y-0.5">
          {items === null ? (
            <div className="flex items-center gap-2 text-xs text-muted-foreground py-2">
              <Loader2 className="h-3 w-3 animate-spin" />
              Loading...
            </div>
          ) : items.length === 0 ? (
            <p className="text-xs text-muted-foreground py-2">No execution data recorded.</p>
          ) : (
            items.map((item, idx) => (
              <TimelineRow key={`${item.seq}-${idx}`} item={item} />
            ))
          )}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

// ─── Shared timeline row rendering ──────────────────────────────────────────

function TimelineRow({ item }: { item: TimelineItem }) {
  switch (item.type) {
    case "tool_use":
      return <ToolCallRow item={item} />;
    case "tool_result":
      return <ToolResultRow item={item} />;
    case "thinking":
      return <ThinkingRow item={item} />;
    case "text":
      return <TextRow item={item} />;
    case "error":
      return <ErrorRow item={item} />;
    default:
      return null;
  }
}

function ToolCallRow({ item }: { item: TimelineItem }) {
  const [open, setOpen] = useState(false);
  const summary = getToolSummary(item);
  const hasInput = item.input && Object.keys(item.input).length > 0;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-center gap-1.5 rounded px-1 -mx-1 py-0.5 text-xs hover:bg-accent/30 transition-colors">
        <ChevronRight
          className={cn(
            "h-3 w-3 shrink-0 text-muted-foreground transition-transform",
            open && "rotate-90",
            !hasInput && "invisible",
          )}
        />
        <span className="font-medium text-foreground shrink-0">{item.tool}</span>
        {summary && <span className="truncate text-muted-foreground">{summary}</span>}
      </CollapsibleTrigger>
      {hasInput && (
        <CollapsibleContent>
          <pre className="ml-[18px] mt-0.5 max-h-32 overflow-auto rounded bg-muted/50 p-2 text-[11px] text-muted-foreground whitespace-pre-wrap break-all">
            {redactSecrets(JSON.stringify(item.input, null, 2))}
          </pre>
        </CollapsibleContent>
      )}
    </Collapsible>
  );
}

function ToolResultRow({ item }: { item: TimelineItem }) {
  const [open, setOpen] = useState(false);
  const output = item.output ?? "";
  if (!output) return null;

  const preview = output.length > 120 ? output.slice(0, 120) + "..." : output;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-start gap-1.5 rounded px-1 -mx-1 py-0.5 text-xs hover:bg-accent/30 transition-colors">
        <ChevronRight
          className={cn("h-3 w-3 shrink-0 text-muted-foreground transition-transform mt-0.5", open && "rotate-90")}
        />
        <span className="text-muted-foreground/70 truncate">
          {item.tool ? `${item.tool} result: ` : "result: "}{preview}
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre className="ml-[18px] mt-0.5 max-h-40 overflow-auto rounded bg-muted/50 p-2 text-[11px] text-muted-foreground whitespace-pre-wrap break-all">
          {output.length > 4000 ? output.slice(0, 4000) + "\n... (truncated)" : output}
        </pre>
      </CollapsibleContent>
    </Collapsible>
  );
}

function ThinkingRow({ item }: { item: TimelineItem }) {
  const [open, setOpen] = useState(false);
  const text = item.content ?? "";
  if (!text) return null;

  const preview = text.length > 150 ? text.slice(0, 150) + "..." : text;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-start gap-1.5 rounded px-1 -mx-1 py-0.5 text-xs hover:bg-accent/30 transition-colors">
        <Brain className="h-3 w-3 shrink-0 text-info/60 mt-0.5" />
        <span className="text-muted-foreground italic truncate">{preview}</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre className="ml-[18px] mt-0.5 max-h-40 overflow-auto rounded bg-info/5 p-2 text-[11px] text-muted-foreground whitespace-pre-wrap break-words">
          {text}
        </pre>
      </CollapsibleContent>
    </Collapsible>
  );
}

function TextRow({ item }: { item: TimelineItem }) {
  const text = item.content ?? "";
  if (!text.trim()) return null;
  const lines = text.trim().split("\n").filter(Boolean);
  const last = lines[lines.length - 1] ?? "";
  if (!last) return null;

  return (
    <div className="flex items-start gap-1.5 px-1 -mx-1 py-0.5 text-xs">
      <span className="h-3 w-3 shrink-0" />
      <span className="text-muted-foreground/60 truncate">{last}</span>
    </div>
  );
}

function ErrorRow({ item }: { item: TimelineItem }) {
  return (
    <div className="flex items-start gap-1.5 px-1 -mx-1 py-0.5 text-xs">
      <AlertCircle className="h-3 w-3 shrink-0 text-destructive mt-0.5" />
      <span className="text-destructive">{item.content}</span>
    </div>
  );
}
