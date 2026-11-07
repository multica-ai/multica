import { cn } from "../../lib/utils";
import { WORDMARK, WORDMARK_STROKE } from "../../lib/multica-brand";

interface MulticaWordmarkProps extends React.ComponentProps<"svg"> {
  /**
   * Write the letters along their centre lines and spin the star onto the i
   * (M1, ~2s), starting after this many ms. Omit for the static mark.
   */
  writeDelayMs?: number;
  /** Letters hop one after another and the star turns 45° on hover. Website nav only. */
  hop?: boolean;
  /** Accessible name; the drawn letters carry no text. */
  title?: string;
}

const LETTER_MS = 150;
const STAR_AFTER_MS = 6 * LETTER_MS + 420;
const HOP_STAGGER_MS = 40;

/**
 * The Multica wordmark (G2 with the star on the i). Drawn, not typed — never
 * substitute a font. Colour follows `currentColor`; height follows `className`
 * (1em default), measured from the top of the star to the baseline.
 */
export function MulticaWordmark({
  writeDelayMs,
  hop = false,
  title = "Multica",
  className,
  ...props
}: MulticaWordmarkProps) {
  const write = writeDelayMs != null;
  return (
    <svg
      viewBox={WORDMARK.viewBox}
      role="img"
      aria-label={title}
      fill="none"
      stroke="currentColor"
      strokeWidth={WORDMARK_STROKE}
      className={cn("inline-block h-[1em] w-auto shrink-0 overflow-visible", hop && "multica-hop-on-hover", className)}
      {...props}
    >
      {WORDMARK.letters.map((letter, i) => (
        <g key={i} style={hop ? { animationDelay: `${i * HOP_STAGGER_MS}ms` } : undefined}>
          <path
            d={letter.d}
            pathLength={write ? 1 : undefined}
            className={write ? "multica-write" : undefined}
            style={write ? { animationDelay: `${writeDelayMs + i * LETTER_MS}ms` } : undefined}
          />
          {letter.star ? (
            <path
              d={letter.star}
              fill="currentColor"
              stroke="none"
              className={cn("multica-star", write && "multica-star-in")}
              style={write ? { animationDelay: `${writeDelayMs + STAR_AFTER_MS}ms` } : undefined}
            />
          ) : null}
        </g>
      ))}
    </svg>
  );
}
