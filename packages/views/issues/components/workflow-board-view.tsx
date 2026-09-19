"use client";

import { useMemo, type ComponentProps } from "react";
import type { IssueTableGroupDescriptor } from "@multica/core/types";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { Button } from "@multica/ui/components/ui/button";
import { BoardView } from "./board-view";
import { WorkflowLaneFrame } from "./workflow-lane-frame";
import { useT } from "../../i18n";
import type { IssueGroupBranches } from "../surface/use-issue-group-branches";
import { workflowLaneBranches } from "../utils/workflow-lanes";

type Props = ComponentProps<typeof BoardView>;
const EMPTY_STATUSES: NonNullable<Props["workflowStatuses"]> = [];

/** Each lane owns a DndContext: a drag can only target its own concrete nodes. */
function WorkflowLane({ lane, branches, ...props }: Props & {
  lane: IssueTableGroupDescriptor;
  branches: IssueGroupBranches;
}) {
  const statusFilters = useViewStore((s) => s.statusFilters);
  const laneBranches = useMemo(
    () => workflowLaneBranches(branches, lane, statusFilters),
    [branches, lane, statusFilters],
  );
  return <BoardView {...props} groupBranches={laneBranches} workflowStatuses={EMPTY_STATUSES} ownWorkflow
    onCreateIssue={(defaults) => props.onCreateIssue?.({
      ...defaults,
      // Explicit null overrides a remembered draft project. The create form
      // requires a choice, then resolves that project's effective workflow.
      project_id: props.projectId ?? null,
      required_workflow_id: lane.value.kind === "workflow" ? lane.value.workflow_id ?? undefined : undefined,
      require_project_choice: !props.projectId,
    })}
  />;
}

export function WorkflowBoardView(props: Props) {
  const { t } = useT("issues");
  const statusFilters = useViewStore((s) => s.statusFilters);
  const hiddenStatuses = useViewStore((s) => s.hiddenStatuses);
  const branches = props.groupBranches;
  if (!branches) return null;
  const lanes = branches.descriptors.filter((lane) => lane.value.kind === "workflow");
  // An explicitly opened empty project still offers its active drop targets.
  if (lanes.length === 0 && !branches.isError && props.workflowStatuses?.length) {
    return <BoardView {...props} ownWorkflow onCreateIssue={(defaults) => props.onCreateIssue?.({
      ...defaults, project_id: props.projectId ?? null,
      required_workflow_id: props.workflowStatuses?.[0]?.workflow_id,
      require_project_choice: !props.projectId,
    })} />;
  }
  const primaryKey = lanes.find((lane) => lane.value.kind === "workflow" && lane.value.is_default)?.key ?? lanes[0]?.key;
  const single = lanes.length === 1 && !branches.hasMoreGroups;
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-auto">
      {lanes.map((lane) => {
        const name = lane.value.kind === "workflow" ? lane.value.name : "";
        const board = <WorkflowLane {...props} lane={lane} branches={branches} />;
        if (single) return <section key={lane.key} className="flex min-h-0 flex-1 flex-col">{board}</section>;
        return <WorkflowLaneFrame key={lane.key} laneKey={lane.key} primary={lane.key === primaryKey}
          name={name || t(($) => $.board.legacy_workflow)} count={lane.count}
          maxColumnCount={Math.max(0, ...(lane.secondary_groups ?? []).filter(({ value, count }) => {
            if (value.kind !== "workflow_status") return false;
            const keys = [value.workflow_status_id, value.status].filter((key): key is string => !!key);
            const selected = keys.some((key) => statusFilters.includes(key));
            return count > 0 || selected || (statusFilters.length === 0 && !keys.some((key) => hiddenStatuses.includes(key)));
          }).map((cell) => cell.count))}>
          {board}
        </WorkflowLaneFrame>;
      })}
      {(branches.hasMoreGroups || branches.isError) && (
        <div className="flex shrink-0 justify-center p-3">
          <Button variant="ghost" disabled={branches.isLoadingMoreGroups}
            onClick={branches.isError ? branches.retryGroups : branches.loadMoreGroups}>
            {branches.isError ? t(($) => $.table.load_more_failed_retry) : t(($) => $.board.more_workflows)}
          </Button>
        </div>
      )}
    </div>
  );
}
