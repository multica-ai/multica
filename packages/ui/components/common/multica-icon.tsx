import { useId, type CSSProperties } from "react";
import { cn } from "../../lib/utils";
import {
  ICON_M_CENTRE_LINE,
  ICON_M_PATH,
  ICON_STROKE,
  ICON_TILE_RADIUS,
  iconEdge,
  iconMTransform,
} from "../../lib/multica-brand";

export type MulticaIconMode =
  /** The mark at rest. */
  | "static"
  /** A highlight runs along the m: an agent at work, loading, waiting. */
  | "think"
  /** Tile pops in and the m rises from below — app launch, sign-in. */
  | "intro"
  /** `intro`, then `think` until whatever is loading unmounts it. */
  | "launch";

interface MulticaIconProps extends Omit<React.ComponentProps<"svg">, "mode"> {
  mode?: MulticaIconMode;
  /** `circle` is the avatar form; the m sits the same way inside it. */
  shape?: "square" | "circle";
  /**
   * Rendered size in px. Sets width/height and the edge thickness, which the
   * spec pins to at least 1px. Omit to size with `className` (1em default).
   */
  size?: number;
  /** Accessible name. Without one the mark is decorative. */
  title?: string;
}

/** When the launch sequence hands over from the risen m to the think loop. */
const LAUNCH_THINK_AT = "1.5s";

/**
 * The Multica icon (F1): the wordmark's m, too big for its tile and cropped
 * by it. Black tile with a white m in light mode, inverted in dark mode.
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
  const clipId = `multica-icon-${useId().replace(/[^a-zA-Z0-9_-]/g, "")}`;
  const edge = iconEdge(size ?? 24);
  const intro = mode === "intro" || mode === "launch";
  const tileClass = "fill-multica-ink dark:fill-multica-paper";
  const mFill = "fill-multica-paper dark:fill-multica-ink";
  const mStroke = "stroke-multica-paper dark:stroke-multica-ink";

  const filled = <path d={ICON_M_PATH} className={mFill} />;
  const thinking = (
    <>
      <path d={ICON_M_CENTRE_LINE} fill="none" strokeWidth={ICON_STROKE} opacity={0.22} className={mStroke} />
      <path
        d={ICON_M_CENTRE_LINE}
        fill="none"
        strokeWidth={ICON_STROKE}
        pathLength={1}
        className={cn("multica-think", mStroke)}
      />
    </>
  );
  const delay = (s: string): CSSProperties => ({ animationDelay: s });

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
      <defs>
        <clipPath id={clipId}>
          {shape === "circle" ? (
            <circle cx={50} cy={50} r={50 - edge} />
          ) : (
            <rect
              x={edge}
              y={edge}
              width={100 - 2 * edge}
              height={100 - 2 * edge}
              rx={Math.max(ICON_TILE_RADIUS - edge, 1)}
            />
          )}
        </clipPath>
      </defs>
      {shape === "circle" ? (
        <circle cx={50} cy={50} r={50} className={tileClass} />
      ) : (
        <rect width={100} height={100} rx={ICON_TILE_RADIUS} className={tileClass} />
      )}
      <g clipPath={`url(#${clipId})`}>
        <g className={intro ? "multica-rise" : undefined} style={intro ? delay("0.35s") : undefined}>
          <g transform={iconMTransform(shape)}>
            {mode === "think" ? thinking : null}
            {mode === "static" || mode === "intro" ? filled : null}
            {mode === "launch" ? (
              <>
                <g className="multica-fade-out" style={delay(LAUNCH_THINK_AT)}>{filled}</g>
                <g className="multica-fade-in" style={delay(LAUNCH_THINK_AT)}>{thinking}</g>
              </>
            ) : null}
          </g>
        </g>
      </g>
    </svg>
  );
}
