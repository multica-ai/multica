import { STATUS_CONFIG } from "@multica/core/issues/config";
import { phaseStatus, type WorkpadProgress } from "@multica/cerebro-artifacts/core";
import { cn } from "@multica/ui/lib/utils";

// FIR-3765 — WorkpadProgressCircle is the Workpad header's own status circle.
//
// It deliberately mirrors the geometry of the shared StatusIcon
// (packages/views/issues/components/status-icon.tsx): same 14×14 viewBox, same
// centre, same outer ring radius and stroke, same white check at completion — so
// the panel's header circle reads as the SAME family as the phase circles
// directly beneath it.
//
// The one difference is the point of it: StatusIcon snaps to three discrete
// looks (empty / half-full / full), while this fills its pie PROPORTIONALLY to
// done/total. A plan at 5/22 shows a small wedge, one at 18/22 a nearly full
// circle — so the header alone tells you how far the plan has come.
//
// Colour is taken from the same STATUS_CONFIG the phase circles use, keyed by
// `phaseStatus`, so the two can never drift apart: nothing done → muted, part
// done → warning, all done → info.

const CX = 7;
const CY = 7;
const OUTER_R = 6;
const FILL_R = 3.5;

// Pie wedge from 12 o'clock, clockwise — same construction as StatusIcon's.
function piePath(progress: number): string {
  const angle = 2 * Math.PI * progress;
  const endX = CX + FILL_R * Math.sin(angle);
  const endY = CY - FILL_R * Math.cos(angle);
  const largeArc = progress > 0.5 ? 1 : 0;
  return `M${CX},${CY} L${CX},${CY - FILL_R} A${FILL_R},${FILL_R} 0 ${largeArc},1 ${endX},${endY} Z`;
}

export function WorkpadProgressCircle({
  progress,
  className = "size-4",
}: {
  progress: WorkpadProgress;
  className?: string;
}) {
  const { done, total } = progress;
  // An empty plan reads as "nothing done" rather than dividing by zero.
  const fraction = total > 0 ? Math.min(1, Math.max(0, done / total)) : 0;
  const complete = total > 0 && done >= total;
  const cfg = STATUS_CONFIG[phaseStatus(progress)] ?? STATUS_CONFIG.todo;

  return (
    <svg
      viewBox="0 0 14 14"
      fill="none"
      aria-hidden="true"
      data-testid="workpad-progress-circle"
      data-fraction={fraction}
      className={cn(className, cfg.iconColor, "shrink-0")}
    >
      <circle
        cx={CX}
        cy={CY}
        r={OUTER_R}
        fill="none"
        stroke="currentColor"
        strokeWidth={1.5}
      />
      {complete ? (
        <>
          <circle cx={CX} cy={CY} r={OUTER_R} fill="currentColor" />
          <path
            d="M10.951 4.24896C11.283 4.58091 11.283 5.11909 10.951 5.45104L5.95104 10.451C5.61909 10.783 5.0809 10.783 4.74896 10.451L2.74896 8.45104C2.41701 8.11909 2.41701 7.5809 2.74896 7.24896C3.0809 6.91701 3.61909 6.91701 3.95104 7.24896L5.35 8.64792L9.74896 4.24896C10.0809 3.91701 10.6191 3.91701 10.951 4.24896Z"
            fill="white"
            stroke="none"
          />
        </>
      ) : fraction > 0 ? (
        <path d={piePath(fraction)} fill="currentColor" />
      ) : null}
    </svg>
  );
}
