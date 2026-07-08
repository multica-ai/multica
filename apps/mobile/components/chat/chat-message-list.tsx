/**
 * Chat message list — user / assistant bubbles, oldest at top, newest at
 * bottom. Initial render lands at the bottom; new arrivals auto-scroll
 * when the user is anchored near the bottom; reading history is never
 * yanked down.
 *
 * Behavioral parity (apps/mobile/CLAUDE.md):
 *   - Render ALL message roles. Unknown role values are downgraded to
 *     "assistant" by ChatMessageSchema's `.catch()`, so this list never
 *     needs to silently drop a row.
 *   - Render `failure_reason` messages with destructive styling — same
 *     boolean as web's destructive bubble + failureReasonLabel().
 *
 * v1 simplifications:
 *   - No "Replied in Ns" badge under assistant bubbles (elapsed_ms is
 *     parsed but not displayed). Easy v2 add — show below the bubble.
 *   - No attachment card rendering. Attachments embedded as
 *     `![](url)` / `[name](url)` in `content` flow through the existing
 *     markdown renderer.
 *
 * Interaction: long-press inside a bubble fires a native iOS
 * `ActionSheetIOS` (Copy / Select Text / Cancel). While the sheet is on
 * screen the targeted bubble's border highlights. The assistant branch
 * has no border baseline because its bubble has no shell — adding a 2px
 * baseline would shift layout per message. See `useChatMessageLongPress`
 * in `./message-long-press.tsx`.
 *
 * List engine: FlashList v2 (Shopify). FlatList was the original choice
 * (per the now-outdated "no FlashList" baseline in apps/mobile/CLAUDE.md
 * — written before FlashList v2 stabilised). FlatList's `scrollToEnd` is
 * janky on variable-height lists by RN's own docs admission, and our
 * markdown bubbles render in multiple async passes (Shiki highlight,
 * image natural-size, lightbox provider injection) — each pass used to
 * fire onContentSizeChange and trigger another forced scroll, causing
 * the "open chat → feels stuck" jank. FlashList v2 replaces the manual
 * scroll dance with `maintainVisibleContentPosition`
 * (default-on; locks visible item across content changes) +
 * `startRenderingFromBottom` (initial paint at bottom, no setTimeout
 * hacks). Cell recycling also keeps scroll-up smooth.
 */
import { ActivityIndicator, Pressable, View } from "react-native";
import { FlashList } from "@shopify/flash-list";
import { Ionicons } from "@expo/vector-icons";
import { useQuery } from "@tanstack/react-query";
import type {
  ChatMessage,
  ChatPendingTask,
  TaskMessagePayload,
} from "@multica/core/types";
import type { AgentAvailability } from "@multica/core/agents";
import { taskMessagesOptions } from "@/data/queries/chat";
import { Text } from "@/components/ui/text";
import { Markdown } from "@/lib/markdown";
import { failureReasonLabel } from "@/lib/failure-reason-label";
import { formatElapsedMs } from "@/lib/format-elapsed";
import { cn } from "@/lib/utils";
import { useChatSelectStore } from "@/data/chat-select-store";
import { useChatMessageLongPress } from "./message-long-press";
import { ChatEmptyState } from "./chat-empty-state";
import { ChatTimeline } from "./chat-timeline";
import { StatusPill } from "./status-pill";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";

interface Props {
  messages: ChatMessage[];
  loading: boolean;
  /** Has the workspace ever started a chat? Drives empty-state copy. */
  hasSessions: boolean;
  /** Currently picked / inherited agent's display name. */
  agentName?: string;
  /** Receive a starter-prompt tap. Caller writes into the draft store
   *  (or focuses the composer with the text) — empty state stays neutral
   *  about send vs. preview. */
  onPickPrompt: (text: string) => void;
  /** Server-authoritative pending-task snapshot for the active session.
   *  Used to render the live timeline + status line as the last item in
   *  the message stream, mirroring web's
   *  `packages/views/chat/components/chat-message-list.tsx` placement. */
  pendingTask?: ChatPendingTask | null;
  /** Live timeline rows for the in-flight task. Already fetched by the
   *  parent so this list doesn't have to manage its own subscription. */
  liveTaskMessages?: TaskMessagePayload[];
  /** Resolved availability — drives the StatusPill's "Offline" /
   *  "Reconnecting" stages. Pass `undefined` while loading. */
  availability?: AgentAvailability;
}

