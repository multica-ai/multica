import type { ProjectHealth } from "@multica/core/types/project";
import { cn } from "@multica/ui/lib/utils";

interface HealthPillProps {
  health: ProjectHealth | null | undefined;
  /** Render only the colored dot (with the health as an accessible label/tooltip). */
  dotOnly?: boolean;
  className?: string;
}

const CONFIG: Record<ProjectHealth, { label: string; dot: string; text: string }> = {
  on_track: { label: "On track", dot: "bg-emerald-500", text: "text-emerald-600 dark:text-emerald-400" },
  at_risk: { label: "At risk", dot: "bg-amber-500", text: "text-amber-600 dark:text-amber-400" },
  off_track: { label: "Off track", dot: "bg-red-500", text: "text-red-600 dark:text-red-400" },
};

export function HealthPill({ health, dotOnly, className }: HealthPillProps) {
  const cfg = health ? CONFIG[health as ProjectHealth] : undefined;
  const label = cfg?.label ?? "No update";
  const dotClass = cfg?.dot ?? "bg-muted-foreground/40";

  if (dotOnly) {
    return (
      <span
        role="img"
        aria-label={label}
        title={label}
        className={cn("inline-block h-2 w-2 shrink-0 rounded-full", dotClass, className)}
      />
    );
  }

  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 text-xs font-medium",
        cfg ? cfg.text : "text-muted-foreground",
        className,
      )}
    >
      <span className={cn("h-2 w-2 rounded-full", dotClass)} />
      {label}
    </span>
  );
}
