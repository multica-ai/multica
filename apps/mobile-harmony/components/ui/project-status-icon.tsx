/**
 * HarmonyOS port of apps/mobile/components/ui/project-status-icon.tsx —
 * visual identity per status, mirrors the design intent of the web
 * `project-status-icon` (and the issue StatusIcon shape).
 *
 * Geometry follows status-icon.tsx (14×14 viewBox, 6r outer ring) so the
 * project status icon visually rhymes with issue statuses on the same
 * screen. react-native-svg is not wired on this platform (renders blank),
 * so the same geometry is expressed with layered Views: stroked circles
 * become bordered discs, the half progress pie becomes a half-disc
 * corner-radius trick, pause/cancel bars become rotated or upright rounded
 * rectangles, and the done check is the font-rendered Ionicons checkmark.
 * All lengths are scaled by k = size / 14.
 */
import React from "react";
import { StyleSheet, View } from "react-native";
import type { ProjectStatus } from "@multica/core/types";
import { Icon } from "@/components/ui/icon";
import { projectStatusColor } from "@/lib/project-status";

// viewBox-14 unit constants (same grid as the issue StatusIcon).
const STROKE = 1.5;
const RING_D = 13.5; // outer diameter of the stroked r=6 circle
const PIE_D = 7; // filled pie diameter (fill radius 3.5)

type Box = {
  position: "absolute";
  left: number;
  top: number;
  width: number;
  height: number;
};

function makeDraw(k: number) {
  const s = (v: number) => v * k;
  /** Absolute box centered on the 14-unit grid's (7,7). */
  const box = (w: number, h: number): Box => ({
    position: "absolute",
    left: s(7 - w / 2),
    top: s(7 - h / 2),
    width: s(w),
    height: s(h),
  });
  const bar = (len: number, deg: number, color: string) => ({
    ...box(len, STROKE),
    borderRadius: s(STROKE / 2),
    backgroundColor: color,
    transform: deg === 0 ? undefined : [{ rotate: `${deg}deg` }],
  });
  return { s, box, bar };
}

function Ring({ color, k }: { color: string; k: number }) {
  const { s, box } = makeDraw(k);
  return (
    <View
      style={{
        ...box(RING_D, RING_D),
        borderRadius: s(RING_D / 2),
        borderWidth: s(STROKE),
        borderColor: color,
      }}
    />
  );
}

function HalfPie({ color, k }: { color: string; k: number }) {
  const { s, box } = makeDraw(k);
  return (
    <View
      style={{
        ...box(PIE_D / 2, PIE_D),
        left: s(7),
        borderTopRightRadius: s(PIE_D / 2),
        borderBottomRightRadius: s(PIE_D / 2),
        backgroundColor: color,
      }}
    />
  );
}

function PauseBars({ color, k }: { color: string; k: number }) {
  const { s } = makeDraw(k);
  // viewBox lines x=5.5 / x=8.5, y 4.5→9.5, stroke 1.5 (stroke straddles
  // the path, so the left edge is x - 0.75).
  const bar = (x: number) => ({
    position: "absolute" as const,
    left: s(x - STROKE / 2),
    top: s(4.5),
    width: s(STROKE),
    height: s(5),
    borderRadius: s(STROKE / 2),
    backgroundColor: color,
  });
  return (
    <>
      <View style={bar(5.5)} />
      <View style={bar(8.5)} />
    </>
  );
}

function CancelX({ color, k }: { color: string; k: number }) {
  const { bar } = makeDraw(k);
  return (
    <>
      <View style={bar(Math.SQRT2 * 4, 45, color)} />
      <View style={bar(Math.SQRT2 * 4, -45, color)} />
    </>
  );
}

function DoneCheck({ k }: { k: number }) {
  // Inner check mark (white, on top of the filled disc). Font-rendered
  // Ionicons checkmark standing in for the SVG path; the glyph fills
  // roughly two-thirds of its point size, matching the SVG check's 63%
  // box width.
  const { box } = makeDraw(k);
  return (
    <View style={[box(PIE_D, PIE_D), styles.center]}>
      <Icon name="checkmark" size={Math.round(12 * k)} color="#ffffff" />
    </View>
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
  const k = size / 14;
  return (
    <View style={[styles.canvas, { width: size, height: size }]}>
      {status === "planned" ? (
        <Ring color={color} k={k} />
      ) : status === "in_progress" ? (
        <>
          <Ring color={color} k={k} />
          <HalfPie color={color} k={k} />
        </>
      ) : status === "paused" ? (
        <>
          <Ring color={color} k={k} />
          <PauseBars color={color} k={k} />
        </>
      ) : status === "completed" ? (
        <>
          {/* Filled disc (r=6 fill + the stroked circle on top of it). */}
          <View
            style={{
              position: "absolute",
              left: 0.25 * k,
              top: 0.25 * k,
              width: RING_D * k,
              height: RING_D * k,
              borderRadius: (RING_D / 2) * k,
              backgroundColor: color,
            }}
          />
          <DoneCheck k={k} />
        </>
      ) : status === "cancelled" ? (
        <>
          <Ring color={color} k={k} />
          <CancelX color={color} k={k} />
        </>
      ) : (
        // Unknown server enum value — render the planned ring so the row
        // still reads as "a project" rather than crashing or going blank.
        <Ring color={color} k={k} />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  canvas: { position: "relative" },
  center: { alignItems: "center", justifyContent: "center" },
});
