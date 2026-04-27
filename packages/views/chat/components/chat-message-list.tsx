"use client";

import { useState, useRef, useMemo } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import {
  Loader2,
  ChevronRight,
  ChevronDown,
  Brain,
  AlertCircle,
  Trash2,
  RotateCw,
  MoreHorizontal,
  Copy,
} from "lucide-react";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { useAutoScroll } from "@multica/ui/hooks/use-auto-scroll";
import { taskMessagesOptions, chatKeys } from "@multica/core/chat/queries";
import { api } from "@multica/core/api";
import { copyMarkdown } from "../../editor";
import { Markdown } from "@multica/views/common/markdown";
import type { ChatMessage, TaskMessagePayload } from "@multica/core/types";
import type { ChatTimelineItem } from "@multica/core/chat";

// ─── Public component ────────────────────────────────────────────────────

interface ChatMessageListProps {
  messages: ChatMessage[];
  /** When set, streams the live timeline for this task from task-messages cache. */
  pendingTaskId: string | null;
  isWaiting: boolean;
  /** Active session — required for delete/retry actions. */
  sessionId: string | null;
  sessionCreatorId: string | null;
  /** Owner of the agent for this session (for assistant-message delete). */
  agentOwnerId: string | null;
  userId: string | undefined;
  isSessionArchived: boolean;
  wsId: string;
}

