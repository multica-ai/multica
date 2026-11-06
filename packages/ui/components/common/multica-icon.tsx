import type { CSSProperties } from "react";
import { cn } from "../../lib/utils";
import {
  ICON_TILE_RADIUS,
  STAR_ARM_ANGLES,
  iconStar,
  iconStarTransform,
  starArmPath,
} from "../../lib/multica-brand";

export type MulticaIconMode =
  /** The mark at rest. */
  | "static"
  /** The centre opens and the arms light up in turn: an agent at work, loading, waiting. */
  | "think"
  /** Tile pops in, arms grow out of the centre as the star turns into place — app launch, sign-in. */
  | "intro"
  /** `intro`, then `think` until whatever is loading unmounts it. */
  | "launch";

interface MulticaIconProps extends Omit<React.ComponentProps<"svg">, "mode"> {
  mode?: MulticaIconMode;
  /** `circle` is the avatar form. */
  shape?: "square" | "circle";
  /**
   * Rendered size in px. Sets width/height and picks the optical size (24px and
   * below draws a bigger, heavier star). Omit to size with `className` (1em
   * default); the small optical size is used then.
   */
  size?: number;
  /** Accessible name. Without one the mark is decorative. */
  title?: string;
}

/** When the launch sequence hands over from the settled star to the think loop. */
const LAUNCH_THINK_AT = "1.5s";
const INTRO_AT_S = 0.15;
const ARM_STAGGER_MS = 55;
/** Think: one lap of the eight arms. */
const THINK_LAP_MS = 800;
/** Think: the open centre, in the star's grid. */
const THINK_HOLE = 15;

/**
 * The Multica icon: the star from the wordmark's i on a rounded tile. Black
 * tile with a white star in light mode, inverted in dark mode.
 */
export function MulticaIcon({
  mode = "static",
  shape = "square",
  size,
  title,
  className,
  style,
  ...props
}: MulticaIconProps) {
  const { scale, stroke } = iconStar(size, shape);
  const intro = mode === "intro" || mode === "launch";
  const tileClass = "fill-multica-ink dark:fill-multica-paper";
  const starClass = "fill-multica-paper dark:fill-multica-ink";
  const delay = (ms: number | string): CSSProperties => ({
    animationDelay: typeof ms === "number" ? `${ms}ms` : ms,
  });

  const arms = (variant: "static" | "grow" | "think") =>
    STAR_ARM_ANGLES.map((angle, k) => (
      <g key={angle} transform={`rotate(${angle} 50 50)`}>
        <path
          d={starArmPath(stroke, variant === "think" ? THINK_HOLE : 0)}
          className={cn(
            starClass,
            variant === "grow" && "multica-arm-grow",
            variant === "think" && "multica-arm-fade",
          )}
          style={
            variant === "grow"
              ? delay(INTRO_AT_S * 1000 + k * ARM_STAGGER_MS)
              : variant === "think"
                ? delay(k * (THINK_LAP_MS / STAR_ARM_ANGLES.length) - THINK_LAP_MS)
                : undefined
          }
        />
      </g>
    ));
  const settled = (
    <g className="multica-settle" style={delay(INTRO_AT_S * 1000)}>
      {arms("grow")}
    </g>
  );

  return (
    <svg
      viewBox="0 0 100 100"
      role={title ? "img" : undefined}
      aria-label={title}
      aria-hidden={title ? undefined : true}
      className={cn("inline-block shrink-0", size == null && "size-[1em]", intro && "multica-pop", className)}
      style={{ ...(size != null && { width: size, height: size }), ...(intro && delay("0.1s")), ...style }}
      {...props}
    >
      {shape === "circle" ? (
        <circle cx={50} cy={50} r={50} className={tileClass} />
      ) : (
        <rect width={100} height={100} rx={ICON_TILE_RADIUS} className={tileClass} />
      )}
      <g transform={iconStarTransform(scale)}>
        {mode === "static" ? arms("static") : null}
        {mode === "think" ? arms("think") : null}
        {mode === "intro" ? settled : null}
        {mode === "launch" ? (
          <>
            <g className="multica-fade-out" style={delay(LAUNCH_THINK_AT)}>{settled}</g>
            <g className="multica-fade-in" style={delay(LAUNCH_THINK_AT)}>{arms("think")}</g>
          </>
        ) : null}
      </g>
    </svg>
  );
}
