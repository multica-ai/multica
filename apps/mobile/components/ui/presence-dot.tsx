/**
 * Three-state presence dot — drives the agent availability indicator across
 * the app. Mirror of web's AgentStatusDot (`packages/views/common/actor-avatar.tsx:188`)
 * with one platform tweak: React Native has no `ring-*` utility, so the
 * "cut out the avatar background" effect uses `border-2 border-background`
 * (visually equivalent — 1.5–2 px solid border in the background colour).
 *
 * Color mapping is identical to the web `availabilityConfig`
 * (`packages/views/agents/presence.ts:46`):
 *   online   → success         (green)
 *   paused   → brand           (brand tone) — runtime intentionally paused
 *   unstable → warning         (amber) — runtime offline < 5 min
 *   offline  → muted/40        (gray)
 *
 * Pure presentation. Caller passes the already-derived `AgentAvailability`
 * (typically from `useAgentPresence`). Loading states are handled at the
 * call site — this component always renders.
 */
import { View } from "react-native";
import type { AgentAvailability } from "@multica/core/agents";
import { cn } from "@/lib/utils";

interface Props {
  availability: AgentAvailability;
  /** Diameter in pt. Default 8 matches the standard avatar-corner dot. */
  size?: number;
}

const DOT_CLASS: Record<AgentAvailability, string> = {
  online: "bg-success",
  // Inactive states (paused/offline/archived) read red, not brand/grey, so
  // online (green) vs not-online (red) is unmistakable. Mirrors web's
  // availabilityConfig in packages/views/agents/presence.ts.
  paused: "bg-destructive",
  unstable: "bg-warning",
  offline: "bg-destructive",
  // Retired agent (agent.archived_at set) — red, mirrors web's archived dot.
  archived: "bg-destructive",
};

export function PresenceDot({ availability, size = 8 }: Props) {
  return (
    <View
      style={{ width: size, height: size, borderRadius: size / 2 }}
      className={cn(
        "border-2 border-background",
        DOT_CLASS[availability],
      )}
    />
  );
}
