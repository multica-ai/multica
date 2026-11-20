"use client";

import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { NO_PROJECT_SIDEBAR_SOURCE, projectSidebarSource, sidebarProjects } from "@multica/core/issues/sidebar-list";
import { useSidebarProjectPrefs } from "@multica/core/issues/sidebar-project-prefs";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import { useWorkspacePaths } from "@multica/core/paths";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Issue } from "@multica/core/types";
import { useSidebarSafe } from "@multica/ui/components/ui/sidebar";
import { useNavigation } from "../../navigation";
import { useAppShell } from "../app-shell-context";

/**
 * Opens the issue's project row, or "No project", in the Work panel and
 * scrolls to the issue, or `null` off an issue route (the panel only marks
 * the route's issue) or when the panel does not show that row.
 */
export function useShowInSidebar(issue: Pick<Issue, "id" | "project_id"> | null | undefined): (() => void) | null {
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);
  const shell = useAppShell();
  const sidebar = useSidebarSafe();
  const paths = useWorkspacePaths();
  const { pathname } = useNavigation();
  const { data: projects } = useQuery({ ...projectListOptions(wsId), enabled: !!issue?.project_id });
  const { projectSettings, projectOrder } = useSidebarProjectPrefs(wsId);
  const projectId = issue?.project_id;
  if (!issue || !pathname.startsWith(paths.issues() + "/")) return null;
  let source: string;
  if (projectId) {
    if (!projects) return null;
    const listed = sidebarProjects(projects, projectSettings, projectOrder, userId).some(
      (project) => project.id === projectId,
    );
    if (!listed) return null;
    source = projectSidebarSource(projectId);
  } else {
    if (!projectSettings.showNoProject) return null;
    source = NO_PROJECT_SIDEBAR_SOURCE;
  }
  return () => {
    // `navCollapsed` is always false in the compact layout, where the nav is a sheet.
    if (sidebar?.isCompact) sidebar.setOpenMobile(true);
    else if (shell?.navCollapsed) shell.toggleNav();
    useSidebarTreeStore.getState().requestLocate({ issueId: issue.id, source });
  };
}
