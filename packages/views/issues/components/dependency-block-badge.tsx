"use client";

import { Link2 } from "lucide-react";

export function DependencyBlockBadge({ count }: { count: number }) {
  if (count <= 0) return null;

  return (
    <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-warning/10 px-1.5 py-0.5 text-[11px] font-medium text-warning">
      <Link2 className="h-3 w-3" />
      Blocked by {count}
    </span>
  );
}
