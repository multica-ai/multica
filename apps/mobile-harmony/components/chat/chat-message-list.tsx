/**
 * Chat message list — user / assistant bubbles, oldest at top, newest at
 * bottom. Initial render lands at the bottom; new arrivals auto-scroll
 * when the user is anchored near the bottom; reading history is never
 * yanked down. HarmonyOS port of
 * apps/mobile/components/chat/chat-message-list.tsx.
 *
 * Behavioral parity (apps/mobile/CLAUDE.md):
 *   - Render ALL message roles. Unknown role values are downgraded to
 *     "assistant" by ChatMessageSchema's `.catch()`, so this list never
 *     needs to silently drop a row.
 *   - Render `failure_reason` messages with destructive styling — same
 *     boolean as web's destructive bubble + failureReasonLabel().
 *
 * Platform substitutions (RNOH 0.82 — see apps/mobile-harmony/AGENTS.md):
 *   - FlashList v2 is not on this platform's dependency matrix; the list
 *     engine is core FlatList with a manual scroll anchor that reproduces
 *     FlashList's `maintainVisibleContentPosition` contract: initial paint
 *     scrolls to the end once, and content growth auto-scrolls only while
 *     the user sits within 20% of the bottom. The `key` on the first
 *     message id still forces a remount on session switch, so the anchor
 *     refs reset and the new session lands at its own bottom.
 *   - The iOS ImageSequenceProvider lightbox context is not ported; inline
 *     images render through the ported Markdown pipeline directly.
 *   - Long-press bubbles route through `onMessageLongPress` — the screen
 *     owns the ChatMessageLongPressMenu sheet (an overlay must mount above
 *     the list, not inside a clipping cell). The highlight ring is driven
 *     by the `menuMessageId` prop, replacing the iOS hook's `isPressed`.
 *   - `keyboardDismissMode` uses "on-drag" ("interactive" is iOS-only).
 *
 * v1 simplifications carried over from iOS:
 *   - Attachments bound to a message but NOT referenced inline in `content`
 *     render as standalone cards below the bubble via `CommentAttachmentList`
 *     (same component the comment thread uses). Inline `![](url)` /
 *     `[name](url)` flow through the markdown renderer and are de-duped out
 *     of the card list.
 */
import { useCallback, useRef, useState } from "react";
import { ActivityIndicator, FlatList, StyleSheet, View, type NativeScrollEvent, type NativeSyntheticEvent } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import type {
  Agent,
  ChatMessage,
  ChatPendingTask,
  ChatQuickAction,
  TaskMessagePayload,
} from "@multica/core/types";
import type { AgentAvailability } from "@multica/core/agents";
import { continuousCorners } from "@/lib/radius";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";
import { taskMessagesOptions } from "@/data/queries/chat";
import { Text } from "@/components/ui/text";
import { Icon } from "@/components/ui/icon";
import { Markdown } from "@/lib/markdown/markdown";
import { failureReasonLabel } from "@/lib/failure-reason-label";
import { formatElapsedMs } from "@/lib/format-elapsed";
import { useChatSelectStore } from "@/data/chat-select-store";
import { ChatEmptyState } from "./chat-empty-state";
import { ChatTimeline } from "./chat-timeline";
// Reuse the comment thread's standalone attachment list — same design web
// reuses in chat (AttachmentList). Renders any bound attachment not already
// referenced inline in the message content, with same-file dedup.
import { CommentAttachmentList } from "@/components/issue/comment-attachment-list";
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
  /** Currently picked / inherited agent. */
  agent: Agent | null;
  /** Receive a conversation-starter tap. Caller writes into the draft store
   *  (or focuses the composer with the text) — empty state stays neutral
   *  about send vs. preview. */
  onPickPrompt: (text: string) => void;
  /** Send a persisted assistant follow-up without first copying it into draft. */
  onQuickAction?: (action: ChatQuickAction) => void | Promise<unknown>;
  quickActionsDisabled?: boolean;
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
  /** Id of the message whose long-press action sheet is open — drives the
   *  bubble highlight ring (iOS: the hook's `isPressed` flag). */
  menuMessageId?: string | null;
  /** Opens the screen-level long-press action sheet for a bubble. */
  onMessageLongPress?: (message: ChatMessage) => void;
}

/** Auto-scroll keeps working while the user is within this fraction of the
 *  bottom (FlashList's autoscrollToBottomThreshold value). */
const NEAR_BOTTOM_THRESHOLD = 0.2;

