import { beforeAll, afterEach, expect, it, vi } from "vitest";
import { useState } from "react";
import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { notifyManager, QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { issueViewKeys } from "@multica/core/issue-views/queries";
import { useActiveIssueViewStore, issueViewContainerKey } from "@multica/core/issue-views/active-view-store";
import { paths } from "@multica/core/paths";
import { ApiClient, setApiInstance } from "@multica/core/api";
import { trajectoryCapabilityKeys } from "@multica/core/trajectory/capability";
import type { IssueSurfaceController } from "../../../surface/use-issue-surface-controller";
import { NavigationProvider, type NavigationAdapter } from "../../../../navigation";
import { renderWithI18n } from "../../../../test/i18n";
import { TrajectoryRouteHost, type TrajectoryHeaderRenderer } from "./trajectory-route-host";
import type { TrajectoryPageLoader } from "./trajectory-entry";
import { graphFixture, graphId, graphQuery } from "./graph-fixture";

vi.mock("@multica/core/paths", async original => ({ ...await original<typeof import("@multica/core/paths")>(), useWorkspacePaths: () => paths.workspace("synthetic") }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => graphId(2) }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: Object.assign((selector: (state: unknown) => unknown) => selector({ user: { id: graphId(1) }, status: "authenticated" }), { subscribe: () => () => {} }) }));
vi.mock("@multica/core/realtime", () => ({ useWS: () => ({ subscribe: () => () => {}, onReconnect: () => () => {} }) }));
const clients: QueryClient[] = [];
// Transform the lazy page chunk once up front; it imports the issues page filter menu.
beforeAll(async () => { await import("./trajectory-active-page"); }, 60_000);
afterEach(() => { cleanup(); clients.forEach(c => c.clear()); clients.length = 0; vi.unstubAllGlobals(); });

function NativeList({ trailing, mode = "table", projectId }: {
  trailing: TrajectoryHeaderRenderer;
  mode?: IssueSurfaceController["viewMode"];
  projectId?: string;
}) {
  const views = useQuery<{ id: string; query?: { statusFilters?: string[] } }[]>({ queryKey: issueViewKeys.list(graphId(2), { scope_type: "workspace" }), enabled: false }).data;
  const [search, setSearch] = useState("A");
  const [status, setStatus] = useState("in_review");
  const query = { ...graphQuery, scope: projectId ? { kind: "project", project_id: projectId } : graphQuery.scope, search: mode === "table" ? "debounced old value" : undefined, filters: { statuses: views?.[0]?.query?.statusFilters ?? [status], advanced: { match: "all" as const, children: [{ field: "priorities" as const, values: ["urgent"] }] } } };
  const controller = { projectId, viewMode: mode, tableSearch: mode === "table" ? search : "stale table search", tableQuerySpec: query, isLoading: false, filterDependencyError: null } as unknown as IssueSurfaceController;
  return <section aria-label="Native list"><input aria-label="Native search" value={search} onChange={e => setSearch(e.target.value)} /><select aria-label="Native status" value={status} onChange={e => setStatus(e.target.value)}><option>in_review</option><option>todo</option></select><output aria-label="Native advanced">priority = urgent</output><button type="button">Native Display</button>{trailing(controller)}</section>;
}
function harness(params = "view=native-A", loadPage?: TrajectoryPageLoader, mode: IssueSurfaceController["viewMode"] = "table", viewReady = true, activeMatches = true, extra: Partial<NavigationAdapter> = {}, projectId?: string) {
  const fixture = graphFixture(); vi.stubGlobal("fetch", fixture.fetch); setApiInstance(new ApiClient(""));
  const adapter: NavigationAdapter = { pathname: "/synthetic/issues", searchParams: new URLSearchParams(params), hash: "#native-fragment", push: vi.fn(), replace: vi.fn(), back: vi.fn(), getShareableUrl: p => p, ...extra };
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client);
  client.setQueryData(trajectoryCapabilityKeys.detail(graphId(2)), { enabled: true });
  const viewId = adapter.searchParams.get("view");
  useActiveIssueViewStore.setState({ active: viewId && activeMatches ? { [issueViewContainerKey(graphId(2), { scope_type: "workspace" })]: viewId } : {} });
  if (viewReady && viewId) client.setQueryData(issueViewKeys.list(graphId(2), { scope_type: "workspace" }), [{ id: viewId }]);
  const result = renderWithI18n(<QueryClientProvider client={client}><NavigationProvider value={adapter}><TrajectoryRouteHost loadPage={loadPage}>{trailing => <NativeList trailing={trailing} mode={mode} projectId={projectId} />}</TrajectoryRouteHost></NavigationProvider></QueryClientProvider>);
  return { fixture, adapter, client, result, node: screen.getByRole("textbox", { name: "Native search", hidden: true }) };
}

