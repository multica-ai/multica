/**
 * Three-state presence dot — port of apps/mobile/components/ui/presence-
 * dot.tsx, driving the agent availability indicator. Mirror of web's
 * AgentStatusDot (packages/views/common/actor-avatar.tsx) with one platform
 * tweak: React Native has no `ring-*` utility, so the "cut out the avatar
 * background" effect uses a 2px solid border in the background colour.
 *
 * Color mapping is identical to the web `availabilityConfig`
 * (packages/views/agents/presence.ts):
 *   online   → success         (green)
 *   unstable → warning         (amber) — runtime offline < 5 min
 *   offline  → mutedForeground/40 (gray)
 *   archived → mutedForeground/40 (gray, retired agent)
 *
 * Pure presentation. Caller passes the already-derived `AgentAvailability`
 * (on iOS typically from useAgentPresence — that hook is not ported yet).
 * Loading states are handled at the call site — this component always
 * renders. The translucent gray goes through withAlpha() because THEME
 * tokens are opaque hsl strings.
 */
import { StyleSheet, View } from "react-native";
import type { AgentAvailability } from "@multica/core/agents";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

interface Props {
  availability: AgentAvailability;
  /** Diameter in pt. Default 8 matches the standard avatar-corner dot. */
  size?: number;
}

export function PresenceDot({ availability, size = 8 }: Props) {
  const c = useThemeColors();
  const color =
    availability === "online"
      ? c.success
      : availability === "unstable"
        ? c.warning
        : withAlpha(c.mutedForeground, 0.4);

  return (
    <View
      style={[
        {
          width: size,
          height: size,
          borderRadius: size / 2,
          backgroundColor: color,
        },
        styles.cutout,
      ]}
    />
  );
}

// border-2 border-background — the background-colour ring that reads as a
// cutout against whatever surface the dot overlaps.
const styles = StyleSheet.create({
  cutout: { borderWidth: 2, borderCurve: "circular" },
});
