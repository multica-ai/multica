/**
 * In-flight task status — harmony-side port of
 * apps/mobile/components/chat/status-pill.tsx (itself a mirror of
 * packages/views/chat/components/task-status-pill.tsx).
 *
 * Visual choices match web's intent ("diagnostic inline text, not a
 * notification chip") adapted for RN:
 *
 *   - No chrome. No border, no background, no rounded-full pill. Just a
 *     line of muted text that lives at the end of the message stream.
 *   - "Breathing dots" instead of CSS shimmer. Three small dots fading
 *     in/out with a staggered phase — the same "AI is alive" signal as
 *     iMessage's typing dots.
 *   - No Stop button inline. The composer swaps Send → Stop while
 *     `sending===true` (chat-composer.tsx).
 *
 * Stage logic (queued / dispatched / running × taskMessages → stage label)
 * mirrors web's `pickStageKeys` exactly — same priority order, same
 * fallback. Differences are visual-only.
 *
 * Platform delta: react-native-reanimated is not wired into this app's
 * Babel pipeline (AGENTS.md — the matrix moves together), so the breathing
 * dots use core RN Animated loop/timing with the same 400ms ramp and
 * 150ms/300ms stagger.
 */
import { useEffect, useRef, useState } from "react";
import {
  Animated,
  StyleSheet,
  Text,
  View,
  type TextStyle,
} from "react-native";
import type {
  ChatPendingTask,
  TaskMessagePayload,
} from "@multica/core/types";
import type { AgentAvailability } from "@multica/core/agents";
import { formatElapsedSecs } from "@/lib/format-elapsed";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

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
  bash: "Running command",
  exec: "Running command",
  read: "Reading files",
  glob: "Reading files",
  grep: "Searching code",
  write: "Making edits",
  edit: "Making edits",
  multi_edit: "Making edits",
  multiedit: "Making edits",
  web_search: "Searching web",
  websearch: "Searching web",
};

function pickStage(
  status: string | undefined,
  taskMessages: readonly TaskMessagePayload[],
  availability: AgentAvailability | undefined,
): Stage {
  // Mirrors web: deferred is an older turn waiting for retry backoff, not
  // active model work, so it must not fall through to "Thinking".
  if (status === "deferred") return { label: "Retrying" };
  if (
    (status === "queued" || status === "dispatched") &&
    availability === "offline"
  ) {
    return { label: "Offline", static: true };
  }
  if (
    (status === "queued" || status === "dispatched") &&
    availability === "unstable"
  ) {
    return { label: "Reconnecting" };
  }
  if (status === "queued") return { label: "Queued" };
  if (status === "dispatched") return { label: "Starting up" };

  let latest: TaskMessagePayload | null = null;
  for (let i = taskMessages.length - 1; i >= 0; i--) {
    const m = taskMessages[i];
    if (m && m.type !== "error" && m.type !== "tool_result") {
      latest = m;
      break;
    }
  }
  if (!latest) return { label: "Thinking" };
  if (latest.type === "thinking") return { label: "Thinking" };
  if (latest.type === "text") return { label: "Typing" };
  if (latest.type === "tool_use") {
    const slug = (latest.tool ?? "").toLowerCase();
    return { label: TOOL_LABELS[slug] ?? "Working" };
  }
  return { label: "Thinking" };
}

// Tabular figures for the 1Hz counter — proportional digits change the text
// width on 9s → 10s, which reflows the whole row once a second. Hoisted so
// the once-a-second re-render doesn't hand Text a fresh style object.
const TABULAR_NUMS: TextStyle = { fontVariant: ["tabular-nums"] };

export function StatusPill({
  pendingTask,
  taskMessages = [],
  availability,
}: Props) {
  const c = useThemeColors();
  const taskId = pendingTask?.task_id;
  const createdAt = pendingTask?.created_at;

  // Anchor — locked per task. Reset on task_id change so a new run
  // restarts the timer from 0; mid-run we never reassign, otherwise the
  // counter would visibly snap backwards when a server `created_at`
  // arrives a few hundred ms before the optimistic `Date.now()` anchor.
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
  const stage = pickStage(status, taskMessages, availability);

  return (
    <View style={styles.row} accessibilityLiveRegion="polite">
      {stage.static ? null : <BreathingDots tint={c.mutedForeground} />}
      <Text style={[styles.label, { color: c.mutedForeground }]} numberOfLines={1}>
        {stage.label}
        <Text
          style={[styles.elapsed, { color: withAlpha(c.mutedForeground, 0.7) }]}
        >
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
// progress" affordance iMessage uses for typing indicators. The second and
// third dots start 150ms / 300ms late so the wave reads as motion rather
// than flicker.
function BreathingDots({ tint }: { tint: string }) {
  const dots = useRef<[Animated.Value, Animated.Value, Animated.Value]>([
    new Animated.Value(0.3),
    new Animated.Value(0.3),
    new Animated.Value(0.3),
  ]).current;

  useEffect(() => {
    const loop = (v: Animated.Value) =>
      Animated.loop(
        Animated.sequence([
          Animated.timing(v, {
            toValue: 1,
            duration: 400,
            useNativeDriver: true,
          }),
          Animated.timing(v, {
            toValue: 0.3,
            duration: 400,
            useNativeDriver: true,
          }),
        ]),
      );
    const anims = [loop(dots[0]), loop(dots[1]), loop(dots[2])];
    anims[0].start();
    const t2 = setTimeout(() => anims[1].start(), 150);
    const t3 = setTimeout(() => anims[2].start(), 300);
    return () => {
      clearTimeout(t2);
      clearTimeout(t3);
      anims.forEach((a) => a.stop());
    };
  }, [dots]);

  return (
    <View style={styles.dotsRow}>
      {dots.map((v, i) => (
        <Animated.View
          key={i}
          style={[
            styles.dot,
            { backgroundColor: tint, opacity: v },
          ]}
        />
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  // flex-row items-center gap-1.5 px-1
  row: { flexDirection: "row", alignItems: "center", gap: 6, paddingHorizontal: 4 },
  // text-xs text-muted-foreground
  label: { fontSize: 12 },
  // text-xs text-muted-foreground/70 + tabular figures
  elapsed: { fontSize: 12, fontVariant: ["tabular-nums"] },
  // flex-row items-center gap-0.5
  dotsRow: { flexDirection: "row", alignItems: "center", gap: 2 },
  // h-1 w-1 rounded-full
  dot: { width: 4, height: 4, borderRadius: 2 },
});
