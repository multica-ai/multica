import { useId, type CSSProperties } from "react";
import { cn } from "@multica/ui/lib/utils";

/*
 * Multica brand marks, brand guidelines v3.1 (MUL-8444). Black and white only.
 *
 * The wordmark is the primary mark and always stands alone: it is drawn as
 * centre-lines with a 16-unit stroke (arch 62, x-height 76, ascender 30), so
 * never retype it in a font or set it beside the icon as a lockup. The icon is
 * for square slots only (app icon, favicon, avatar): an m with a 22-unit
 * stroke, scaled 0.96 and cropped by its tile, inside a thin edge in the tile
 * colour so the tile keeps its outline on any background.
 */

/** Wordmark geometry in its own units; the viewBox runs from the top of the l to the baseline. */
export const WORDMARK = {
  top: -30.5,
  width: 421.36,
  height: 106.5,
  stroke: 16,
  letters: [
    "M8 76V31A23 23 0 0 1 54 31V76M54 31A23 23 0 0 1 100 31V76",
    "M128 0V45A23 23 0 0 0 174 45M174 0V76",
    "M202 -30V76",
    "M239.6 -19.8V76M219.6 8H259.6",
    "M277.8 0V76",
    "M344.103 14.737A23 23 0 0 0 304.84 31V45A23 23 0 0 0 344.103 61.263",
    "M367.36 31A23 23 0 0 1 413.36 31V45A23 23 0 0 1 367.36 45ZM413.36 0V76",
  ],
  /** The tittle of the i, the fifth letter. */
  tittle: { cx: 277.8, cy: -18.6, r: 8.96 },
} as const;

/** The icon's m, filled, with the transform that places it on its 100-unit tile. */
export const ICON_M = "M0 85.25V34A34 34 0 0 1 68 34V85.25H46V34A12 12 0 0 0 22 34V85.25ZM46 85.25V34A34 34 0 0 1 114 34V85.25H92V34A12 12 0 0 0 68 34V85.25Z";
export const ICON_M_TRANSFORM = "translate(-4.72 22) scale(0.96)";
/** The same m as one centre-line (stroke 22), for the thinking motion. */
const ICON_M_LINE = "M11 85.25V34A23 23 0 0 1 57 34V85.25V34A23 23 0 0 1 103 34V85.25";

const viewBox = `0 ${WORDMARK.top} ${WORDMARK.width} ${WORDMARK.height}`;

/**
 * The wordmark. `height` is the full height, top of the l to the baseline.
 * Inside a `group` link the letters hop one after another on hover.
 */
export function BrandWordmark({ height = 22, className }: { height?: number; className?: string }) {
  return (
    <svg
      viewBox={viewBox}
      height={height}
      width={+((height * WORDMARK.width) / WORDMARK.height).toFixed(2)}
      aria-hidden
      className={cn("shrink-0 overflow-visible text-site-ink", className)}
    >
      <g fill="none" stroke="currentColor" strokeWidth={WORDMARK.stroke}>
        {WORDMARK.letters.map((d, index) => (
          <g key={d} className="site-letter" style={{ "--i": index } as CSSProperties}>
            <path d={d} />
            {index === 4 ? <circle {...WORDMARK.tittle} fill="currentColor" stroke="none" /> : null}
          </g>
        ))}
      </g>
    </svg>
  );
}

/** The app icon: black tile, white m (or reversed with `light`). `size` in px sets the edge. */
export function BrandIcon({ size, light, className }: { size: number; light?: boolean; className?: string }) {
  const clip = useId();
  const edge = Math.max(2.6, 120 / size);
  return (
    <svg viewBox="0 0 100 100" width={size} height={size} aria-hidden className={cn("shrink-0", className)}>
      <defs>
        <clipPath id={clip}>
          <rect x={edge} y={edge} width={100 - 2 * edge} height={100 - 2 * edge} rx={Math.max(22.5 - edge, 1)} />
        </clipPath>
      </defs>
      <rect width="100" height="100" rx="22.5" fill={light ? "#fff" : "#0b0b0c"} />
      <g clipPath={`url(#${clip})`}>
        <path transform={ICON_M_TRANSFORM} d={ICON_M} fill={light ? "#0b0b0c" : "#fff"} />
      </g>
    </svg>
  );
}

/** The icon as a round avatar, for people-and-bot lists. */
export function BrandAvatar({ size, className }: { size: number; className?: string }) {
  const clip = useId();
  const edge = Math.max(3, 100 / size);
  return (
    <svg viewBox="0 0 100 100" width={size} height={size} aria-hidden className={cn("shrink-0", className)}>
      <defs>
        <clipPath id={clip}>
          <circle cx="50" cy="50" r={50 - edge} />
        </clipPath>
      </defs>
      <circle cx="50" cy="50" r="50" fill="#0b0b0c" />
      <g clipPath={`url(#${clip})`}>
        <path transform="translate(-4.72 24) scale(0.96)" d={ICON_M} fill="#fff" />
      </g>
    </svg>
  );
}

/**
 * Agent at work: a light runs along the m's centre-line, in the current
 * colour. Under reduced motion it is the plain m.
 */
export function ThinkingMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 114 86" aria-hidden className={cn("size-2.5 shrink-0 overflow-visible", className)}>
      <g fill="none" stroke="currentColor" strokeWidth="22">
        <path d={ICON_M_LINE} className="opacity-25 motion-reduce:opacity-100" />
        <path d={ICON_M_LINE} pathLength={1} className="site-thinking motion-reduce:hidden" />
      </g>
    </svg>
  );
}
