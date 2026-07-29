"use client";

import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { Virtuoso, type Components } from "react-virtuoso";
import { cn } from "@multica/ui/lib/utils";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button } from "@multica/ui/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { ChevronRight, ChevronDown, Brain, AlertCircle, AlertTriangle, Copy } from "lucide-react";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { isTaskMessageTaskId, taskMessagesOptions } from "@multica/core/chat/queries";
import { RichContent } from "../../rich-content";
import { RichContentScrollRootProvider } from "../../rich-content/scroll-root";
import { copyText } from "@multica/ui/lib/clipboard";
import { AttachmentList } from "../../issues/components/comment-card";
import type { AgentAvailability } from "@multica/core/agents";
import type {
  ChatMessage,
  ChatPendingTask,
  TaskFailureReason,
  TaskMessagePayload,
} from "@multica/core/types";
import type { ChatTimelineItem } from "@multica/core/chat";
import { buildTimeline } from "../../common/task-transcript";
import { TaskStatusPill } from "./task-status-pill";
import { formatElapsedMs } from "../lib/format";
import { splitTimeline, extractCopyText } from "../lib/copy-text";
import { useT } from "../../i18n";

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
  firstItemIndex?: number;
  hasOlderMessages?: boolean;
  isFetchingOlderMessages?: boolean;
  onLoadOlderMessages?: () => void;
  /** Transform assistant task text for embedded chat protocols before render/copy. */
  transformContent?: (content: string) => string;
}

// ─── Virtuoso chrome ─────────────────────────────────────────────────────
//
// Header/Footer MUST be stable component references (module scope), never
// inline arrows in the `components` prop: an inline `components={{ Footer:
// () => … }}` creates a new component *type* every render, so React unmounts
// and remounts the whole Header/Footer subtree each time. During task
// streaming that tore down and rebuilt the entire live timeline — every row
// and every Markdown parse — on every `task:message` event, freezing the
// renderer for seconds at a time (MUL-3960). Per-render data flows through
// Virtuoso's `context` prop instead, which reaches these components as an
// ordinary prop (re-render, not remount).

interface ChatListContext {
  isFetchingOlderMessages: boolean;
  showStatusPill: boolean;
  pendingTask: ChatPendingTask | null | undefined;
  liveTaskMessages: readonly TaskMessagePayload[] | undefined;
  availability: AgentAvailability | undefined;
}

/**
 * One Virtuoso row. A live (still-streaming) task and the persisted assistant
 * message it becomes share ONE key — `task:<taskId>` — so the handoff replaces
 * this item's data in place instead of unmounting a Footer subtree and mounting
 * a different row (MUL-4922). That identity is what keeps an already-rendered
 * Mermaid diagram or HTML iframe mounted across task completion.
 */
type ChatRenderItem =
  | { key: string; kind: "message"; message: ChatMessage; taskId: string | null }
  | { key: string; kind: "live"; taskId: string };

/**
 * Row key for a persisted message. Assistant turns carrying a task_id key on
 * the task so they can inherit the live row; everything else keys on its own
 * id.
 */
function messageRowKey(message: ChatMessage): string {
  return message.role === "assistant" && message.task_id
    ? `task:${message.task_id}`
    : message.id;
}

function ChatListHeader({ context }: { context?: ChatListContext }) {
  const { t } = useT("chat");
  return (
    <div className="mx-auto w-full max-w-4xl px-5 pt-4">
      {context?.isFetchingOlderMessages && (
        <div className="text-center text-xs text-muted-foreground">
          {t(($) => $.message_list.loading_older)}
        </div>
      )}
    </div>
  );
}

// The Footer now carries only the status pill — task chrome, not content. The
// live timeline moved into a real row so it can keep its identity when the
// task completes (see ChatRenderItem).
function ChatListFooter({ context }: { context?: ChatListContext }) {
  if (!context) return null;
  if (!context.showStatusPill || !context.pendingTask) return null;
  return (
    <div className="mx-auto w-full max-w-4xl px-5 pb-4 space-y-4">
      <TaskStatusPill
        pendingTask={context.pendingTask}
        taskMessages={context.liveTaskMessages ?? []}
        availability={context.availability}
      />
    </div>
  );
}

const LIST_COMPONENTS: Components<ChatRenderItem, ChatListContext> = {
  Header: ChatListHeader,
  Footer: ChatListFooter,
};

