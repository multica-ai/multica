"use client";

import { useState, type ComponentProps } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectMoveMayHandOff, resolveProjectWorkflow, useIssueWorkflows } from "@multica/core/issue-workflows";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Project, UpdateIssueRequest } from "@multica/core/types";
import { WorkflowHandoffConfirmDialog } from "../../workflows/handoff-confirm-dialog";
import { ProjectPicker } from "./project-picker";

/**
 * The project picker of a single issue (MUL-7420). Moving the issue into a
 * project whose workflow hands off the column it lands on first confirms the
 * handoff, like a status change would; the write then enters that column.
 * Create and batch surfaces use the plain picker, where a move stays
 * administrative.
 */
export function IssueProjectPicker({
  issue,
  onUpdate,
  ...props
}: ComponentProps<typeof ProjectPicker> & {
  issue: { id: string; identifier: string; status: string; project_id: string | null };
}) {
  const wsId = useWorkspaceId();
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { workflows } = useIssueWorkflows(wsId);
  const [pending, setPending] = useState<Project | null>(null);

  const update = (updates: Partial<UpdateIssueRequest>) => {
    const target = updates.project_id ? projects.find((p) => p.id === updates.project_id) : undefined;
    if (
      target &&
      target.id !== issue.project_id &&
      projectMoveMayHandOff(resolveProjectWorkflow(workflows, target), issue.status)
    ) {
      setPending(target);
      return;
    }
    onUpdate(updates);
  };

  return (
    <>
      <ProjectPicker {...props} onUpdate={update} />
      <WorkflowHandoffConfirmDialog
        issue={pending ? issue : null}
        toStatus=""
        toProject={pending}
        onCancel={() => setPending(null)}
        onConfirm={({ stopPreviousRuns }) => {
          const next = pending;
          setPending(null);
          if (next) {
            onUpdate({
              project_id: next.id,
              enter_workflow_step: true,
              ...(stopPreviousRuns ? { stop_previous_assignee_runs: true } : {}),
            });
          }
        }}
      />
    </>
  );
}
