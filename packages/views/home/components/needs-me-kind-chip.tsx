"use client";

import type { NeedsMeItem } from "@multica/core/home";
import { cn } from "@multica/ui/lib/utils";

// Status entries borrow their status's semantic tone so the chip reads as the
// status it is; inbox-derived kinds get their own quiet tones.
const KIND_TONE: Record<NeedsMeItem["kind"], string> = {
  in_review: "bg-info/10 text-info",
  blocked: "bg-destructive/10 text-destructive",
  mentioned: "bg-brand/10 text-brand",
  action_required: "bg-warning/10 text-warning",
};

export function NeedsMeKindChip({
  item,
  label,
  className,
}: {
  item: NeedsMeItem;
  label: string;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center rounded-sm px-1.5 py-0.5 text-caption font-medium",
        KIND_TONE[item.kind],
        className,
      )}
    >
      {label}
    </span>
  );
}
