"use client";

import { useState, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "@multica/i18n/react";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import { ChevronRight, ChevronDown, Brain, AlertCircle, AlertTriangle } from "lucide-react";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { useAutoScroll } from "@multica/ui/hooks/use-auto-scroll";
import { taskMessagesOptions } from "@multica/core/chat/queries";
import { Markdown } from "@multica/views/common/markdown";
import type { AgentAvailability } from "@multica/core/agents";
import type { ChatMessage, ChatPendingTask, TaskMessagePayload, TaskFailureReason } from "@multica/core/types";
import type { ChatTimelineItem } from "@multica/core/chat";
import { getFailureReasonLabel } from "../../agents/components/tabs/task-failure";
import { TaskStatusPill } from "./task-status-pill";
import { formatElapsedMs } from "../lib/format";

// ─── Public component ────────────────────────────────────────────────────

interface ChatMessageListProps {
  messages: ChatMessage[];
  /**
   * Server-authoritative pending-task snapshot. `null` / undefined means
   * no in-flight task — list renders without StatusPill.
   */
  pendingTask: ChatPendingTask | null | undefined;
  /** Resolved presence; pass `undefined` while loading to keep the pill copy neutral. */
  availability: AgentAvailability | undefined;
}

export function ChatMessageList({
  messages,
  pendingTask,
  availability,
}: ChatMessageListProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);
  useAutoScroll(scrollRef);

  const pendingTaskId = pendingTask?.task_id ?? null;

  // Once the assistant message for this pending task has landed in the
  // messages list, AssistantMessage owns its rendering — suppress the live
  // timeline (and pill) to avoid rendering the same content in two places
  // during the invalidate → refetch window.
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
  const showStatusPill = !!pendingTaskId && !pendingAlreadyPersisted && !!pendingTask;

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
            <TimelineView items={liveTimeline} />
          </div>
        )}
        {showStatusPill && pendingTask && (
          <TaskStatusPill
            pendingTask={pendingTask}
            taskMessages={liveTaskMessages ?? []}
            availability={availability}
          />
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
  const t = useT("chat");
  const taskId = message.task_id;

  // Use the shared taskMessagesOptions so this cache entry is the same one
  // seeded by useRealtimeSync during task execution — zero refetch when the
  // task finishes, since WS already populated it.
  const { data: taskMessages } = useQuery({
    ...taskMessagesOptions(taskId ?? ""),
    enabled: !!taskId,
  });

  const timeline: ChatTimelineItem[] = (taskMessages ?? []).map(toTimelineItem);

  // Failure bubble path: when the server's FailTask wrote a failure
  // chat_message (failure_reason set), render a destructive bubble with the
  // human-readable reason label + collapsible raw errMsg + the same timeline
  // so the user can see exactly where the run broke.
  if (message.failure_reason) {
    return (
      <FailureBubble
        reason={message.failure_reason}
        rawError={message.content}
        timeline={timeline}
        elapsedMs={message.elapsed_ms}
      />
    );
  }

  return (
    <div className="w-full space-y-1.5">
      {timeline.length > 0 ? (
        <TimelineView items={timeline} />
      ) : (
        <div className="text-sm leading-relaxed prose prose-sm dark:prose-invert max-w-none">
          <Markdown>{message.content}</Markdown>
        </div>
      )}
      {message.elapsed_ms != null && (
        <ElapsedCaption verb={t("messages_replied_in")} elapsedMs={message.elapsed_ms} />
      )}
    </div>
  );
}

// Persisted "Replied in 38s" / "Failed after 12s" line under the assistant
// bubble. Reads `elapsed_ms` straight off the chat_message — server computes
// it once at task completion, so this caption is identical across reloads
// and devices. Skipped silently when null (legacy messages predating
// migration 063 + user messages).
function ElapsedCaption({
  verb,
  elapsedMs,
  className,
}: {
  verb: string;
  elapsedMs: number;
  className?: string;
}) {
  return (
    <div className={cn("text-[11px] text-muted-foreground/80", className)}>
      {verb} {formatElapsedMs(elapsedMs)}
    </div>
  );
}

function FailureBubble({
  reason,
  rawError,
  timeline,
  elapsedMs,
}: {
  reason: string;
  rawError: string;
  timeline: ChatTimelineItem[];
  elapsedMs?: number | null;
}) {
  const t = useT("chat");
  const [open, setOpen] = useState(false);
  // Map the back-end enum to copy via the shared label table; an unknown
  // reason (e.g. a future enum value the front-end doesn't ship yet)
  // falls back to a generic "Task failed" so we never render a bare slug.
  const label =
    getFailureReasonLabel(t, reason as TaskFailureReason) ?? t("messages_task_failed");

  return (
    <div className="w-full space-y-1.5">
      {/* Failure read as an inline, low-key note — not a destructive
       *  alert. Intentionally borderless / no background tint: a chat
       *  failure is informational ("this didn't work"), not a system
       *  error. The icon + muted destructive text are signal enough,
       *  the rest stays in the normal reply rhythm. */}
      <div className="flex items-start gap-1.5 text-sm">
        <AlertTriangle className="size-3.5 shrink-0 text-destructive/80 mt-0.5" />
        <div className="flex-1 min-w-0">
          <div className="text-destructive/90">{label}</div>
          {rawError.trim() && (
            <Collapsible open={open} onOpenChange={setOpen}>
              <CollapsibleTrigger className="mt-0.5 flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground transition-colors">
                {open ? (
                  <ChevronDown className="size-3" />
                ) : (
                  <ChevronRight className="size-3" />
                )}
                <span>{t("messages_show_details")}</span>
              </CollapsibleTrigger>
              <CollapsibleContent>
                <pre className="mt-1 max-h-40 overflow-auto rounded bg-muted/40 p-2 text-[11px] text-muted-foreground whitespace-pre-wrap break-all">
                  {rawError}
                </pre>
              </CollapsibleContent>
            </Collapsible>
          )}
        </div>
      </div>
      {timeline.length > 0 && <TimelineView items={timeline} />}
      {elapsedMs != null && (
        <ElapsedCaption verb={t("messages_failed_after")} elapsedMs={elapsedMs} />
      )}
    </div>
  );
}

// ─── Timeline: flat interleaved text + collapsible tool groups ───────────

interface TimelineSegment {
  kind: "text" | "tools";
  items: ChatTimelineItem[];
}

/** Split items into segments: consecutive non-text → "tools", consecutive text → merged "text". */
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
    if (item.type === "text") {
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

function TimelineView({ items }: { items: ChatTimelineItem[] }) {
  const segments = segmentTimeline(items);

  return (
    <>
      {segments.map((seg, i) =>
        seg.kind === "text" ? (
          <div key={seg.items[0]!.seq} className="text-sm leading-relaxed prose prose-sm dark:prose-invert max-w-none">
            <Markdown>{seg.items.map((t) => t.content ?? "").join("")}</Markdown>
          </div>
        ) : (
          <ToolGroupCollapsible
            key={seg.items[0]!.seq}
            items={seg.items}
            defaultOpen={i === segments.length - 1}
          />
        ),
      )}
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
  const t = useT("chat");
  const [open, setOpen] = useState(defaultOpen ?? false);
  const toolCount = items.filter((i) => i.type === "tool_use").length;
  const label = t("messages_n_tools", { count: toolCount });

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
  const t = useT("chat");
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
          {item.tool ? `${item.tool} ${t("messages_result")}` : t("messages_result")}{preview}
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre className="ml-[18px] mt-0.5 max-h-40 overflow-auto rounded bg-muted/50 p-2 text-[11px] text-muted-foreground whitespace-pre-wrap break-all">
          {output.length > 4000 ? output.slice(0, 4000) + `\n${t("messages_truncated")}` : output}
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

// ─── Shared ──────────────────────────────────────────────────────────────

