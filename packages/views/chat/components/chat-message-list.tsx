"use client";

import { useState, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import { Loader2, ChevronRight, ChevronDown, Brain, AlertCircle, GitBranch, HelpCircle } from "lucide-react";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { useAutoScroll } from "@multica/ui/hooks/use-auto-scroll";
import { taskMessagesOptions } from "@multica/core/chat/queries";
import { Markdown } from "@multica/views/common/markdown";
import { api } from "@multica/core/api";
import { createLogger } from "@multica/core/logger";
import type { ChatMessage, TaskMessagePayload } from "@multica/core/types";
import type { ChatTimelineItem } from "@multica/core/chat";

const logger = createLogger("chat.repo-plan");

// ─── Public component ────────────────────────────────────────────────────

interface ChatMessageListProps {
  messages: ChatMessage[];
  /** When set, streams the live timeline for this task from task-messages cache. */
  pendingTaskId: string | null;
  isWaiting: boolean;
}

export function ChatMessageList({
  messages,
  pendingTaskId,
  isWaiting,
}: ChatMessageListProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);
  useAutoScroll(scrollRef);

  // Once the assistant message for this pending task has landed in the
  // messages list, AssistantMessage owns its rendering — suppress the live
  // timeline to avoid rendering the same content in two places during the
  // invalidate → refetch window.
  const pendingAlreadyPersisted = !!pendingTaskId && messages.some(
    (m) => m.role === "assistant" && m.task_id === pendingTaskId,
  );

  // Live timeline for the in-flight task. useRealtimeSync keeps this cache
  // current via setQueryData on task:message events.
  const showLiveTimeline = !!pendingTaskId && !pendingAlreadyPersisted;
  const { data: liveTaskMessages } = useQuery({
    ...taskMessagesOptions(pendingTaskId ?? ""),
    enabled: showLiveTimeline,
  });
  const liveTimeline: ChatTimelineItem[] = (liveTaskMessages ?? []).map(toTimelineItem);
  const hasLive = showLiveTimeline && liveTimeline.length > 0;

  return (
    <div ref={scrollRef} style={fadeStyle} className="flex-1 overflow-y-auto">
      {/* Inner container matches issue / project detail width convention
       *  (max-w-4xl + mx-auto) so switching between chat and content
       *  views doesn't jolt the reading width. px-5 is a touch tighter
       *  than issue-detail's px-8 because the chat window can be narrow. */}
      <div className="mx-auto w-full max-w-4xl px-5 py-4 space-y-4">
        {messages.map((msg) => (
          <MessageBubble key={msg.id} message={msg} />
        ))}
        {hasLive && (
          <div className="w-full space-y-1.5">
            <TimelineView items={liveTimeline} taskId={pendingTaskId ?? undefined} />
          </div>
        )}
        {isWaiting && !hasLive && !pendingAlreadyPersisted && (
          <Loader2 className="size-4 animate-spin text-muted-foreground" />
        )}
      </div>
    </div>
  );
}

/**
 * Placeholder shown while `chat_message` for a session is being fetched
 * (initial refresh, or switching to an un-cached session). Shape roughly
 * mirrors an assistant → user → assistant exchange so the window doesn't
 * shift under the user when real messages arrive.
 */
export function ChatMessageSkeleton() {
  return (
    <div className="flex-1 overflow-hidden">
      <div className="mx-auto w-full max-w-4xl px-5 py-4 space-y-5">
        <div className="space-y-2">
          <Skeleton className="h-3.5 w-3/4" />
          <Skeleton className="h-3.5 w-1/2" />
        </div>
        <div className="flex justify-end">
          <Skeleton className="h-8 w-48 rounded-2xl" />
        </div>
        <div className="space-y-2">
          <Skeleton className="h-3.5 w-2/3" />
          <Skeleton className="h-3.5 w-5/6" />
          <Skeleton className="h-3.5 w-1/3" />
        </div>
      </div>
    </div>
  );
}

function toTimelineItem(m: TaskMessagePayload): ChatTimelineItem {
  return {
    seq: m.seq,
    type: m.type,
    tool: m.tool,
    content: m.content,
    input: m.input,
    output: m.output,
  };
}

// ─── Message bubbles ─────────────────────────────────────────────────────

