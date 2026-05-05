import { useEffect } from "react";
import {
  createMemoryRouter,
  Navigate,
  Outlet,
  useMatches,
  useParams,
  useSearchParams,
} from "react-router-dom";
import type { RouteObject } from "react-router-dom";
import { IssueDetailPage } from "./pages/issue-detail-page";
import { ProjectDetailPage } from "./pages/project-detail-page";
import { AutopilotDetailPage } from "./pages/autopilot-detail-page";
import { IssuesPage } from "@multica/views/issues/components";
import { ProjectsPage } from "@multica/views/projects/components";
import { FileManagerPage } from "@multica/cerebro-artifacts/views/components";
import {
  DocumentNewPage,
  DocumentViewPage,
  DocumentEditPage,
} from "@multica/cerebro-artifacts/views/pages";
import { AttachmentViewPage } from "@multica/cerebro-attachments/views/pages";
import { AutopilotsPage } from "@multica/views/autopilots/components";
import { MyIssuesPage } from "@multica/views/my-issues";
import { RuntimesPage } from "@multica/views/runtimes";
import { SkillsPage } from "@multica/views/skills";
import { DaemonRuntimeCard } from "./components/daemon-runtime-card";
import { AgentsPage } from "@multica/views/agents";
import { InboxPage as UpstreamInboxPage } from "@multica/views/inbox";
import { CerebroInboxPage } from "@multica/cerebro-inbox";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";

function InboxRoute() {
  const useCerebro = useFeatureFlag("cerebro_inbox");
  return useCerebro ? <CerebroInboxPage /> : <UpstreamInboxPage />;
}
import { NotificationsPage } from "@multica/cerebro-notifications/views";
import { SettingsPage } from "@multica/views/settings";
import { MemberDetailPage } from "@multica/cerebro-users/views";
import { Server } from "lucide-react";
import { DaemonSettingsTab } from "./components/daemon-settings-tab";
import { WorkspaceRouteLayout } from "./components/workspace-route-layout";
import { cerebroFeatureFlagTabs } from "@multica/cerebro-feature-flags";

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

/**
 * Route definitions shared by all tabs.
 *
 * Every tab path is workspace-scoped: `/{slug}/{route}/...`. Pre-workspace
 * flows (create workspace, accept invite) are NOT routes — they render as a
 * window-level overlay via `WindowOverlay`, dispatched by the navigation
 * adapter's transition-path interception. The `activeWorkspaceSlug` in the
 * tab store decides which workspace's tabs are visible in the TabBar;
 * workspace-less state (zero-workspace user) shows the overlay instead.
 *
 * The root index route stays as a harmless safety net. With per-workspace
 * tabs, nothing should construct a tab at `/` — but if one ever slips
 * through (malformed persisted state that dodges the migration, direct
 * router.navigate from unforeseen code), the index falls back to null
 * rather than 404; App.tsx's bootstrap repoints activeWorkspaceSlug on the
 * next render pass.
 */
export const appRoutes: RouteObject[] = [
  {
    element: <PageShell />,
    children: [
      { index: true, element: null },
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
            path: "documents",
            element: <DocumentsRoute />,
            handle: { title: "Documents" },
          },
          {
            path: "documents/new",
            element: <DocumentNewRoute />,
            handle: { title: "New document" },
          },
          {
            path: "documents/:id",
            element: <DocumentViewRoute />,
            handle: { title: "Document" },
          },
          {
            path: "documents/:id/edit",
            element: <DocumentEditRoute />,
            handle: { title: "Edit document" },
          },
          {
            path: "attachments/:id",
            element: <AttachmentViewRoute />,
            handle: { title: "Attachment" },
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
          {
            path: "members/:memberId",
            element: <MemberDetailRoute />,
            handle: { title: "Member" },
          },
          { path: "inbox", element: <InboxRoute />, handle: { title: "Inbox" } },
          {
            path: "notifications",
            element: <NotificationsPage />,
            handle: { title: "Notifications" },
          },
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
                  ...cerebroFeatureFlagTabs,
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

// Tiny route wrappers — react-router-dom hooks aren't available inside
// shared @multica/views, so the page components accept ids as props.
function DocumentsRoute() {
  const [search] = useSearchParams();
  return <FileManagerPage initialFolderId={search.get("folder")} />;
}

function DocumentNewRoute() {
  const [search] = useSearchParams();
  return <DocumentNewPage folderId={search.get("folder")} />;
}

function DocumentViewRoute() {
  const params = useParams<{ id: string }>();
  return <DocumentViewPage artifactId={params.id ?? ""} />;
}

function DocumentEditRoute() {
  const params = useParams<{ id: string }>();
  return <DocumentEditPage artifactId={params.id ?? ""} />;
}

function AttachmentViewRoute() {
  const params = useParams<{ id: string }>();
  return <AttachmentViewPage attachmentId={params.id ?? ""} />;
}

function MemberDetailRoute() {
  const params = useParams<{ memberId: string }>();
  return <MemberDetailPage memberId={params.memberId ?? ""} />;
}
