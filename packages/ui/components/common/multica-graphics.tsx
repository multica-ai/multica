import { cn } from "../../lib/utils";
import { STAR_GRAPHIC_STROKE, starPath } from "../../lib/multica-brand";

const STAR_STAGGER_S = 0.11;
/** Space between stars in a row, as a share of one star's width. */
const ROW_GAP = 0.4;
const THIN_STAR = starPath(STAR_GRAPHIC_STROKE);

interface MulticaStarRowProps extends React.ComponentProps<"svg"> {
  /** Number of stars; each one stands for a teammate. */
  count: number;
  /**
   * `center`: stars turn into place from the middle outwards (backgrounds).
   * `join`: left to right, and the last one arrives after a beat (someone just
   * joined). Omit for a static row.
   */
  enter?: "center" | "join";
  /** Seconds before the first star moves. */
  delay?: number;
  /** Classes for the last star, e.g. to light up the newest teammate. */
  lastClassName?: string;
}

/**
 * A row of thin stars, one per teammate — the star-field graphic from the
 * brand guidelines. Decorative and low-contrast; fill follows `currentColor`.
 */
export function MulticaStarRow({
  count,
  enter,
  delay = 0,
  lastClassName,
  className,
  ...props
}: MulticaStarRowProps) {
  const step = 100 * (1 + ROW_GAP);
  const width = step * (count - 1) + 100;
  const mid = (count - 1) / 2;
  const delayOf = (i: number) => {
    if (enter === "center") return delay + Math.abs(i - mid) * STAR_STAGGER_S;
    if (i === count - 1) return delay + i * STAR_STAGGER_S + 0.3;
    return delay + i * STAR_STAGGER_S;
  };
  return (
    <svg
      viewBox={`0 0 ${width} 100`}
      fill="currentColor"
      aria-hidden="true"
      className={cn("block", className)}
      {...props}
    >
      {Array.from({ length: count }, (_, i) => (
        <g key={i} transform={`translate(${step * i} 0)`}>
          <path
            d={THIN_STAR}
            className={cn(enter && "multica-star-enter", i === count - 1 && lastClassName)}
            style={enter ? { animationDelay: `${delayOf(i)}s` } : undefined}
          />
        </g>
      ))}
    </svg>
  );
}

interface MulticaBleedMarkProps extends React.ComponentProps<"svg"> {
  /** Turn into place once, after this many seconds. Omit for static. */
  enterDelay?: number;
}

/**
 * A large thin star meant to bleed off the edges of its container — the big
 * graphic from the brand guidelines. Position it with `className`; fill
 * follows `currentColor`.
 */
export function MulticaBleedMark({ enterDelay, className, ...props }: MulticaBleedMarkProps) {
  return (
    <svg viewBox="0 0 100 100" fill="currentColor" aria-hidden="true" className={cn("block", className)} {...props}>
      <path
        d={THIN_STAR}
        className={enterDelay != null ? "multica-star-enter" : undefined}
        style={enterDelay != null ? { animationDelay: `${enterDelay}s` } : undefined}
      />
    </svg>
  );
}
