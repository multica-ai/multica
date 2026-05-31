// Cerebro workflow v2a (FIR-1550) — pure presentation resolver.
//
// Turns a status model into the data the board/picker need to RENDER a
// project's pipeline, while every issue still stores only its base status.
// Kept framework-free so it can be unit-tested without React.

import type { IssueStatus } from "@multica/core/types";
import { ALL_STATUSES } from "@multica/core/issues/config";
import type { CerebroStatusModel } from "./types";

const VALID_BASE = new Set<string>(ALL_STATUSES);

export interface ResolvedCustomStatus {
  /** Stable key (within the model) the picker pins via `custom_status_key`. */
  key: string;
  label: string;
  color?: string;
  baseStatus: IssueStatus;
  position: number;
  description?: string;
}

export interface ResolvedStatusPresentation {
  modelId: string;
  modelName: string;
  /** Base statuses in model order, deduped (first occurrence wins). */
  orderedBaseStatuses: IssueStatus[];
  /** Model label per base status. */
  labelByBase: Record<string, string>;
  /** Model color per base status (omitted when the entry has no color). */
  colorByBase: Record<string, string>;
  /**
   * v2b (FIR-1550): every custom status in model order, no deduplication.
   * Two statuses under the same base BOTH appear here so the picker can
   * render them as distinct options. `customByKey` is a key→entry lookup.
   */
  customStatuses: ResolvedCustomStatus[];
  customByKey: Record<string, ResolvedCustomStatus>;
}

/**
 * Build the render-time presentation for one model.
 *
 * `orderedBaseStatuses` + `labelByBase` + `colorByBase` keep v2a behaviour
 * (one column per base, first-entry wins) so the board renderer doesn't
 * change shape. `customStatuses` is the v2b list — every entry, no dedup —
 * which the picker uses to render true sub-statuses and pin via
 * `custom_status_key`. Unknown base values are skipped so a drifted model
 * never injects a phantom column or picker option.
 */
export function buildStatusPresentation(
  model: CerebroStatusModel,
): ResolvedStatusPresentation {
  const orderedBaseStatuses: IssueStatus[] = [];
  const labelByBase: Record<string, string> = {};
  const colorByBase: Record<string, string> = {};
  const customStatuses: ResolvedCustomStatus[] = [];
  const customByKey: Record<string, ResolvedCustomStatus> = {};

  const sorted = [...model.statuses].sort((a, b) => a.position - b.position);
  for (const entry of sorted) {
    if (!VALID_BASE.has(entry.base_status)) continue;
    const base = entry.base_status as IssueStatus;
    if (!(base in labelByBase)) {
      labelByBase[base] = entry.label;
      if (entry.color) colorByBase[base] = entry.color;
      orderedBaseStatuses.push(base);
    }
    const custom: ResolvedCustomStatus = {
      key: entry.key,
      label: entry.label,
      color: entry.color || undefined,
      baseStatus: base,
      position: entry.position,
      description: entry.description || undefined,
    };
    customStatuses.push(custom);
    customByKey[entry.key] = custom;
  }

  return {
    modelId: model.id,
    modelName: model.name,
    orderedBaseStatuses,
    labelByBase,
    colorByBase,
    customStatuses,
    customByKey,
  };
}