it("captures current search plus ordinary and advanced filters only on click and preserves native A on normal return", async () => {
  let captured: unknown;
  const load = vi.fn(async () => ({ default: (props: import("./trajectory-entry").TrajectoryPageProps) => { captured = props.query; return <><p>Page B</p><button type="button" onClick={props.onNativeFallback}>Back to list</button></>; } }));
  const h = harness(undefined, load);
  expect(load).not.toHaveBeenCalled(); expect(h.fixture.requests).toHaveLength(0);
  fireEvent.change(h.node, { target: { value: "A latest" } });
  fireEvent.click(screen.getByRole("button", { name: "Open trajectory" }));
  await screen.findByText("Page B");
  expect(captured).toMatchObject({ search: "A latest", filters: { statuses: ["in_review"], advanced: { match: "all", children: [{ field: "priorities", values: ["urgent"] }] } } });
  fireEvent.click(screen.getByRole("button", { name: "Back to list" }));
  expect(screen.getByRole("textbox", { name: "Native search" })).toBe(h.node); expect(h.node).toHaveValue("A latest");
  expect(screen.getByRole("combobox", { name: "Native status" })).toHaveValue("in_review");
  expect(screen.getByRole("button", { name: "Native Display" })).toBeEnabled();
  expect(h.adapter.replace).toHaveBeenLastCalledWith("/synthetic/issues?view=native-A#native-fragment");
});

it.each(["load", "render"])("returns to the mounted native list after a %s fault through the registered entry", async fault => {
  const load = fault === "load" ? vi.fn().mockRejectedValue(new Error("synthetic chunk")) : vi.fn().mockResolvedValue({ default: () => { throw new Error("synthetic render"); } });
  const h = harness(undefined, load); fireEvent.click(screen.getByRole("button", { name: "Open trajectory" }));
  await screen.findByRole("alert"); fireEvent.click(screen.getByRole("button", { name: "Back to list" }));
  expect(h.node).toBeVisible(); expect(h.node).toHaveValue("A");
  expect(screen.getByRole("combobox", { name: "Native status" })).toHaveValue("in_review");
  expect(screen.getByLabelText("Native advanced")).toHaveTextContent("priority = urgent");
});

it("keeps Graph B search and expansion isolated before returning from a read failure", async () => {
  const h = harness(); fireEvent.click(screen.getByRole("button", { name: "Open trajectory" }));
  const search = await screen.findByRole("textbox", { name: "Search tasks" });
  fireEvent.change(search, { target: { value: "Synthetic" } });
  await screen.findByText("Synthetic parent");
  await waitFor(() => expect(screen.getByRole("button", { name: "Refresh" })).not.toBeDisabled());
  fireEvent.click(await screen.findByRole("button", { name: "Expand runs Synthetic parent" }));
  await screen.findByRole("button", { name: "Record details Synthetic execution" });
  expect(h.fixture.requests.some(r => r.path.endsWith("/messages"))).toBe(false);
  h.fixture.fail(503); fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
  await screen.findByRole("alert"); fireEvent.click(screen.getByRole("button", { name: "Back to list" }));
  expect(h.node).toBeVisible(); expect(h.node).toHaveValue("A");
  expect(screen.getByRole("combobox", { name: "Native status" })).toHaveValue("in_review");
});

it("opens valid cold links from the current native snapshot without dropping view or reopening after exit", async () => {
  const load = vi.fn(async () => ({ default: (props: import("./trajectory-entry").TrajectoryPageProps) => <><p>Cold page</p><button type="button" onClick={props.onNativeFallback}>Back to list</button></> }));
  const h = harness("view=native-A&trajectory=1", load);
  await screen.findByText("Cold page"); fireEvent.click(screen.getByRole("button", { name: "Back to list" }));
  await waitFor(() => expect(h.node).toBeVisible());
  expect(load).toHaveBeenCalledTimes(1);
  expect(h.adapter.replace).toHaveBeenLastCalledWith("/synthetic/issues?view=native-A#native-fragment");
});

it("leaves malformed cold links on the native surface with no graph load or request", () => {
  const load = vi.fn(); const h = harness("view=native-A&trajectory=1&target_kind=run&target_id=bad", load);
  expect(screen.getByRole("alert")).toHaveTextContent("Invalid trajectory link");
  expect(h.node).toBeVisible(); expect(load).not.toHaveBeenCalled(); expect(h.fixture.requests).toHaveLength(0);
});

