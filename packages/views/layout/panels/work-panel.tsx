"use client";

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { isIssueUuid } from "@multica/core/issues/canonical-id";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { NO_PROJECT_SIDEBAR_SOURCE, projectSidebarSource, sidebarProjects } from "@multica/core/issues/sidebar-list";
import { useSidebarProjectPrefs } from "@multica/core/issues/sidebar-project-prefs";
import { useSidebarKeptStore } from "@multica/core/issues/stores/sidebar-kept-store";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Project } from "@multica/core/types";
import { resolveClickIntent, useHeldUntilArrival, useNavigation } from "../../navigation";
import { AutoArchivePromptDialog } from "../../inbox/components/auto-archive-prompt-dialog";
import { IssueContextMenuProvider } from "../../issues/actions";
import { useIssueViewActions } from "../../issue-views/use-view-actions";
import { useT } from "../../i18n";
import { PanelBody, PanelHeader, isPathWithin } from "../panel-nav";
import { INBOX_LIST, InboxSection, useInboxSectionRows } from "./inbox-section";
import { PinnedSection, usePanelPins } from "./pinned-section";
import { useKeptActiveItem } from "./sidebar-tree";
import { ProjectsSection, useIssueInTree } from "./work-tree";
import { ViewsSection, sidebarViewItemSource } from "./views-section";

const EMPTY_PROJECTS: Project[] = [];
/** The Projects section's key in the kept-rows store, apart from every sidebar source. */
const PROJECTS_LIST = "projects";
/** Pinned rows' place in the clicked-issue record, apart from every sidebar source. */
const PINS_LIST = "pins";

interface ClickedIssue {
  segment: string;
  list: string | null;
}

/**
 * The `{id}` of a `{base}/{id}` route — for `/issues/{id}`, a UUID or an
 * identifier like `MUL-123`.
 */
export function routeSegment(pathname: string, base: string): string | null {
  if (!pathname.startsWith(base + "/")) return null;
  const segment = pathname.slice(base.length + 1).split("/")[0];
  return segment ? decodeURIComponent(segment) : null;
}

/**
 * Ends the rows the sidebar lists keep (`useKeptActiveItem`), and the Inbox
 * rows kept in place (`keepOpenInboxRow`), once their issue, project or
 * notification is no longer the page. Mounted with the shell rather than the
 * Work panel, which unmounts on leaving Work and with every compact sheet
 * close.
 */
export function KeptRowsRelease() {
  const p = useWorkspacePaths();
  const { pathname, searchParams } = useNavigation();
  const wsId = useCurrentWorkspace()?.id ?? "";
  const issueSegment = routeSegment(pathname, p.issues());
  const { data: issue } = useQuery({
    ...issueDetailOptions(wsId, issueSegment ?? ""),
    enabled: false,
  });
  // `undefined` while an identifier route waits for the page to load its issue.
  const issueId = issueSegment ? (issue?.id ?? (isIssueUuid(issueSegment) ? issueSegment : undefined)) : null;
  const projectId = routeSegment(pathname, p.projects());
  // The notification the Inbox page has open, as its rows are keyed.
  const onInbox = pathname === p.inbox();
  const inboxKey = onInbox ? searchParams.get("issue") : null;
  const wasOnInbox = useRef(false);
  useLayoutEffect(() => {
    if (issueId === undefined) return;
    useSidebarKeptStore.getState().release([issueId, projectId].filter((id) => id !== null));
    if (wsId) {
      const store = useInboxSelectionStore.getState();
      store.releaseRows(wsId, [issueId, inboxKey].filter((id) => id !== null));
      // Keep the Inbox Zero undo context only while on /inbox, or while the
      // just-cleared issue is still handing off its navigation to /inbox.
      const lastCleared = store.lastClearedByWorkspace[wsId];
      if (lastCleared && !onInbox && (wasOnInbox.current || issueId !== lastCleared.key)) {
        store.clearLastCleared(wsId);
      }
    }
    wasOnInbox.current = onInbox;
  }, [wsId, issueId, projectId, inboxKey, onInbox]);
  return null;
}

