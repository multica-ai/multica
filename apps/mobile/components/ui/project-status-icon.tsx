/**
 * Mobile ProjectStatusIcon — visual identity per status, mirrors the design
 * intent of the web `project-status-icon` (and the issue StatusIcon shape).
 *
 * Geometry follows status-icon.tsx (14×14 viewBox, 6r outer ring) so the
 * project status icon visually rhymes with issue statuses on the same screen.
 */
import * as React from "react";
import Svg, { Circle, Line, Path } from "react-native-svg";
import type { ProjectStatus } from "@multica/core/types";
import { projectStatusColor } from "@/lib/project-status";

const CX = 7;
const CY = 7;
const OUTER_R = 6;
const FILL_R = 3.5;

function piePath(progress: number): string {
  const angle = 2 * Math.PI * progress;
  const endX = CX + FILL_R * Math.sin(angle);
  const endY = CY - FILL_R * Math.cos(angle);
  const largeArc = progress > 0.5 ? 1 : 0;
  return `M${CX},${CY} L${CX},${CY - FILL_R} A${FILL_R},${FILL_R} 0 ${largeArc},1 ${endX},${endY} Z`;
}

function Ring({
  color,
  children,
}: {
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
      {children}
    </>
  );
}

function PauseBars({ color }: { color: string }) {
  return (
    <>
      <Line
        x1={5.5}
        y1={4.5}
        x2={5.5}
        y2={9.5}
        stroke={color}
        strokeWidth={1.5}
        strokeLinecap="round"
      />
      <Line
        x1={8.5}
        y1={4.5}
        x2={8.5}
        y2={9.5}
        stroke={color}
        strokeWidth={1.5}
        strokeLinecap="round"
      />
    </>
  );
}

function CancelX({ color }: { color: string }) {
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

function DoneCheck() {
  return (
    <Path
      d="M10.951 4.24896C11.283 4.58091 11.283 5.11909 10.951 5.45104L5.95104 10.451C5.61909 10.783 5.0809 10.783 4.74896 10.451L2.74896 8.45104C2.41701 8.11909 2.41701 7.5809 2.74896 7.24896C3.0809 6.91701 3.61909 6.91701 3.95104 7.24896L5.35 8.64792L9.74896 4.24896C10.0809 3.91701 10.6191 3.91701 10.951 4.24896Z"
      fill="#ffffff"
    />
  );
}

export function ProjectStatusIcon({
  status,
  size = 16,
}: {
  status: ProjectStatus | string;
  size?: number;
}) {
  const color = projectStatusColor(status);
  return (
    <Svg width={size} height={size} viewBox="0 0 14 14">
      {status === "planned" ? (
        <Ring color={color} />
      ) : status === "in_progress" ? (
        <Ring color={color}>
          <Path d={piePath(0.5)} fill={color} />
        </Ring>
      ) : status === "paused" ? (
        <Ring color={color}>
          <PauseBars color={color} />
        </Ring>
      ) : status === "completed" ? (
        <>
          <Circle cx={CX} cy={CY} r={OUTER_R} fill={color} />
          <DoneCheck />
        </>
      ) : status === "cancelled" ? (
        <Ring color={color}>
          <CancelX color={color} />
        </Ring>
      ) : (
        // Unknown server enum value — render the planned ring so the row
        // still reads as "a project" rather than crashing or going blank.
        <Ring color={color} />
      )}
    </Svg>
  );
}