export function ChatMessageList({
  messages,
  pendingTask,
  availability,
  firstItemIndex = 0,
  hasOlderMessages = false,
  isFetchingOlderMessages = false,
  onLoadOlderMessages,
  transformContent,
}: ChatMessageListProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [scrollContainerEl, setScrollContainerEl] = useState<HTMLDivElement | null>(null);
  const [isNearBottom, setIsNearBottom] = useState(true);
  const setScrollContainerRef = useCallback((node: HTMLDivElement | null) => {
    scrollRef.current = node;
    setScrollContainerEl(node);
  }, []);
  // Soft edge fade hinting more content above/below. Kept small so it barely
  // grazes full-bleed previews (image / HTML) at the edges.
  const fadeStyle = useScrollFade(scrollRef, 16);

  const pendingTaskId = pendingTask?.task_id ?? null;

  // Once the assistant message for this pending task has landed in the
  // messages list, AssistantMessage owns its rendering — suppress the live
  // timeline (and pill) to avoid rendering the same content in two places
  // during the invalidate → refetch window.
  const pendingAlreadyPersisted = !!pendingTaskId && messages.some(
    (m) => m.role === "assistant" && m.task_id === pendingTaskId,
  );

  // Live timeline for the in-flight task. useRealtimeSync keeps this cache
  // current via setQueryData on task:message events. Only used here to decide
  // whether the live row exists and to feed the status pill — the row itself
  // reads the same cache entry through AssistantMessage.
  const showLiveTimeline = !!pendingTaskId && !pendingAlreadyPersisted;
  const canFetchLiveTimeline = isTaskMessageTaskId(pendingTaskId) && !pendingAlreadyPersisted;
  const { data: liveTaskMessages } = useQuery({
    ...taskMessagesOptions(pendingTaskId ?? ""),
    enabled: canFetchLiveTimeline,
  });
  const hasLive = showLiveTimeline && (liveTaskMessages?.length ?? 0) > 0;
  const showStatusPill = !!pendingTaskId && !pendingAlreadyPersisted && !!pendingTask;

  // Persisted messages plus, while a task is in flight, one synthetic trailing
  // row for it. When the assistant message persists, `hasLive` goes false and
  // the message takes the SAME key at the SAME position — an in-place data
  // swap, not a remount.
  const renderItems: ChatRenderItem[] = useMemo(() => {
    const items: ChatRenderItem[] = messages.map((message) => ({
      key: messageRowKey(message),
      kind: "message" as const,
      message,
      taskId: message.task_id ?? null,
    }));
    if (hasLive && pendingTaskId) {
      items.push({ key: `task:${pendingTaskId}`, kind: "live", taskId: pendingTaskId });
    }
    return items;
  }, [messages, hasLive, pendingTaskId]);

  const firstIndex = renderItems.length > 0 ? firstItemIndex : 0;

  const listContext: ChatListContext = {
    isFetchingOlderMessages,
    showStatusPill,
    pendingTask,
    liveTaskMessages,
    availability,
  };

  return (
    <div
      ref={setScrollContainerRef}
      data-tab-scroll-root
      style={fadeStyle}
      className="flex-1 overflow-y-auto"
    >
      {!scrollContainerEl ? (
        <div className="mx-auto w-full max-w-4xl px-5 pt-4 space-y-3">
          <ChatMessageSkeleton />
        </div>
      ) : (
      // Chat scrolls inside its own element, so rich blocks must measure
      // "near-viewport" against that element rather than the browser viewport —
      // otherwise a diagram only starts loading once it is already on screen.
      <RichContentScrollRootProvider scrollRoot={scrollContainerEl}>
      <Virtuoso
        customScrollParent={scrollContainerEl}
        data={renderItems}
        firstItemIndex={firstIndex}
        // Open pinned to the newest message. The list is remounted per session
        // (`key={activeSessionId}` upstream), so this initial position is
        // re-applied on every session switch. Without it a fresh Virtuoso
        // renders from the top and the only thing that can scroll it down is
        // `followOutput`, which reacts to post-mount data growth — leaving the
        // landing spot racy: cached sessions resolve synchronously and stick at
        // the top, while fetched ones sometimes catch a growth tick and land at
        // the bottom. `align: "end"` bottom-aligns even a last message taller
        // than the viewport, so switching sessions always shows the latest reply.
        initialTopMostItemIndex={{ index: "LAST", align: "end" }}
        increaseViewportBy={{ top: 400, bottom: 600 }}
        atBottomThreshold={120}
        atBottomStateChange={setIsNearBottom}
        followOutput={() => (!isFetchingOlderMessages && isNearBottom ? "smooth" : false)}
        startReached={() => {
          if (hasOlderMessages && !isFetchingOlderMessages) {
            onLoadOlderMessages?.();
          }
        }}
        computeItemKey={(_, item) => item.key}
        context={listContext}
        components={LIST_COMPONENTS}
        itemContent={(_, item) => (
          <div className="mx-auto w-full max-w-4xl px-5 py-2">
            <MessageBubble
              item={item}
              isPending={!!pendingTaskId && item.taskId === pendingTaskId}
              transformContent={transformContent}
            />
          </div>
        )}
      />
      </RichContentScrollRootProvider>
      )}
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

// ─── Message bubbles ─────────────────────────────────────────────────────

// memo: every streamed task:message re-renders ChatMessageList, and with it
// every VISIBLE row via itemContent. Message objects are referentially
// stable for unchanged messages and isPending is a boolean, so a shallow
// memo skips reconciling rows the stream didn't touch — the persisted
// history stays inert while only the live footer updates.
const MessageBubble = memo(function MessageBubble({
  item,
  isPending,
  transformContent,
}: {
  item: ChatRenderItem;
  isPending: boolean;
  transformContent?: (content: string) => string;
}) {
  // The live row and the persisted assistant row both land here under one key,
  // and both render <AssistantMessage> — same component type, same position —
  // so React reconciles rather than remounts at task completion.
  if (item.kind === "live") {
    return (
      <AssistantMessage
        taskId={item.taskId}
        isPending={isPending}
        transformContent={transformContent}
      />
    );
  }

  const { message } = item;

  if (message.role === "user") {
    return (
      <div className="flex justify-end">
        <div className="rounded-2xl bg-muted px-3.5 py-2 text-sm max-w-[80%] break-words">
          {/* User messages are authored as markdown in ContentEditor, so they
           * render through the SAME RichContent as assistant replies and as
           * Issue/Comment — a Mermaid fence a user pastes is a diagram here
           * too. `compact` trims the leading/trailing block margins so a
           * single-line bubble stays as tight as the plain-text version. */}
          <RichContent
            content={message.content}
            attachments={message.attachments}
            density="compact"
            phase="settled"
          />
          <AttachmentList
            attachments={message.attachments}
            content={message.content}
            className="mt-1.5"
          />
        </div>
      </div>
    );
  }

  return (
    <AssistantMessage
      taskId={message.task_id ?? null}
      message={message}
      isPending={isPending}
      transformContent={transformContent}
    />
  );
});

/**
 * Assistant turn body — renders BOTH the in-flight (live) and the persisted
 * form of one task (MUL-4922).
 *
 * `message` is undefined while the task streams and becomes the persisted
 * `chat_message` when it lands. Both forms are rendered by this one component,
 * mounted under one stable row key (`task:<taskId>`), so the live → persisted
 * handoff is a prop change rather than an unmount: the RichContent subtree and
 * any Mermaid diagram / HTML iframe inside it stay mounted, keep their pan-zoom
 * state, and never re-run their expensive render. Before this, the live
 * timeline lived in Virtuoso's Footer and the persisted row keyed on
 * `message.id`, so every completed task tore down and rebuilt its diagrams.
 *
 * The timeline itself comes from `taskMessagesOptions(taskId)` in both forms —
 * the same cache entry useRealtimeSync seeds during execution — so no refetch
 * and no data discontinuity happens at the handoff either.
 */
function AssistantMessage({
  taskId,
  message,
  isPending,
  transformContent,
}: {
  taskId: string | null;
  message?: ChatMessage;
  isPending: boolean;
  transformContent?: (content: string) => string;
}) {
  const canFetchTaskMessages = isTaskMessageTaskId(taskId);

  // Use the shared taskMessagesOptions so this cache entry is the same one
  // seeded by useRealtimeSync during task execution — zero refetch when the
  // task finishes, since WS already populated it.
  const { data: taskMessages } = useQuery({
    ...taskMessagesOptions(taskId ?? ""),
    enabled: canFetchTaskMessages,
  });

  // Memoized on the cache array identity: mergeTaskMessagesBySeq preserves the
  // array reference when a duplicate event arrives, so this recomputes only
  // when a genuinely new message lands.
  const timeline: ChatTimelineItem[] = useMemo(
    () => transformTimeline(buildTimeline(taskMessages ?? []), transformContent),
    [taskMessages, transformContent],
  );

  // Content is settled once the persisted message exists; until then text is
  // still arriving and a trailing fence may be half-written.
  const phase: "streaming" | "settled" = message ? "settled" : "streaming";

  // Failure bubble path: when the server's FailTask wrote a failure
  // chat_message (failure_reason set), render a destructive bubble with the
  // human-readable reason label + collapsible raw errMsg + the same timeline
  // so the user can see exactly where the run broke.
  if (message?.failure_reason) {
    return (
      <FailureBubble
        reason={message.failure_reason}
        rawError={message.content}
        timeline={timeline}
        elapsedMs={message.elapsed_ms}
      />
    );
  }

  // no_response path (MUL-4351): the agent completed this direct-chat turn
  // without any text. Keep whatever tool/thinking timeline the run produced and
  // show a localized "no text reply" notice instead of an empty markdown block.
  const isNoResponse = message?.message_kind === "no_response";

  return (
    <div className="w-full space-y-1.5">
      {timeline.length > 0 && (
        <TimelineView
          items={timeline}
          attachments={message?.attachments}
          phase={phase}
          isStreaming={!message}
        />
      )}
      {isNoResponse ? (
        <NoResponseNotice />
      ) : message && timeline.length === 0 ? (
        <RichContent
          content={message.content}
          attachments={message.attachments}
          density="compact"
          phase="settled"
          className="leading-relaxed"
        />
      ) : null}
      {message && (
        <>
          <AttachmentList
            attachments={message.attachments}
            content={message.content}
          />
          <MessageFooter
            message={message}
            timeline={timeline}
            isPending={isPending}
          />
        </>
      )}
    </div>
  );
}

function transformTimeline(
  timeline: ChatTimelineItem[],
  transformContent?: (content: string) => string,
): ChatTimelineItem[] {
  if (!transformContent) return timeline;
  return timeline.map((item) =>
    item.type === "text" && item.content
      ? { ...item, content: transformContent(item.content) }
      : item,
  );
}

// Muted, localized notice shown in place of assistant text when a turn
// completed with no reply (message_kind === "no_response"). Explains the empty
// turn instead of rendering a blank bubble (MUL-4351).
function NoResponseNotice() {
  const { t } = useT("chat");
  return (
    <div className="text-sm italic text-muted-foreground">
      {t(($) => $.message_list.no_response)}
    </div>
  );
}

// Inline footer row beneath the assistant reply: "Replied in 38s · [Copy]".
// Action icons live here (not as a hover-floating overlay) so they're
// discoverable on first read and don't shift content. Buttons stay quiet
// (muted) until hover. Copy is suppressed during streaming because the
// final text is still being appended.
function MessageFooter({
  message,
  timeline,
  isPending,
}: {
  message: ChatMessage;
  timeline: ChatTimelineItem[];
  isPending: boolean;
}) {
  // A no_response turn has nothing to copy, and its caption uses a neutral
  // "Finished in Xs" instead of "Replied in Xs" (MUL-4351).
  const isNoResponse = message.message_kind === "no_response";
  const showCopy = !isPending && !isNoResponse;
  if (message.elapsed_ms == null && !showCopy) return null;
  return (
    <div className="flex items-center gap-1.5">
      {message.elapsed_ms != null && (
        <ElapsedCaption
          variant={isNoResponse ? "finished" : "replied"}
          elapsedMs={message.elapsed_ms}
        />
      )}
      {showCopy && <MessageCopyButton message={message} timeline={timeline} />}
    </div>
  );
}

function MessageCopyButton({
  message,
  timeline,
}: {
  message: ChatMessage;
  timeline: ChatTimelineItem[];
}) {
  const { t } = useT("chat");
  const handleCopy = async () => {
    if (await copyText(extractCopyText(message, timeline))) {
      toast.success(t(($) => $.message_list.copied_toast));
    } else {
      toast.error(t(($) => $.message_list.copy_failed_toast));
    }
  };
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant="ghost"
            size="icon-xs"
            className="text-muted-foreground/70 hover:text-foreground"
            onClick={handleCopy}
            aria-label={t(($) => $.message_list.copy_action)}
          />
        }
      >
        <Copy />
      </TooltipTrigger>
      <TooltipContent side="top">
        {t(($) => $.message_list.copy_action)}
      </TooltipContent>
    </Tooltip>
  );
}

