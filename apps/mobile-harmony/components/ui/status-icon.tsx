/**
 * HarmonyOS port of apps/mobile/components/ui/status-icon.tsx.
 *
 * Geometry mirrors packages/views/issues/components/status-icon.tsx (14×14
 * viewBox, center 7,7) so the visual identity of each issue status is
 * recognizable across web/desktop and mobile. This is a behavioral parity
 * concern: users should not get a different mental model of "what status
 * this is" depending on the client.
 *
 * The iOS file draws with react-native-svg, which is not wired on this
 * platform (RNOH native component layer — renders blank), so the same
 * geometry is expressed with layered Views: stroked circles become bordered
 * discs, the progress pies become half/quarter-disc corner-radius tricks
 * (exact for the 0.5 / 0.75 fills used here), bars become rotated rounded
 * rectangles, and the done check is the font-rendered Ionicons checkmark.
 * All lengths are scaled by k = size / 14.
 *
 * Custom statuses use their selected shape, falling back to their category
 * glyph, while concrete built-ins retain their more specific
 * progress/review/blocked glyphs inside the category. Callers that hold the
 * workspace catalog pass `category` and `color`; callers that only hold a
 * key get the exact built-in resolution.
 */
import React from "react";
import { StyleSheet, View } from "react-native";
import type {
  BuiltInIssueStatus,
  IssueStatus,
  IssueStatusCategory,
} from "@multica/core/types";
import { Icon } from "@/components/ui/icon";
import {
  isBuiltInIssueStatus,
  statusCategoryOfKey,
  statusIconRenderer,
} from "@/lib/issue-status";

// Mirrors STATUS_CONFIG.iconColor in packages/core/issues/config/status.ts —
// translated to hex (see apps/mobile/tailwind.config.js). Keyed on CATEGORY:
// a custom status inherits its category's token unless the catalog gave it a
// colour of its own.
const CATEGORY_COLOR: Record<IssueStatusCategory, string> = {
  unstarted: "#71717a",
  started: "#eab308", // warning
  done: "#3b82f6", // info
  closed: "#71717a",
};

const BUILT_IN_COLOR: Record<BuiltInIssueStatus, string> = {
  backlog: "#71717a",
  todo: "#71717a",
  in_progress: "#eab308",
  in_review: "#22c55e", // success
  done: "#3b82f6", // info
  blocked: "#dc2626", // destructive
  cancelled: "#71717a",
};

// Unit-space constants (viewBox 14): ring r=6 stroke 1.5 → outer edge at
// 6.75 from center; pie fill radius 3.5; slash through the pie circle;
// cancelled X across (5,5)→(9,9).
const STROKE = 1.5;
const RING_D = 13.5; // outer diameter of the stroked circle
const PIE_D = 7; // filled pie diameter (FILL_R * 2)
const SLASH_LEN = 4.95; // blocked slash (chord across the pie circle)
const X_LEN = Math.SQRT2 * 4; // cancelled X arm length ((5,5)→(9,9))

type Box = {
  position: "absolute";
  left: number;
  top: number;
  width: number;
  height: number;
};

/** View-based redraw helpers. All inputs are in viewBox units; every value
 *  is scaled by k = size / 14 before hitting layout. */
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
  const disc = (d: number, color: string | null): ViewStyleWithRadius => ({
    ...box(d, d),
    borderRadius: s(d / 2),
    ...(color ? { backgroundColor: color } : null),
  });
  const bar = (len: number, deg: number, color: string) => ({
    ...box(len, STROKE),
    borderRadius: s(STROKE / 2),
    backgroundColor: color,
    transform: [{ rotate: `${deg}deg` }],
  });
  return { s, box, disc, bar };
}

type ViewStyleWithRadius = Box & {
  borderRadius: number;
  borderWidth?: number;
  borderColor?: string;
  backgroundColor?: string;
};

