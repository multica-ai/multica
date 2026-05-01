import type { ComponentProps } from "react";
import { cn } from "../../lib/utils";

/**
 * AsymblLogo — Forge brand mark.
 *
 * Renders the Asymbl bracket-and-dot mark from a transparent PNG so it can sit
 * on light or dark backgrounds without recolouring (the mark uses fixed brand
 * colors: light blue bracket + red circle).
 *
 * Backwards-compatible re-export as `MulticaIcon` keeps existing call sites
 * working without touching every import. The original Multica clip-path
 * asterisk is replaced; the spin/animation props are accepted but no-ops
 * because the Asymbl mark is not a spinning glyph.
 *
 * Source assets live in `/brand/` (see apps/web/public/brand/).
 */

interface AsymblLogoProps extends ComponentProps<"span"> {
  /** Accepted for API compatibility with the prior MulticaIcon — has no visual effect. */
  animate?: boolean;
  /** Accepted for API compatibility with the prior MulticaIcon — has no visual effect. */
  noSpin?: boolean;
  /** If true, wrap the mark in a bordered rounded square. */
  bordered?: boolean;
  /** Size of the bordered icon: "sm" (default), "md", "lg". */
  size?: "sm" | "md" | "lg";
}

const borderedSizes = {
  sm: { wrapper: "p-1.5", icon: "size-3.5" },
  md: { wrapper: "p-2", icon: "size-4" },
  lg: { wrapper: "p-2.5", icon: "size-5" },
};

const ASSET_SRC = "/brand/asymbl-mark.png";
const ASSET_ALT = "Asymbl";

export function AsymblLogo({
  className,
  animate: _animate,
  noSpin: _noSpin,
  bordered = false,
  size = "sm",
  ...props
}: AsymblLogoProps) {
  if (bordered) {
    const sizeConfig = borderedSizes[size];
    return (
      <span
        className={cn(
          "inline-flex items-center justify-center border border-border rounded-md",
          sizeConfig.wrapper,
          className
        )}
        {...props}
      >
        <img
          src={ASSET_SRC}
          alt={ASSET_ALT}
          className={cn("block", sizeConfig.icon)}
        />
      </span>
    );
  }

  return (
    <span
      className={cn("inline-block size-[1em]", className)}
      {...props}
    >
      <img
        src={ASSET_SRC}
        alt={ASSET_ALT}
        className="block size-full object-contain"
      />
    </span>
  );
}

// Backwards-compatible alias so existing imports keep working without
// touching every consumer file. Renaming the file would force a much larger
// diff and complicate cherry-picking upstream Multica patches.
export const MulticaIcon = AsymblLogo;