// Theme-independent layout styles (module-level so FlatList props and
// separators keep stable identities across renders).
const layout = StyleSheet.create({
  flex: { flex: 1 },
  // paddingHorizontal-16 paddingTop-12 paddingBottom-16
  content: { paddingHorizontal: 16, paddingTop: 12, paddingBottom: 16 },
  separator: { height: 12 },
  loadingWrap: { flex: 1, alignItems: "center", justifyContent: "center" },
  // ListFooter: paddingTop-12 + gap-2
  footer: { paddingTop: 12, gap: 8 },
  // gap-1.5 between assistant body blocks / quick-action rows
  gapRow: { gap: 6 },
  // self-start max-w-[80%]
  failureOuter: { alignSelf: "flex-start", maxWidth: "80%" },
  blocked: { opacity: 0.5 },
  pillPressed: { opacity: 0.7 },
});

// Theme-dependent styles — THEME tokens are opaque hsl strings, so the
// NativeWind `bg-x/nn` translucent shades convert through withAlpha().
const styles = (c: ReturnType<typeof useThemeColors>) =>
  StyleSheet.create({
    // self-end max-w-[80%] gap-1.5 rounded-xl border-2 px-3.5 py-2
    userBubble: {
      alignSelf: "flex-end",
      maxWidth: "80%",
      gap: 6,
      borderRadius: 12,
      borderWidth: 2,
      borderColor: "transparent",
      paddingHorizontal: 14,
      paddingVertical: 8,
      ...continuousCorners,
    },
    // no_response italic notice — text-sm italic text-muted-foreground
    noResponseText: {
      fontSize: 14,
      fontStyle: "italic",
      color: c.mutedForeground,
    },
    // flex-row flex-wrap gap-2 pt-0.5
    quickActionsRow: {
      flexDirection: "row",
      flexWrap: "wrap",
      gap: 8,
      paddingTop: 2,
    },
    // min-h-10 max-w-full flex-row items-center gap-1 rounded-full border px-3
    quickActionPill: {
      minHeight: 40,
      maxWidth: "100%",
      flexDirection: "row",
      alignItems: "center",
      gap: 4,
      borderRadius: 999,
      borderWidth: 1,
      paddingHorizontal: 12,
      paddingVertical: 8,
    },
    // text-sm font-medium
    quickActionLabel: { fontSize: 14, fontWeight: "500", flexShrink: 1 },
    // text-xs text-muted-foreground/80 mt-1
    elapsedCaption: {
      fontSize: 12,
      color: withAlpha(c.mutedForeground, 0.8),
    },
    // rounded-xl border-2 bg-destructive/10 px-3.5 py-2
    failureBubble: {
      borderRadius: 12,
      borderWidth: 2,
      backgroundColor: withAlpha(c.destructive, 0.1),
      paddingHorizontal: 14,
      paddingVertical: 8,
      ...continuousCorners,
    },
    // text-xs font-semibold text-destructive
    failureLabel: { fontSize: 12, fontWeight: "600", color: c.destructive },
    // mt-1 flex-row items-center gap-1
    failureToggle: {
      marginTop: 4,
      flexDirection: "row",
      alignItems: "center",
      gap: 4,
    },
    // text-xs text-muted-foreground
    failureToggleLabel: { fontSize: 12, color: c.mutedForeground },
    // mt-1 rounded-xs bg-muted/40 px-2 py-1.5
    failureDetailBox: {
      marginTop: 4,
      borderRadius: 2,
      backgroundColor: withAlpha(c.muted, 0.4),
      paddingHorizontal: 8,
      paddingVertical: 6,
    },
    failureDetailText: { fontSize: 12, color: c.mutedForeground },
  });

