"use client";

import type { ProjectProgress } from "@/shared/types";
import { cn } from "@/lib/utils";

export function ProjectProgressBar({
  progress,
  className,
}: {
  progress?: ProjectProgress;
  className?: string;
}) {
  const pct = progress?.percent ?? 0;
  const total = progress?.total ?? 0;
  const completed = progress?.completed ?? 0;

  return (
    <div className={cn("flex items-center gap-2", className)}>
      <div className="h-1.5 flex-1 rounded-full bg-muted overflow-hidden">
        <div
          className="h-full rounded-full bg-primary transition-all"
          style={{ width: `${Math.min(pct, 100)}%` }}
        />
      </div>
      <span className="text-xs text-muted-foreground tabular-nums shrink-0">
        {completed}/{total}
      </span>
    </div>
  );
}
