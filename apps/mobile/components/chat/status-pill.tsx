/**
 * In-flight task status — mobile-side mirror of
 * `packages/views/chat/components/task-status-pill.tsx`.
 *
 * Visual choices match web's intent ("diagnostic inline text, not a
 * notification chip") adapted for RN:
 *
 *   - No chrome. No border, no background, no rounded-full pill. Just a
 *     line of muted text that lives at the end of the message stream.
 *   - "Breathing dots" instead of CSS shimmer. RN can't do
 *     `background-clip: text` gradient sweeps (web's
 *     `animate-chat-text-shimmer`), so we use the next-best activity cue:
 *     three small dots fading in/out with a staggered phase. Same
 *     "AI is alive" signal as iMessage's typing dots / ChatGPT iOS's
 *     thinking indicator.
 *   - No Stop button inline. The composer already swaps Send → Stop
 *     while `sending===true` (chat-composer.tsx). A second Stop here
 *     was redundant chrome.
 *
 * Stage logic (queued / dispatched / running × taskMessages → stage label)
 * mirrors web's `pickStageKeys` exactly — same priority order, same
 * fallback. Differences are visual-only.
 */
import { useEffect, useRef, useState } from "react";
import { View, type TextStyle } from "react-native";
import Animated, {
  cancelAnimation,
  useAnimatedStyle,
  useSharedValue,
  withRepeat,
  withSequence,
  withTiming,
  type SharedValue,
} from "react-native-reanimated";
import type {
  ChatPendingTask,
  TaskMessagePayload,
} from "@multica/core/types";
import type { AgentAvailability } from "@multica/core/agents";
import { Text } from "@/components/ui/text";
import { formatElapsedSecs } from "@/lib/format-elapsed";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { useT } from "@/lib/i18n";

interface Props {
  pendingTask: ChatPendingTask | null | undefined;
  taskMessages?: readonly TaskMessagePayload[];
  /** Resolved presence; pass `undefined` to suppress availability hints
   *  during loading so the line never flashes "Offline" speculatively. */
  availability?: AgentAvailability;
}

interface Stage {
  label: string;
  /** True for static labels (e.g. "Offline") where the breathing dots
   *  shouldn't animate — there's nothing for the user to wait on. */
  static?: boolean;
}

const TOOL_LABELS: Record<string, string> = {
  bash: "chat:status.running_command",
  exec: "chat:status.running_command",
  read: "chat:status.reading_files",
  glob: "chat:status.reading_files",
  grep: "chat:status.searching_code",
  write: "chat:status.making_edits",
  edit: "chat:status.making_edits",
  multi_edit: "chat:status.making_edits",
  multiedit: "chat:status.making_edits",
  web_search: "chat:status.searching_web",
  websearch: "chat:status.searching_web",
};

function pickStage(
  status: string | undefined,
  taskMessages: readonly TaskMessagePayload[],
  availability: AgentAvailability | undefined,
  t: (key: string) => string,
): Stage {
  // Mirrors web: deferred is an older turn waiting for retry backoff, not
  // active model work, so it must not fall through to "Thinking".
  if (status === "deferred") return { label: t("chat:status.retrying") };
  if (
    (status === "queued" || status === "dispatched") &&
    availability === "offline"
  ) {
    return { label: t("chat:status.offline"), static: true };
  }
  if (
    (status === "queued" || status === "dispatched") &&
    availability === "unstable"
  ) {
    return { label: t("chat:status.reconnecting") };
  }
  if (status === "queued") return { label: t("chat:status.queued") };
  if (status === "dispatched") return { label: t("chat:status.starting") };

  let latest: TaskMessagePayload | null = null;
  for (let i = taskMessages.length - 1; i >= 0; i--) {
    const m = taskMessages[i];
    if (m && m.type !== "error" && m.type !== "tool_result") {
      latest = m;
      break;
    }
  }
  if (!latest) return { label: t("chat:status.thinking") };
  if (latest.type === "thinking") return { label: t("chat:status.thinking") };
  if (latest.type === "text") return { label: t("chat:status.typing") };
  if (latest.type === "tool_use") {
    const slug = (latest.tool ?? "").toLowerCase();
    const key = TOOL_LABELS[slug];
    return { label: key ? t(key) : t("chat:status.working") };
  }
  return { label: t("chat:status.thinking") };
}

// Tabular figures for the 1Hz counter — proportional digits change the text
// width on 9s → 10s, which reflows the whole row once a second.
//
// This CANNOT be `className="tabular-nums"`. Tailwind compiles that utility to
// `font-variant-numeric`, and react-native-css-interop's property allow-list
// only carries `font-variant-caps` — the declaration is dropped, so the class
// is a silent no-op on RN. `fontVariant` is the working equivalent. Hoisted so
// the once-a-second re-render doesn't hand Text a fresh style object.
const TABULAR_NUMS: TextStyle = { fontVariant: ["tabular-nums"] };

