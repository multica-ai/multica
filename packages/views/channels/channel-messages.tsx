"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { channelMessagesOptions, channelMembersOptions, useChannelStore } from "@multica/core/channels";
import { taskMessagesOptions } from "@multica/core/chat/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import type { ChannelMessage, ChannelMember, TaskMessagePayload } from "@multica/core/types";
import type { ChatTimelineItem } from "@multica/core/chat";
import { cn } from "@multica/ui/lib/utils";
import { Bot, User, ChevronRight, ChevronDown, Brain, AlertCircle, Loader2 } from "lucide-react";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import { Markdown } from "@multica/views/common/markdown";
import { splitTimeline } from "../chat/lib/copy-text";
import type { ChannelPendingTask } from "@multica/core/channels";
import { useT } from "../i18n";

// Stable empty array to avoid infinite re-renders from Zustand selector.
const EMPTY_TASKS: ChannelPendingTask[] = [];

// ─── Timeline components (mirror chat's TimelineView, scoped to channel) ─────

function ChannelTimelineView({
  items,
  isStreaming,
}: {
  items: ChatTimelineItem[];
  isStreaming?: boolean;
}) {
  const { preface, middle, final } = splitTimeline(items);
  return (
    <>
      {preface.length > 0 && (
        <div className="text-sm leading-relaxed prose prose-sm dark:prose-invert max-w-none pl-5">
          <Markdown>{preface.map((t) => t.content ?? "").join("")}</Markdown>
        </div>
      )}
      {middle.length > 0 && (
        <div className="pl-5">
          <ProcessFold items={middle} defaultOpen={!!isStreaming} />
        </div>
      )}
      {final.length > 0 && (
        <div className="text-sm leading-relaxed prose prose-sm dark:prose-invert max-w-none pl-5">
          <Markdown>{final.map((t) => t.content ?? "").join("")}</Markdown>
        </div>
      )}
    </>
  );
}