function MessageBubble({ message }: { message: ChatMessage }) {
  if (message.role === "user") {
    return (
      <div className="flex justify-end">
        <div className="rounded-2xl bg-muted px-3.5 py-2 text-sm max-w-[80%] break-words">
          {/* User messages are authored as markdown in ContentEditor, so
           * render them through the same pipeline as assistant replies.
           * Neutralise prose's leading/trailing margin so single-line
           * bubbles stay as compact as the plain-text version used to. */}
          <div className="prose prose-sm dark:prose-invert max-w-none [&>*:first-child]:mt-0 [&>*:last-child]:mb-0">
            <Markdown>{message.content}</Markdown>
          </div>
        </div>
      </div>
    );
  }

  return <AssistantMessage message={message} />;
}

function AssistantMessage({
  message,
}: {
  message: ChatMessage;
}) {
  const taskId = message.task_id;

  // Use the shared taskMessagesOptions so this cache entry is the same one
  // seeded by useRealtimeSync during task execution — zero refetch when the
  // task finishes, since WS already populated it.
  const { data: taskMessages } = useQuery({
    ...taskMessagesOptions(taskId ?? ""),
    enabled: !!taskId,
  });

  const timeline: ChatTimelineItem[] = (taskMessages ?? []).map(toTimelineItem);

  return (
    <div className="w-full space-y-1.5">
      {timeline.length > 0 ? (
        <TimelineView items={timeline} taskId={taskId ?? undefined} />
      ) : (
        <div className="text-sm leading-relaxed prose prose-sm dark:prose-invert max-w-none">
          <Markdown>{message.content}</Markdown>
        </div>
      )}
    </div>
  );
}

// ─── Timeline: flat interleaved text + collapsible tool groups ───────────

interface TimelineSegment {
  kind: "text" | "tools" | "plan";
  items: ChatTimelineItem[];
}

function isPlanType(t: ChatTimelineItem["type"]): boolean {
  return t === "repo_plan" || t === "repo_clarification";
}

/** Split items into segments: text, collapsible tool groups, and standalone repo-plan cards. */
function segmentTimeline(items: ChatTimelineItem[]): TimelineSegment[] {
  const segments: TimelineSegment[] = [];
  let toolBuf: ChatTimelineItem[] = [];
  let textBuf: ChatTimelineItem[] = [];

  const flushTools = () => {
    if (toolBuf.length > 0) {
      segments.push({ kind: "tools", items: toolBuf });
      toolBuf = [];
    }
  };

  const flushText = () => {
    if (textBuf.length > 0) {
      segments.push({ kind: "text", items: textBuf });
      textBuf = [];
    }
  };

  for (const item of items) {
    if (isPlanType(item.type)) {
      flushText();
      flushTools();
      // Each plan item is its own segment so the card / chip is never
      // buried inside a collapsed tool group.
      segments.push({ kind: "plan", items: [item] });
    } else if (item.type === "text") {
      flushTools();
      textBuf.push(item);
    } else {
      flushText();
      toolBuf.push(item);
    }
  }
  flushText();
  flushTools();
  return segments;
}

function TimelineView({ items, taskId }: { items: ChatTimelineItem[]; taskId?: string }) {
  const segments = segmentTimeline(items);

  return (
    <>
      {segments.map((seg, i) => {
        if (seg.kind === "text") {
          return (
            <div key={seg.items[0]!.seq} className="text-sm leading-relaxed prose prose-sm dark:prose-invert max-w-none">
              <Markdown>{seg.items.map((t) => t.content ?? "").join("")}</Markdown>
            </div>
          );
        }
        if (seg.kind === "plan") {
          const item = seg.items[0]!;
          return item.type === "repo_plan" ? (
            <RepoPlanChip key={item.seq} item={item} />
          ) : (
            <RepoClarificationCard key={item.seq} item={item} taskId={taskId} />
          );
        }
        return (
          <ToolGroupCollapsible
            key={seg.items[0]!.seq}
            items={seg.items}
            defaultOpen={i === segments.length - 1}
          />
        );
      })}
    </>
  );
}

