import { cn } from "@multica/ui/lib/utils";
import type { WorkspaceStatus } from "@/lib/types";

const LABELS: Record<WorkspaceStatus, string> = {
  active: "Active",
  idle: "Idle",
  error: "Error",
};

// packages/ui's Badge variants (default/secondary/destructive/outline/ghost/
// link) don't map 1:1 to the prototype's active(green)/idle(gray)/error(red)
// dot+label status indicator, so this wraps a plain span using the same
// semantic --success/--muted-foreground/--destructive tokens Badge itself
// draws from, rather than hardcoding colors (CLAUDE.md CSS Architecture rule).
const DOT_CLASSES: Record<WorkspaceStatus, string> = {
  active: "bg-success",
  idle: "bg-muted-foreground",
  error: "bg-destructive",
};

const TEXT_CLASSES: Record<WorkspaceStatus, string> = {
  active: "text-success",
  idle: "text-muted-foreground",
  error: "text-destructive",
};

export function StatusBadge({ status }: { status: WorkspaceStatus }) {
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-body font-medium", TEXT_CLASSES[status])}>
      <span className={cn("size-2 rounded-full", DOT_CLASSES[status])} aria-hidden />
      {LABELS[status]}
    </span>
  );
}
