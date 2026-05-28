"use client";

import { cn } from "@multica/ui/lib/utils";
import type { Label } from "@multica/core/types";

export type LabelCreateScope = "project" | "workspace";

export function labelScopeProjectId(scope: LabelCreateScope, projectId?: string | null) {
  return scope === "project" && projectId ? projectId : null;
}

export function labelMatchesScope(label: Pick<Label, "project_id">, scope: LabelCreateScope, projectId?: string | null) {
  if (scope === "workspace") return !label.project_id;
  return Boolean(projectId) && label.project_id === projectId;
}

export function LabelScopeSegment({
  value,
  onValueChange,
  projectLabel,
  workspaceLabel,
  ariaLabel,
  className,
  fullWidth = false,
}: {
  value: LabelCreateScope;
  onValueChange: (value: LabelCreateScope) => void;
  projectLabel: string;
  workspaceLabel: string;
  ariaLabel: string;
  className?: string;
  fullWidth?: boolean;
}) {
  return (
    <div
      role="group"
      aria-label={ariaLabel}
      className={cn(
        "inline-flex h-8 items-center gap-0.5 rounded-md bg-muted p-0.5",
        fullWidth && "w-full",
        className,
      )}
    >
      <ScopeButton
        active={value === "project"}
        label={projectLabel}
        onClick={() => onValueChange("project")}
        fullWidth={fullWidth}
      />
      <ScopeButton
        active={value === "workspace"}
        label={workspaceLabel}
        onClick={() => onValueChange("workspace")}
        fullWidth={fullWidth}
      />
    </div>
  );
}

function ScopeButton({
  active,
  label,
  onClick,
  fullWidth,
}: {
  active: boolean;
  label: string;
  onClick: () => void;
  fullWidth: boolean;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "inline-flex h-7 shrink-0 items-center justify-center rounded-[5px] px-2.5 text-xs font-medium transition-colors",
        fullWidth && "flex-1",
        active
          ? "bg-background text-foreground shadow-sm"
          : "text-muted-foreground hover:text-foreground",
      )}
    >
      {label}
    </button>
  );
}
