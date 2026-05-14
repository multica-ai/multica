/**
 * Chat message list — user / assistant bubbles, oldest at top, newest at
 * bottom. Auto-scrolls to the bottom when the list length increases (new
 * message arrived or optimistic send seeded the cache).
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
 *     markdown renderer. See plan-velvety-puddle.md "v2 follow-up".
 *
 * Layout uses a plain FlatList (mobile baseline — no FlashList — see
 * `components/issue/timeline-list.tsx:7`).
 */
import { useEffect, useRef } from "react";
import { ActivityIndicator, FlatList, View } from "react-native";
import type { ChatMessage } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Markdown } from "@/lib/markdown";
import { failureReasonLabel } from "@/lib/failure-reason-label";

interface Props {
  messages: ChatMessage[];
  loading: boolean;
}

export function ChatMessageList({ messages, loading }: Props) {
  const listRef = useRef<FlatList<ChatMessage>>(null);
  const lastLenRef = useRef(messages.length);

  // Auto-scroll to end whenever the list grows. We don't do it on every
  // render — that would fight the user's manual scroll-up to read history.
  // Assignment runs BEFORE the conditional so the ref tracks actual length
  // on every render, not only when the conditional is false.
  useEffect(() => {
    const grew = messages.length > lastLenRef.current;
    lastLenRef.current = messages.length;
    if (!grew) return;
    // Defer one tick — FlatList content needs to lay out before the
    // scroll position is valid. Without the delay, scrollToEnd lands
    // on the previous content size.
    const id = setTimeout(() => {
      listRef.current?.scrollToEnd({ animated: true });
    }, 0);
    return () => clearTimeout(id);
  }, [messages.length]);

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
      <View className="flex-1 items-center justify-center px-6">
        <Text className="text-sm text-muted-foreground text-center">
          Start the conversation.
        </Text>
      </View>
    );
  }

  return (
    <FlatList
      ref={listRef}
      data={messages}
      keyExtractor={(m) => m.id}
      renderItem={({ item }) => <MessageRow message={item} />}
      contentContainerClassName="px-3 py-3 gap-2"
      onContentSizeChange={() => {
        // Initial mount: jump straight to the bottom without animation so
        // the user lands on the latest message, not history.
        if (messages.length > 0) {
          listRef.current?.scrollToEnd({ animated: false });
        }
      }}
    />
  );
}

function MessageRow({ message }: { message: ChatMessage }) {
  const isUser = message.role === "user";
  const isFailure = !!message.failure_reason;

  if (isFailure) {
    return (
      <View className="self-start max-w-[88%] rounded-2xl border border-destructive/30 bg-destructive/10 px-3 py-2">
        <Text className="text-xs font-semibold text-destructive">
          {failureReasonLabel(message.failure_reason)}
        </Text>
        {message.content ? (
          <Text className="text-sm text-foreground mt-1" selectable>
            {message.content}
          </Text>
        ) : null}
      </View>
    );
  }

  if (isUser) {
    // Plain text for user messages — markdown's MARKDOWN_STYLE colors are
    // calibrated for dark-on-light, which renders poorly against a
    // primary-colored bubble. Mention serialisation `[MUL-1](mention://…)`
    // shows as raw markdown text in the user's own message; this is the
    // explicit v1 trade-off (see plan-velvety-puddle.md). Assistant
    // messages still go through the rich Markdown pipeline below.
    return (
      <View className="self-end max-w-[88%] rounded-2xl bg-primary px-3 py-2">
        <Text className="text-sm text-primary-foreground" selectable>
          {message.content}
        </Text>
      </View>
    );
  }

  // Assistant
  return (
    <View className="self-start max-w-[88%]">
      <Markdown content={message.content} />
    </View>
  );
}
