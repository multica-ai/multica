"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { isIssueUuid } from "@multica/core/issues/canonical-id";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { projectSidebarSource, sidebarProjects } from "@multica/core/issues/sidebar-list";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Project } from "@multica/core/types";
import { useNavigation } from "../../navigation";
import { IssueContextMenuProvider } from "../../issues/actions";
import { useT } from "../../i18n";
import { PanelBody, PanelHeader } from "../panel-nav";
import { PinnedSection, usePanelPins } from "./pinned-section";
import { ProjectsSection, useIssueIdInTree } from "./work-tree";
import { ViewsSection } from "./views-section";

const EMPTY_PROJECTS: Project[] = [];

/** The `{id}` of an `/issues/{id}` route — a UUID or an identifier like `MUL-123`. */
function issueRouteSegment(pathname: string, issuesPath: string): string | null {
  if (!pathname.startsWith(issuesPath + "/")) return null;
  const segment = pathname.slice(issuesPath.length + 1).split("/")[0];
  return segment ? decodeURIComponent(segment) : null;
}

export function WorkPanel() {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const { pathname } = useNavigation();
  const wsId = useCurrentWorkspace()?.id;
  const userId = useAuthStore((s) => s.user?.id);

  const { data: projects = EMPTY_PROJECTS } = useQuery({
    ...projectListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  // Cache read only: the detail page owns resolving the route's issue.
  const issueSegment = issueRouteSegment(pathname, p.issues());
  const { data: currentIssue } = useQuery({
    ...issueDetailOptions(wsId ?? "", issueSegment ?? ""),
    enabled: false,
  });
  // A tree row takes the highlight from "All issues" while it lists the issue.
  const treeIssueId = useIssueIdInTree(wsId ?? "", issueSegment);
  // The tree and a UUID route name the issue before the page has loaded it;
  // waiting for the load would light "All issues" first.
  const currentIssueId = issueSegment
    ? (currentIssue?.id ?? treeIssueId ?? (isIssueUuid(issueSegment) ? issueSegment : null))
    : null;
  // An issue can sit in Pinned and in the tree at once. The row that was
  // clicked keeps the highlight; arriving any other way (search, a link, a
  // shortcut) lights the pin.
  const [clickedIssue, setClickedIssue] = useState<{ segment: string; fromPins: boolean } | null>(null);
  const treeClaimsIssue =
    !!clickedIssue &&
    !clickedIssue.fromPins &&
    !!currentIssueId &&
    (clickedIssue.segment === currentIssueId || clickedIssue.segment === currentIssue?.identifier);
  const pins = usePanelPins(wsId, userId, pathname, treeClaimsIssue ? null : currentIssueId);
  const rememberClickedIssue = (event: React.MouseEvent) => {
    const link = (event.target as HTMLElement).closest("a[href]");
    if (!link) return;
    const path = new URL(link.getAttribute("href") ?? "", window.location.href).pathname;
    const segment = issueRouteSegment(path, p.issues());
    setClickedIssue(segment ? { segment, fromPins: !!link.closest("[data-pinned-section]") } : null);
  };
  // A lit pin already marks the issue; opening its project would only push
  // the rest of the panel down.
  const revealProjectId =
    issueSegment && !pins.isActivePinnedRoute ? (currentIssue?.project_id ?? null) : null;

  const setExpanded = useSidebarTreeStore((s) => s.setExpanded);
  useEffect(() => {
    if (revealProjectId) setExpanded(projectSidebarSource(revealProjectId), true);
  }, [revealProjectId, setExpanded]);

  const projectSettings = useSidebarTreeStore((s) => s.projectSettings);
  const projectOrder = useSidebarTreeStore((s) => s.projectOrder);
  const shownProjects = useMemo(() => {
    const shown = sidebarProjects(projects, projectSettings, projectOrder, userId);
    const revealed = projects.find((project) => project.id === revealProjectId);
    return revealed && !shown.includes(revealed) ? [...shown, revealed] : shown;
  }, [projects, projectSettings, projectOrder, userId, revealProjectId]);
  const issueLitInTree = treeIssueId !== null;
  const plainRowsLit = !pins.isActivePinnedRoute;

  return (
    <>
      <PanelHeader title={t(($) => $.spaces.work)} />
      {/* Outside PanelBody: clicks inside the menu's portal still bubble
          through the React tree, and must not reach rememberClickedIssue. */}
      <IssueContextMenuProvider variant="sidebar">
        <PanelBody onClickCapture={rememberClickedIssue}>
          {wsId && (
            <ViewsSection
              wsId={wsId}
              pathname={pathname}
              plainRowsLit={plainRowsLit}
              issueLitInTree={issueLitInTree}
              activeIssueId={plainRowsLit ? currentIssueId : null}
            />
          )}

          {wsId && (
            <div data-pinned-section className="contents">
              <PinnedSection pins={pins} wsId={wsId} />
            </div>
          )}

          {wsId && (
            <ProjectsSection
              wsId={wsId}
              userId={userId}
              pathname={pathname}
              projects={shownProjects}
              totalProjects={projects.length}
              plainRowsLit={plainRowsLit}
              activeIssueId={plainRowsLit ? currentIssueId : null}
            />
          )}
        </PanelBody>
      </IssueContextMenuProvider>
    </>
  );
}
