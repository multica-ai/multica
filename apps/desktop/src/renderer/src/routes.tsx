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
import { SkillDetailPage } from "./pages/skill-detail-page";
import { AgentDetailPage } from "./pages/agent-detail-page";
import { RuntimeDetailPage } from "./pages/runtime-detail-page";
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
import { SkillsPage } from "@multica/views/skills";
import { DesktopRuntimesPage } from "./components/desktop-runtimes-page";
import { AgentsPage } from "@multica/views/agents";
import { InboxPage } from "@multica/views/inbox";
import { NotificationsPage } from "@multica/cerebro-notifications/views";
import { SettingsPage } from "@multica/views/settings";
import { MemberDetailPage } from "@multica/cerebro-users/views";
import { GroupDetailView } from "@multica/cerebro-groups/views";
import { useNavigation } from "@multica/views/navigation";
import { useCurrentWorkspace } from "@multica/core/paths";
import { TasksPage } from "@multica/cerebro-tasks";
import { PermissionsPage } from "@multica/cerebro-permissions";
import { SearchPage } from "@multica/views/search";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";
import { Download, Server } from "lucide-react";
import { DaemonSettingsTab } from "./components/daemon-settings-tab";
import { UpdatesSettingsTab } from "./components/updates-settings-tab";
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
          {
            path: "issues",
            element: (
              <ErrorBoundary>
                <IssuesPage />
              </ErrorBoundary>
            ),
            handle: { title: "Issues" },
          },
          {
            path: "issues/:id",
            element: <IssueDetailPage />,
            handle: { title: "Issue" },
          },
          {
            path: "channels/:id",
            element: <IssueDetailPage />,
            handle: { title: "Channel" },
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
            element: <DesktopRuntimesPage />,
            handle: { title: "Runtimes" },
          },
          {
            path: "runtimes/:id",
            element: <RuntimeDetailPage />,
            handle: { title: "Runtime" },
          },
          { path: "skills", element: <SkillsPage />, handle: { title: "Skills" } },
          {
            path: "skills/:id",
            element: <SkillDetailPage />,
            handle: { title: "Skill" },
          },
          { path: "agents", element: <AgentsPage />, handle: { title: "Agents" } },
          {
            path: "agents/:id",
            element: <AgentDetailPage />,
            handle: { title: "Agent" },
          },
          {
            path: "members/:memberId",
            element: <MemberDetailRoute />,
            handle: { title: "Member" },
          },
          {
            path: "groups/:id",
            element: <GroupDetailRoute />,
            handle: { title: "Group" },
          },
          { path: "inbox", element: <InboxPage />, handle: { title: "Inbox" } },
          { path: "search", element: <SearchPage />, handle: { title: "Search" } },
          { path: "tasks", element: <TasksPage />, handle: { title: "Tasks" } },
          {
            path: "permissions",
            element: <PermissionsPage />,
            handle: { title: "Permissions" },
          },
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
                  {
                    value: "updates",
                    label: "Updates",
                    icon: Download,
                    content: <UpdatesSettingsTab />,
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

// JEH-1067 — Cerebro Groups detail (Bundle B). The shared view lives in
// @multica/cerebro-groups/views; this wrapper pulls the id from the URL and
// supplies an onBack callback that returns to Settings → Groups. Desktop's
// memory router doesn't carry the workspaceSlug as a route param the way the
// web app's URL does, so the slug is read from the workspace store via
// useCurrentWorkspace instead.
function GroupDetailRoute() {
  const params = useParams<{ id: string }>();
  const navigation = useNavigation();
  const workspace = useCurrentWorkspace();
  return (
    <GroupDetailView
      groupId={params.id ?? ""}
      onBack={() =>
        navigation.push(`/${workspace?.slug ?? ""}/settings?tab=groups`)
      }
    />
  );
}