it.each(["list", "board", "swimlane"] as const)("does not inject hidden table search into an unsearched %s view", async mode => {
  let query: unknown;
  const h = harness(undefined, async () => ({ default: props => { query = props.query; return <p>Native mode snapshot</p>; } }), mode);
  fireEvent.change(h.node, { target: { value: "Live native A" } });
  fireEvent.click(screen.getByRole("button", { name: "Open trajectory" }));
  await screen.findByText("Native mode snapshot");
  expect(query).not.toHaveProperty("search");
});

it("waits for the native saved-view cache before click or cold-link activation, with no additional read", async () => {
  let query: unknown;
  const load = vi.fn(async () => ({ default: (props: import("./trajectory-entry").TrajectoryPageProps) => { query = props.query; return <p>Resolved saved A</p>; } }));
  const h = harness("view=saved-A&trajectory=1", load, "table", false);
  expect(screen.getByRole("button", { name: "Open trajectory", hidden: true })).toBeDisabled();
  expect(load).not.toHaveBeenCalled(); expect(h.fixture.requests).toHaveLength(0);
  await act(async () => { h.client.setQueryData(issueViewKeys.list(graphId(2), { scope_type: "workspace" }), [{ id: "saved-A", query: { statusFilters: ["todo"] } }]); });
  await screen.findByText("Resolved saved A");
  expect(query).toMatchObject({ filters: { statuses: ["todo"] } });
  expect(h.fixture.requests).toHaveLength(0);
});

it("waits for URL view adoption even when the requested view is already cached", async () => {
  const load = vi.fn(async () => ({ default: () => <p>Cached saved A</p> }));
  const h = harness("view=cached-A&trajectory=1", load, "table", true, false);
  expect(screen.getByRole("button", { name: "Open trajectory" })).toBeDisabled();
  expect(load).not.toHaveBeenCalled();
  await act(async () => { useActiveIssueViewStore.getState().setActive(issueViewContainerKey(graphId(2), { scope_type: "workspace" }), "cached-A"); });
  await screen.findByText("Cached saved A");
  expect(h.fixture.requests).toHaveLength(0);
});

const nativeQueries = (client: QueryClient) => client.getQueryCache().getAll().filter(query => query.queryKey[0] !== "trajectory-capability");

function cacheOnlyHarness(projectId?: string, existing = true) {
  const client = new QueryClient(); clients.push(client);
  client.setQueryData(trajectoryCapabilityKeys.detail(graphId(2)), { enabled: true });
  const scope = projectId ? { scope_type: "project" as const, scope_id: projectId } : { scope_type: "workspace" as const };
  const queryKey = issueViewKeys.list(graphId(2), scope);
  const sourceRead = vi.fn(async () => [{ id: "native-A" }]);
  const nativeOptions = { queryKey, queryFn: sourceRead, staleTime: 120000, gcTime: Infinity };
  const query = existing ? client.getQueryCache().build(client, nativeOptions) : undefined;
  query?.setData([{ id: "native-A" }]);
  useActiveIssueViewStore.setState({ active: { [issueViewContainerKey(graphId(2), scope)]: "native-A" } });
  const adapter: NavigationAdapter = { pathname: "/synthetic/issues", searchParams: new URLSearchParams("view=native-A"), hash: "", push: vi.fn(), replace: vi.fn(), back: vi.fn(), getShareableUrl: p => p };
  const controller = { projectId, viewMode: "table", tableSearch: "A", tableQuerySpec: graphQuery, isLoading: false, filterDependencyError: null } as unknown as IssueSurfaceController;
  function Native({ trailing }: { trailing: TrajectoryHeaderRenderer }) {
    const [search, setSearch] = useState("A");
    return <><input aria-label="Cached native search" value={search} onChange={event => setSearch(event.target.value)} />{trailing(controller)}</>;
  }
  const before = { queries: client.getQueryCache().getAll(), options: query?.options, observers: query?.getObserversCount() };
  const load = vi.fn();
  const result = renderWithI18n(<QueryClientProvider client={client}><NavigationProvider value={adapter}><TrajectoryRouteHost loadPage={load}>{trailing => <Native trailing={trailing} />}</TrajectoryRouteHost></NavigationProvider></QueryClientProvider>);
  return { client, queryKey, sourceRead, query, before, load, result };
}

