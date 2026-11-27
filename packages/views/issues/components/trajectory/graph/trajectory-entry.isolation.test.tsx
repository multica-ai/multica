import { beforeAll, afterEach, expect, it, vi } from "vitest";
import { useState } from "react";
import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiClient, setApiInstance } from "@multica/core/api";
import { createTrajectoryEntryStore } from "@multica/core/trajectory/web-entry";
import { createIssueViewStore } from "@multica/core/issues/stores/view-store";
import { useStore } from "zustand";
import { paths } from "@multica/core/paths";
import { NavigationProvider, type NavigationAdapter } from "../../../../navigation";
import { renderWithI18n } from "../../../../test/i18n";
import { TrajectoryEntry, type TrajectoryPageLoader } from "./trajectory-entry";
import { graphFixture, graphId, graphQuery } from "./graph-fixture";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => graphId(2) }));
const authEvents = vi.hoisted(() => ({ listeners: new Set<(state: { user: { id: string }; status: string }) => void>() }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: Object.assign((selector: (state: unknown) => unknown) => selector({ user: { id: graphId(1) }, status: "authenticated" }), { subscribe: (listener: (state: { user: { id: string }; status: string }) => void) => { authEvents.listeners.add(listener); return () => authEvents.listeners.delete(listener); } }) }));
vi.mock("@multica/core/realtime", () => ({ useWS: () => ({ subscribe: () => () => {}, onReconnect: () => () => {} }) }));
vi.mock("@multica/core/paths", async original => ({ ...await original<typeof import("@multica/core/paths")>(), useWorkspacePaths: () => paths.workspace("synthetic") }));
const adapter: NavigationAdapter = { pathname: "/synthetic/issues", searchParams: new URLSearchParams(), hash: "", push: vi.fn(), replace: vi.fn(), back: vi.fn(), getShareableUrl: path => path };
const clients: QueryClient[] = [];
// Transform the lazy page chunk once up front; it imports the issues page filter menu.
beforeAll(async () => { await import("./trajectory-active-page"); }, 60_000);
afterEach(() => { cleanup(); clients.forEach(client => client.clear()); clients.length = 0; vi.unstubAllGlobals(); });

function harness(loadPage?: TrajectoryPageLoader) {
  const fixture = graphFixture(), entry = createTrajectoryEntryStore();
  const native = createIssueViewStore("synthetic-trajectory-isolation"); native.setState({ statusFilters: ["in_review"] });
  const seed = { ...graphQuery, search: "A", filters: { statuses: ["in_review"] } };
  const original = JSON.stringify(seed);
  function NativeList() {
    const [search, setSearch] = useState("A");
    const statuses = useStore(native, state => state.statusFilters);
    return <section aria-label="Native list"><input aria-label="Native search" value={search} onChange={event => setSearch(event.target.value)} /><output aria-label="Native status">{statuses.join(",")}</output></section>;
  }
  vi.stubGlobal("fetch", fixture.fetch); setApiInstance(new ApiClient(""));
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client);
  renderWithI18n(<QueryClientProvider client={client}><NavigationProvider value={adapter}><TrajectoryEntry store={entry} query={seed} loadPage={loadPage}><NativeList /></TrajectoryEntry></NavigationProvider></QueryClientProvider>);
  const node = screen.getByRole("textbox", { name: "Native search" });
  fireEvent.change(node, { target: { value: "A kept locally" } });
  return { entry, fixture, native, seed, original, node };
}

it("loads the feature only on activation and keeps native local/store state after a chunk failure", async () => {
  const load = vi.fn().mockRejectedValue(new Error("synthetic chunk failure"));
  const state = harness(load);
  expect(load).not.toHaveBeenCalled(); expect(state.fixture.requests).toHaveLength(0);
  act(() => state.entry.getState().setActive(true));
  await screen.findByRole("alert"); expect(load).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByRole("button", { name: "Back to list" }));
  expect(screen.getByRole("textbox", { name: "Native search" })).toBe(state.node);
  expect(state.node).toHaveValue("A kept locally"); expect(state.native.getState().statusFilters).toEqual(["in_review"]);
});

it("contains a feature render fault and returns to the same native list", async () => {
  const load = vi.fn().mockResolvedValue({ default: () => { throw new Error("synthetic render fault"); } });
  const state = harness(load); act(() => state.entry.getState().setActive(true));
  await screen.findByRole("alert"); fireEvent.click(screen.getByRole("button", { name: "Back to list" }));
  expect(state.node).toBeVisible(); expect(state.node).toHaveValue("A kept locally");
  expect(state.native.getState().statusFilters).toEqual(["in_review"]);
});

it("keeps Graph search B page-local and returns after a request failure with native A and filters intact", async () => {
  const state = harness(); act(() => state.entry.getState().setActive(true));
  const search = await screen.findByRole("textbox", { name: "Search tasks" });
  fireEvent.change(search, { target: { value: "B" } });
  await waitFor(() => expect(screen.getByRole("button", { name: "Refresh" })).not.toBeDisabled());
  expect(JSON.stringify(state.seed)).toBe(state.original);
  expect(state.native.getState().statusFilters).toEqual(["in_review"]);
  state.fixture.fail(503);
  fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
  await screen.findByRole("alert");
  fireEvent.click(screen.getByRole("button", { name: "Back to list" }));
  expect(screen.getByRole("textbox", { name: "Native search" })).toBe(state.node);
  expect(state.node).toHaveValue("A kept locally"); expect(state.native.getState().statusFilters).toEqual(["in_review"]);
});

it("closes the active page on an account change without navigating back to the former scope", async () => {
  const state = harness(); act(() => state.entry.getState().setActive(true));
  await screen.findByRole("textbox", { name: "Search tasks" });
  act(() => { for (const listener of [...authEvents.listeners]) listener({ user: { id: graphId(3) }, status: "authenticated" }); });
  await waitFor(() => expect(state.entry.getState().active).toBe(false));
  expect(screen.queryByRole("textbox", { name: "Search tasks" })).not.toBeInTheDocument();
  expect(state.node).toBeVisible(); expect(state.node).toHaveValue("A kept locally");
});