function ToolGroupCollapsible({
  items,
  defaultOpen,
}: {
  items: ChatTimelineItem[];
  defaultOpen?: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen ?? false);
  const toolCount = items.filter((i) => i.type === "tool_use").length;
  const label = `${toolCount} ${toolCount === 1 ? "tool" : "tools"}`;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground transition-colors">
        {open ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
        <span>{label}</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="mt-1 rounded-lg border bg-muted/20 p-2 space-y-0.5">
          {items.map((item) => (
            <ItemRow key={item.seq} item={item} />
          ))}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

// ─── Individual item rows ────────────────────────────────────────────────

function ItemRow({ item }: { item: ChatTimelineItem }) {
  switch (item.type) {
    case "tool_use":
      return <ToolCallRow item={item} />;
    case "tool_result":
      return <ToolResultRow item={item} />;
    case "thinking":
      return <ThinkingRow item={item} />;
    case "error":
      return <ErrorRow item={item} />;
    default:
      return null;
  }
}

function shortenPath(p: string): string {
  const parts = p.split("/");
  if (parts.length <= 3) return p;
  return ".../" + parts.slice(-2).join("/");
}

function getToolSummary(item: ChatTimelineItem): string {
  if (!item.input) return "";
  const inp = item.input as Record<string, string>;
  if (inp.query) return inp.query;
  if (inp.file_path) return shortenPath(inp.file_path);
  if (inp.path) return shortenPath(inp.path);
  if (inp.pattern) return inp.pattern;
  if (inp.description) return String(inp.description);
  if (inp.command) {
    const cmd = String(inp.command);
    return cmd.length > 100 ? cmd.slice(0, 100) + "..." : cmd;
  }
  if (inp.prompt) {
    const p = String(inp.prompt);
    return p.length > 100 ? p.slice(0, 100) + "..." : p;
  }
  if (inp.skill) return String(inp.skill);
  for (const v of Object.values(inp)) {
    if (typeof v === "string" && v.length > 0 && v.length < 120) return v;
  }
  return "";
}

function ToolCallRow({ item }: { item: ChatTimelineItem }) {
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
            {JSON.stringify(item.input, null, 2)}
          </pre>
        </CollapsibleContent>
      )}
    </Collapsible>
  );
}

function ToolResultRow({ item }: { item: ChatTimelineItem }) {
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

function ThinkingRow({ item }: { item: ChatTimelineItem }) {
  const [open, setOpen] = useState(false);
  const text = item.content ?? "";
  if (!text) return null;

  const preview = text.length > 150 ? text.slice(0, 150) + "..." : text;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-start gap-1.5 rounded px-1 -mx-1 py-0.5 text-xs hover:bg-accent/30 transition-colors">
        <Brain className="h-3 w-3 shrink-0 text-muted-foreground/60 mt-0.5" />
        <span className="text-muted-foreground italic truncate">{preview}</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre className="ml-[18px] mt-0.5 max-h-40 overflow-auto rounded bg-muted/30 p-2 text-[11px] text-muted-foreground whitespace-pre-wrap break-words">
          {text}
        </pre>
      </CollapsibleContent>
    </Collapsible>
  );
}

function ErrorRow({ item }: { item: ChatTimelineItem }) {
  return (
    <div className="flex items-start gap-1.5 px-1 -mx-1 py-0.5 text-xs">
      <AlertCircle className="h-3 w-3 shrink-0 text-destructive mt-0.5" />
      <span className="text-destructive">{item.content}</span>
    </div>
  );
}

// ─── Repo plan (router decision for chat tasks) ──────────────────────────

interface RepoPlanInput {
  repo_url?: string;
  repo_description?: string;
  confidence?: number;
  reason?: string;
}

interface RepoClarificationCandidate {
  url: string;
  description?: string;
  reason?: string;
}

interface RepoClarificationInput {
  question?: string;
  candidates?: RepoClarificationCandidate[];
}

function repoShortName(url: string): string {
  // Strip trailing .git and pick the last path segment; fall back to url.
  const cleaned = url.replace(/\.git$/i, "");
  const parts = cleaned.split("/").filter(Boolean);
  return parts[parts.length - 1] ?? url;
}

/**
 * Small chip announcing which repo the planner chose. Sits inline in the
 * assistant timeline so it never feels heavier than a tool row, but uses
 * a solid pill shape to read as a decision rather than an operation.
 */
function RepoPlanChip({ item }: { item: ChatTimelineItem }) {
  const input = (item.input ?? {}) as RepoPlanInput;
  const url = input.repo_url ?? "";
  if (!url) return null;
  const confidence = typeof input.confidence === "number" ? input.confidence : 1;
  const low = confidence < 0.8;

  return (
    <div className="inline-flex items-center gap-1.5 rounded-md border bg-card px-2 py-1 text-xs">
      <span
        className={cn(
          "h-1.5 w-1.5 rounded-full",
          low ? "bg-amber-500 animate-pulse" : "bg-emerald-500",
        )}
      />
      <GitBranch className="size-3 text-muted-foreground" />
      <span className="font-medium">{repoShortName(url)}</span>
      {input.repo_description && (
        <span className="text-muted-foreground">· {input.repo_description}</span>
      )}
      {low && (
        <span className="text-muted-foreground">· 置信 {Math.round(confidence * 100)}%</span>
      )}
    </div>
  );
}

