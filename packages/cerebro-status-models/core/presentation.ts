// Cerebro workflow v2a (FIR-1550) — pure presentation resolver.
//
// Turns a status model into the data the board/picker need to RENDER a
// project's pipeline, while every issue still stores only its base status.
// Kept framework-free so it can be unit-tested without React.

import type { IssueStatus } from "@multica/core/types";
import { ALL_STATUSES } from "@multica/core/issues/config";
import type { CerebroStatusModel } from "./types";

const VALID_BASE = new Set<string>(ALL_STATUSES);

export interface ResolvedStatusPresentation {
  modelId: string;
  modelName: string;
  /** Base statuses in model order, deduped (first occurrence wins). */
  orderedBaseStatuses: IssueStatus[];
  /** Model label per base status. */
  labelByBase: Record<string, string>;
  /** Model color per base status (omitted when the entry has no color). */
  colorByBase: Record<string, string>;
}

/**
 * Build the render-time presentation for one model.
 *
 * Duplicate-base statuses collapse to the FIRST entry for that base — true
 * separate columns for two statuses sharing one base need a per-issue custom
 * status id and are out of v2a scope (FIR-1553). Unknown base values are
 * skipped so a drifted model never injects a phantom column.
 */
export function buildStatusPresentation(
  model: CerebroStatusModel,
): ResolvedStatusPresentation {
  const orderedBaseStatuses: IssueStatus[] = [];
  const labelByBase: Record<string, string> = {};
  const colorByBase: Record<string, string> = {};

  const sorted = [...model.statuses].sort((a, b) => a.position - b.position);
  for (const entry of sorted) {
    if (!VALID_BASE.has(entry.base_status)) continue;
    const base = entry.base_status as IssueStatus;
    if (base in labelByBase) continue; // first-match collapse
    labelByBase[base] = entry.label;
    if (entry.color) colorByBase[base] = entry.color;
    orderedBaseStatuses.push(base);
  }

  return {
    modelId: model.id,
    modelName: model.name,
    orderedBaseStatuses,
    labelByBase,
    colorByBase,
  };
}
