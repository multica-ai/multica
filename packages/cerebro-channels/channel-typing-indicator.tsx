"use client";

// TECH-3664 — Slack-style "is typing…" line shown inside a channel / DM
// conversation when another participant is composing a reply. The live
// `typingUserIds` come from `useChannelTyping` (@multica/core/presence) in the
// host (channel-detail); this component is purely presentational. Gated by the
// `cerebro_typing_indicators` flag. Renders nothing when no one else is typing.
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useActorName } from "@multica/core/workspace/hooks";

/** Three bouncing dots, Slack-style (pure CSS via Tailwind's animate-bounce). */
function TypingDots() {
  return (
    <span className="inline-flex items-center gap-0.5" aria-hidden="true">
      <span className="size-1 animate-bounce rounded-full bg-current [animation-delay:-0.3s]" />
      <span className="size-1 animate-bounce rounded-full bg-current [animation-delay:-0.15s]" />
      <span className="size-1 animate-bounce rounded-full bg-current" />
    </span>
  );
}

export interface ChannelTypingIndicatorProps {
  /** Member user IDs currently typing here (self already excluded by the hook). */
  typingUserIds: Set<string>;
}

/** In-conversation "X is typing…" line. Place it just above the composer. */
export function ChannelTypingIndicator({ typingUserIds }: ChannelTypingIndicatorProps) {
  const enabled = useFeatureFlag("cerebro_typing_indicators");
  const { getMemberName } = useActorName();

  if (!enabled || typingUserIds.size === 0) return null;

  const names = [...typingUserIds].map((id) => getMemberName(id));
  let label: string;
  if (names.length === 1) label = `${names[0]} is typing`;
  else if (names.length === 2) label = `${names[0]} and ${names[1]} are typing`;
  else label = "Several people are typing";

  return (
    <div
      className="flex items-center gap-1.5 px-3 py-1 text-xs italic text-muted-foreground"
      aria-live="polite"
    >
      <TypingDots />
      <span className="truncate">{label}…</span>
    </div>
  );
}