export function ChatMessageList({
  messages,
  loading,
  hasSessions,
  agentName,
  onPickPrompt,
  pendingTask,
  liveTaskMessages,
  availability,
}: Props) {
  // Top-level selection subscription gates the outer "tap-outside-to-dismiss"
  // Pressable below. When null, the Pressable stays disabled and every tap
  // passes through to the list cells / bubble long-press wrappers normally.
  const selectingId = useChatSelectStore((s) => s.selectingId);

  if (loading && messages.length === 0) {
    return (
      <View className="flex-1 items-center justify-center">
        <ActivityIndicator />
      </View>
    );
  }

  if (messages.length === 0) {
    // Empty new-chat state. Lives here (rather than the parent screen) so
    // the empty state and the rendered list share spacing/layout rules.
    return (
      <ChatEmptyState
        hasSessions={hasSessions}
        agentName={agentName}
        onPickPrompt={onPickPrompt}
      />
    );
  }

  // Show the live trace + status line until the persisted assistant
  // message lands. Once chat:done writes the assistant row, AssistantRow's
  // own timeline (read from the same cache entry) owns the render — no
  // double-rendering.
  const pendingTaskId = pendingTask?.task_id ?? null;
  const pendingAlreadyPersisted =
    !!pendingTaskId &&
    messages.some(
      (m) => m.role === "assistant" && m.task_id === pendingTaskId,
    );
  const showLiveSection = !!pendingTaskId && !pendingAlreadyPersisted;
  const showLiveTimeline =
    showLiveSection && (liveTaskMessages?.length ?? 0) > 0;

  return (
    // Outer Pressable owns the "tap anywhere outside the selected bubble
    // to exit text-selection mode" gesture. Disabled when no message is
    // selected, so it's a layout-only wrapper and every tap passes straight
    // through to the FlashList cells. Active state captures any tap that
    // didn't fire an inner Pressable — bubble cells in selecting mode
    // render their body without a Pressable wrapper (see `MessageRow`'s
    // `if (isSelecting) return body;`), so taps on the selected bubble
    // also dismiss, matching iOS Notes / iMessage behaviour. Scroll
    // gestures are unaffected (Pressable only intercepts non-drag taps).
    <Pressable
      onPress={
        selectingId
          ? () => useChatSelectStore.getState().clear()
          : undefined
      }
      disabled={!selectingId}
      style={{ flex: 1 }}
    >
    {/* `key` on first message id forces remount on session switch so
        `startRenderingFromBottom` re-fires and we land at the new
        session's bottom (instead of inheriting the previous session's
        scroll position). Cheap because sessions are switched, not
        re-rendered every keystroke. */}
    <FlashList
      key={messages[0]?.id ?? "empty"}
      data={messages}
      keyExtractor={(m) => m.id}
      renderItem={({ item }) => <MessageRow message={item} />}
      ItemSeparatorComponent={MessageSeparator}
      ListFooterComponent={
        showLiveSection ? (
          <View style={{ paddingTop: 12 }} className="gap-2">
            {showLiveTimeline ? (
              <ChatTimeline items={liveTaskMessages ?? []} isStreaming />
            ) : null}
            <StatusPill
              pendingTask={pendingTask}
              taskMessages={liveTaskMessages}
              availability={availability}
            />
          </View>
        ) : null
      }
      // Outer padding mirrors web's max-w-4xl px-5 py-4 container at
      // mobile scale. Vertical gap between bubbles handled by
      // ItemSeparatorComponent (FlashList doesn't honour `gap-*` on
      // contentContainer the way FlatList's gap-via-NativeWind did).
      contentContainerStyle={{
        paddingHorizontal: 16,
        paddingTop: 12,
        paddingBottom: 16,
      }}
      // Chat behavior: initial render at the bottom; when new messages
      // arrive AND the user is within 20% of the bottom, auto-scroll.
      // Reading history (further than 20% up) is preserved. This single
      // prop replaces the entire FlatList-era guard ref dance.
      maintainVisibleContentPosition={{
        autoscrollToBottomThreshold: 0.2,
        startRenderingFromBottom: true,
      }}
      // Any user-initiated scroll exits message text-selection mode —
      // matches iMessage's behavior where scrolling implicitly commits /
      // dismisses the selection caret. Hooks both drag-start and the
      // momentum kick after a flick so a fast scroll can't escape.
      onScrollBeginDrag={() => useChatSelectStore.getState().clear()}
      onMomentumScrollBegin={() => useChatSelectStore.getState().clear()}
      // iMessage-style keyboard dismissal: dragging the list pulls the
      // keyboard down with the finger (iOS); tapping empty space between
      // bubbles dismisses it. `handled` keeps Pressables inside bubbles
      // (long-press action sheet etc.) firing normally.
      keyboardDismissMode="interactive"
      keyboardShouldPersistTaps="handled"
    />
    </Pressable>
  );
}

