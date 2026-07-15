import type { IssueProperty } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";

export function PropertyIcon({
  property,
  className,
}: {
  property?: Pick<IssueProperty, "icon"> | null;
  className?: string;
}) {
  if (!property?.icon) return null;

  return (
    <span
      aria-hidden="true"
      className={cn(
        "inline-flex size-4 shrink-0 items-center justify-center overflow-hidden text-sm leading-none",
        className,
      )}
    >
      {property.icon}
    </span>
  );
}
