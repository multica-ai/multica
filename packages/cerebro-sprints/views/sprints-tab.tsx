"use client";

import { SprintList } from "./sprint-list";

interface Props {
  workspaceId: string;
  projectId: string;
}

export function SprintsTab({ workspaceId, projectId }: Props) {
  return (
    <div className="flex flex-col overflow-y-auto">
      <SprintList workspaceId={workspaceId} projectId={projectId} />
    </div>
  );
}
