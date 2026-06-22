"use client";

import type { WorkflowStage } from "@multica/core/types";
import { useT } from "../../../i18n";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { Plus } from "lucide-react";
import { StageCard } from "./stage-card";

export interface StageCanvasProps {
  stages: WorkflowStage[];
  selectedStageId: string | null;
  onStageSelect: (stageId: string) => void;
  onAddStage?: () => void;
  unassignedNodeCount?: number;
}

export function StageCanvas({ stages, selectedStageId, onStageSelect, onAddStage, unassignedNodeCount = 0 }: StageCanvasProps) {
  const { t } = useT("workflows");

  // ── Empty state ──
  if (stages.length === 0) {
    return (
      <div
        data-testid="empty-stage-state"
        className="flex flex-col items-center justify-center gap-3 py-12 text-center"
      >
        <p className="text-sm font-medium text-muted-foreground">
          {t(($) => $.overview.stage_canvas.empty_title)}
        </p>
        <p className="text-xs text-muted-foreground max-w-md">
          {t(($) => $.overview.stage_canvas.empty_description)}
        </p>
        <Button data-testid="add-stage-button" size="sm" onClick={onAddStage}>
          <Plus className="mr-1.5 h-4 w-4" />
          {t(($) => $.overview.stage_canvas.create_first)}
        </Button>

        {unassignedNodeCount > 0 && (
          <UnassignedCard
            count={unassignedNodeCount}
            isSelected={selectedStageId === "unassigned"}
            onSelect={() => onStageSelect("unassigned")}
          />
        )}
      </div>
    );
  }

  // ── Stage cards ──
  return (
    <div
      data-testid="stage-canvas"
      className="flex gap-3 overflow-x-auto pb-2"
    >
      {stages.map((stage, index) => (
        <StageCard
          key={stage.id}
          stage={stage}
          isSelected={selectedStageId === stage.id}
          onSelect={onStageSelect}
          stageLabel={t(($) => $.overview.stage_canvas.stage_n_of_m, { n: index + 1, m: stages.length })}
          nodesCountLabel={t(($) => $.overview.stage_canvas.nodes_count, { count: stage.node_count })}
        />
      ))}
      <Button
        data-testid="add-stage-button"
        variant="outline"
        size="sm"
        className="shrink-0 h-auto min-h-[104px] px-4"
        onClick={onAddStage}
      >
        <Plus className="h-4 w-4 mr-1" />
        {t(($) => $.overview.stage_canvas.add_stage)}
      </Button>

      {unassignedNodeCount > 0 && (
        <UnassignedCard
          count={unassignedNodeCount}
          isSelected={selectedStageId === "unassigned"}
          onSelect={() => onStageSelect("unassigned")}
        />
      )}
    </div>
  );
}

/** Virtual card for nodes with no stage assignment (stage_id = null). */
function UnassignedCard({
  count,
  isSelected,
  onSelect,
}: {
  count: number;
  isSelected: boolean;
  onSelect: () => void;
}) {
  const { t } = useT("workflows");
  return (
    <button
      data-testid="stage-card-unassigned"
      data-selected={isSelected ? "true" : "false"}
      onClick={onSelect}
      className={cn(
        "flex shrink-0 flex-col gap-1.5 rounded-lg border-2 border-dashed p-4 text-left transition-colors min-w-[160px]",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        isSelected
          ? "border-primary bg-accent text-accent-foreground"
          : "border-muted-foreground/30 bg-card text-muted-foreground hover:bg-accent/50",
      )}
      aria-pressed={isSelected}
    >
      <span className="text-xs text-muted-foreground">{t(($) => $.overview.stage_canvas.unassigned)}</span>
      <span className="text-sm font-medium truncate">{t(($) => $.overview.stage_canvas.unassigned)}</span>
      <span className="text-xs text-muted-foreground">
        {t(($) => $.overview.stage_canvas.nodes_count, { count })}
      </span>
    </button>
  );
}
