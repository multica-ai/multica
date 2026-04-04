"use client";

import type { ProjectStatus } from "@/shared/types";
import { PROJECT_STATUS_CONFIG } from "@/features/projects/config/status";
import { cn } from "@/lib/utils";

export function ProjectStatusBadge({
  status,
  className,
}: {
  status: ProjectStatus;
  className?: string;
}) {
  const config = PROJECT_STATUS_CONFIG[status];
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium",
        config.bg,
        config.color,
        className,
      )}
    >
      {config.label}
    </span>
  );
}
