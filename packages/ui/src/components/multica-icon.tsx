import { cn } from "@multica/ui/lib/utils";

/**
 * Pure CSS 8-pointed asterisk icon matching the Multica logo.
 * Uses currentColor so it adapts to light/dark themes automatically.
 * Clip-path polygon traced from the original SVG path coordinates.
 */
export function MulticaIcon({
  className,
  ...props
}: React.ComponentProps<"span">) {
  return (
    <span
      className={cn("inline-block size-[1em]", className)}
      aria-hidden="true"
      {...props}
    >
      <span
        className="block size-full bg-current"
        style={{
          clipPath: `polygon(
            45% 62.1%, 45% 100%, 55% 100%, 55% 62.1%,
            81.8% 88.9%, 88.9% 81.8%, 62.1% 55%, 100% 55%,
            100% 45%, 62.1% 45%, 88.9% 18.2%, 81.8% 11.1%,
            55% 37.9%, 55% 0%, 45% 0%, 45% 37.9%,
            18.2% 11.1%, 11.1% 18.2%, 37.9% 45%, 0% 45%,
            0% 55%, 37.9% 55%, 11.1% 81.8%, 18.2% 88.9%
          )`,
        }}
      />
    </span>
  );
}
