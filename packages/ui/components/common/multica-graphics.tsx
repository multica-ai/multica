import { cn } from "../../lib/utils";
import { ICON_ARCH, ICON_STROKE, archPath } from "../../lib/multica-brand";

const ARCH_STAGGER_S = 0.11;
const BLEED_HEIGHT = 140;
const M_WIDTH = 2 * ICON_ARCH - ICON_STROKE;

interface MulticaArcadeProps extends React.ComponentProps<"svg"> {
  /** Number of arches; each one stands for a teammate. */
  count: number;
  /** Leg length in arch units (an arch is 68 wide). */
  legs?: number;
  /**
   * `center`: arches rise from the middle outwards (backgrounds).
   * `join`: arches rise left to right, and the last one rises after a beat
   * (someone just joined). Omit for a static row.
   */
  rise?: "center" | "join";
  /** Seconds before the first arch moves. */
  delay?: number;
  /** Classes for the last arch, e.g. to highlight the newest teammate. */
  lastClassName?: string;
}

/**
 * A row of arches built from the icon's module — the graphic extension of the
 * m. Decorative and low-contrast; fill follows `currentColor`.
 */
export function MulticaArcade({
  count,
  legs = 76,
  rise,
  delay = 0,
  lastClassName,
  className,
  ...props
}: MulticaArcadeProps) {
  const step = ICON_ARCH - ICON_STROKE;
  const width = step * (count - 1) + ICON_ARCH;
  const mid = (count - 1) / 2;
  const delayOf = (i: number) => {
    if (rise === "center") return delay + Math.abs(i - mid) * ARCH_STAGGER_S;
    if (i === count - 1) return delay + i * ARCH_STAGGER_S + 0.3;
    return delay + i * ARCH_STAGGER_S;
  };
  return (
    <svg
      viewBox={`0 0 ${width} ${legs}`}
      fill="currentColor"
      aria-hidden="true"
      className={cn("block", className)}
      {...props}
    >
      {Array.from({ length: count }, (_, i) => (
        <path
          key={i}
          d={archPath(step * i, 0, legs, ICON_ARCH, ICON_STROKE)}
          className={cn(rise && "multica-arch-rise", i === count - 1 && lastClassName)}
          style={rise ? { animationDelay: `${delayOf(i)}s` } : undefined}
        />
      ))}
    </svg>
  );
}

interface MulticaBleedMarkProps extends React.ComponentProps<"svg"> {
  /** Rise into place once, after this many seconds. Omit for static. */
  riseDelay?: number;
}

/**
 * A large m meant to bleed off the bottom and right of its container — the
 * icon's "too big to fit" idea as a surface graphic. Position it with
 * `className`; fill follows `currentColor`.
 */
export function MulticaBleedMark({ riseDelay, className, ...props }: MulticaBleedMarkProps) {
  return (
    <svg
      viewBox={`0 0 ${M_WIDTH} ${BLEED_HEIGHT}`}
      fill="currentColor"
      aria-hidden="true"
      className={cn("block", className)}
      {...props}
    >
      <g
        className={riseDelay != null ? "multica-arch-rise" : undefined}
        style={riseDelay != null ? { animationDelay: `${riseDelay}s` } : undefined}
      >
        <path
          d={
            archPath(0, 0, BLEED_HEIGHT, ICON_ARCH, ICON_STROKE) +
            archPath(ICON_ARCH - ICON_STROKE, 0, BLEED_HEIGHT, ICON_ARCH, ICON_STROKE)
          }
        />
      </g>
    </svg>
  );
}