// Persisted "Replied in 38s" / "Failed after 12s" line under the assistant
// bubble. Reads `elapsed_ms` straight off the chat_message — server computes
// it once at task completion, so this caption is identical across reloads
// and devices. Skipped silently when null (legacy messages predating
// migration 063 + user messages).
function ElapsedCaption({
  variant,
  elapsedMs,
  className,
}: {
  variant: "replied" | "failed" | "finished";
  elapsedMs: number;
  className?: string;
}) {
  const { t } = useT("chat");
  const elapsed = formatElapsedMs(elapsedMs);
  const text =
    variant === "replied"
      ? t(($) => $.message_list.replied_in, { elapsed })
      : variant === "finished"
        ? t(($) => $.message_list.finished_in, { elapsed })
        : t(($) => $.message_list.failed_after, { elapsed });
  return (
    <div className={cn("text-xs text-muted-foreground/80", className)}>
      {text}
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
  const { t } = useT("chat");
  const [open, setOpen] = useState(false);
  // Chat gets its own friendly, reassuring copy per failure reason — plain
  // language + a "try again" nudge — instead of the terse developer labels
  // (`failureReasonLabel`) used on the agent-detail / execution-log surfaces.
  // An unknown reason (a future enum value this build doesn't ship yet) falls
  // back to a generic friendly line. The raw error stays tucked under the
  // collapsible below for anyone who wants the technical detail.
  const chatFailureCopy: Record<TaskFailureReason, string> = {
    agent_error: t(($) => $.message_list.failure.agent_error),
    timeout: t(($) => $.message_list.failure.timeout),
    codex_semantic_inactivity: t(($) => $.message_list.failure.codex_semantic_inactivity),
    runtime_offline: t(($) => $.message_list.failure.runtime_offline),
    runtime_recovery: t(($) => $.message_list.failure.runtime_recovery),
    manual: t(($) => $.message_list.failure.manual),
  };
  const label =
    chatFailureCopy[reason as TaskFailureReason] ??
    t(($) => $.message_list.failure.fallback);

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
              <CollapsibleTrigger className="mt-0.5 flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors">
                {open ? (
                  <ChevronDown className="size-3" />
                ) : (
                  <ChevronRight className="size-3" />
                )}
                <span>{t(($) => $.message_list.show_details)}</span>
              </CollapsibleTrigger>
              <CollapsibleContent>
                <pre className="mt-1 max-h-40 overflow-auto rounded bg-muted/40 p-2 text-xs text-muted-foreground whitespace-pre-wrap break-all">
                  {rawError}
                </pre>
              </CollapsibleContent>
            </Collapsible>
          )}
        </div>
      </div>
      {timeline.length > 0 && <TimelineView items={timeline} />}
      {elapsedMs != null && (
        <ElapsedCaption variant="failed" elapsedMs={elapsedMs} />
      )}
    </div>
  );
}

