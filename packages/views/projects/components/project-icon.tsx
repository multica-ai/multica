import type { Project } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";

export type ProjectIconSize = "sm" | "md" | "lg";

export interface ProjectIconProps {
  project?: Pick<Project, "icon"> | null;
  size?: ProjectIconSize;
  className?: string;
}

const SIZE_CLASS: Record<ProjectIconSize, string> = {
  sm: "size-3.5 text-xs leading-none",
  md: "size-4 text-sm leading-none",
  lg: "size-6 text-2xl leading-none",
};

export function ProjectIcon({ project, size = "sm", className }: ProjectIconProps) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "inline-flex shrink-0 items-center justify-center",
        SIZE_CLASS[size],
        className,
      )}
    >
      {project?.icon || "📁"}
    </span>
  );
}