export function WorkPanel() {
  const { t } = useT("layout");
  const p = useWorkspacePaths();
  const { pathname, searchParams, openInNewTab } = useNavigation();
  const wsId = useCurrentWorkspace()?.id;
  const userId = useAuthStore((s) => s.user?.id);
  const viewActions = useIssueViewActions();

  const { data: projects = EMPTY_PROJECTS } = useQuery({
    ...projectListOptions(wsId ?? ""),
    enabled: !!wsId,
  });
  // Cache read only: the detail page owns resolving the route's issue.
  const issueSegment = routeSegment(pathname, p.issues());
  const { data: currentIssue } = useQuery({
    ...issueDetailOptions(wsId ?? "", issueSegment ?? ""),
    enabled: false,
  });
  const inTree = useIssueInTree(wsId ?? "", issueSegment);
  // The tree and a UUID route name the issue before the page has loaded it;
  // waiting for the load would light "All issues" first.
  const currentIssueId = issueSegment
    ? (currentIssue?.id ?? inTree.issueId ?? (isIssueUuid(issueSegment) ? issueSegment : null))
    : null;
  // An issue can sit in Pinned and in several lists at once, but lights in
  // one place: the list it was clicked in. Arriving any other way (search, a
  // link, a shortcut) lights the pin, else the first list that shows it.
  const [liveClickedIssue, setClickedIssue] = useState<ClickedIssue | null>(null);
  const isOpenIssue = (segment: string) =>
    !!currentIssueId && (segment === currentIssueId || segment === currentIssue?.identifier);
  // Until the route reaches the issue a click names, the open issue keeps the
  // records it had, or its highlight would fall through to another list, or
  // All issues, for the length of the load.
  const clicked = useHeldUntilArrival(
    currentIssueId,
    liveClickedIssue,
    !!liveClickedIssue && !isOpenIssue(liveClickedIssue.segment),
  );
  const clickedIssue = clicked.value;
  const clickedList = clickedIssue && isOpenIssue(clickedIssue.segment) ? clickedIssue.list : undefined;
  // The notification open on the Inbox page beside the section. Every Inbox
  // row opens there, so an issue page is never the Inbox's.
  const inboxOpenKey = pathname === p.inbox() ? searchParams.get("issue") : null;
  const inboxRows = useInboxSectionRows(wsId, inboxOpenKey);
  const treeClaimsIssue = clickedList !== undefined && clickedList !== PINS_LIST;
  const pins = usePanelPins(wsId, userId, pathname, treeClaimsIssue ? null : currentIssueId);
  const requestLocate = useSidebarTreeStore((s) => s.requestLocate);
  const rememberClickedIssue = (event: React.MouseEvent) => {
    // A middle click opens a tab; other buttons open nothing.
    if (event.type === "auxclick" && event.button !== 1) return;
    const link = (event.target as HTMLElement).closest("a[href]");
    if (!link) return;
    const path = new URL(link.getAttribute("href") ?? "", window.location.href).pathname;
    // An Inbox row's hover card renders in a portal, outside the row's list.
    const list = link.closest("[data-pinned-section]")
      ? PINS_LIST
      : link.closest("[data-inbox-card]")
        ? INBOX_LIST
        : (link.closest("[data-sidebar-list]")?.getAttribute("data-sidebar-list") ?? null);
    // Inbox and tree links open the issue at its identifier; the lists
    // record, and the issue header compares, the issue's id.
    const segment = link.getAttribute("data-issue-id") || routeSegment(path, p.issues());
    // A click that opens a tab or window, as AppLink resolves it, leaves this
    // page and its records where they are.
    if (resolveClickIntent(event) !== "push" || (event.shiftKey && !openInNewTab)) return;
    requestLocate(null);
    // An Inbox row opens the Inbox page, not an issue page.
    setClickedIssue(segment && list !== INBOX_LIST ? { segment, list } : null);
  };

  const { projectSettings, projectOrder } = useSidebarProjectPrefs(wsId ?? "");
  const filteredProjects = useMemo(
    () => sidebarProjects(projects, projectSettings, projectOrder, userId),
    [projects, projectSettings, projectOrder, userId],
  );
  const activeProjectCount = useMemo(() => projects.filter((project) => !project.archived_at).length, [projects]);
  const openProject = projects.find((project) => isPathWithin(pathname, p.projectDetail(project.id)));
  const shownProjects = useKeptActiveItem(PROJECTS_LIST, filteredProjects, openProject?.id ?? null, openProject);
  const plainRowsLit = !pins.isActivePinnedRoute;

  // Opening an issue never opens its project: the tree changes only when the
  // user toggles a row or asks to show the issue there.
  const projectsCollapsed = useSidebarTreeStore((s) => s.projectsCollapsed);
  const issueProjectId = currentIssue?.project_id ?? null;
  const showNoProject = !projectsCollapsed && projectSettings.showNoProject;
  // The Projects row the open issue belongs under: its project's, or "No project".
  const issueHomeSource = projectsCollapsed
    ? null
    : issueProjectId
      ? shownProjects.some((project) => project.id === issueProjectId)
        ? projectSidebarSource(issueProjectId)
        : null
      : currentIssue && showNoProject
        ? NO_PROJECT_SIDEBAR_SOURCE
        : null;
  // An expanded row always lists its open issue (`ownsOpen`).
  const issueHomeExpanded = useSidebarTreeStore((s) => !!issueHomeSource && s.expanded.includes(issueHomeSource));
  const listing = issueHomeExpanded ? [...inTree.sources, issueHomeSource!] : inTree.sources;
  const listOrder = [
    ...viewActions.sidebar.items.map(sidebarViewItemSource),
    ...shownProjects.map((project) => projectSidebarSource(project.id)),
    ...(showNoProject ? [NO_PROJECT_SIDEBAR_SOURCE] : []),
  ];
  const litSource =
    !plainRowsLit || !currentIssueId
      ? null
      : clickedList && listing.includes(clickedList)
        ? clickedList
        : issueHomeExpanded
          ? issueHomeSource
          : (listOrder.find((source) => listing.includes(source)) ?? null);
  const locatedSource = plainRowsLit && !litSource ? issueHomeSource : null;
  // An issue in a project the panel hides, or under the collapsed Projects
  // section, stays unmarked rather than lighting a list that may not show it.
  const issueInAllIssues =
    plainRowsLit &&
    !!currentIssue &&
    !currentIssue.project_id &&
    litSource === null &&
    locatedSource === null;

  const locate = useSidebarTreeStore((s) => s.locate);
  const setExpanded = useSidebarTreeStore((s) => s.setExpanded);
  const setProjectsCollapsed = useSidebarTreeStore((s) => s.setProjectsCollapsed);
  useEffect(() => {
    if (!locate || locate.issueId !== currentIssueId) return;
    setProjectsCollapsed(false);
    setExpanded(locate.source, true);
    setClickedIssue({ segment: locate.issueId, list: locate.source });
  }, [locate, currentIssueId, setExpanded, setProjectsCollapsed]);

  return (
    <>
      <PanelHeader title={t(($) => $.spaces.work)} />
      {/* Outside PanelBody: clicks inside the menu's portal still bubble
          through the React tree, and must not reach rememberClickedIssue. */}
      <IssueContextMenuProvider variant="sidebar">
        <PanelBody onClickCapture={rememberClickedIssue} onAuxClickCapture={rememberClickedIssue}>
          {wsId && <PinnedSection pins={pins} wsId={wsId} />}

          {wsId && (
            <InboxSection
              wsId={wsId}
              rows={inboxRows}
              pathname={pathname}
            />
          )}
          {wsId && <AutoArchivePromptDialog wsId={wsId} active={!!inboxOpenKey} />}

          {wsId && (
            <ViewsSection
              wsId={wsId}
              pathname={pathname}
              plainRowsLit={plainRowsLit}
              issueInAllIssues={issueInAllIssues}
              activeIssueId={currentIssueId}
              litSource={litSource}
              actions={viewActions}
            />
          )}

          {wsId && (
            <ProjectsSection
              wsId={wsId}
              userId={userId}
              pathname={pathname}
              projects={shownProjects}
              totalProjects={activeProjectCount}
              plainRowsLit={plainRowsLit}
              activeIssueId={currentIssueId}
              litSource={litSource}
              locatedSource={locatedSource}
            />
          )}
        </PanelBody>
      </IssueContextMenuProvider>
    </>
  );
}