// ─── Timeline: outer process fold + final text (Conductor-style) ─────────
//
// splitTimeline (lib/copy-text.ts) carves the items into:
//   preface — text before the first thinking/tool item
//   middle  — first → last non-text item (inclusive, may sandwich text)
//   final   — text after the last non-text item
//
// We render preface + final outside an outer Collapsible ("X steps") that
// wraps middle. The inner row Collapsibles (ThinkingRow / ToolCallRow /
// ToolResultRow) are unchanged — clicking them toggles independently of
// the outer fold. Copy mirrors what's visible when the outer fold is
// closed: preface + final, never middle. See extractCopyText for the
// authoritative copy logic.

function TimelineView({
  items,
  isStreaming,
  attachments,
  phase = "settled",
}: {
  items: ChatTimelineItem[];
  isStreaming?: boolean;
  attachments?: import("@multica/core/types").Attachment[];
  phase?: "streaming" | "settled";
}) {
  const { preface, middle, final } = splitTimeline(items);

  return (
    <>
      {preface.length > 0 && (
        <RichContent
          content={preface.map((t) => t.content ?? "").join("")}
          attachments={attachments}
          density="compact"
          phase={phase}
          className="leading-relaxed"
        />
      )}
      {middle.length > 0 && (
        <OuterProcessFold
          items={middle}
          isStreaming={!!isStreaming}
          attachments={attachments}
          phase={phase}
        />
      )}
      {final.length > 0 && (
        <RichContent
          content={final.map((t) => t.content ?? "").join("")}
          attachments={attachments}
          density="compact"
          phase={phase}
          className="leading-relaxed"
        />
      )}
    </>
  );
}

