/**
 * HarmonyOS port of apps/mobile/components/ui/priority-icon.tsx.
 *
 * Geometry mirrors packages/views/issues/components/priority-icon.tsx
 * (16×16 viewBox, 4 ascending bars, "none" rendered as a center dash). Bar
 * counts mirror packages/core/issues/config/priority.ts PRIORITY_CONFIG.bars
 * — behavioral parity rule: same priority → same number of filled bars
 * across clients.
 *
 * react-native-svg is not wired on this platform (renders blank), so the
 * bars are rounded Views positioned on the same 16-unit grid (k = size/16);
 * the "none" dash is a rounded bar. As on iOS, there is no urgent pulse
 * animation in v1 (defer until animation polish iteration).
 */
import { StyleSheet, View } from "react-native";
import type { IssuePriority } from "@multica/core/types";

const BARS: Record<IssuePriority, number> = {
  urgent: 4,
  high: 3,
  medium: 2,
  low: 1,
  none: 0,
};

// Mirrors PRIORITY_CONFIG.color in packages/core/issues/config/priority.ts.
const COLOR: Record<IssuePriority, string> = {
  urgent: "#dc2626", // destructive
  high: "#eab308", // warning
  medium: "#eab308", // warning
  low: "#3b82f6", // info
  none: "#71717a", // muted-foreground
};

// viewBox-16 unit constants: bars 3 wide with a 1-unit gap starting at x=1,
// bottoms on y=12, heights 3/6/9/12; stroke width 1.5.
const BAR_W = 3;
const BAR_X0 = 1;
const BAR_PITCH = 4;
const BAR_BASE = 12;
const STROKE = 1.5;

export function PriorityIcon({
  priority,
  size = 14,
}: {
  priority: IssuePriority;
  size?: number;
}) {
  const k = size / 16;
  if (priority === "none") {
    return (
      <View style={[styles.canvas, { width: size, height: size }]}>
        {/* Center dash: viewBox line (3,8)→(13,8), stroke 1.5. */}
        <View
          style={{
            position: "absolute",
            left: 3 * k,
            top: (8 - STROKE / 2) * k,
            width: 10 * k,
            height: STROKE * k,
            borderRadius: (STROKE / 2) * k,
            backgroundColor: COLOR.none,
          }}
        />
      </View>
    );
  }

  const filled = BARS[priority];
  const color = COLOR[priority];

  return (
    <View style={[styles.canvas, { width: size, height: size }]}>
      {[0, 1, 2, 3].map((i) => {
        const h = (i + 1) * 3;
        return (
          <View
            key={i}
            style={{
              position: "absolute",
              left: (BAR_X0 + i * BAR_PITCH) * k,
              top: (BAR_BASE - h) * k,
              width: BAR_W * k,
              height: h * k,
              borderRadius: 0.5 * k,
              backgroundColor: color,
              opacity: i < filled ? 1 : 0.2,
            }}
          />
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  canvas: { position: "relative" },
});
