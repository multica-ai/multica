"use client";

// Cerebro workflow v2a (FIR-1550) — the presentation seam.
//
// Upstream board/picker components call useStatusPresentation(). With no
// provider mounted (the common case, every non-project surface) the hook
// returns DEFAULT and those components render exactly as upstream does.
// CerebroStatusModelProvider wraps a single project's issue surface and, when
// that project has an assigned status model, swaps in the model's labels,
// colors and column order — all derived in the cerebro zone.

import { createContext, useContext, useMemo, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import type { IssueStatus } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  cerebroStatusModelAssignmentsOptions,
  cerebroStatusModelsListOptions,
} from "../core/queries";
import { buildStatusPresentation, type ResolvedCustomStatus } from "../core/presentation";

export interface StatusPresentationOption {
  baseStatus: IssueStatus;
  label: string;
  color?: string;
}

export interface StatusPresentationValue {
  /** True only when the surrounding project has a usable status model. */
  hasModel: boolean;
  modelName?: string;
  modelId?: string;
  /** Base statuses to show as board columns, in model order. */
  orderedBaseStatuses?: IssueStatus[];
  /** Model label for a base status, or undefined → caller uses upstream label. */
  getLabel: (base: IssueStatus) => string | undefined;
  /** Model color (hex) for a base status, or undefined → caller uses upstream. */
  getColor: (base: IssueStatus) => string | undefined;
  /** Picker options for covered base statuses, in model order. */
  options?: StatusPresentationOption[];
  /**
   * v2b (FIR-1550): every custom status in model order, no deduplication.
   * Pickers iterate this to render true sub-statuses; pinning a row passes
   * `custom_status_key` alongside the base `status` so the backend
   * resolver writes the sidecar.
   */
  customStatuses?: ResolvedCustomStatus[];
  /** v2b key-lookup so the trigger / badge can resolve label + color. */
  getCustomByKey?: (key: string) => ResolvedCustomStatus | undefined;
}

const DEFAULT: StatusPresentationValue = {
  hasModel: false,
  getLabel: () => undefined,
  getColor: () => undefined,
};

const StatusPresentationContext = createContext<StatusPresentationValue>(DEFAULT);

export function useStatusPresentation(): StatusPresentationValue {
  return useContext(StatusPresentationContext);
}

export function CerebroStatusModelProvider({
  projectId,
  children,
}: {
  projectId: string;
  children: ReactNode;
}) {
  const enabled = useFeatureFlag("cerebro_workflows");
  const wsId = useWorkspaceId();

  const { data: assignments } = useQuery({
    ...cerebroStatusModelAssignmentsOptions(wsId),
    enabled: enabled && !!wsId,
  });
  const { data: models } = useQuery({
    ...cerebroStatusModelsListOptions(wsId),
    enabled: enabled && !!wsId,
  });

  const value = useMemo<StatusPresentationValue>(() => {
    if (!enabled) return DEFAULT;
    const assignment = assignments?.assignments.find((a) => a.project_id === projectId);
    if (!assignment) return DEFAULT;
    const model = models?.status_models.find((m) => m.id === assignment.status_model_id);
    if (!model) return DEFAULT;

    const resolved = buildStatusPresentation(model);
    if (resolved.orderedBaseStatuses.length === 0) return DEFAULT;

    return {
      hasModel: true,
      modelName: resolved.modelName,
      modelId: resolved.modelId,
      orderedBaseStatuses: resolved.orderedBaseStatuses,
      getLabel: (base) => resolved.labelByBase[base],
      getColor: (base) => resolved.colorByBase[base],
      options: resolved.orderedBaseStatuses.map((base) => ({
        baseStatus: base,
        label: resolved.labelByBase[base]!,
        color: resolved.colorByBase[base],
      })),
      customStatuses: resolved.customStatuses,
      getCustomByKey: (key) => resolved.customByKey[key],
    };
  }, [enabled, assignments, models, projectId]);

  return (
    <StatusPresentationContext.Provider value={value}>
      {children}
    </StatusPresentationContext.Provider>
  );
}
