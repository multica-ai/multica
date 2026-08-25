import { Rectangle, type BarRectangleItem } from "recharts";
import { bucketLabel } from "./bucket-label";

interface InteractiveBarSegmentProps extends BarRectangleItem {
  label: string;
  onActivate?: (bucketStart: string) => void;
}

/** A single chart bar segment with the same activation affordance for pointer and keyboard users. */
export function InteractiveBarSegment({ label, onActivate, payload, value, ...props }: InteractiveBarSegmentProps) {
  const bucketStart = typeof payload?.bucketStart === "string" ? payload.bucketStart : null;
  const count = Array.isArray(value) ? Math.abs(value[1] - value[0]) : value;
  const activate = () => {
    if (bucketStart) onActivate?.(bucketStart);
  };
  const interactive = bucketStart !== null && onActivate !== undefined && count > 0;

  return (
    <Rectangle
      {...props}
      role={interactive ? "button" : undefined}
      tabIndex={interactive ? 0 : undefined}
      aria-label={interactive ? `${label}: ${count} on ${bucketLabel(bucketStart)}. Show workspace breakdown.` : undefined}
      className={interactive ? "cursor-pointer focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary" : undefined}
      onClick={interactive ? activate : undefined}
      onKeyDown={
        interactive
          ? (event) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                activate();
              }
            }
          : undefined
      }
    />
  );
}
