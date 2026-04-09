import { useState, useEffect } from "react";
import { cn } from "@multica/ui/lib/utils";

interface MulticaIconProps extends React.ComponentProps<"span"> {
  /**
   * If true, play a one-time entrance spin animation.
   */
  animate?: boolean;
  /**
   * If true, disable hover spin animation.
   */
  noSpin?: boolean;
  /**
   * If true, show a border around the icon.
   */
  bordered?: boolean;
  /**
   * Size of the bordered icon: "sm" (default), "md", "lg"
   */
  size?: "sm" | "md" | "lg";
}

const borderedSizes = {
  sm: { wrapper: "p-1.5", icon: "size-3.5" },
  md: { wrapper: "p-2", icon: "size-4" },
  lg: { wrapper: "p-2.5", icon: "size-5" },
};

/**
 * Pure CSS 8-pointed asterisk icon matching the Multica logo.
 * Uses currentColor so it adapts to light/dark themes automatically.
 * Clip-path polygon traced from the original SVG path coordinates.
 */
export function MulticaIcon({
  className,
  animate = false,
  noSpin = false,
  bordered = false,
  size = "sm",
  ...props
}: MulticaIconProps) {
  const [entranceDone, setEntranceDone] = useState(!animate);

  useEffect(() => {
    if (!animate) return;
    const timer = setTimeout(() => setEntranceDone(true), 600);
    return () => clearTimeout(timer);
  }, [animate]);

  const clipPath = `polygon(
    45% 62.1%, 45% 100%, 55% 100%, 55% 62.1%,
    81.8% 88.9%, 88.9% 81.8%, 62.1% 55%, 100% 55%,
    100% 45%, 62.1% 45%, 88.9% 18.2%, 81.8% 11.1%,
    55% 37.9%, 55% 0%, 45% 0%, 45% 37.9%,
    18.2% 11.1%, 11.1% 18.2%, 37.9% 45%, 0% 45%,
    0% 55%, 37.9% 55%, 11.1% 81.8%, 18.2% 88.9%
  )`;

  if (bordered) {
    const sizeConfig = borderedSizes[size];
    return (
      <span
        className={cn(
          "inline-flex items-center justify-center border border-border rounded-md",
          sizeConfig.wrapper,
          className
        )}
        aria-hidden="true"
        {...props}
      >
        <span
          className={cn(
            "block",
            sizeConfig.icon,
            !entranceDone && "animate-entrance-spin",
            entranceDone && !noSpin && "hover:animate-spin"
          )}
        >
          <span
            className="block size-full bg-current"
            style={{ clipPath }}
          />
        </span>
      </span>
    );
  }

  return (
    <span
      className={cn(
        "inline-block size-[1em]",
        !entranceDone && "animate-entrance-spin",
        entranceDone && !noSpin && "hover:animate-spin",
        className
      )}
      aria-hidden="true"
      {...props}
    >
      <span
        className="block size-full bg-current"
        style={{ clipPath }}
      />
    </span>
  );
}
