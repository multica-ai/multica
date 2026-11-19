import Svg, { Circle, Line, Path, Rect } from "react-native-svg";
import { statusCategoryOfKey } from "@multica/core/issues";
import type { IssuePriority, IssueStatusCategory } from "@multica/core/types";
import { useTheme, type Palette } from "../theme";

// Geometry of packages/views/issues/components/status-icon.tsx and priority-icon.tsx.
const C = 7;
const OUTER = 6;
const FILL = 3.5;

function pie(progress: number) {
  const angle = 2 * Math.PI * progress;
  return `M${C},${C} L${C},${C - FILL} A${FILL},${FILL} 0 ${progress > 0.5 ? 1 : 0},1 ${C + FILL * Math.sin(angle)},${C - FILL * Math.cos(angle)} Z`;
}

type Glyph = "dotted" | "circle" | "half" | "three_quarters" | "check" | "slash" | "cross";

const BUILT_IN: Record<string, { glyph: Glyph; color: (p: Palette) => string }> = {
  backlog: { glyph: "dotted", color: (p) => p.label2 },
  todo: { glyph: "circle", color: (p) => p.label2 },
  in_progress: { glyph: "half", color: (p) => p.warning },
  in_review: { glyph: "three_quarters", color: (p) => p.success },
  done: { glyph: "check", color: (p) => p.info },
  blocked: { glyph: "slash", color: (p) => p.destructive },
  cancelled: { glyph: "cross", color: (p) => p.label2 },
};

const BY_CATEGORY: Record<IssueStatusCategory, string> = {
  unstarted: "todo",
  started: "in_progress",
  done: "done",
  closed: "cancelled",
};

interface StatusIconProps {
  status: string;
  /** A custom status's `#rrggbb` and geometry; built-ins keep their own. */
  color?: string | null;
  icon?: string | null;
  size?: number;
}

export function StatusIcon({ status, color, icon, size = 16 }: StatusIconProps) {
  const palette = useTheme();
  const builtIn = BUILT_IN[status];
  const fallback = BUILT_IN[BY_CATEGORY[statusCategoryOfKey(status)] ?? "todo"]!;
  const glyph = builtIn?.glyph ?? (icon as Glyph | null) ?? fallback.glyph;
  const tint = builtIn ? builtIn.color(palette) : (color ?? fallback.color(palette));

  if (glyph === "dotted") {
    return (
      <Svg width={size} height={size} viewBox="0 0 14 14">
        {Array.from({ length: 16 }, (_, i) => {
          const a = (i / 16) * Math.PI * 2 - Math.PI / 2;
          return <Circle key={i} cx={C + OUTER * Math.cos(a)} cy={C + OUTER * Math.sin(a)} r={0.55} fill={tint} />;
        })}
      </Svg>
    );
  }
  return (
    <Svg width={size} height={size} viewBox="0 0 14 14" fill="none">
      <Circle cx={C} cy={C} r={OUTER} stroke={tint} strokeWidth={1.5} fill={glyph === "check" ? tint : "none"} />
      {glyph === "half" && <Path d={pie(0.5)} fill={tint} />}
      {glyph === "three_quarters" && <Path d={pie(0.75)} fill={tint} />}
      {glyph === "check" && (
        <Path
          d="M10.951 4.24896C11.283 4.58091 11.283 5.11909 10.951 5.45104L5.95104 10.451C5.61909 10.783 5.0809 10.783 4.74896 10.451L2.74896 8.45104C2.41701 8.11909 2.41701 7.5809 2.74896 7.24896C3.0809 6.91701 3.61909 6.91701 3.95104 7.24896L5.35 8.64792L9.74896 4.24896C10.0809 3.91701 10.6191 3.91701 10.951 4.24896Z"
          fill={palette.background}
        />
      )}
      {glyph === "slash" && (
        <Line
          x1={C + FILL * Math.cos(Math.PI * 0.75)}
          y1={C - FILL * Math.sin(Math.PI * 0.75)}
          x2={C + FILL * Math.cos(-Math.PI * 0.25)}
          y2={C - FILL * Math.sin(-Math.PI * 0.25)}
          stroke={tint}
          strokeWidth={1.5}
          strokeLinecap="round"
        />
      )}
      {glyph === "cross" && <Path d="M5 5 L9 9 M9 5 L5 9" stroke={tint} strokeWidth={1.5} strokeLinecap="round" />}
    </Svg>
  );
}

const PRIORITY: Record<IssuePriority, { bars: number; color: (p: Palette) => string }> = {
  urgent: { bars: 4, color: (p) => p.destructive },
  high: { bars: 3, color: (p) => p.priorityHigh },
  medium: { bars: 2, color: (p) => p.warning },
  low: { bars: 1, color: (p) => p.info },
  none: { bars: 0, color: (p) => p.label2 },
};

export function PriorityIcon({ priority, size = 15, mono = false }: { priority: IssuePriority; size?: number; mono?: boolean }) {
  const palette = useTheme();
  const cfg = PRIORITY[priority] ?? PRIORITY.none;
  const tint = mono ? palette.label : cfg.color(palette);
  if (cfg.bars === 0) {
    return (
      <Svg width={size} height={size} viewBox="0 0 16 16">
        <Line x1={3} y1={8} x2={13} y2={8} stroke={tint} strokeWidth={1.5} strokeLinecap="round" />
      </Svg>
    );
  }
  if (cfg.bars === 4) {
    return (
      <Svg width={size} height={size} viewBox="0 0 16 16">
        <Rect x={2} y={2} width={12} height={12} rx={3} fill={tint} />
        <Line x1={8} y1={5} x2={8} y2={8.6} stroke={palette.background} strokeWidth={1.7} strokeLinecap="round" />
        <Circle cx={8} cy={11} r={0.95} fill={palette.background} />
      </Svg>
    );
  }
  return (
    <Svg width={size} height={size} viewBox="0 0 16 16">
      {[6, 9, 12].map((height, i) => (
        <Rect key={i} x={2 + i * 4.25} y={14 - height} width={3.5} height={height} rx={1} fill={tint} opacity={i < cfg.bars ? 1 : 0.35} />
      ))}
    </Svg>
  );
}
