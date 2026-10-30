import { useEffect, useLayoutEffect } from "react";
import { createMemoryRouter, Outlet, useLocation, useMatches } from "react-router-dom";
import type { RouteObject } from "react-router-dom";
import { IssueDetailPage } from "./pages/issue-detail-page";
import { ProjectDetailPage } from "./pages/project-detail-page";
import { AutopilotDetailPage } from "./pages/autopilot-detail-page";
import { SkillDetailPage } from "./pages/skill-detail-page";
import { AgentDetailPage } from "./pages/agent-detail-page";
import { AiBuilderSessionPage } from "./pages/ai-builder-session-page";
import { MemberDetailPage } from "./pages/member-detail-page";
import {
  RuntimeDetailPage,
  RuntimeSettingsPage,
} from "./pages/runtime-detail-page";
import { AttachmentPreviewRoute } from "./pages/attachment-preview-page";
import { IssuesPage } from "@multica/views/issues/components";
import { ProjectsPage } from "@multica/views/projects/components";
import { DashboardPage } from "@multica/views/dashboard";
import { AutopilotsPage } from "@multica/views/autopilots/components";
import { MyIssuesPage } from "@multica/views/my-issues";
import { SkillsPage } from "@multica/views/skills";
import { DesktopRuntimesPage } from "./components/desktop-runtimes-page";
import { DesktopAgentsPage } from "./components/desktop-agents-page";
import {
  AiCreateAgentPage,
  ManualCreateAgentPage,
} from "@multica/views/agents";
import { DesktopChooseRuntimePage } from "./components/desktop-choose-runtime-page";
import { AgentCreatedPage } from "./pages/agent-created-page";
import { SquadsPage, SquadDetailPage as SquadDetailPageView } from "@multica/views/squads/components";
import { AllViewsPage } from "@multica/views/issue-views";
import { InboxPage } from "@multica/views/inbox";
import { ChatPage } from "@multica/views/chat";
import { SettingsPage } from "@multica/views/settings";
import { useT } from "@multica/views/i18n";
import { Download, Server } from "@multica/ui/icons";
import { DaemonSettingsTab } from "./components/daemon-settings-tab";
import { UpdatesSettingsTab } from "./components/updates-settings-tab";
import { WorkspaceRouteLayout } from "./components/workspace-route-layout";
import { DesktopRouteErrorPage } from "./components/route-error-page";
import { IssueViewUrlSync } from "./platform/issue-view-tabs";
import { useRenderedRouteStore } from "./stores/rendered-route-store";

const WORKSPACE_VIEW_SCOPE = { scope_type: "workspace" } as const;
const MY_VIEW_SCOPE = { scope_type: "my" } as const;

/**
 * Wraps `SettingsPage` so the desktop-only extra tabs can pull their labels
 * from i18n. The route element has to be a component (not a literal JSX
 * value) for `useT` to run.
 */
function DesktopSettingsRoute() {
  const { t } = useT("settings");
  return (
    <SettingsPage
      extraDeviceTabs={[
        {
          value: "daemon",
          label: t(($) => $.desktop.daemon.title),
          icon: Server,
          content: <DaemonSettingsTab />,
        },
        {
          value: "updates",
          label: t(($) => $.desktop.tabs.updates),
          icon: Download,
          content: <UpdatesSettingsTab />,
        },
      ]}
    />
  );
}

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

/**
 * Publishes the committed pathname. A layout effect, so the shell's update
 * lands in the same frame as the page it describes.
 */
function RenderedPathnameSync() {
  const { pathname } = useLocation();
  const setPathname = useRenderedRouteStore((s) => s.setPathname);
  useLayoutEffect(() => {
    setPathname(pathname);
  }, [pathname, setPathname]);
  return null;
}

/** Wrapper that renders route children + TitleSync */
function PageShell() {
  return (
    <>
      <TitleSync />
      <RenderedPathnameSync />
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
    errorElement: <DesktopRouteErrorPage />,
    children: [
      { index: true, element: null },
      {
        path: ":workspaceSlug",
        element: <WorkspaceRouteLayout />,
        children: [
          // A bare `/{slug}` URL is normalized to `/{slug}/issues` by
          // sanitizeTabPath before it ever becomes a session, so the index
          // route is unreachable in practice; null keeps it a harmless
          // safety net instead of an in-router <Navigate> (MUL-4741
          // invariant 1: the router never self-navigates).
          { index: true, element: null },
          {
            path: "issues",
            element: (
              <IssueViewUrlSync scope={WORKSPACE_VIEW_SCOPE}>
                <IssuesPage />
              </IssueViewUrlSync>
            ),
            handle: { title: "Issues" },
          },
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
            element: (
              <IssueViewUrlSync scope={MY_VIEW_SCOPE}>
                <MyIssuesPage />
              </IssueViewUrlSync>
            ),
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
            handle: { title: "Machine" },
          },
          {
            path: "runtimes/:id/runtime/:runtimeId",
            element: <RuntimeSettingsPage />,
            handle: { title: "Runtime" },
          },
          { path: "skills", element: <SkillsPage />, handle: { title: "Skills" } },
          {
            path: "skills/:id",
            element: <SkillDetailPage />,
            handle: { title: "Skill" },
          },
          { path: "agents", element: <DesktopAgentsPage />, handle: { title: "Agents" } },
          {
            path: "agents/new",
            element: <DesktopChooseRuntimePage />,
            handle: { title: "Create Agent" },
          },
          {
            path: "agents/new/done/:agentId",
            element: <AgentCreatedPage />,
            handle: { title: "Create Agent" },
          },
          {
            path: "agents/new/manual",
            element: <ManualCreateAgentPage />,
            handle: { title: "Create Agent" },
          },
          {
            path: "agents/new/ai",
            element: <AiCreateAgentPage />,
            handle: { title: "Create Agent" },
          },
          {
            path: "agents/new/ai/:sessionId",
            element: <AiBuilderSessionPage />,
            handle: { title: "Create Agent" },
          },
          {
            path: "agents/:id",
            element: <AgentDetailPage />,
            handle: { title: "Agent" },
          },
          {
            path: "members/:id",
            element: <MemberDetailPage />,
            handle: { title: "Member" },
          },
          { path: "squads", element: <SquadsPage />, handle: { title: "Squads" } },
          { path: "views", element: <AllViewsPage />, handle: { title: "Views" } },
          {
            path: "squads/:id",
            element: <SquadDetailPageView />,
            handle: { title: "Squad" },
          },
          { path: "inbox", element: <InboxPage />, handle: { title: "Inbox" } },
          { path: "chat", element: <ChatPage />, handle: { title: "Chat" } },
          {
            path: "attachments/:id/preview",
            element: <AttachmentPreviewRoute />,
            handle: { title: "Attachment" },
          },
          {
            path: "usage",
            element: <DashboardPage />,
            handle: { title: "Usage" },
          },
          {
            path: "settings",
            element: <DesktopSettingsRoute />,
            handle: { title: "Settings" },
          },
        ],
      },
    ],
  },
];

/**
 * Create THE app router (MUL-4741 single-router session architecture).
 * There is exactly one instance, owned by the tab Coordinator; it projects
 * the active tab session's URL and is never navigated by anything else.
 */
export function createAppRouter() {
  return createMemoryRouter(appRoutes, {
    initialEntries: ["/"],
  });
}
