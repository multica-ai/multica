/**
 * Mobile ProjectPriorityIcon — reuses the same 4-bar geometry as the issue
 * PriorityIcon. Project priority enum is identical to issue priority enum
 * (urgent/high/medium/low/none), so visually identical bars communicate the
 * same meaning across surfaces — desirable for behavioral parity.
 *
 * Colors are kept identical to the issue PriorityIcon hex map.
 */
import Svg, { Line, Rect } from "react-native-svg";
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

export function ProjectPriorityIcon({
  priority,
  size = 14,
}: {
  priority: ProjectPriority | string;
  size?: number;
}) {
  const filled = projectPriorityBars(priority);
  const color = colorFor(priority);

  if (filled === 0) {
    return (
      <Svg width={size} height={size} viewBox="0 0 16 16">
        <Line
          x1={3}
          y1={8}
          x2={13}
          y2={8}
          stroke={color}
          strokeWidth={1.5}
          strokeLinecap="round"
        />
      </Svg>
    );
  }

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
