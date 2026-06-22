"use client";

import type { WorkflowStage } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";

export interface StageCardProps {
  stage: WorkflowStage;
  isSelected: boolean;
  onSelect: (stageId: string) => void;
  stageLabel: string;
  nodesCountLabel: string;
}

export function StageCard({
  stage,
  isSelected,
  onSelect,
  stageLabel,
  nodesCountLabel,
}: StageCardProps) {
  return (
    <button
      data-testid={`stage-card-${stage.id}`}
      data-selected={isSelected ? "true" : "false"}
      onClick={() => onSelect(stage.id)}
      className={cn(
        "flex shrink-0 flex-col gap-1.5 rounded-lg border p-4 text-left transition-colors min-w-[160px]",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        isSelected
          ? "border-primary bg-accent text-accent-foreground"
          : "border-border bg-card hover:bg-accent/50",
      )}
      aria-pressed={isSelected}
      aria-label={`${stage.name}, ${stageLabel}`}
    >
      <span className="text-xs text-muted-foreground">{stageLabel}</span>
      <span className="text-sm font-medium truncate">{stage.name}</span>
      <span className="text-xs text-muted-foreground">{nodesCountLabel}</span>
    </button>
  );
}
