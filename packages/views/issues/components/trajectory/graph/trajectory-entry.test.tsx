import { beforeAll, afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { isGraphScopeClosed } from "@multica/core/trajectory/graph-web";
import { ApiClient, setApiInstance } from "@multica/core/api";
import { createTrajectoryEntryStore } from "@multica/core/trajectory/web-entry";
import { paths } from "@multica/core/paths";
import { NavigationProvider, type NavigationAdapter } from "../../../../navigation";
import { renderWithI18n } from "../../../../test/i18n";
import { TrajectoryEntry } from "./trajectory-entry";
import { graphFixture, graphId, graphQuery, graphScope } from "./graph-fixture";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => graphId(2) }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: Object.assign((selector: (state: unknown) => unknown) => selector({ user: { id: graphId(1) }, status: "authenticated" }), { subscribe: () => () => {} }) }));
const wsMethods = vi.hoisted(() => ({ subscribe: () => () => {}, onReconnect: () => () => {} }));
vi.mock("@multica/core/realtime", () => ({ useWS: () => wsMethods }));
vi.mock("@multica/core/paths", async importOriginal => ({ ...await importOriginal<typeof import("@multica/core/paths")>(), useWorkspacePaths: () => paths.workspace("synthetic") }));
const adapter: NavigationAdapter = { pathname: "/synthetic/issues", searchParams: new URLSearchParams(), hash: "", push: vi.fn(), replace: vi.fn(), back: vi.fn(), getShareableUrl: path => path };
const clients: QueryClient[] = [];
// Transform the lazy page chunk once up front; it imports the issues page filter menu.
beforeAll(async () => { await import("./trajectory-active-page"); }, 60_000);
afterEach(() => { cleanup(); clients.forEach(client => client.clear()); clients.length = 0; vi.unstubAllGlobals(); });
it("opens graph from the existing entry store without capability traffic or an extra toolbar", async () => {
  const fixture = graphFixture(), store = createTrajectoryEntryStore();
  vi.stubGlobal("fetch", fixture.fetch); setApiInstance(new ApiClient(""));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client);
  renderWithI18n(<QueryClientProvider client={client}><NavigationProvider value={adapter}><TrajectoryEntry store={store} query={graphQuery}><p>Native task list</p></TrajectoryEntry></NavigationProvider></QueryClientProvider>);
  expect(screen.getByText("Native task list")).toBeVisible();
  expect(screen.queryByRole("button", { name: "Check availability" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Open trajectory" })).not.toBeInTheDocument();
  expect(fixture.requests).toHaveLength(0);
  act(() => store.getState().setActive(true));
  await screen.findByRole("button", { name: "First project" });
  expect(fixture.requests.map(request => request.path)).toEqual(["/api/issues/table/rows", "/api/issues/trajectory"]);
  act(() => store.getState().setActive(false));
  expect(screen.getByText("Native task list")).toBeVisible();
});
it("keeps invalid cold links on the native surface without reading a graph", () => {
  const fixture = graphFixture();vi.stubGlobal("fetch", fixture.fetch);setApiInstance(new ApiClient(""));
  const client = new QueryClient();clients.push(client);
  renderWithI18n(<QueryClientProvider client={client}><TrajectoryEntry store={createTrajectoryEntryStore(true)} query={graphQuery} navigation={{ requested: false, error: "invalid_link" }}><p>Native task list</p></TrajectoryEntry></QueryClientProvider>);
  expect(screen.getByRole("alert")).toHaveTextContent("Invalid trajectory link");
  expect(screen.getByText("Native task list")).toBeVisible();
  expect(fixture.requests).toHaveLength(0);
});


it("keeps graph search edits in the captured page query", async () => {
  const fixture = graphFixture(), store = createTrajectoryEntryStore(true), query = { ...graphQuery, search: "A" };
  vi.stubGlobal("fetch", fixture.fetch); setApiInstance(new ApiClient(""));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client);
  renderWithI18n(<QueryClientProvider client={client}><NavigationProvider value={adapter}><TrajectoryEntry store={store} query={query}><p>Native task list</p></TrajectoryEntry></NavigationProvider></QueryClientProvider>);
  fireEvent.change(await screen.findByRole("textbox", { name: "Search tasks" }), { target: { value: "中文任务" } });
  expect(query.search).toBe("A");
});


it("does not reopen a denied scope when a failed filter directory is cleared", async () => {
  const fixture = graphFixture(), store = createTrajectoryEntryStore(true);
  vi.stubGlobal("fetch", fixture.fetch); setApiInstance(new ApiClient(""));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client);
  const view = (ready: boolean, filtered = false) => <QueryClientProvider client={client}><NavigationProvider value={adapter}><TrajectoryEntry store={store} query={{ ...graphQuery, filters: filtered ? { priorities: ["urgent"] } : {} }} ready={ready}><p>Filter directory unavailable</p></TrajectoryEntry></NavigationProvider></QueryClientProvider>;
  const result = renderWithI18n(view(true));
  await screen.findByText("Synthetic parent");
  fixture.fail(403);
  fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
  await waitFor(() => expect(isGraphScopeClosed(client, graphScope)).toBe(true));
  const reads = fixture.requests.length;
  result.rerender(view(false, true));
  expect(screen.getByText("Filter directory unavailable")).toBeVisible();
  result.rerender(view(true));
  await screen.findByRole("alert");
  expect(isGraphScopeClosed(client, graphScope)).toBe(true);
  expect(fixture.requests).toHaveLength(reads);
});


it("clears an initial URL selection permanently after a required directory pauses the graph", async () => {
  const fixture = graphFixture(), store = createTrajectoryEntryStore(true), select = vi.fn();
  vi.stubGlobal("fetch", fixture.fetch); setApiInstance(new ApiClient(""));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client);
  const navigation = { requested: true, selection: { kind: "run" as const, id: graphId(20), issueId: graphId(10) }, select };
  const view = (ready: boolean, filtered = false) => <QueryClientProvider client={client}><NavigationProvider value={adapter}><TrajectoryEntry store={store} query={{ ...graphQuery, filters: filtered ? { priorities: ["urgent"] } : {} }} navigation={navigation} ready={ready}><p>Required directory unavailable</p></TrajectoryEntry></NavigationProvider></QueryClientProvider>;
  const result = renderWithI18n(view(true));
  await screen.findByRole("dialog", { name: "Run · 00000020" });
  result.rerender(view(false, true));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(select).toHaveBeenCalledWith(null);
  result.rerender(view(true));
  await screen.findByRole("textbox", { name: "Search tasks" });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
