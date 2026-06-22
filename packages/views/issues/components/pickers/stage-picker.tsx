"use client";

import { useState } from "react";
import { Milestone } from "lucide-react";
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

/**
 * Stage options (Stage 1..top) the picker offers. `top` always covers the
 * current stage, the highest sibling stage (`maxStage`), and one beyond it so a
 * new stage can be added — floored so Stage 1–3 are always selectable.
 */
export function stageOptions(stage: number | null, maxStage = 0): number[] {
  const top = Math.max(stage ?? 0, maxStage, 2) + 1;
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
  const open = controlledOpen ?? internalOpen;
  const setOpen = controlledOnOpenChange ?? setInternalOpen;
  const { t } = useT("issues");

  const options = stageOptions(stage, maxStage);

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
    >
      <PickerItem
        selected={stage == null}
        onClick={() => {
          onUpdate({ stage: null });
          setOpen(false);
        }}
      >
        <span className="truncate text-xs text-muted-foreground">{t(($) => $.stage.none)}</span>
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
          <span className="inline-flex items-center gap-1.5 text-xs">
            <Milestone className="h-3 w-3 shrink-0 text-muted-foreground" />
            {t(($) => $.stage.value, { n: s })}
          </span>
        </PickerItem>
      ))}
    </PropertyPicker>
  );
}
