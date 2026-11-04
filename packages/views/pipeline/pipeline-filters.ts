import type {
  Initiative,
  InitiativeStatus,
  ListInitiativesParams,
  PipelineStage,
} from "@multica/core/pipeline";

/**
 * The status choices of the board. Open is running work (active and operating);
 * shelved work leaves the active board, and closed work is kept for the record.
 */
export const STATUS_FILTERS = ["open", "active", "operating", "shelved", "closed"] as const;
export type StatusFilter = (typeof STATUS_FILTERS)[number];

const OPEN_STATUSES: readonly InitiativeStatus[] = ["active", "operating"];

/** The status in the URL, or Open when it is missing or unknown. */
export function parseStatusFilter(value: string | null): StatusFilter {
  return (STATUS_FILTERS as readonly string[]).includes(value ?? "") ? (value as StatusFilter) : "open";
}

/** Open is not a server status, so it asks for every initiative and filters them here. */
export function statusParamFor(filter: StatusFilter): ListInitiativesParams["status"] {
  return filter === "open" ? undefined : filter;
}

/** Whether an initiative is running work: what Open shows and what a squad's room badge counts. */
export function isOpenInitiative(initiative: Pick<Initiative, "status">): boolean {
  return OPEN_STATUSES.includes(initiative.status);
}

export function matchesStatusFilter(initiative: Pick<Initiative, "status">, filter: StatusFilter): boolean {
  return filter === "open" ? isOpenInitiative(initiative) : initiative.status === filter;
}

/** One board column. `stage` is null for a column of initiatives whose stage the pipeline no longer lists. */
export interface StageColumn {
  key: string;
  stage: PipelineStage | null;
  initiatives: Initiative[];
}

/**
 * The board's columns: one per stage in pipeline order, then one for each
 * stage key the pipeline does not list, so no initiative disappears from view.
 */
export function stageColumns(
  stages: readonly PipelineStage[],
  initiatives: readonly Initiative[],
): StageColumn[] {
  const columns: StageColumn[] = stages.map((stage) => ({ key: stage.key, stage, initiatives: [] }));
  const byKey = new Map(columns.map((column) => [column.key, column]));
  for (const initiative of initiatives) {
    let column = byKey.get(initiative.stage_key);
    if (!column) {
      column = { key: initiative.stage_key, stage: null, initiatives: [] };
      byKey.set(column.key, column);
      columns.push(column);
    }
    column.initiatives.push(initiative);
  }
  return columns;
}

/** The number the Company room badge shows for a squad: the open initiatives it owns. */
export function ownedOpenCount(initiatives: readonly Initiative[], squadId: string): number {
  return initiatives.filter((initiative) => initiative.owner_squad_id === squadId && isOpenInitiative(initiative))
    .length;
}
