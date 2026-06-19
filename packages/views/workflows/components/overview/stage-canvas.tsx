"use client";

import type { WorkflowStage } from "@multica/core/types";
import { useT } from "../../../i18n";
import { Button } from "@multica/ui/components/ui/button";
import { Plus } from "lucide-react";
import { StageCard } from "./stage-card";

export interface StageCanvasProps {
  stages: WorkflowStage[];
  selectedStageId: string | null;
  onStageSelect: (stageId: string) => void;
}

export function StageCanvas({ stages, selectedStageId, onStageSelect }: StageCanvasProps) {
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
        <Button data-testid="add-stage-button" size="sm">
          <Plus className="mr-1.5 h-4 w-4" />
          {t(($) => $.overview.stage_canvas.create_first)}
        </Button>
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
      >
        <Plus className="h-4 w-4 mr-1" />
        {t(($) => $.overview.stage_canvas.add_stage)}
      </Button>
    </div>
  );
}
