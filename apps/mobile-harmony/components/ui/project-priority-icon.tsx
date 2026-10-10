/**
 * HarmonyOS port of apps/mobile/components/ui/project-priority-icon.tsx —
 * reuses the same 4-bar geometry as the issue PriorityIcon. Project
 * priority enum is identical to the issue priority enum
 * (urgent/high/medium/low/none), so visually identical bars communicate
 * the same meaning across surfaces — desirable for behavioral parity.
 *
 * Colors are kept identical to the issue PriorityIcon hex map.
 * react-native-svg is not wired on this platform (renders blank), so the
 * bars are rounded Views positioned on the same 16-unit grid
 * (k = size / 16).
 */
import { StyleSheet, View } from "react-native";
import type { ProjectPriority } from "@multica/core/types";
import { projectPriorityBars } from "@/lib/project-status";

const COLOR: Record<ProjectPriority, string> = {
  urgent: "#dc2626",
  high: "#eab308",
  medium: "#eab308",
  low: "#3b82f6",
  none: "#71717a",
};

function colorFor(priority: string): string {
  return (COLOR as Record<string, string>)[priority] ?? COLOR.none;
}

// Same viewBox-16 grid as the issue PriorityIcon.
const BAR_W = 3;
const BAR_X0 = 1;
const BAR_PITCH = 4;
const BAR_BASE = 12;
const STROKE = 1.5;

export function ProjectPriorityIcon({
  priority,
  size = 14,
}: {
  priority: ProjectPriority | string;
  size?: number;
}) {
  const filled = projectPriorityBars(priority);
  const color = colorFor(priority);
  const k = size / 16;

  if (filled === 0) {
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
            backgroundColor: color,
          }}
        />
      </View>
    );
  }

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