/**
 * Interactive card surfaced when the planner was unsure. The task is paused
 * (status='awaiting_user') until the user picks a repo and the backend flips
 * it back to 'queued'. We optimistically hide the buttons after submit so a
 * slow network doesn't invite a double-click.
 */
function RepoClarificationCard({
  item,
  taskId,
}: {
  item: ChatTimelineItem;
  taskId?: string;
}) {
  const input = (item.input ?? {}) as RepoClarificationInput;
  const candidates = input.candidates ?? [];
  const question = input.question ?? "需要你确认一下仓库";
  const qc = useQueryClient();

  const [selected, setSelected] = useState<string | null>(candidates[0]?.url ?? null);
  const [submitting, setSubmitting] = useState(false);
  const [resolvedUrl, setResolvedUrl] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  if (resolvedUrl) {
    return (
      <div className="inline-flex items-center gap-1.5 rounded-md border bg-card px-2 py-1 text-xs">
        <span className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
        <GitBranch className="size-3 text-muted-foreground" />
        <span className="font-medium">{repoShortName(resolvedUrl)}</span>
        <span className="text-muted-foreground">· 已确认</span>
      </div>
    );
  }

  const handleConfirm = async () => {
    if (!taskId || !selected || submitting) return;
    setSubmitting(true);
    setError(null);
    try {
      await api.resolveChatTaskRepo(taskId, selected);
      setResolvedUrl(selected);
      // Let the task/message caches refetch so we re-render with any
      // new timeline entries the daemon emits after resuming.
      qc.invalidateQueries({ queryKey: ["task-messages", taskId] });
      qc.invalidateQueries({ queryKey: ["pending-chat-tasks"] });
    } catch (e) {
      logger.warn("resolve-repo failed", { taskId, err: e });
      setError(e instanceof Error ? e.message : "操作失败");
      setSubmitting(false);
    }
  };

  return (
    <div className="rounded-xl border bg-card p-3 max-w-md">
      <div className="mb-1 flex items-center gap-1.5 text-sm font-medium">
        <HelpCircle className="size-3.5 text-muted-foreground" />
        {question}
      </div>
      <div className="mb-3 text-xs text-muted-foreground">
        请选择这个任务在哪个仓库执行：
      </div>
      <div className="space-y-1.5" role="radiogroup">
        {candidates.map((c) => {
          const isSelected = c.url === selected;
          return (
            <button
              key={c.url}
              type="button"
              role="radio"
              aria-checked={isSelected}
              disabled={submitting}
              onClick={() => setSelected(c.url)}
              className={cn(
                "flex w-full items-start gap-2 rounded-lg border p-2.5 text-left transition-colors",
                isSelected
                  ? "border-foreground bg-accent"
                  : "hover:border-muted-foreground/50 hover:bg-muted/40",
                submitting && "opacity-60",
              )}
            >
              <span
                className={cn(
                  "mt-0.5 flex size-3.5 shrink-0 items-center justify-center rounded-full border-2",
                  isSelected ? "border-foreground" : "border-muted-foreground/40",
                )}
              >
                {isSelected && <span className="size-1.5 rounded-full bg-foreground" />}
              </span>
              <span className="min-w-0 flex-1">
                <span className="block truncate text-xs font-medium">
                  {repoShortName(c.url)}
                </span>
                {c.description && (
                  <span className="block truncate text-[11px] text-muted-foreground">
                    {c.description}
                  </span>
                )}
                {c.reason && (
                  <span className="mt-0.5 block text-[11px] text-muted-foreground/80">
                    {c.reason}
                  </span>
                )}
              </span>
            </button>
          );
        })}
      </div>
      {error && <div className="mt-2 text-[11px] text-destructive">{error}</div>}
      <div className="mt-3 flex items-center justify-end gap-2">
        <button
          type="button"
          disabled={!taskId || !selected || submitting}
          onClick={handleConfirm}
          className={cn(
            "rounded-md bg-foreground px-3 py-1 text-xs font-medium text-background",
            "disabled:opacity-40 disabled:cursor-not-allowed",
            "hover:bg-foreground/90 transition-colors",
          )}
        >
          {submitting ? "确认中…" : "确认"}
        </button>
      </div>
    </div>
  );
}

// ─── Shared ──────────────────────────────────────────────────────────────