export function StatusPill({
  pendingTask,
  taskMessages = [],
  availability,
}: Props) {
  const taskId = pendingTask?.task_id;
  const createdAt = pendingTask?.created_at;
  const { t } = useT("chat");

  // Anchor — locked per task. Reset on task_id change so a new run
  // restarts the timer from 0; mid-run we never reassign, otherwise the
  // counter would visibly snap backwards when a server `created_at`
  // arrives a few hundred ms before the optimistic `Date.now()` anchor.
  // (Stored in a `useEffect`-driven mutable; useRef would also work but
  // we already touch state on tick, so a tiny extra hook is fine.)
  const anchorMs = useTaskAnchor(taskId, createdAt);

  // 1Hz tick — the only reason this hook exists is to force a re-render
  // every second. We don't read the tick value; we read Date.now() at
  // render time.
  useTick(!!taskId, 1000);

  if (!taskId) return null;

  // Deferred retries retain task messages from the earlier attempt, so the
  // newer server status must win over those stale running hints.
  const status =
    pendingTask?.status === "deferred"
      ? "deferred"
      : taskMessages.length > 0
        ? "running"
        : pendingTask?.status;
  const elapsedSec = Math.max(0, Math.floor((Date.now() - anchorMs) / 1000));
  const stage = pickStage(status, taskMessages, availability, t);

  return (
    <View
      className="flex-row items-center gap-1.5 px-1"
      accessibilityLiveRegion="polite"
    >
      {stage.static ? null : <BreathingDots />}
      <Text className="text-xs text-muted-foreground" numberOfLines={1}>
        {stage.label}
        <Text className="text-xs text-muted-foreground/70" style={TABULAR_NUMS}>
          {" · "}
          {formatElapsedSecs(elapsedSec)}
        </Text>
      </Text>
    </View>
  );
}

// ─── helpers ──────────────────────────────────────────────────────────────

function useTaskAnchor(
  taskId: string | undefined,
  createdAt: string | undefined,
): number {
  const ref = useRef<{ id: string | undefined; ms: number }>({
    id: undefined,
    ms: Date.now(),
  });
  if (ref.current.id !== taskId) {
    const t = createdAt ? Date.parse(createdAt) : NaN;
    ref.current = {
      id: taskId,
      ms: Number.isFinite(t) ? t : Date.now(),
    };
  }
  return ref.current.ms;
}

function useTick(enabled: boolean, intervalMs: number) {
  const [, setN] = useState(0);
  useEffect(() => {
    if (!enabled) return;
    const id = setInterval(() => setN((n) => n + 1), intervalMs);
    return () => clearInterval(id);
  }, [enabled, intervalMs]);
}

// Three small dots, fading in/out on a staggered phase — same "in
// progress" affordance iMessage uses for typing indicators. Each dot
// owns its own SharedValue; the second and third are kicked off via
// setTimeout (150ms / 300ms) so the wave reads as motion rather than
// flicker.
function BreathingDots() {
  const { colorScheme } = useColorScheme();
  const tint = THEME[colorScheme].mutedForeground;
  const d1 = useSharedValue(0.3);
  const d2 = useSharedValue(0.3);
  const d3 = useSharedValue(0.3);

  useEffect(() => {
    const start = (v: SharedValue<number>) => {
      v.value = withRepeat(
        withSequence(
          withTiming(1, { duration: 400 }),
          withTiming(0.3, { duration: 400 }),
        ),
        -1,
      );
    };
    start(d1);
    const t2 = setTimeout(() => start(d2), 150);
    const t3 = setTimeout(() => start(d3), 300);
    return () => {
      clearTimeout(t2);
      clearTimeout(t3);
      cancelAnimation(d1);
      cancelAnimation(d2);
      cancelAnimation(d3);
    };
  }, [d1, d2, d3]);

  const s1 = useAnimatedStyle(() => ({ opacity: d1.value }));
  const s2 = useAnimatedStyle(() => ({ opacity: d2.value }));
  const s3 = useAnimatedStyle(() => ({ opacity: d3.value }));

  return (
    <View className="flex-row items-center gap-0.5">
      <Animated.View
        style={[s1, { backgroundColor: tint }]}
        className="h-1 w-1 rounded-full"
      />
      <Animated.View
        style={[s2, { backgroundColor: tint }]}
        className="h-1 w-1 rounded-full"
      />
      <Animated.View
        style={[s3, { backgroundColor: tint }]}
        className="h-1 w-1 rounded-full"
      />
    </View>
  );
}
