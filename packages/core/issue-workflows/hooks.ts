import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { projectListOptions } from "../projects/queries";
import type { IssueWorkflow, Project } from "../types";
import { issueWorkflowListOptions, resolveProjectWorkflow } from "./queries";

const EMPTY: IssueWorkflow[] = [];

/** The workspace's workflows, in name order. Empty until loaded. */
export function useIssueWorkflows(wsId: string): { workflows: IssueWorkflow[]; isLoaded: boolean } {
  const { data } = useQuery({ ...issueWorkflowListOptions(wsId), enabled: Boolean(wsId) });
  return useMemo(() => ({ workflows: data ?? EMPTY, isLoaded: data !== undefined }), [data]);
}

/**
 * A project and the workflow it uses. `workflow` is null for the Default
 * workflow — including while either list is still loading, which renders the
 * Default behavior rather than an empty board.
 */
export function useProjectWithWorkflow(
  wsId: string,
  projectId: string | null | undefined,
): { project: Project | null; workflow: IssueWorkflow | null } {
  const { workflows } = useIssueWorkflows(wsId);
  const { data: projects } = useQuery({
    ...projectListOptions(wsId),
    enabled: Boolean(wsId) && Boolean(projectId),
  });
  return useMemo(() => {
    const project = projectId ? projects?.find((p) => p.id === projectId) ?? null : null;
    return { project, workflow: resolveProjectWorkflow(workflows, project) };
  }, [projectId, projects, workflows]);
}

/** The workflow a project uses, or null for the Default workflow. */
export function useProjectWorkflow(
  wsId: string,
  projectId: string | null | undefined,
): IssueWorkflow | null {
  return useProjectWithWorkflow(wsId, projectId).workflow;
}
