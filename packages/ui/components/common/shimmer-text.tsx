import type { ComponentProps } from "react";
import { cn } from "@multica/ui/lib/utils";

interface ShimmerTextProps extends Omit<ComponentProps<"span">, "children"> {
  children: string;
  active?: boolean;
}

/** A single-line activity label. Only the decorative highlight moves. */
export function ShimmerText({
  children,
  active = true,
  className,
  ...props
}: ShimmerTextProps) {
  return (
    <span className={cn(active && "shimmer-text", className)} {...props}>
      {children}
      {active && (
        <span className="shimmer-text-window" aria-hidden="true">
          <span className="shimmer-text-copy">{children}</span>
        </span>
      )}
    </span>
  );
}
