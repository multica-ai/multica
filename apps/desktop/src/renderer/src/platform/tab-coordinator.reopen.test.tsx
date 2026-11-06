// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { ApiError, setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { RESOURCES } from "@multica/views/locales";
import { getActiveTab, useTabStore } from "@/stores/tab-store";
import { TabContent } from "@/components/tab-content";
import { getAppRouter } from "./tab-coordinator";

vi.mock("@multica/core/auth", () => ({ useAuthStore: (selector: (state: { user: { id: string } }) => unknown) => selector({ user: { id: "fake-account" } }) }));
vi.mock("@multica/core/hooks", async (importOriginal) => ({
  ...await importOriginal<typeof import("@multica/core/hooks")>(),
  useWorkspaceId: () => "fake-workspace-id",
}));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});
vi.mock("./issue-view-tabs", () => ({ initIssueViewTabs: () => {} }));
// Only the route table is a fixture. Use the real coordinator, active host,
// navigation adapter, issue resolution/query path and unavailable/error UI.
vi.mock("@/routes", async () => {
  const { createMemoryRouter, Outlet, useParams } = await import("react-router-dom");
  const { DesktopNavigationProvider } = await import("./navigation");
  const { IssueDetailRoute } = await import("@multica/views/issues/components");
  const { DesktopRouteErrorPage } = await import("@/components/route-error-page");
  function Issue() { return <IssueDetailRoute routeId={useParams().id!} />; }
  return {
    createAppRouter: () => createMemoryRouter([{
      path: "/", element: <DesktopNavigationProvider><Outlet /></DesktopNavigationProvider>,
      errorElement: <DesktopRouteErrorPage />,
      children: [
        { path: "/acme/issues", element: <div>Current issues list</div> },
        { path: "/acme/projects", element: <div>Current projects</div> },
        { path: "/acme/issues/:id", element: <Issue /> },
      ],
    }]),
  };
});

afterEach(() => { cleanup(); getAppRouter().dispose(); });

it("projects recovered destinations through the real host and current access queries, and excludes recovery closes", async () => {
  Object.assign(window, { desktopAPI: { runtimeConfig: { ok: true, config: { appUrl: "https://fixture.invalid" } }, windowContext: { kind: "main" } } });
  const getIssue = vi.fn().mockImplementation((id: string) => Promise.reject(
    new ApiError("Unavailable", id === "DENIED-1" ? 403 : 404, "Unavailable"),
  ));
  setApiInstance({ getIssue } as unknown as ApiClient);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const store = useTabStore.getState();
  store.reset();
  store.switchWorkspace("acme");
  const closed = store.openTab("/acme/projects?sort=desc#board", "Projects", { activate: true });
  store.commitViewState(closed, "/acme/projects", "draft", "outgoing state");
  store.closeTab(closed);
  render(<QueryClientProvider client={qc}><I18nProvider locale="en" resources={RESOURCES}><TabContent /></I18nProvider></QueryClientProvider>);
  expect(await screen.findByText("Current issues list")).toBeInTheDocument();
  act(() => store.reopenClosedTab());
  expect(await screen.findByText("Current projects")).toBeInTheDocument();
  expect(getAppRouter().state.location).toMatchObject({ pathname: "/acme/projects", search: "?sort=desc", hash: "#board" });
  expect(getActiveTab(useTabStore.getState())?.memento).toEqual({ scroll: {}, view: {} });

  for (const id of ["DELETED-1", "DENIED-1"]) {
    act(() => {
      store.closeTab(store.openTab(`/acme/issues/${id}?filter=mine#comment-1`, "Saved title", { activate: true }));
      store.reopenClosedTab();
    });
    await waitFor(() => expect(getIssue).toHaveBeenCalledWith(id));
    expect(await screen.findByText("Issue not found")).toBeInTheDocument();
    expect(screen.queryByText("Saved title")).toBeNull();
    expect(getAppRouter().state.location.pathname).toBe(`/acme/issues/${id}`);
  }
  expect(getIssue).toHaveBeenCalledTimes(2);

  act(() => {
    store.closeTab(store.openTab("/acme/not-a-route", "Broken route", { activate: true }));
    store.reopenClosedTab();
  });
  expect(await screen.findByText("This page doesn't exist")).toBeInTheDocument();
  const before = useTabStore.getState().closedTabsByWorkspace.acme;
  fireEvent.click(screen.getByRole("button", { name: "Close tab" }));
  expect(useTabStore.getState().closedTabsByWorkspace.acme).toBe(before);
  expect(getActiveTab(useTabStore.getState())?.url).not.toBe("/acme/not-a-route");
  qc.clear();
});