it.each([undefined, "project-A"])("preserves the native query and its options through host mount and unmount (%s)", projectId => {
  const h = cacheOnlyHarness(projectId);
  expect(h.query!.options.queryFn).toBe(h.sourceRead);
  expect(h.query!.options).toMatchObject({ staleTime: 120000 });
  expect(h.query!.options).toBe(h.before.options);
  expect(h.query!.getObserversCount()).toBe(h.before.observers);
  expect(h.client.getQueryCache().getAll()).toEqual(h.before.queries);
  expect(h.sourceRead).not.toHaveBeenCalled(); expect(h.load).not.toHaveBeenCalled();
  h.result.unmount();
  expect(h.query!.options).toBe(h.before.options);
  expect(h.query!.getObserversCount()).toBe(h.before.observers);
  expect(h.client.getQueryCache().getAll()).toEqual(h.before.queries);
});

it("waits for a missing native query without creating a cache entry or fetching", () => {
  const h = cacheOnlyHarness(undefined, false);
  expect(screen.getByRole("button", { name: "Open trajectory" })).toBeDisabled();
  expect(nativeQueries(h.client)).toHaveLength(0);
  expect(h.sourceRead).not.toHaveBeenCalled(); expect(h.load).not.toHaveBeenCalled();
  h.result.unmount();
  expect(nativeQueries(h.client)).toHaveLength(0);
});

it("follows exact cache removal and replacement while preserving the native list state", async () => {
  const h = cacheOnlyHarness("project-A");
  const native = screen.getByRole("textbox", { name: "Cached native search" });
  fireEvent.change(native, { target: { value: "A kept" } });
  expect(screen.getByRole("button", { name: "Open trajectory" })).toBeEnabled();
  await act(async () => { h.client.removeQueries({ queryKey: h.queryKey, exact: true }); });
  await waitFor(() => expect(screen.getByRole("button", { name: "Open trajectory" })).toBeDisabled());
  expect(nativeQueries(h.client)).toHaveLength(0);
  await act(async () => {
    h.client.setQueryData(issueViewKeys.list(graphId(2), { scope_type: "project", scope_id: "project-B" }), [{ id: "native-A" }]);
  });
  expect(screen.getByRole("button", { name: "Open trajectory" })).toBeDisabled();
  const nativeOptions = { queryKey: h.queryKey, queryFn: h.sourceRead, staleTime: 120000, gcTime: Infinity };
  const replacement = h.client.getQueryCache().build(h.client, nativeOptions);
  const replacementOptions = replacement.options;
  await act(async () => { replacement.setData([{ id: "native-A" }]); });
  await waitFor(() => expect(screen.getByRole("button", { name: "Open trajectory" })).toBeEnabled());
  expect(replacement.options).toBe(replacementOptions);
  expect(replacement.options.queryFn).toBe(h.sourceRead); expect(replacement.options).toMatchObject({ staleTime: 120000 });
  expect(replacement.getObserversCount()).toBe(0);
  expect(screen.getByRole("textbox", { name: "Cached native search" })).toBe(native); expect(native).toHaveValue("A kept");
  expect(h.sourceRead).not.toHaveBeenCalled(); expect(h.load).not.toHaveBeenCalled();
});

it("cancels deferred cold-link capture when the host unmounts", async () => {
  const load = vi.fn(async () => ({ default: () => <p>Unmounted cold page</p> }));
  const h = harness("view=native-A&trajectory=1", load);
  await act(async () => {
    h.result.unmount();
    await new Promise<void>(resolve => notifyManager.schedule(resolve));
  });
  expect(load).not.toHaveBeenCalled();
  expect(h.fixture.requests).toHaveLength(0);
});

function capabilityHarness(params: string, capability: unknown) {
  const fixture = graphFixture(); fixture.setCapability(capability);
  vi.stubGlobal("fetch", fixture.fetch); setApiInstance(new ApiClient(""));
  const adapter: NavigationAdapter = { pathname: "/synthetic/issues", searchParams: new URLSearchParams(params), hash: "", push: vi.fn(), replace: vi.fn(), back: vi.fn(), getShareableUrl: p => p };
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client);
  useActiveIssueViewStore.setState({ active: {} });
  renderWithI18n(<QueryClientProvider client={client}><NavigationProvider value={adapter}><TrajectoryRouteHost>{trailing => <NativeList trailing={trailing} />}</TrajectoryRouteHost></NavigationProvider></QueryClientProvider>);
  return { fixture, adapter, client };
}

it.each([{ enabled: false }, { enabled: "true" }, {}])("hides the entry unless the capability is an explicit enabled: true (%j)", async capability => {
  const h = capabilityHarness("", capability);
  await waitFor(() => expect(h.fixture.capabilityReads()).toBe(1));
  await act(async () => {});
  expect(screen.queryByRole("button", { name: "Open trajectory" })).not.toBeInTheDocument();
  expect(h.fixture.requests).toHaveLength(0);
});