function MessageSeparator() {
  return <View style={{ height: 12 }} />;
}

function MessageRow({ message }: { message: ChatMessage }) {
  const isUser = message.role === "user";
  const isFailure = !!message.failure_reason;
  const isSelecting = useChatSelectStore(
    (s) => s.selectingId === message.id,
  );
  const longPress = useChatMessageLongPress(message);

  if (isFailure) {
    return (
      <FailureBubble
        reasonLabel={failureReasonLabel(message.failure_reason)}
        rawError={message.content}
        elapsedMs={message.elapsed_ms ?? null}
        isSelecting={isSelecting}
        longPress={longPress}
      />
    );
  }

  if (isUser) {
    // User bubble: same Markdown pipeline as assistant — `@mention`
    // serialisation `[MUL-1](mention://issue/<id>)`, inline links, and
    // inline code resolve identically to web's
    // `packages/views/chat/components/chat-message-list.tsx` user branch.
    // Width is capped at 80% so the bubble keeps the iMessage-style
    // trailing alignment instead of stretching across the column.
    const body = (
      <View
        className={cn(
          "self-end max-w-[80%] rounded-2xl border-2 px-3.5 py-2 transition-colors",
          isSelecting
            ? "bg-primary/5 border-primary/30"
            : longPress.isPressed
              ? "bg-muted border-primary/30"
              : "bg-muted border-transparent",
        )}
      >
        <Markdown
          content={message.content}
          attachments={message.attachments}
          selectable={isSelecting}
          compact
        />
      </View>
    );
    if (isSelecting) return body;
    return (
      <Pressable
        onLongPress={longPress.onLongPress}
        delayLongPress={500}
      >
        {body}
      </Pressable>
    );
  }

  // Assistant: timeline fold + markdown + elapsed caption. See
  // AssistantRow for why timeline is lifted into its own component.
  return (
    <AssistantRow
      message={message}
      isSelecting={isSelecting}
      longPress={longPress}
    />
  );
}

/**
 * Persisted assistant message. Renders:
 *
 *   - Process-steps fold (from `task-messages` cache; same cache fed by
 *     the live timeline above, so completed runs keep their trace).
 *   - Markdown content (the model's final answer).
 *   - "Replied in Ns" caption when `elapsed_ms` is stamped.
 *
 * Web's equivalent is `AssistantMessage` in packages/views/chat/components/
 * chat-message-list.tsx — same shape, simplified for RN (no inner Tooltip
 * / Copy button — long-press already exposes Copy via the native action
 * sheet, and selection mode owns the highlight, so a hover-only Copy
 * affordance would be redundant on mobile).
 */
