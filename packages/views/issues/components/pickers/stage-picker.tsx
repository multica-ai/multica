"use client";

import { useState } from "react";
import { Milestone, Plus } from "lucide-react";
import type { UpdateIssueRequest } from "@multica/core/types";
import { PropertyPicker, PickerItem } from "./property-picker";
import { useT } from "../../../i18n";

/**
 * Highest stage assigned among a parent's children (0 when none are staged).
 * Tells {@link StagePicker} how far to extend its option list so an already-used
 * higher stage stays selectable when creating or editing a sibling.
 */
export function maxSiblingStage(children: readonly { stage: number | null }[]): number {
  return children.reduce((m, c) => (c.stage != null && c.stage > m ? c.stage : m), 0);
}

export const DEFAULT_STAGE_OPTIONS_FLOOR = 5;

/**
 * Stage options (Stage 1..top) the picker offers. `top` always covers the
 * current stage, the highest sibling stage (`maxStage`), and one beyond it so a
 * new stage can be added — floored so Stage 1–5 are always selectable.
 */
export function stageOptions(
  stage: number | null,
  maxStage = 0,
  floor = DEFAULT_STAGE_OPTIONS_FLOOR,
): number[] {
  const top = Math.max(stage ?? 0, maxStage, floor - 1) + 1;
  return Array.from({ length: top }, (_, i) => i + 1);
}

export function StagePicker({
  stage,
  onUpdate,
  maxStage = 0,
  trigger: customTrigger,
  triggerRender,
  open: controlledOpen,
  onOpenChange: controlledOnOpenChange,
  align,
  defaultOpen = false,
}: {
  stage: number | null;
  onUpdate: (updates: Partial<UpdateIssueRequest>) => void;
  /** Highest stage among siblings, so the picker can offer one beyond it. */
  maxStage?: number;
  trigger?: React.ReactNode;
  triggerRender?: React.ReactElement;
  open?: boolean;
  onOpenChange?: (v: boolean) => void;
  align?: "start" | "center" | "end";
  /** Open the picker on first mount (progressive-disclosure sidebars). */
  defaultOpen?: boolean;
}) {
  const [internalOpen, setInternalOpen] = useState(defaultOpen);
  const [extraStageCount, setExtraStageCount] = useState(0);
  const open = controlledOpen ?? internalOpen;
  const setOpen = (v: boolean) => {
    if (!v) setExtraStageCount(0);
    (controlledOnOpenChange ?? setInternalOpen)(v);
  };
  const { t } = useT("issues");

  const effectiveMax = Math.max(
    maxStage,
    (stage ?? 0) + extraStageCount,
    extraStageCount ? DEFAULT_STAGE_OPTIONS_FLOOR + extraStageCount - 1 : 0,
  );
  const options = stageOptions(stage, effectiveMax);

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-44"
      align={align}
      triggerRender={triggerRender}
      trigger={
        customTrigger ?? (
          <>
            <Milestone className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">
              {stage == null
                ? t(($) => $.stage.none)
                : t(($) => $.stage.value, { n: stage })}
            </span>
          </>
        )
      }
      footer={
        <button
          type="button"
          onClick={() => setExtraStageCount((c) => c + 1)}
          className="flex w-full items-center gap-1.5 rounded-xs px-2 py-1 text-caption text-muted-foreground hover:bg-accent/50 hover:text-foreground transition-colors cursor-pointer"
        >
          <Plus className="h-3 w-3 shrink-0" />
          <span>{t(($) => $.stage.add)}</span>
        </button>
      }
    >
      {/* "No stage" — always the first row, matching every other picker. Keeps
          the value rows' icon column so the labels line up. */}
      <PickerItem
        emptyValue
        selected={stage == null}
        onClick={() => {
          onUpdate({ stage: null });
          setOpen(false);
        }}
      >
        <span className="inline-flex items-center gap-1.5 text-caption text-muted-foreground">
          <Milestone className="h-3 w-3 shrink-0" />
          <span className="truncate">{t(($) => $.stage.none)}</span>
        </span>
      </PickerItem>
      {options.map((s) => (
        <PickerItem
          key={s}
          selected={s === stage}
          onClick={() => {
            onUpdate({ stage: s });
            setOpen(false);
          }}
        >
          <span className="inline-flex items-center gap-1.5 text-caption">
            <Milestone className="h-3 w-3 shrink-0 text-muted-foreground" />
            {t(($) => $.stage.value, { n: s })}
          </span>
        </PickerItem>
      ))}
    </PropertyPicker>
  );
}
