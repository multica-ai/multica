import { createHashRouter, Navigate, Outlet, useMatches } from "react-router-dom";
import { useEffect } from "react";
import { DesktopLayout } from "./components/desktop-layout";
import { DesktopLoginPage } from "./pages/login";
import { IssueDetailPage } from "./pages/issue-detail-page";
// Extracted pages from @multica/views
import { IssuesPage } from "@multica/views/issues/components";
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

export const router = createHashRouter([
  {
    path: "/",
    element: <DesktopLayout />,
    children: [
      {
        element: <PageShell />,
        children: [
          { index: true, element: <Navigate to="/issues" replace /> },
          { path: "issues", element: <IssuesPage />, handle: { title: "Issues" } },
          { path: "issues/:id", element: <IssueDetailPage />, handle: { title: "Issue" } },
          { path: "my-issues", element: <MyIssuesPage />, handle: { title: "My Issues" } },
          { path: "runtimes", element: <RuntimesPage />, handle: { title: "Runtimes" } },
          { path: "skills", element: <SkillsPage />, handle: { title: "Skills" } },
          { path: "agents", element: <AgentsPage />, handle: { title: "Agents" } },
          { path: "inbox", element: <InboxPage />, handle: { title: "Inbox" } },
          { path: "settings", element: <SettingsPage />, handle: { title: "Settings" } },
        ],
      },
    ],
  },
  { path: "/login", element: <DesktopLoginPage /> },
]);