function AssistantRow({
  message,
  isSelecting,
  longPress,
}: {
  message: ChatMessage;
  isSelecting: boolean;
  longPress: ReturnType<typeof useChatMessageLongPress>;
}) {
  // Read the cached timeline if any. `enabled` (in taskMessagesOptions) is
  // gated on isTaskMessageTaskId — optimistic id prefixes never fetch, so
  // freshly-sent messages don't spam the API while waiting for the real
  // task_id to land. Cached cells (after live timeline finished) return
  // synchronously with no network roundtrip.
  const { data: timeline = [] } = useQuery(
    taskMessagesOptions(message.task_id),
  );
  const body = (
    <View className="gap-1.5">
      {timeline.length > 0 ? (
        <ChatTimeline items={timeline} />
      ) : null}
      <Markdown
        content={message.content}
        attachments={message.attachments}
        selectable={isSelecting}
      />
      {message.elapsed_ms != null ? (
        <ElapsedCaption variant="replied" elapsedMs={message.elapsed_ms} />
      ) : null}
    </View>
  );
  if (isSelecting) return body;
  return (
    <Pressable onLongPress={longPress.onLongPress} delayLongPress={500}>
      {body}
    </Pressable>
  );
}

// Persistent caption rendered under the assistant bubble / failure bubble
// once the server has written `elapsed_ms`. Server computes once at task
// completion, so this caption is identical across reloads and clients.
function ElapsedCaption({
  variant,
  elapsedMs,
}: {
  variant: "replied" | "failed";
  elapsedMs: number;
}) {
  const label =
    variant === "replied"
      ? `Replied in ${formatElapsedMs(elapsedMs)}`
      : `Failed after ${formatElapsedMs(elapsedMs)}`;
  return (
    <Text className="text-xs text-muted-foreground/80 mt-1">{label}</Text>
  );
}

function FailureBubble({
  reasonLabel,
  rawError,
  elapsedMs,
  isSelecting,
  longPress,
}: {
  reasonLabel: string;
  rawError: string;
  elapsedMs: number | null;
  isSelecting: boolean;
  longPress: ReturnType<typeof useChatMessageLongPress>;
}) {
  const hasRawError = rawError.trim().length > 0;

  // B6: pass `selectable={isSelecting}` rather than hard-coding
  // `selectable` — otherwise UIKit's text-selection gesture pre-empts
  // our long-press handler and the action sheet never fires. Select-mode
  // cue is the border-tint to primary; bg stays destructive so the
  // failure signal is never lost.
  const body = (
    <View className="self-start max-w-[80%]">
      <View
        className={cn(
          "rounded-2xl border-2 bg-destructive/10 px-3.5 py-2 transition-colors",
          isSelecting || longPress.isPressed
            ? "border-primary/30"
            : "border-destructive/30",
        )}
      >
        <Text className="text-xs font-semibold text-destructive">
          {reasonLabel}
        </Text>
        {hasRawError ? (
          <Collapsible>
            <CollapsibleTrigger asChild>
              <View
                accessibilityRole="button"
                accessibilityLabel="Show error details"
                className="mt-1 flex-row items-center gap-1 active:opacity-70"
              >
                <Ionicons
                  name="chevron-forward"
                  size={12}
                  color="#71717a"
                />
                <Text className="text-xs text-muted-foreground">
                  Show details
                </Text>
              </View>
            </CollapsibleTrigger>
            <CollapsibleContent>
              <View className="mt-1 rounded bg-muted/40 px-2 py-1.5">
                <Text
                  className="text-xs text-muted-foreground"
                  selectable={isSelecting}
                >
                  {rawError}
                </Text>
              </View>
            </CollapsibleContent>
          </Collapsible>
        ) : null}
      </View>
      {elapsedMs != null ? (
        <ElapsedCaption variant="failed" elapsedMs={elapsedMs} />
      ) : null}
    </View>
  );
  if (isSelecting) return body;
  return (
    <Pressable onLongPress={longPress.onLongPress} delayLongPress={500}>
      {body}
    </Pressable>
  );
}
