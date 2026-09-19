"use client";

import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { projectListOptions } from "../projects/queries";
import { useWorkspaceId } from "../hooks";
import { useIssuesScopeStore, type GlobalIssuesPage } from "./stores/issues-scope-store";

export function useIssueProjectScope(page: GlobalIssuesPage) {
  const wsId = useWorkspaceId();
  const selected = useIssuesScopeStore((s) => s.projects[page] ?? null);
  const setProject = useIssuesScopeStore((s) => s.setProject);
  const projects = useQuery(projectListOptions(wsId));
  const unavailable = selected !== null && projects.isSuccess &&
    !projects.data.some((project) => project.id === selected);
  useEffect(() => {
    if (unavailable) setProject(page, null);
  }, [page, setProject, unavailable]);
  return {
    projectId: unavailable ? null : selected,
    setProjectId: (projectId: string | null) => setProject(page, projectId),
  };
}