function OuterProcessFold({
  items,
  isStreaming,
  attachments,
  phase = "settled",
}: {
  items: ChatTimelineItem[];
  isStreaming?: boolean;
  attachments?: import("@multica/core/types").Attachment[];
  phase?: "streaming" | "settled";
}) {
  const { t } = useT("chat");
  // Open while the task streams (so the user watches progress), collapsed once
  // it settles. This used to fall out of a remount: the live TimelineView was
  // torn down and the persisted one mounted closed. The row is now stable
  // across that handoff (MUL-4922) — which is the point, it keeps Mermaid and
  // HTML blocks alive — so the collapse has to be expressed directly.
  const [open, setOpen] = useState(!!isStreaming);
  const wasStreaming = useRef(!!isStreaming);
  useEffect(() => {
    if (wasStreaming.current && !isStreaming) setOpen(false);
    wasStreaming.current = !!isStreaming;
  }, [isStreaming]);
  const stepCount = items.length;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors">
        {open ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
        <span>{t(($) => $.message_list.process_steps, { count: stepCount })}</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="mt-1 rounded-lg border bg-muted/20 p-2 space-y-0.5">
          {items.map((item) =>
            item.type === "text" ? (
              <MiddleTextRow
                key={item.seq}
                item={item}
                attachments={attachments}
                phase={phase}
              />
            ) : (
              <ItemRow key={item.seq} item={item} />
            ),
          )}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

// Intermediate text segment rendered inside the outer fold. Visually
// down-shifted (xs / muted) so it reads as part of the agent's process,
// not the final answer — the final answer renders below the fold at full
// prose size.
function MiddleTextRow({
  item,
  attachments,
  phase = "settled",
}: {
  item: ChatTimelineItem;
  attachments?: import("@multica/core/types").Attachment[];
  phase?: "streaming" | "settled";
}) {
  return (
    <div className="py-0.5 text-xs text-muted-foreground">
      <RichContent
        content={item.content ?? ""}
        attachments={attachments}
        density="compact"
        phase={phase}
      />
    </div>
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
          <pre className="ml-[18px] mt-0.5 max-h-32 overflow-auto rounded bg-muted/50 p-2 text-xs text-muted-foreground whitespace-pre-wrap break-all">
            {JSON.stringify(item.input, null, 2)}
          </pre>
        </CollapsibleContent>
      )}
    </Collapsible>
  );
}