export function ChatMessageList({
  messages,
  pendingTaskId,
  isWaiting,
  sessionId,
  sessionCreatorId,
  agentOwnerId,
  userId,
  isSessionArchived,
  wsId,
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
          <MessageBubble
            key={msg.id}
            message={msg}
            sessionId={sessionId}
            sessionCreatorId={sessionCreatorId}
            agentOwnerId={agentOwnerId}
            userId={userId}
            isSessionArchived={isSessionArchived}
            wsId={wsId}
          />
        ))}
        {hasLive && (
          <div className="w-full space-y-1.5">
            <TimelineView items={liveTimeline} />
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

interface MessageBubbleContext {
  sessionId: string | null;
  sessionCreatorId: string | null;
  agentOwnerId: string | null;
  userId: string | undefined;
  isSessionArchived: boolean;
  wsId: string;
}

function MessageBubble({ message, ...ctx }: { message: ChatMessage } & MessageBubbleContext) {
  if (message.role === "user") {
    return (
      <div className="flex justify-end">
        <div className="flex max-w-[80%] items-start gap-1">
          <div className="rounded-2xl bg-muted px-3.5 py-2 text-sm break-words min-w-0">
            <div className="prose prose-sm dark:prose-invert max-w-none [&>*:first-child]:mt-0 [&>*:last-child]:mb-0">
              <Markdown>{message.content}</Markdown>
            </div>
          </div>
          <MessageActionsMenu message={message} copyText={message.content} menuAlign="end" {...ctx} />
        </div>
      </div>
    );
  }

  return <AssistantMessage message={message} {...ctx} />;
}

function MessageActionsMenu({
  message,
  copyText,
  menuAlign,
  sessionId,
  sessionCreatorId,
  agentOwnerId,
  userId,
  isSessionArchived,
  wsId,
}: { message: ChatMessage; copyText: string; menuAlign: "end" | "start" } & MessageBubbleContext) {
  const qc = useQueryClient();
  const [confirmDeleteOpen, setConfirmDeleteOpen] = useState(false);
  const optimistic = message.id.startsWith("optimistic-");
  const canDelete =
    !optimistic &&
    !!sessionId &&
    !!userId &&
    ((message.role === "user" && userId === sessionCreatorId) ||
      (message.role === "assistant" && userId === agentOwnerId));
  const canRetry =
    !optimistic &&
    !isSessionArchived &&
    !!sessionId &&
    userId === sessionCreatorId &&
    message.role === "user";

  const deleteMut = useMutation({
    mutationFn: async () => {
      if (!sessionId) throw new Error("no session");
      await api.deleteChatMessage(sessionId, message.id);
    },
    onMutate: async () => {
      if (!sessionId) return {};
      await qc.cancelQueries({ queryKey: chatKeys.messages(sessionId) });
      const prev = qc.getQueryData<ChatMessage[]>(chatKeys.messages(sessionId));
      qc.setQueryData<ChatMessage[]>(chatKeys.messages(sessionId), (old) =>
        (old ?? []).filter((m) => m.id !== message.id),
      );
      return { prev };
    },
    onError: (_err, _vars, context) => {
      if (!sessionId) return;
      if (context?.prev) {
        qc.setQueryData(chatKeys.messages(sessionId), context.prev);
      }
      toast.error("Failed to delete message");
    },
    onSuccess: () => {
      toast.success("Message deleted");
      setConfirmDeleteOpen(false);
    },
    onSettled: () => {
      if (!sessionId) return;
      qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
      qc.invalidateQueries({ queryKey: chatKeys.pendingTask(sessionId) });
      qc.invalidateQueries({ queryKey: chatKeys.pendingTasks(wsId) });
    },
  });

  const retryMut = useMutation({
    mutationFn: async () => {
      if (!sessionId) throw new Error("no session");
      return api.retryChatMessage(sessionId, message.id);
    },
    onSuccess: (res) => {
      if (!sessionId) return;
      qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
      qc.setQueryData(chatKeys.pendingTask(sessionId), {
        task_id: res.task_id,
        status: "queued",
      });
      qc.invalidateQueries({ queryKey: chatKeys.pendingTasks(wsId) });
      toast.success("Retrying…");
    },
    onError: () => {
      toast.error("Failed to retry message");
    },
  });

  const handleCopy = () => {
    void copyMarkdown(copyText).then(
      () => toast.success("Copied"),
      () => toast.error("Failed to copy"),
    );
  };

  const showActionsFooter = canRetry || canDelete;

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="ghost"
              size="icon-sm"
              className="size-7 shrink-0 text-muted-foreground"
              aria-label="Message actions"
            >
              <MoreHorizontal className="size-4" />
            </Button>
          }
        />
        <DropdownMenuContent align={menuAlign === "end" ? "end" : "start"} className="w-44">
          <DropdownMenuItem onClick={handleCopy}>
            <Copy className="size-3.5" />
            Copy
          </DropdownMenuItem>
          {showActionsFooter && <DropdownMenuSeparator />}
          {canRetry && (
            <DropdownMenuItem
              disabled={retryMut.isPending || deleteMut.isPending}
              onClick={() => retryMut.mutate()}
            >
              <RotateCw className="size-3.5" />
              Retry
            </DropdownMenuItem>
          )}
          {canDelete && (
            <DropdownMenuItem
              variant="destructive"
              disabled={deleteMut.isPending || retryMut.isPending}
              onClick={() => setConfirmDeleteOpen(true)}
            >
              <Trash2 className="size-3.5" />
              Delete
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>

      <AlertDialog open={confirmDeleteOpen} onOpenChange={setConfirmDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete this message?</AlertDialogTitle>
            <AlertDialogDescription>
              This cannot be undone. Related task logs may also be removed depending on server
              configuration.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={() => deleteMut.mutate()}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function AssistantMessage({
  message,
  ...ctx
}: {
  message: ChatMessage;
} & MessageBubbleContext) {
  const taskId = message.task_id;

  // Use the shared taskMessagesOptions so this cache entry is the same one
  // seeded by useRealtimeSync during task execution — zero refetch when the
  // task finishes, since WS already populated it.
  const { data: taskMessages } = useQuery({
    ...taskMessagesOptions(taskId ?? ""),
    enabled: !!taskId,
  });

  const timeline: ChatTimelineItem[] = (taskMessages ?? []).map(toTimelineItem);

  const copyText = useMemo(() => {
    const base = message.content?.trim() ?? "";
    if (base) return message.content;
    return timeline
      .filter((t) => t.type === "text" && (t.content ?? "").trim())
      .map((t) => t.content ?? "")
      .join("\n\n");
  }, [message.content, timeline]);

  return (
    <div className="w-full space-y-1.5">
      <div className="flex items-start gap-1">
        <MessageActionsMenu
          message={message}
          copyText={copyText}
          menuAlign="start"
          {...ctx}
        />
        <div className="min-w-0 flex-1 space-y-1.5">
          {timeline.length > 0 ? (
            <TimelineView items={timeline} />
          ) : (
            <div className="text-sm leading-relaxed prose prose-sm dark:prose-invert max-w-none">
              <Markdown>{message.content}</Markdown>
            </div>
          )}
        </div>
      </div>
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

// ─── Shared ──────────────────────────────────────────────────────────────