it("hides the entry when the capability read fails", async () => {
  const fixture = graphFixture();
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => String(input).includes("/api/trajectory/capability") ? new Response("down", { status: 503 }) : fixture.fetch(input, init));
  setApiInstance(new ApiClient(""));
  const adapter: NavigationAdapter = { pathname: "/synthetic/issues", searchParams: new URLSearchParams(), hash: "", push: vi.fn(), replace: vi.fn(), back: vi.fn(), getShareableUrl: p => p };
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client);
  renderWithI18n(<QueryClientProvider client={client}><NavigationProvider value={adapter}><TrajectoryRouteHost>{trailing => <NativeList trailing={trailing} />}</TrajectoryRouteHost></NavigationProvider></QueryClientProvider>);
  await waitFor(() => expect(client.getQueryState(trajectoryCapabilityKeys.detail(graphId(2)))?.status).toBe("error"));
  expect(screen.queryByRole("button", { name: "Open trajectory" })).not.toBeInTheDocument();
});

it.each(["trajectory=1", "trajectory=invalid"])("drops a trajectory link when the workspace capability is disabled (%s)", async params => {
  const h = capabilityHarness(params, { enabled: false });
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  await waitFor(() => expect(h.adapter.replace).toHaveBeenCalledWith("/synthetic/issues"));
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(h.fixture.requests).toHaveLength(0);
});

it("returns to the mounted native list with its search when a graph read 404s after the flag is turned off", async () => {
  const h = capabilityHarness("", { enabled: true });
  const native = await screen.findByRole("textbox", { name: "Native search" });
  fireEvent.click(await screen.findByRole("button", { name: "Open trajectory" }));
  await screen.findByRole("button", { name: "First project" });
  expect(screen.getByRole("textbox", { name: "Native search", hidden: true })).not.toBeVisible();
  h.fixture.setCapability({ enabled: false });
  h.fixture.fail(404);
  await act(async () => { await h.client.refetchQueries({ queryKey: ["trajectory-graph"] }); });
  await waitFor(() => expect(screen.getByRole("textbox", { name: "Native search" })).toBeVisible());
  expect(screen.getByRole("textbox", { name: "Native search" })).toBe(native);
  expect(native).toHaveValue("A");
  expect(screen.queryByRole("button", { name: "Open trajectory" })).not.toBeInTheDocument();
});

it("writes an in-page selection to the URL in place, while leaving still navigates", async () => {
  const load = vi.fn(async () => ({ default: (props: import("./trajectory-entry").TrajectoryPageProps) => <>
    <button type="button" onClick={() => props.navigation?.select?.({ kind: "run", id: "r1", issueId: "i1", seq: 4 })}>Select run</button>
    <button type="button" onClick={() => props.navigation?.select?.(null)}>Close run</button>
    <button type="button" onClick={props.onNativeFallback}>Back to list</button>
  </> }));
  const replaceInPlace = vi.fn();
  const h = harness("view=native-A&trajectory=1", load, "table", true, true, { replaceInPlace });
  fireEvent.click(await screen.findByRole("button", { name: "Select run" }));
  expect(replaceInPlace).toHaveBeenLastCalledWith("/synthetic/issues?view=native-A&trajectory=1&target_kind=run&target_id=r1&target_issue_id=i1&seq=4#native-fragment");
  fireEvent.click(screen.getByRole("button", { name: "Close run" }));
  expect(replaceInPlace).toHaveBeenLastCalledWith("/synthetic/issues?view=native-A&trajectory=1#native-fragment");
  expect(h.adapter.replace).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Back to list" }));
  expect(h.adapter.replace).toHaveBeenLastCalledWith("/synthetic/issues?view=native-A#native-fragment");
});

it("includes the Issues tab in a project link even when the tab was remembered locally", async () => {
  const projectId = graphId(3), runId = graphId(4);
  const pathname = `/synthetic/projects/${projectId}`;
  const load: TrajectoryPageLoader = async () => ({ default: ({ navigation }) =>
    <button type="button" onClick={() => navigation?.select?.({ kind: "run", id: runId })}>Select run</button>,
  });
  const replaceInPlace = vi.fn();
  harness("", load, "table", true, true, { pathname, replaceInPlace }, projectId);
  fireEvent.click(await screen.findByRole("button", { name: "Open trajectory" }));
  fireEvent.click(await screen.findByRole("button", { name: "Select run" }));
  expect(replaceInPlace).toHaveBeenCalledWith(`${pathname}?tab=issues&trajectory=1&target_kind=run&target_id=${runId}#native-fragment`);
});