function ToolResultRow({ item }: { item: ChatTimelineItem }) {
  const { t } = useT("chat");
  const [open, setOpen] = useState(false);
  const output = item.output ?? "";
  if (!output) return null;

  const preview = output.length > 120 ? output.slice(0, 120) + "..." : output;
  const labelPrefix = item.tool
    ? t(($) => $.message_list.tool_result_named, { tool: item.tool })
    : t(($) => $.message_list.tool_result_unnamed);

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-start gap-1.5 rounded px-1 -mx-1 py-0.5 text-xs hover:bg-accent/30 transition-colors">
        <ChevronRight
          className={cn("h-3 w-3 shrink-0 text-muted-foreground transition-transform mt-0.5", open && "rotate-90")}
        />
        <span className="text-muted-foreground/70 truncate">
          {labelPrefix}{preview}
        </span>
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

  const preview = text.length > 150 ? text.slice(0, 150) + "..." : text;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-start gap-1.5 rounded px-1 -mx-1 py-0.5 text-xs hover:bg-accent/30 transition-colors">
        <Brain className="h-3 w-3 shrink-0 text-muted-foreground/60 mt-0.5" />
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

function ErrorRow({ item }: { item: ChatTimelineItem }) {
  return (
    <div className="flex items-start gap-1.5 px-1 -mx-1 py-0.5 text-xs">
      <AlertCircle className="h-3 w-3 shrink-0 text-destructive mt-0.5" />
      <span className="text-destructive">{item.content}</span>
    </div>
  );
}

// ─── Shared ──────────────────────────────────────────────────────────────
