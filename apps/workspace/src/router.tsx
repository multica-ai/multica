import {
  createRootRoute,
  createRoute,
  createRouter,
  Navigate,
  Outlet,
} from "@tanstack/react-router";
import { useEffect } from "react";
import { MulticaIcon } from "@/components/multica-icon";
import { AppShell } from "@/app-shell";
import { useAuthStore } from "@/features/auth";
import LoginPage from "@/features/auth/components/login-page";
import InboxPage from "@/features/inbox/components/inbox-page";
import { DashboardLayout } from "@/features/layout/components/dashboard-layout";
import { IssuesPage } from "@/features/issues/components/issues-page";
import { WorkbenchIssuesPage } from "@/features/issues/components/workbench-issues-page";
import { useIssueViewStore } from "@/features/issues/stores/view-store";
import {
  backlogViewStore,
  todayViewStore,
  upcomingViewStore,
} from "@/features/issues/stores/workbench-view-stores";
import { IssueDetail } from "@/features/issues/components/issue-detail";
import {
  deriveBacklogIssues,
  deriveTodayIssues,
  deriveUpcomingIssues,
} from "@/features/issues/utils/workbench-view";
import { MyIssuesPage } from "@/features/my-issues";
import AgentsPage from "@/features/agents/components/agents-page";
import SettingsPage from "@/features/settings/components/settings-page";
import { RuntimesPage } from "@/features/runtimes";
import { ProjectsPage } from "@/features/projects";
import { ProjectBoardPage } from "@/features/projects/components/project-board-page";
import { SkillsPage } from "@/features/skills";

function LoadingScreen() {
  return (
    <div className="flex h-screen items-center justify-center">
      <MulticaIcon className="size-6 animate-pulse" />
    </div>
  );
}

function HomePage() {
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  if (isLoading) return <LoadingScreen />;
  if (!user) return <Navigate to="/login" replace />;

  return (
    <DashboardLayout>
      <InboxPage />
    </DashboardLayout>
  );
}

function NotFoundRedirect() {
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  if (isLoading) return <LoadingScreen />;
  return <Navigate to={user ? "/" : "/login"} replace />;
}

function ProtectedLayout() {
  return (
    <DashboardLayout>
      <Outlet />
    </DashboardLayout>
  );
}

function IssueDetailRoute() {
  const { id } = issueDetailRoute.useParams();
  return <IssueDetail issueId={id} />;
}

function BoardPage() {
  const setViewMode = useIssueViewStore((s) => s.setViewMode);

  useEffect(() => {
    setViewMode("board");
  }, [setViewMode]);

  return (
    <IssuesPage
      breadcrumbLabel="Board"
      forcedViewMode="board"
      hideViewToggle
    />
  );
}

function BacklogPage() {
  return (
    <WorkbenchIssuesPage
      breadcrumbLabel="Backlog"
      emptyTitle="No backlog work"
      emptyDescription="Create an issue or move existing work to backlog."
      store={backlogViewStore}
      deriveIssues={deriveBacklogIssues}
    />
  );
}

function TodayPage() {
  return (
    <WorkbenchIssuesPage
      breadcrumbLabel="Today"
      emptyTitle="Nothing scheduled for today"
      emptyDescription="Issues scheduled for today will appear here."
      store={todayViewStore}
      deriveIssues={deriveTodayIssues}
    />
  );
}

function UpcomingPage() {
  return (
    <WorkbenchIssuesPage
      breadcrumbLabel="Upcoming"
      emptyTitle="Nothing upcoming yet"
      emptyDescription="Future scheduled work will appear here."
      store={upcomingViewStore}
      deriveIssues={deriveUpcomingIssues}
    />
  );
}

function AgentDetailPage() {
  const { id } = agentDetailRoute.useParams();
  return <AgentsPage selectedAgentId={id} syncSelectionToPath />;
}

function ProjectDetailPage() {
  const { id } = projectDetailRoute.useParams();
  return <ProjectsPage selectedProjectId={id} syncSelectionToPath />;
}

function ProjectBoardRoutePage() {
  const { id } = projectBoardRoute.useParams();
  return <ProjectBoardPage projectId={id} />;
}

const rootRoute = createRootRoute({
  component: AppShell,
  notFoundComponent: NotFoundRedirect,
});

const homeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: HomePage,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "login",
  component: LoginPage,
});

const protectedRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "protected",
  component: ProtectedLayout,
});

const issuesRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "issues",
  component: IssuesPage,
});

const issueDetailRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "issues/$id",
  component: IssueDetailRoute,
});

const boardRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "board",
  component: BoardPage,
});

const projectsRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "projects",
  component: ProjectsPage,
});

const projectDetailRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "projects/$id",
  component: ProjectDetailPage,
});

const projectBoardRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "projects/$id/board",
  component: ProjectBoardRoutePage,
});

const backlogRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "backlog",
  component: BacklogPage,
});

const todayRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "today",
  component: TodayPage,
});

const upcomingRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "upcoming",
  component: UpcomingPage,
});

const inboxRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "inbox",
  component: InboxPage,
});

const notificationsRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "notifications",
  component: InboxPage,
});

const myIssuesRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "my-issues",
  component: MyIssuesPage,
});

const myWorkRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "my-work",
  component: MyIssuesPage,
});

const agentsRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "agents",
  component: AgentsPage,
});

const agentDetailRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "agents/$id",
  component: AgentDetailPage,
});

const runtimesRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "runtimes",
  component: RuntimesPage,
});

const skillsRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "skills",
  component: SkillsPage,
});

const settingsRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "settings",
  component: SettingsPage,
});

const routeTree = rootRoute.addChildren([
  homeRoute,
  loginRoute,
  protectedRoute.addChildren([
    issuesRoute,
    issueDetailRoute,
    boardRoute,
    projectsRoute,
    projectDetailRoute,
    projectBoardRoute,
    backlogRoute,
    todayRoute,
    upcomingRoute,
    inboxRoute,
    notificationsRoute,
    myIssuesRoute,
    myWorkRoute,
    agentsRoute,
    agentDetailRoute,
    runtimesRoute,
    skillsRoute,
    settingsRoute,
  ]),
]);

export const router = createRouter({
  routeTree,
  defaultPreload: "intent",
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
