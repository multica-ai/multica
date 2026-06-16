/**
 * Three-state presence dot — drives the agent availability indicator across
 * the app. Mirror of web's AgentStatusDot (`packages/views/common/actor-avatar.tsx`)
 * and its `availabilityConfig` (`packages/views/agents/presence.ts`).
 *
 * Slack-style dot (TECH-3686): every state is a filled centre on a thin grey
 * outline ring (`border-2 border-muted-foreground`). State reads from the
 * centre fill, not an alarming colour — inactive is hollow, never red:
 *   online   → success    (green centre, grey ring)
 *   unstable → warning    (amber centre, grey ring) — runtime offline < 5 min
 *   paused   → background  (empty/hollow centre, grey ring) — intentionally paused
 *   offline  → background  (empty/hollow centre, grey ring)
 *   archived → background  (empty/hollow centre, grey ring) — retired agent
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
  // Inactive states (paused/offline/archived) are hollow — an empty (background)
  // centre on the shared grey ring, the Slack away look. No red. Mirrors web's
  // availabilityConfig in packages/views/agents/presence.ts.
  paused: "bg-background",
  unstable: "bg-warning",
  offline: "bg-background",
  archived: "bg-background",
};

export function PresenceDot({ availability, size = 8 }: Props) {
  return (
    <View
      style={{ width: size, height: size, borderRadius: size / 2 }}
      className={cn(
        "border-2 border-muted-foreground",
        DOT_CLASS[availability],
      )}
    />
  );
}