function ProgressCircle({
  progress,
  color,
  k,
}: {
  progress: number;
  color: string;
  k: number;
}) {
  const { s, box, disc } = makeDraw(k);
  return (
    <>
      {/* Stroked outer ring → bordered disc */}
      <View style={{ ...disc(RING_D, null), borderWidth: s(STROKE), borderColor: color }} />
      {progress === 1 ? (
        <View style={disc(RING_D, color)} />
      ) : progress > 0 ? (
        <>
          {/* Right half (the 0.5 fill)… */}
          <View
            style={{
              ...box(PIE_D / 2, PIE_D),
              left: s(7),
              borderTopRightRadius: s(PIE_D / 2),
              borderBottomRightRadius: s(PIE_D / 2),
              backgroundColor: color,
            }}
          />
          {/* …plus the top-left quarter (the 0.75 fill). */}
          {progress > 0.5 ? (
            <View
              style={{
                ...box(PIE_D / 2, PIE_D / 2),
                borderTopLeftRadius: s(PIE_D / 2),
                backgroundColor: color,
              }}
            />
          ) : null}
        </>
      ) : null}
    </>
  );
}

function BacklogIcon({ color, k }: { color: string; k: number }) {
  const { s, box } = makeDraw(k);
  const count = 16;
  const dotD = 1.1; // dot r 0.55 in viewBox units
  return (
    <>
      {Array.from({ length: count }, (_, i) => {
        const angle = (i / count) * 360;
        return (
          <View
            key={i}
            style={{
              ...box(dotD, dotD),
              borderRadius: s(dotD / 2),
              backgroundColor: color,
              transform: [{ rotate: `${angle}deg` }, { translateY: -s(6) }],
            }}
          />
        );
      })}
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

function BlockedSlash({ color, k }: { color: string; k: number }) {
  const { bar } = makeDraw(k);
  // Diagonal slash through the empty ring (🚫 style): (4.53,4.53)→(9.47,9.47).
  return <View style={bar(SLASH_LEN, 45, color)} />;
}

function CancelledX({ color, k }: { color: string; k: number }) {
  const { bar } = makeDraw(k);
  return (
    <>
      <View style={bar(X_LEN, 45, color)} />
      <View style={bar(X_LEN, -45, color)} />
    </>
  );
}

export function StatusIcon({
  status,
  category: categoryProp,
  color: colorProp,
  icon,
  size = 16,
}: {
  status: IssueStatus;
  /**
   * Resolved category, for callers that hold the workspace catalog. Without it
   * the key resolves on its own — exact for the 7 built-ins, `unstarted` for a
   * custom key this render has no catalog for.
   */
  category?: IssueStatusCategory;
  /** A custom status's `#rrggbb`. Built-ins keep their category token. */
  color?: string | null;
  icon?: string | null;
  size?: number;
}) {
  const category = categoryProp ?? statusCategoryOfKey(status);
  const builtIn = isBuiltInIssueStatus(status) ? status : null;
  const iconStatus = statusIconRenderer(status, category, icon);
  const color = builtIn
    ? BUILT_IN_COLOR[builtIn]
    : colorProp ?? CATEGORY_COLOR[category];
  const k = size / 14;
  return (
    <View style={[styles.canvas, { width: size, height: size }]}>
      {iconStatus === "backlog" ? (
        <BacklogIcon color={color} k={k} />
      ) : iconStatus === "todo" ? (
        <ProgressCircle progress={0} color={color} k={k} />
      ) : iconStatus === "in_progress" ? (
        <ProgressCircle progress={0.5} color={color} k={k} />
      ) : iconStatus === "in_review" ? (
        <ProgressCircle progress={0.75} color={color} k={k} />
      ) : iconStatus === "done" ? (
        <>
          <ProgressCircle progress={1} color={color} k={k} />
          <DoneCheck k={k} />
        </>
      ) : iconStatus === "blocked" ? (
        <>
          <ProgressCircle progress={0} color={color} k={k} />
          <BlockedSlash color={color} k={k} />
        </>
      ) : (
        <>
          <ProgressCircle progress={0} color={color} k={k} />
          <CancelledX color={color} k={k} />
        </>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  canvas: { position: "relative" },
  center: { alignItems: "center", justifyContent: "center" },
});
