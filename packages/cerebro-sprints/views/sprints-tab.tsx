"use client";

import { useQuery } from "@tanstack/react-query";

import { sprintSettingsOptions } from "../core/queries";
import { RecurringTasksPanel } from "./recurring-tasks-panel";
import { SprintList } from "./sprint-list";
import { SprintSettingsPanel } from "./sprint-settings-panel";

interface Props {
  workspaceId: string;
  projectId: string;
}

export function SprintsTab({ workspaceId, projectId }: Props) {
  const settingsQuery = useQuery(sprintSettingsOptions(workspaceId, projectId));
  const enabled = !!settingsQuery.data?.enabled;

  return (
    <div className="flex flex-col divide-y overflow-y-auto">
      <SprintSettingsPanel workspaceId={workspaceId} projectId={projectId} />
      {enabled && (
        <>
          <SprintList workspaceId={workspaceId} projectId={projectId} />
          <RecurringTasksPanel workspaceId={workspaceId} projectId={projectId} />
        </>
      )}
    </div>
  );
}
