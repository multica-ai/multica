import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { EMPTY_ISSUE_VIEW_PREFERENCE } from "@multica/core/api/schemas";
import { issueViewPreferenceOptions } from "@multica/core/issue-views/preferences";
import { SIDEBAR_PROJECTS_SCOPE } from "@multica/core/issues/sidebar-project-prefs";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Project } from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { useShowInSidebar } from "./show-in-sidebar";

vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});
vi.mock("@multica/core/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/hooks")>()),
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/auth", async (importOriginal) => {
  const state = { user: { id: "u1" } };
  return {
    ...(await importOriginal<typeof import("@multica/core/auth")>()),
    useAuthStore: Object.assign((selector: (s: typeof state) => unknown) => selector(state), {
      getState: () => state,
    }),
  };
});

const layout = vi.hoisted(() => ({
  shell: null as { navCollapsed: boolean; toggleNav: () => void } | null,
  sidebar: null as { isCompact: boolean; setOpenMobile: (open: boolean) => void } | null,
}));
vi.mock("../app-shell-context", () => ({ useAppShell: () => layout.shell }));
vi.mock("@multica/ui/components/ui/sidebar", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/ui/components/ui/sidebar")>()),
  useSidebarSafe: () => layout.sidebar,
}));

const PROJECT = { id: "p-1", title: "Launch", status: "in_progress", lead_type: null, lead_id: null } as unknown as Project;
const DONE_PROJECT = { ...PROJECT, id: "p-2", status: "completed" } as Project;
const ISSUE_ID = "0a3f0a8e-6a52-4c1e-9f5e-1b2c3d4e5f60";

function render(pathname: string, projectId: string | null) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData(projectListOptions("ws-1").queryKey, { projects: [PROJECT, DONE_PROJECT], total: 2 });
  qc.setQueryData(issueViewPreferenceOptions("ws-1", SIDEBAR_PROJECTS_SCOPE).queryKey, EMPTY_ISSUE_VIEW_PREFERENCE);
  const adapter: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname,
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
  };
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>
      <NavigationProvider value={adapter}>{children}</NavigationProvider>
    </QueryClientProvider>
  );
  return renderHook(() => useShowInSidebar({ id: ISSUE_ID, project_id: projectId }), { wrapper }).result;
}

beforeEach(() => {
  useSidebarTreeStore.setState({ locate: null });
  layout.shell = null;
  layout.sidebar = null;
});

describe("useShowInSidebar", () => {
  it("asks the Work panel to show the issue under its project", () => {
    const result = render(`/acme/issues/${ISSUE_ID}`, PROJECT.id);
    result.current?.();
    expect(useSidebarTreeStore.getState().locate).toEqual({ issueId: ISSUE_ID, source: `project:${PROJECT.id}` });
  });

  it("opens a collapsed space panel", () => {
    layout.shell = { navCollapsed: true, toggleNav: vi.fn() };
    layout.sidebar = { isCompact: false, setOpenMobile: vi.fn() };
    render(`/acme/issues/${ISSUE_ID}`, PROJECT.id).current?.();
    expect(layout.shell.toggleNav).toHaveBeenCalledOnce();
    expect(layout.sidebar.setOpenMobile).not.toHaveBeenCalled();
  });

  it("opens the nav sheet in the compact layout", () => {
    layout.shell = { navCollapsed: false, toggleNav: vi.fn() };
    layout.sidebar = { isCompact: true, setOpenMobile: vi.fn() };
    render(`/acme/issues/${ISSUE_ID}`, PROJECT.id).current?.();
    expect(layout.sidebar.setOpenMobile).toHaveBeenCalledWith(true);
    expect(layout.shell.toggleNav).not.toHaveBeenCalled();
    expect(useSidebarTreeStore.getState().locate?.issueId).toBe(ISSUE_ID);
  });

  it("is not offered off an issue route, where the panel does not mark the issue", () => {
    expect(render("/acme/inbox", PROJECT.id).current).toBeNull();
  });

  it("is not offered for an issue without a project, or one the sidebar leaves out", () => {
    expect(render(`/acme/issues/${ISSUE_ID}`, null).current).toBeNull();
    expect(render(`/acme/issues/${ISSUE_ID}`, DONE_PROJECT.id).current).toBeNull();
  });
});
