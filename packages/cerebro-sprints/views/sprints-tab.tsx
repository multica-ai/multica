"use client";

import { useQuery } from "@tanstack/react-query";

import { sprintSettingsOptions } from "../core/queries";
import { SprintSettingsPanel } from "./sprint-settings-panel";
import { SprintList } from "./sprint-list";
import { RecurringTasksPanel } from "./recurring-tasks-panel";

interface Props {
  workspaceId: string;
  projectId: string;
}

/**
 * SprintsTab is the project-page tab content. It composes the three sub-
 * panels (settings, sprint list, recurring tasks) into one scrollable view.
 * The host project page mounts this tab gated by the cerebro_sprints
 * feature flag — see project-detail.tsx CEREBRO-PATCH.
 */
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
