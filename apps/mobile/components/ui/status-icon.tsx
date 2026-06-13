/**
 * Mobile StatusIcon — react-native-svg implementation.
 *
 * Geometry mirrors packages/views/issues/components/status-icon.tsx (14×14
 * viewBox, center 7,7) so the visual identity of each issue status is
 * recognizable across web/desktop and mobile. This is a behavioral parity
 * concern (apps/mobile/CLAUDE.md): users should not get a different mental
 * model of "what status this is" depending on the client.
 *
 * Code is mobile-owned — we read and adapt the SVG primitives, we don't
 * import the web component. SVG ops are written with react-native-svg
 * primitives instead of HTML <svg>.
 */
import * as React from "react";
import Svg, { Circle, G, Line, Path } from "react-native-svg";
import type { IssueStatus } from "@multica/core/types";

const CX = 7;
const CY = 7;
const OUTER_R = 6;
const FILL_R = 3.5;

// Mirrors STATUS_CONFIG.iconColor in packages/core/issues/config/status.ts —
// translated to hex (see apps/mobile/tailwind.config.js).
const STATUS_COLOR: Record<IssueStatus, string> = {
  backlog: "#71717a", // muted-foreground
  todo: "#71717a",
  in_progress: "#eab308", // warning
  in_review: "#22c55e", // success
  done: "#3b82f6", // info
  blocked: "#dc2626", // destructive
  cancelled: "#71717a",
};

function piePath(progress: number): string {
  const angle = 2 * Math.PI * progress;
  const endX = CX + FILL_R * Math.sin(angle);
  const endY = CY - FILL_R * Math.cos(angle);
  const largeArc = progress > 0.5 ? 1 : 0;
  return `M${CX},${CY} L${CX},${CY - FILL_R} A${FILL_R},${FILL_R} 0 ${largeArc},1 ${endX},${endY} Z`;
}

function ProgressCircle({
  progress,
  color,
  children,
}: {
  progress: number;
  color: string;
  children?: React.ReactNode;
}) {
  return (
    <>
      <Circle
        cx={CX}
        cy={CY}
        r={OUTER_R}
        fill="none"
        stroke={color}
        strokeWidth={1.5}
      />
      {progress === 1 ? (
        <Circle cx={CX} cy={CY} r={OUTER_R} fill={color} />
      ) : progress > 0 ? (
        <Path d={piePath(progress)} fill={color} />
      ) : null}
      {children}
    </>
  );
}

function BacklogIcon({ color }: { color: string }) {
  const count = 16;
  const dotR = 0.55;
  return (
    <G>
      {Array.from({ length: count }, (_, i) => {
        const angle = (i / count) * Math.PI * 2 - Math.PI / 2;
        return (
          <Circle
            key={i}
            cx={CX + OUTER_R * Math.cos(angle)}
            cy={CY + OUTER_R * Math.sin(angle)}
            r={dotR}
            fill={color}
          />
        );
      })}
    </G>
  );
}

function DoneCheck() {
  // Inner check mark (white, drawn on top of the filled circle).
  return (
    <Path
      d="M10.951 4.24896C11.283 4.58091 11.283 5.11909 10.951 5.45104L5.95104 10.451C5.61909 10.783 5.0809 10.783 4.74896 10.451L2.74896 8.45104C2.41701 8.11909 2.41701 7.5809 2.74896 7.24896C3.0809 6.91701 3.61909 6.91701 3.95104 7.24896L5.35 8.64792L9.74896 4.24896C10.0809 3.91701 10.6191 3.91701 10.951 4.24896Z"
      fill="#ffffff"
    />
  );
}

function BlockedSlash({ color }: { color: string }) {
  // Diagonal slash through the empty ring (🚫 style).
  return (
    <Line
      x1={CX + FILL_R * Math.cos(Math.PI * 0.75)}
      y1={CY - FILL_R * Math.sin(Math.PI * 0.75)}
      x2={CX + FILL_R * Math.cos(-Math.PI * 0.25)}
      y2={CY - FILL_R * Math.sin(-Math.PI * 0.25)}
      stroke={color}
      strokeWidth={1.5}
      strokeLinecap="round"
    />
  );
}

function CancelledX({ color }: { color: string }) {
  return (
    <Path
      d="M5 5 L9 9 M9 5 L5 9"
      fill="none"
      stroke={color}
      strokeWidth={1.5}
      strokeLinecap="round"
    />
  );
}

export function StatusIcon({
  status,
  size = 16,
}: {
  status: IssueStatus;
  size?: number;
}) {
  const color = STATUS_COLOR[status];
  return (
    <Svg width={size} height={size} viewBox="0 0 14 14">
      {status === "backlog" ? (
        <BacklogIcon color={color} />
      ) : status === "todo" ? (
        <ProgressCircle progress={0} color={color} />
      ) : status === "in_progress" ? (
        <ProgressCircle progress={0.5} color={color} />
      ) : status === "in_review" ? (
        <ProgressCircle progress={0.75} color={color} />
      ) : status === "done" ? (
        <ProgressCircle progress={1} color={color}>
          <DoneCheck />
        </ProgressCircle>
      ) : status === "blocked" ? (
        <ProgressCircle progress={0} color={color}>
          <BlockedSlash color={color} />
        </ProgressCircle>
      ) : (
        <ProgressCircle progress={0} color={color}>
          <CancelledX color={color} />
        </ProgressCircle>
      )}
    </Svg>
  );
}
