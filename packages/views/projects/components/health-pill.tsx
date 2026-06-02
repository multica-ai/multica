import type { ProjectHealth } from "@multica/core/types/project";
import { cn } from "@multica/ui/lib/utils";

interface HealthPillProps {
  health: ProjectHealth | null | undefined;
  className?: string;
}

const CONFIG: Record<ProjectHealth, { label: string; dot: string; text: string }> = {
  on_track: { label: "On track", dot: "bg-emerald-500", text: "text-emerald-600 dark:text-emerald-400" },
  at_risk: { label: "At risk", dot: "bg-amber-500", text: "text-amber-600 dark:text-amber-400" },
  off_track: { label: "Off track", dot: "bg-red-500", text: "text-red-600 dark:text-red-400" },
};

export function HealthPill({ health, className }: HealthPillProps) {
  const cfg = health ? CONFIG[health as ProjectHealth] : undefined;
  if (!cfg) {
    return (
      <span className={cn("inline-flex items-center gap-1.5 text-xs text-muted-foreground", className)}>
        <span className="h-2 w-2 rounded-full bg-muted-foreground/40" />
        No update
      </span>
    );
  }
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-xs font-medium", cfg.text, className)}>
      <span className={cn("h-2 w-2 rounded-full", cfg.dot)} />
      {cfg.label}
    </span>
  );
}
