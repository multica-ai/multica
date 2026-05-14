/**
 * Bottom-sticky chat input. Mirrors the structure of
 * `components/issue/comment-composer.tsx` (same mention + markdown toolbar
 * + send pattern) but with two chat-specific differences:
 *
 *   1. No `replyingTo` chip — chat is a flat conversation, not a thread.
 *   2. No file/image buttons — v1 cuts file upload (see plan); we wire
 *      MarkdownToolbar without `onImage` / `onFile` so the buttons hide.
 *   3. Send button flips to a Stop button while `sending===true`, giving
 *      the user a single mid-row affordance for "interrupt the agent".
 *
 * Draft persistence is delegated to the caller — the chat screen owns
 * useChatDraftsStore and feeds `value` + `onChangeText` through here.
 * Keeps this component stateless w.r.t. session id (composer doesn't
 * need to know which session it's typing into).
 */
import { Pressable, TextInput, View } from "react-native";
import Svg, { Path } from "react-native-svg";
import { MOBILE_PLACEHOLDER_COLOR } from "@/components/ui/input-tokens";
import { MarkdownToolbar } from "@/components/editor/markdown-toolbar";
import { useMentionInput } from "@/lib/use-mention-input";
import { MentionSuggestionBar } from "@/components/issue/mention-suggestion-bar";
import { cn } from "@/lib/utils";
import { useEffect, useState } from "react";

interface Props {
  /** Current draft text (controlled). Empty string = no draft. */
  value: string;
  /** Fired on every keystroke. The caller writes to the drafts store. */
  onChangeText: (next: string) => void;
  /** Send the serialised markdown content. Caller resets the input by
   *  setting `value=""` after a successful send. */
  onSend: (content: string) => Promise<void> | void;
  /** Cancel the in-flight agent task. Only callable while `sending===true`. */
  onStop: () => void;
  /** True while an agent task is running for the active session. The
   *  composer still accepts typing (user can queue the next message) but
   *  swaps the Send button for a Stop button. */
  sending: boolean;
  /** Hard-disable typing + send. Used when there's no usable agent in the
   *  workspace or the session is archived (legacy). */
  disabled?: boolean;
  /** When `disabled` is true, replaces the placeholder with the reason. */
  disabledReason?: string;
}

export function ChatComposer({
  value,
  onChangeText,
  onSend,
  onStop,
  sending,
  disabled = false,
  disabledReason,
}: Props) {
  const mention = useMentionInput();
  const [focused, setFocused] = useState(false);

  // Drive the mention hook from the controlled `value`. When the parent
  // resets (post-send) or rehydrates a saved draft (post session-switch),
  // sync the internal text. We only push down — onChangeText is the upward
  // signal — to avoid an infinite ping-pong loop.
  useEffect(() => {
    if (mention.text !== value) {
      // Reset clears markers + selection, which is correct for both empty
      // and full draft hydration. Markers from a different session
      // shouldn't carry over.
      mention.reset();
      if (value) {
        mention.insertAtCursor(value);
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- text managed by mention
  }, [value]);

  const trimmed = mention.text.trim();
  const canSend = !disabled && !sending && trimmed.length > 0;

  const placeholder = disabled
    ? disabledReason ?? "Chat unavailable"
    : sending
      ? "Agent is working…"
      : "Message…";

  async function handleSend() {
    if (!canSend) return;
    const content = mention.serialize().trim();
    if (!content) return;
    // Optimistic clear — the parent's draft store mirrors `value` and will
    // see "" on the next onChangeText; the visual reset is immediate.
    onChangeText("");
    mention.reset();
    try {
      await onSend(content);
    } catch {
      // Restore the text so the user doesn't lose what they typed. We push
      // through onChangeText so the drafts store gets it too.
      onChangeText(content);
    }
  }

  return (
    <View className="border-t border-border bg-background">
      <MentionSuggestionBar {...mention.suggestionBar} />
      <MarkdownToolbar
        onAt={mention.handlers.onAtButtonPress}
        onList={() => mention.insertAtLineStart("- ")}
        onCheckbox={() => mention.insertAtLineStart("- [ ] ")}
        onCode={() => mention.insertAtCursor("\n```\n\n```", 4)}
        onQuote={() => mention.insertAtLineStart("> ")}
        disabled={disabled || sending}
      />
      <View className="px-3 py-2 flex-row items-end gap-1.5">
        <View
          className={cn(
            "flex-1 rounded-2xl border",
            focused
              ? "border-primary/30 bg-secondary"
              : "border-transparent bg-secondary",
            disabled && "opacity-60",
          )}
        >
          <TextInput
            value={mention.text}
            onChangeText={(next) => {
              mention.handlers.onChangeText(next);
              onChangeText(next);
            }}
            selection={mention.selection}
            onSelectionChange={mention.handlers.onSelectionChange}
            onFocus={() => setFocused(true)}
            onBlur={() => setFocused(false)}
            placeholder={placeholder}
            placeholderTextColor={MOBILE_PLACEHOLDER_COLOR}
            multiline
            className="px-4 py-2 text-base text-foreground max-h-32 min-h-8"
            editable={!disabled}
          />
        </View>
        {sending ? (
          <Pressable
            onPress={onStop}
            className="h-8 w-8 rounded-full items-center justify-center bg-foreground active:opacity-80"
            hitSlop={8}
            accessibilityLabel="Stop agent"
          >
            <View className="h-3 w-3 rounded-sm bg-background" />
          </Pressable>
        ) : canSend ? (
          <Pressable
            onPress={handleSend}
            className="h-8 w-8 rounded-full items-center justify-center bg-primary active:opacity-80"
            hitSlop={8}
            accessibilityLabel="Send"
          >
            <SendArrow />
          </Pressable>
        ) : null}
      </View>
    </View>
  );
}

function SendArrow() {
  return (
    <Svg width={16} height={16} viewBox="0 0 16 16" fill="none">
      <Path
        d="M8 13V3M8 3l-4 4M8 3l4 4"
        stroke="#fff"
        strokeWidth={1.8}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </Svg>
  );
}
