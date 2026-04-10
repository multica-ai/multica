import { useEffect } from "react";
import {
  createMemoryRouter,
  Navigate,
  Outlet,
  useMatches,
} from "react-router-dom";
import type { RouteObject } from "react-router-dom";
import { IssueDetailPage } from "./pages/issue-detail-page";
import { ProjectDetailPage } from "./pages/project-detail-page";
import { IssuesPage } from "@multica/views/issues/components";
import { ProjectsPage } from "@multica/views/projects/components";
import { MyIssuesPage } from "@multica/views/my-issues";
import { RuntimesPage } from "@multica/views/runtimes";
import { SkillsPage } from "@multica/views/skills";
import { AgentsPage } from "@multica/views/agents";
import { InboxPage } from "@multica/views/inbox";
import { SettingsPage } from "@multica/views/settings";

/**
 * Sets document.title from the deepest matched route's handle.title.
 * The tab system observes document.title via MutationObserver.
 * Pages with dynamic titles (e.g. issue detail) override by setting
 * document.title directly via useDocumentTitle().
 */
function TitleSync() {
  const matches = useMatches();
  const title = [...matches]
    .reverse()
    .find((m) => (m.handle as { title?: string })?.title)
    ?.handle as { title?: string } | undefined;

  useEffect(() => {
    if (title?.title) document.title = title.title;
  }, [title?.title]);

  return null;
}

/** Wrapper that renders route children + TitleSync */
function PageShell() {
  return (
    <>
      <TitleSync />
      <Outlet />
    </>
  );
}

/** Route definitions shared by all tabs (no layout wrapper). */
export const appRoutes: RouteObject[] = [
  {
    element: <PageShell />,
    children: [
      { index: true, element: <Navigate to="/issues" replace /> },
      { path: "issues", element: <IssuesPage />, handle: { title: "Issues" } },
      {
        path: "issues/:id",
        element: <IssueDetailPage />,
        handle: { title: "Issue" },
      },
      {
        path: "projects",
        element: <ProjectsPage />,
        handle: { title: "Projects" },
      },
      {
        path: "projects/:id",
        element: <ProjectDetailPage />,
        handle: { title: "Project" },
      },
      {
        path: "my-issues",
        element: <MyIssuesPage />,
        handle: { title: "My Issues" },
      },
      {
        path: "runtimes",
        element: <RuntimesPage />,
        handle: { title: "Runtimes" },
      },
      { path: "skills", element: <SkillsPage />, handle: { title: "Skills" } },
      { path: "agents", element: <AgentsPage />, handle: { title: "Agents" } },
      { path: "inbox", element: <InboxPage />, handle: { title: "Inbox" } },
      {
        path: "settings",
        element: <SettingsPage />,
        handle: { title: "Settings" },
      },
    ],
  },
];

/** Create an independent memory router for a tab. */
export function createTabRouter(initialPath: string) {
  return createMemoryRouter(appRoutes, {
    initialEntries: [initialPath],
  });
}
