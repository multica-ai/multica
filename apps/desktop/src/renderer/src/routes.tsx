import { useEffect } from "react";
import {
  createMemoryRouter,
  Navigate,
  Outlet,
  useMatches,
} from "react-router-dom";
import type { RouteObject } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { IssueDetailPage } from "./pages/issue-detail-page";
import { ProjectDetailPage } from "./pages/project-detail-page";
import { AutopilotDetailPage } from "./pages/autopilot-detail-page";
import { IssuesPage } from "@multica/views/issues/components";
import { ProjectsPage } from "@multica/views/projects/components";
import { AutopilotsPage } from "@multica/views/autopilots/components";
import { MyIssuesPage } from "@multica/views/my-issues";
import { RuntimesPage } from "@multica/views/runtimes";
import { SkillsPage } from "@multica/views/skills";
import { DaemonRuntimeCard } from "./components/daemon-runtime-card";
import { AgentsPage } from "@multica/views/agents";
import { InboxPage } from "@multica/views/inbox";
import { SettingsPage } from "@multica/views/settings";
import { NewWorkspacePage } from "@multica/views/workspace/new-workspace-page";
import { InvitePage } from "@multica/views/invite";
import { useNavigation } from "@multica/views/navigation";
import { paths } from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { Server } from "lucide-react";
import { DaemonSettingsTab } from "./components/daemon-settings-tab";
import { WorkspaceRouteLayout } from "./components/workspace-route-layout";

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

function NewWorkspaceRoute() {
  const nav = useNavigation();
  return (
    <NewWorkspacePage
      onSuccess={(ws) => nav.push(paths.workspace(ws.slug).issues())}
    />
  );
}

/**
 * Root index route: resolves the URL-less `/` path to a concrete destination.
 *
 * Runs both on first login (App.tsx seeded the cache) and on app reopen
 * (AuthInitializer seeded the cache). Reading from React Query avoids
 * duplicate fetches across tabs — each tab's memory router hits this
 * component independently but the query is deduped.
 *
 * Sends first-time users without any workspace to /new-workspace,
 * everyone else to their first workspace's issues page. Persisted tab
 * paths that already carry a workspace slug bypass this component
 * entirely.
 */
function IndexRedirect() {
  const { data: wsList, isFetched } = useQuery(workspaceListOptions());

  // Wait for the query to settle so we don't redirect to /new-workspace
  // on the initial render before the seeded/fetched data arrives.
  if (!isFetched) return null;

  const firstWorkspace = wsList?.[0];
  if (firstWorkspace) {
    return <Navigate to={paths.workspace(firstWorkspace.slug).issues()} replace />;
  }
  return <Navigate to={paths.newWorkspace()} replace />;
}

function InviteRoute() {
  const matches = useMatches();
  const match = matches.find((m) => (m.params as { id?: string }).id);
  const id = (match?.params as { id?: string })?.id ?? "";
  return <InvitePage invitationId={id} />;
}

/**
 * Route definitions shared by all tabs.
 *
 * Structure mirrors the web app's [workspaceSlug]/... layout: all dashboard
 * pages live under /:workspaceSlug, with WorkspaceRouteLayout resolving the
 * slug to a workspace and syncing side-effects (api client, persist namespace,
 * Zustand mirror). Global (pre-workspace) routes — new-workspace and invite —
 * sit at the top level alongside the workspace wrapper.
 */
export const appRoutes: RouteObject[] = [
  {
    element: <PageShell />,
    children: [
      // Top-level index: no slug yet. `IndexRedirect` reads the workspace
      // list from React Query cache (seeded by AuthInitializer on reopen
      // or App.tsx on deep-link login) and bounces to the first
      // workspace's issues page — or /new-workspace if the user has none.
      { index: true, element: <IndexRedirect /> },
      {
        path: "new-workspace",
        element: <NewWorkspaceRoute />,
        handle: { title: "Create Workspace" },
      },
      {
        path: "invite/:id",
        element: <InviteRoute />,
        handle: { title: "Accept Invite" },
      },
      {
        path: ":workspaceSlug",
        element: <WorkspaceRouteLayout />,
        children: [
          { index: true, element: <Navigate to="issues" replace /> },
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
            path: "autopilots",
            element: <AutopilotsPage />,
            handle: { title: "Autopilot" },
          },
          {
            path: "autopilots/:id",
            element: <AutopilotDetailPage />,
            handle: { title: "Autopilot" },
          },
          {
            path: "my-issues",
            element: <MyIssuesPage />,
            handle: { title: "My Issues" },
          },
          {
            path: "runtimes",
            element: <RuntimesPage topSlot={<DaemonRuntimeCard />} />,
            handle: { title: "Runtimes" },
          },
          { path: "skills", element: <SkillsPage />, handle: { title: "Skills" } },
          { path: "agents", element: <AgentsPage />, handle: { title: "Agents" } },
          { path: "inbox", element: <InboxPage />, handle: { title: "Inbox" } },
          {
            path: "settings",
            element: (
              <SettingsPage
                extraAccountTabs={[
                  {
                    value: "daemon",
                    label: "Daemon",
                    icon: Server,
                    content: <DaemonSettingsTab />,
                  },
                ]}
              />
            ),
            handle: { title: "Settings" },
          },
        ],
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
