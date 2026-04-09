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
import { useIssueViewStore } from "@/features/issues/stores/view-store";
import { IssueDetail } from "@/features/issues/components/issue-detail";
import { MyIssuesPage } from "@/features/my-issues";
import AgentsPage from "@/features/agents/components/agents-page";
import SettingsPage from "@/features/settings/components/settings-page";
import { RuntimesPage } from "@/features/runtimes";
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

function AgentDetailPage() {
  const { id } = agentDetailRoute.useParams();
  return <AgentsPage selectedAgentId={id} syncSelectionToPath />;
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

const inboxRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "inbox",
  component: InboxPage,
});

const myIssuesRoute = createRoute({
  getParentRoute: () => protectedRoute,
  path: "my-issues",
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
    inboxRoute,
    myIssuesRoute,
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