function ProcessFold({ items, defaultOpen }: { items: ChatTimelineItem[]; defaultOpen?: boolean }) {
  const { t } = useT("channels");
  const [open, setOpen] = useState(defaultOpen ?? false);
  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors">
        {open ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
        <span>{t(($) => $.messages.steps, { count: items.length })}</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="mt-1 rounded-lg border bg-muted/20 p-2 space-y-0.5">
          {items.map((item) =>
            item.type === "text" ? (
              <div
                key={item.seq}
                className="py-0.5 text-xs text-muted-foreground prose prose-sm dark:prose-invert max-w-none [&>*:first-child]:mt-0 [&>*:last-child]:mb-0"
              >
                <Markdown>{item.content ?? ""}</Markdown>
              </div>
            ) : (
              <ItemRow key={item.seq} item={item} />
            )
          )}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

function ItemRow({ item }: { item: ChatTimelineItem }) {
  switch (item.type) {
    case "tool_use":
      return <ToolCallRow item={item} />;
    case "tool_result":
      return <ToolResultRow item={item} />;
    case "thinking":
      return <ThinkingRow item={item} />;
    case "error":
      return (
        <div className="flex items-start gap-1.5 px-1 py-0.5 text-xs">
          <AlertCircle className="size-3 shrink-0 text-destructive mt-0.5" />
          <span className="text-destructive">{item.content}</span>
        </div>
      );
    default:
      return null;
  }
}

function getToolSummary(item: ChatTimelineItem): string {
  if (!item.input) return "";
  const inp = item.input as Record<string, string>;
  if (inp.query) return inp.query;
  if (inp.file_path) return inp.file_path.split("/").slice(-2).join("/");
  if (inp.path) return inp.path.split("/").slice(-2).join("/");
  if (inp.pattern) return inp.pattern;
  if (inp.command) return String(inp.command).slice(0, 80);
  for (const v of Object.values(inp)) {
    if (typeof v === "string" && v.length > 0 && v.length < 100) return v;
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
            "size-3 shrink-0 text-muted-foreground transition-transform",
            open && "rotate-90",
            !hasInput && "invisible",
          )}
        />
        <span className="font-medium text-foreground shrink-0">{item.tool}</span>
        {summary && <span className="truncate text-muted-foreground">{summary}</span>}
      </CollapsibleTrigger>
      {hasInput && (
        <CollapsibleContent>
          <pre className="ml-[18px] mt-0.5 max-h-32 overflow-auto rounded bg-muted/50 p-2 text-xs text-muted-foreground whitespace-pre-wrap break-all">
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
  const preview = output.length > 100 ? output.slice(0, 100) + "..." : output;
  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-start gap-1.5 rounded px-1 -mx-1 py-0.5 text-xs hover:bg-accent/30 transition-colors">
        <ChevronRight
          className={cn("size-3 shrink-0 text-muted-foreground transition-transform mt-0.5", open && "rotate-90")}
        />
        <span className="text-muted-foreground/70 truncate">→ {preview}</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre className="ml-[18px] mt-0.5 max-h-40 overflow-auto rounded bg-muted/50 p-2 text-xs text-muted-foreground whitespace-pre-wrap break-all">
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
  const preview = text.length > 120 ? text.slice(0, 120) + "..." : text;
  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-start gap-1.5 rounded px-1 -mx-1 py-0.5 text-xs hover:bg-accent/30 transition-colors">
        <Brain className="size-3 shrink-0 text-muted-foreground/60 mt-0.5" />
        <span className="text-muted-foreground italic truncate">{preview}</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre className="ml-[18px] mt-0.5 max-h-40 overflow-auto rounded bg-muted/30 p-2 text-xs text-muted-foreground whitespace-pre-wrap break-words">
          {text}
        </pre>
      </CollapsibleContent>
    </Collapsible>
  );
}

// ─── Live agent "thinking" indicator shown during a pending task ──────────────

function AgentThinkingBubble({ taskId, agentName, status }: {
  taskId: string;
  agentName: string;
  status: "queued" | "dispatched" | "running";
}) {
  const { t } = useT("channels");
  // Enable as soon as we have a task_id — WS task:message events start flowing
  // from the moment the daemon claims the task (before status flips to "running").
  const { data: taskMessages = [] } = useQuery({
    ...taskMessagesOptions(taskId),
    enabled: !!taskId,
  });

  const timeline: ChatTimelineItem[] = (taskMessages as TaskMessagePayload[]).map((m) => ({
    seq: m.seq,
    type: m.type,
    tool: m.tool,
    content: m.content,
    input: m.input,
    output: m.output,
  }));

  return (
    <div className="rounded-lg px-3 py-2 bg-purple-50/50 dark:bg-purple-900/10 border border-purple-100 dark:border-purple-800/30">
      <div className="flex items-center gap-1.5 mb-1">
        <Bot className="size-3.5 shrink-0 text-purple-500" />
        <span className="text-xs font-semibold text-purple-600 dark:text-purple-400">{agentName}</span>
        <Loader2 className="size-3 text-purple-400 animate-spin ml-1" />
        <span className="text-[10px] text-muted-foreground">
          {status === "queued"
            ? t(($) => $.messages.status_queued)
            : status === "running"
              ? t(($) => $.messages.status_running)
              : t(($) => $.messages.status_dispatched)}
        </span>
      </div>
      {timeline.length > 0 && (
        <ChannelTimelineView items={timeline} isStreaming />
      )}
    </div>
  );
}

// ─── Author icon & mention highlight ─────────────────────────────────────────

/** Replace @Name tokens with highlighted spans. */
function highlightMentions(text: string) {
  const parts = text.split(/(@\S+)/g);
  return parts.map((part, i) => {
    if (part.startsWith("@")) {
      return (
        <span
          key={i}
          className="inline-flex items-center rounded px-0.5 bg-purple-100 text-purple-700 dark:bg-purple-900/40 dark:text-purple-300 font-medium"
        >
          {part}
        </span>
      );
    }
    return part;
  });
}

function AuthorIcon({ type }: { type: string }) {
  if (type === "agent") return <Bot className="size-3.5 shrink-0 text-purple-500" />;
  return <User className="size-3.5 shrink-0 text-muted-foreground" />;
}

// ─── Detect if content looks like markdown ────────────────────────────────────
// Only use markdown renderer when the content has markdown syntax — plain
// conversational messages stay as plain text for better readability.
function hasMarkdownSyntax(text: string): boolean {
  return /^#{1,6}\s|^\s*[-*+]\s|\*\*|`{1,3}|\[.+\]\(|^>\s|^\s*\d+\.\s/m.test(text);
}

// ─── Completed task timeline — shown below the agent's final reply ────────────
// Reads task messages from cache (already populated by WS stream during execution)
// and renders them in a collapsed fold. The user can expand to review the full
// tool-call trace even after the task has completed.

function CompletedTaskTimeline({ taskId }: { taskId: string }) {
  const { data: taskMessages = [] } = useQuery({
    ...taskMessagesOptions(taskId),
    // Data is already in the cache from the live stream; this just reads it.
    staleTime: Infinity,
  });

  const timeline: ChatTimelineItem[] = (taskMessages as TaskMessagePayload[]).map((m) => ({
    seq: m.seq,
    type: m.type,
    tool: m.tool,
    content: m.content,
    input: m.input,
    output: m.output,
  }));

  if (timeline.length === 0) return null;

  return (
    <div className="pl-5 mt-1">
      <ProcessFold items={timeline} defaultOpen={false} />
    </div>
  );
}

// ─── Single message bubble ─────────────────────────────────────────────────────

function MessageBubble({
  msg,
  authorName,
}: {
  msg: ChannelMessage;
  authorName: string;
}) {
  const { t } = useT("channels");
  const isAgent = msg.author_type === "agent";
  const time = new Date(msg.created_at).toLocaleTimeString("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
  });
  const useMd = isAgent && hasMarkdownSyntax(msg.content);

  return (
    <div
      className={cn(
        "rounded-lg px-3 py-2 hover:bg-muted/40 group",
        msg.status === "deleted" && "opacity-50",
      )}
    >
      {/* Header row */}
      <div className="flex items-center gap-1.5 mb-0.5">
        <AuthorIcon type={msg.author_type} />
        <span
          className={cn(
            "text-xs font-semibold",
            isAgent ? "text-purple-600 dark:text-purple-400" : "text-foreground",
          )}
        >
          {authorName}
        </span>
        <span className="text-[10px] text-muted-foreground">{time}</span>
        {msg.status === "converted" && (
          <span className="ml-1 text-[10px] px-1 py-0.5 rounded bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300">
            {t(($) => $.messages.converted)}
          </span>
        )}
      </div>

      {/* Content */}
      {useMd ? (
        <div className="text-sm leading-relaxed prose prose-sm dark:prose-invert max-w-none pl-5 [&>*:first-child]:mt-0 [&>*:last-child]:mb-0">
          <Markdown>{msg.content}</Markdown>
        </div>
      ) : (
        <p className="text-sm whitespace-pre-wrap leading-relaxed pl-5">
          {isAgent ? msg.content : highlightMentions(msg.content)}
        </p>
      )}

      {/* Tool-call trace — persisted after task completes via task_id */}
      {isAgent && msg.task_id && (
        <CompletedTaskTimeline taskId={msg.task_id} />
      )}
    </div>
  );
}

// ─── Main component ───────────────────────────────────────────────────────────

export function ChannelMessages({ channelId }: { channelId: string }) {
  const { t } = useT("channels");
  const wsId = useWorkspaceId();
  const bottomRef = useRef<HTMLDivElement>(null);

  const { data: messages = [], isLoading } = useQuery(
    channelMessagesOptions(wsId, channelId),
  );
  const { data: members = [] } = useQuery(channelMembersOptions(wsId, channelId));

  // In-flight tasks for THIS channel (multiple agents may be responding concurrently).
  // Use a stable empty array reference to avoid infinite re-renders when no tasks exist.
  const pendingTasks = useChannelStore((s) => s.pendingTasks[channelId]) ?? EMPTY_TASKS;
  const messageCount = (messages as ChannelMessage[]).length;
  const pendingTaskCount = pendingTasks.length;

  // Resolve member names from the members list.
  const nameMap = new Map<string, { name: string; type: "user" | "agent" }>();
  for (const m of members as ChannelMember[]) {
    nameMap.set(m.member_id, { name: m.name || m.member_id, type: m.member_type });
  }

  // Auto-scroll to bottom when new messages arrive or pending tasks change.
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messageCount, pendingTaskCount]);

  if (isLoading) {
    return (
      <div className="flex-1 flex items-center justify-center text-sm text-muted-foreground">
        {t(($) => $.messages.loading)}
      </div>
    );
  }

  if (messages.length === 0 && pendingTasks.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center text-sm text-muted-foreground">
        {t(($) => $.messages.empty)}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-1 p-3 overflow-y-auto">
      {(messages as ChannelMessage[]).map((msg) => {
        const member = nameMap.get(msg.author_id);
        const authorName = member?.name ?? (msg.author_type === "agent"
          ? t(($) => $.members.agent_badge)
          : t(($) => $.members.user_badge));
        return (
          <MessageBubble key={msg.id} msg={msg} authorName={authorName} />
        );
      })}

      {/* Live agent activity bubbles — shown while tasks are running */}
      {pendingTasks.map((task) => (
        <AgentThinkingBubble
          key={task.task_id}
          taskId={task.task_id}
          agentName={nameMap.get(task.agent_id)?.name ?? t(($) => $.members.agent_badge)}
          status={task.status}
        />
      ))}

      <div ref={bottomRef} />
    </div>
  );
}