export function ChatMessageList({
  messages,
  loading,
  hasSessions,
  agent,
  onPickPrompt,
  onQuickAction,
  quickActionsDisabled = false,
  pendingTask,
  liveTaskMessages,
  availability,
  menuMessageId = null,
  onMessageLongPress,
}: Props) {
  const c = useThemeColors();

  // Top-level selection subscription gates the outer "tap-outside-to-dismiss"
  // Pressable below. When null, the Pressable stays disabled and every tap
  // passes through to the list cells / bubble long-press wrappers normally.
  const selectingId = useChatSelectStore((st) => st.selectingId);

  // Manual scroll anchor replacing FlashList's maintainVisibleContentPosition.
  // Refs (not state) — scroll decisions must not re-render. Reset naturally
  // on session switch because the FlatList remounts via `key`.
  const listRef = useRef<FlatList<ChatMessage>>(null);
  const anchor = useRef({
    contentHeight: 0,
    nearBottom: true,
    landed: false,
  });

  const handleScroll = useCallback(
    (e: NativeSyntheticEvent<NativeScrollEvent>) => {
      const { contentSize, contentOffset, layoutMeasurement } = e.nativeEvent;
      const a = anchor.current;
      a.contentHeight = contentSize.height;
      const distanceFromBottom =
        contentSize.height - contentOffset.y - layoutMeasurement.height;
      a.nearBottom =
        distanceFromBottom <
        layoutMeasurement.height * NEAR_BOTTOM_THRESHOLD;
    },
    [],
  );

  const handleContentSizeChange = useCallback((_: number, h: number) => {
    const a = anchor.current;
    const grew = h > a.contentHeight;
    a.contentHeight = h;
    if (!a.landed) {
      // Initial paint of this session → land at the newest message.
      a.landed = true;
      requestAnimationFrame(() =>
        listRef.current?.scrollToEnd({ animated: false }),
      );
      return;
    }
    if (grew && a.nearBottom) {
      // New arrivals while the reader is anchored near the bottom; history
      // readers (further than the threshold up) are never yanked down.
      requestAnimationFrame(() =>
        listRef.current?.scrollToEnd({ animated: true }),
      );
    }
  }, []);

  const clearSelection = useCallback(() => {
    useChatSelectStore.getState().clear();
  }, []);

  if (loading && messages.length === 0) {
    return (
      <View style={layout.loadingWrap}>
        <ActivityIndicator color={c.mutedForeground} />
      </View>
    );
  }

  if (messages.length === 0) {
    // Empty new-chat state. Lives here (rather than the parent screen) so
    // the empty state and the rendered list share spacing/layout rules.
    return (
      <ChatEmptyState
        hasSessions={hasSessions}
        agent={agent}
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
    // through to the list cells. Scroll gestures are unaffected (Pressable
    // only intercepts non-drag taps).
    <Pressable
      onPress={
        selectingId ? () => useChatSelectStore.getState().clear() : undefined
      }
      disabled={!selectingId}
      style={layout.flex}
    >
      {/* `key` on first message id forces remount on session switch so the
          scroll anchor re-fires and we land at the new session's bottom
          (instead of inheriting the previous session's scroll position).
          Cheap because sessions are switched, not re-rendered every
          keystroke. */}
      <FlatList
        ref={listRef}
        key={messages[0]?.id ?? "empty"}
        data={messages}
        keyExtractor={(m) => m.id}
        renderItem={({ item }) => (
          <MessageRow
            message={item}
            onQuickAction={onQuickAction}
            quickActionsDisabled={quickActionsDisabled}
            highlighted={menuMessageId === item.id}
            onLongPress={onMessageLongPress}
          />
        )}
        ItemSeparatorComponent={MessageSeparator}
        ListFooterComponent={
          showLiveSection ? (
            <View style={layout.footer}>
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
        onScroll={handleScroll}
        onContentSizeChange={handleContentSizeChange}
        scrollEventThrottle={32}
        contentContainerStyle={layout.content}
        // Any user-initiated scroll exits message text-selection mode —
        // matches iMessage's behavior where scrolling implicitly commits /
        // dismisses the selection caret. Hooks both drag-start and the
        // momentum kick after a flick so a fast scroll can't escape.
        onScrollBeginDrag={clearSelection}
        onMomentumScrollBegin={clearSelection}
        // iMessage-style keyboard dismissal: dragging the list pulls the
        // keyboard down ("interactive" is iOS-only; "on-drag" is the
        // platform-agnostic equivalent). Tapping empty space between
        // bubbles dismisses it. `handled` keeps Pressables inside bubbles
        // (long-press menu etc.) firing normally.
        keyboardDismissMode="on-drag"
        keyboardShouldPersistTaps="handled"
      />
    </Pressable>
  );
}

function MessageSeparator() {
  return <View style={layout.separator} />;
}

function MessageRow({
  message,
  onQuickAction,
  quickActionsDisabled,
  highlighted,
  onLongPress,
}: {
  message: ChatMessage;
  onQuickAction?: (action: ChatQuickAction) => void | Promise<unknown>;
  quickActionsDisabled: boolean;
  highlighted: boolean;
  onLongPress?: (message: ChatMessage) => void;
}) {
  const c = useThemeColors();
  const s = styles(c);
  const isUser = message.role === "user";
  const isFailure = !!message.failure_reason;
  const isSelecting = useChatSelectStore(
    (st) => st.selectingId === message.id,
  );
  // Highlight ring while the long-press action sheet is on screen for this
  // bubble (iOS: the hook's transient `isPressed` state).
  const isPressed = highlighted && !!onLongPress;

  if (isFailure) {
    return (
      <FailureBubble
        reasonLabel={failureReasonLabel(message.failure_reason)}
        rawError={message.content}
        elapsedMs={message.elapsed_ms ?? null}
        isSelecting={isSelecting}
        isPressed={isPressed}
        onLongPress={() => onLongPress?.(message)}
      />
    );
  }

  if (isUser) {
    // User bubble: same Markdown pipeline as assistant — `@mention`
    // serialisation `[MUL-1](mention://issue/<id>)`, inline links, and
    // inline code resolve identically to web's user branch. Width is
    // capped at 80% so the bubble keeps the iMessage-style trailing
    // alignment instead of stretching across the column.
    const body = (
      <View
        style={[
          s.userBubble,
          { backgroundColor: c.muted },
          isSelecting
            ? {
                // Select-mode cue: primary-tinted bg + border.
                backgroundColor: withAlpha(c.primary, 0.05),
                borderColor: withAlpha(c.primary, 0.3),
              }
            : isPressed
              ? // Long-press cue: border only, bg stays muted.
                { borderColor: withAlpha(c.primary, 0.3) }
              : null,
        ]}
      >
        <Markdown
          content={message.content}
          attachments={message.attachments}
          selectable={isSelecting}
          compact
        />
        <CommentAttachmentList
          attachments={message.attachments}
          content={message.content}
        />
      </View>
    );
    if (isSelecting) return body;
    return (
      <Pressable
        onLongPress={() => onLongPress?.(message)}
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
      onLongPress={() => onLongPress?.(message)}
      onQuickAction={onQuickAction}
      quickActionsDisabled={quickActionsDisabled}
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
 * chat-message-list.tsx — same shape, simplified for RN (no inner Copy
 * button — the long-press menu already exposes Copy, and selection mode
 * owns the highlight, so a Copy affordance would be redundant on mobile).
 * Like iOS, the assistant branch has no pressed-border baseline (its bubble
 * has no shell; a 2px baseline would shift layout per message).
 */
function AssistantRow({
  message,
  isSelecting,
  onLongPress,
  onQuickAction,
  quickActionsDisabled,
}: {
  message: ChatMessage;
  isSelecting: boolean;
  onLongPress: () => void;
  onQuickAction?: (action: ChatQuickAction) => void | Promise<unknown>;
  quickActionsDisabled: boolean;
}) {
  const c = useThemeColors();
  // Read the cached timeline if any. `enabled` (in taskMessagesOptions) is
  // gated on isTaskMessageTaskId — optimistic id prefixes never fetch, so
  // freshly-sent messages don't spam the API while waiting for the real
  // task_id to land. Cached cells (after live timeline finished) return
  // synchronously with no network roundtrip.
  const { data: timeline = [] } = useQuery(
    taskMessagesOptions(message.task_id),
  );
  // no_response (MUL-4351, mirrors packages/views AssistantMessage): the
  // agent completed this turn without text. Keep the tool timeline and show
  // a notice instead of an empty Markdown block; caption reads "Finished in"
  // not "Replied in".
  const isNoResponse = message.message_kind === "no_response";
  const body = (
    <View style={layout.gapRow}>
      {timeline.length > 0 ? <ChatTimeline items={timeline} /> : null}
      {isNoResponse ? (
        <Text style={styles(c).noResponseText}>
          The agent finished this turn without a text reply.
        </Text>
      ) : (
        <Markdown
          content={message.content}
          attachments={message.attachments}
          selectable={isSelecting}
        />
      )}
      {/* Standalone attachment cards for anything not referenced inline. An
          image-only reply is a real ('message') outcome with empty content, so
          it flows through the else-branch above (renders nothing) and the cards
          here ARE the reply. */}
      <CommentAttachmentList
        attachments={message.attachments}
        content={message.content}
      />
      {message.elapsed_ms != null ? (
        <ElapsedCaption
          variant={isNoResponse ? "finished" : "replied"}
          elapsedMs={message.elapsed_ms}
        />
      ) : null}
    </View>
  );
  // Select-mode drops the Pressable wrapper so the platform text-selection
  // gesture never races the long-press handler (same boundary as iOS B6).
  const messageBody = isSelecting ? (
    body
  ) : (
    <Pressable onLongPress={onLongPress} delayLongPress={500}>
      {body}
    </Pressable>
  );
  if (!onQuickAction || (message.quick_actions?.length ?? 0) === 0) {
    return messageBody;
  }
  return (
    <View style={layout.gapRow}>
      {messageBody}
      <QuickActions
        actions={message.quick_actions ?? []}
        disabled={quickActionsDisabled}
        onSelect={onQuickAction}
      />
    </View>
  );
}

function QuickActions({
  actions,
  disabled,
  onSelect,
}: {
  actions: ChatQuickAction[];
  disabled: boolean;
  onSelect: (action: ChatQuickAction) => void | Promise<unknown>;
}) {
  const c = useThemeColors();
  const s = styles(c);
  const [submitting, setSubmitting] = useState(false);
  const blocked = disabled || submitting;

  const handleSelect = async (action: ChatQuickAction) => {
    if (blocked) return;
    setSubmitting(true);
    try {
      await onSelect(action);
    } catch {
      // The send path rolls back its optimistic message. Keep the action
      // usable so a transient request failure can be retried.
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <View style={s.quickActionsRow} accessibilityLabel="Suggested follow-ups">
      {actions.slice(0, 3).map((action, index) => (
        <Pressable
          key={`${action.label}-${index}`}
          accessibilityRole="button"
          accessibilityState={{ disabled: blocked }}
          disabled={blocked}
          onPress={() => void handleSelect(action)}
          style={({ pressed }) => [
            s.quickActionPill,
            action.primary
              ? {
                  borderColor: withAlpha(c.primary, 0.3),
                  backgroundColor: withAlpha(c.primary, 0.1),
                }
              : { borderColor: c.border, backgroundColor: c.background },
            blocked ? layout.blocked : null,
            pressed && !blocked ? layout.pillPressed : null,
          ]}
        >
          <Text
            numberOfLines={1}
            style={[
              s.quickActionLabel,
              { color: action.primary ? c.primary : c.foreground },
            ]}
          >
            {action.label}
          </Text>
          {action.primary ? (
            <Text style={[s.quickActionLabel, { color: c.primary }]}>↗</Text>
          ) : null}
        </Pressable>
      ))}
    </View>
  );
}

// Persistent caption rendered under the assistant bubble / failure bubble
// once the server has written `elapsed_ms`. Server computes once at task
// completion, so this caption is identical across reloads and clients.
function ElapsedCaption({
  variant,
  elapsedMs,
}: {
  variant: "replied" | "failed" | "finished";
  elapsedMs: number;
}) {
  const c = useThemeColors();
  const label =
    variant === "replied"
      ? `Replied in ${formatElapsedMs(elapsedMs)}`
      : variant === "finished"
        ? `Finished in ${formatElapsedMs(elapsedMs)}`
        : `Failed after ${formatElapsedMs(elapsedMs)}`;
  return <Text style={styles(c).elapsedCaption}>{label}</Text>;
}

function FailureBubble({
  reasonLabel,
  rawError,
  elapsedMs,
  isSelecting,
  isPressed,
  onLongPress,
}: {
  reasonLabel: string;
  rawError: string;
  elapsedMs: number | null;
  isSelecting: boolean;
  isPressed: boolean;
  onLongPress: () => void;
}) {
  const c = useThemeColors();
  const s = styles(c);
  const hasRawError = rawError.trim().length > 0;

  // B6: pass `selectable={isSelecting}` rather than hard-coding
  // `selectable` — otherwise the platform text-selection gesture pre-empts
  // our long-press handler and the menu never fires. Select-mode cue is the
  // border-tint to primary; bg stays destructive so the failure signal is
  // never lost.
  const body = (
    <View style={layout.failureOuter}>
      <View
        style={[
          s.failureBubble,
          isSelecting || isPressed
            ? { borderColor: withAlpha(c.primary, 0.3) }
            : { borderColor: withAlpha(c.destructive, 0.3) },
        ]}
      >
        <Text style={s.failureLabel}>{reasonLabel}</Text>
        {hasRawError ? (
          <Collapsible>
            <CollapsibleTrigger
              style={s.failureToggle}
              accessibilityRole="button"
              accessibilityLabel="Show error details"
            >
              <Icon
                name="chevron-forward"
                size={12}
                color={c.mutedForeground}
              />
              <Text style={s.failureToggleLabel}>Show details</Text>
            </CollapsibleTrigger>
            <CollapsibleContent>
              <View style={s.failureDetailBox}>
                <Text style={s.failureDetailText} selectable={isSelecting}>
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
    <Pressable onLongPress={onLongPress} delayLongPress={500}>
      {body}
    </Pressable>
  );
}
