/**
 * Mobile PriorityIcon — react-native-svg implementation.
 *
 * Geometry mirrors packages/views/issues/components/priority-icon.tsx
 * (16×16 viewBox, 4 ascending bars, "none" rendered as a center dash). Bar
 * counts mirror packages/core/issues/config/priority.ts PRIORITY_CONFIG.bars
 * — Behavioral parity rule: same priority → same number of filled bars
 * across clients.
 *
 * Differences from web:
 *   - No urgent pulse animation in v1 (would need reanimated; defer until
 *     animation polish iteration).
 */
import Svg, { Line, Rect } from "react-native-svg";
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

export function PriorityIcon({
  priority,
  size = 14,
}: {
  priority: IssuePriority;
  size?: number;
}) {
  if (priority === "none") {
    return (
      <Svg width={size} height={size} viewBox="0 0 16 16">
        <Line
          x1={3}
          y1={8}
          x2={13}
          y2={8}
          stroke={COLOR.none}
          strokeWidth={1.5}
          strokeLinecap="round"
        />
      </Svg>
    );
  }

  const filled = BARS[priority];
  const color = COLOR[priority];

  return (
    <Svg width={size} height={size} viewBox="0 0 16 16">
      {[0, 1, 2, 3].map((i) => {
        const y = 12 - (i + 1) * 3;
        const h = (i + 1) * 3;
        return (
          <Rect
            key={i}
            x={1 + i * 4}
            y={y}
            width={3}
            height={h}
            rx={0.5}
            fill={color}
            opacity={i < filled ? 1 : 0.2}
          />
        );
      })}
    </Svg>
  );
}
