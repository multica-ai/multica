// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, beforeAll } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { Agent } from "@multica/core/types";
import { useListDetailSplitStore } from "@multica/core/list-detail/stores";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";
import { AgentDetailRoute } from "./agent-detail-route";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    agentDetail: (id: string) => `/acme/agents/${id}`,
  }),
}));

// Desktop / compact layout switch for the two-column detail route.
const layout = vi.hoisted(() => ({ compact: false }));
vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => layout.compact,
  useIsCompact: () => layout.compact,
}));

const railAgents = vi.hoisted(() => [] as unknown[]);
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({
    queryKey: ["workspaces", "ws-1", "agents"],
    queryFn: () => Promise.resolve(railAgents),
  }),
}));

vi.mock("@multica/ui/components/ui/resizable", () => ({
  ResizablePanelGroup: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  ResizablePanel: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  ResizableHandle: () => null,
}));

// The rail's avatar is just a visual slot; the detail pane is not under test.
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <div data-testid="agent-avatar" />,
}));

vi.mock("./agent-detail-page", () => ({
  AgentDetailPage: () => <div data-testid="agent-detail" />,
}));

vi.mock("../../i18n", () => ({
  useT: () => ({ t: () => "" }),
}));

// Node 25 ships a partial `localStorage` shim under jsdom that's missing
// `clear`/`removeItem`; the split store's persist middleware touches it on
// rehydration, so give it a real in-memory Storage.
beforeAll(() => {
  if (typeof globalThis.localStorage?.clear !== "function") {
    const values = new Map<string, string>();
    const storage: Storage = {
      get length() { return values.size; },
      clear: () => values.clear(),
      getItem: (k) => values.get(k) ?? null,
      key: (i) => Array.from(values.keys())[i] ?? null,
      removeItem: (k) => { values.delete(k); },
      setItem: (k, v) => { values.set(k, v); },
    };
    Object.defineProperty(globalThis, "localStorage", { configurable: true, value: storage });
    Object.defineProperty(window, "localStorage", { configurable: true, value: storage });
  }
});

const replace = vi.fn();
const push = vi.fn();

function makeAgent(id: string, name: string): Agent {
  return {
    id,
    workspace_id: "ws-1",
    runtime_id: "runtime-1",
    name,
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_args: [],
    visibility: "workspace",
    permission_mode: "public_to",
    invocation_targets: [{ target_type: "workspace", target_id: null }],
    status: "idle",
    max_concurrent_tasks: 1,
    model: "",
    owner_id: "user-2",
    skills: [],
    created_at: "2026-08-01T00:00:00Z",
    updated_at: "2026-08-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
  };
}

const agentA = makeAgent("ag-1", "Lambda");
const agentB = makeAgent("ag-2", "Falcon");

function renderRoute(routeId: string, qc: QueryClient) {
  const adapter: NavigationAdapter = {
    push,
    replace,
    back: vi.fn(),
    pathname: `/acme/agents/${routeId}`,
    searchParams: new URLSearchParams(),
    getShareableUrl: (p: string) => `https://app.multica.com${p}`,
  };
  return render(
    <QueryClientProvider client={qc}>
      <NavigationProvider value={adapter}>
        <AgentDetailRoute agentId={routeId} />
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

function newClient() {
  return new QueryClient({
    defaultOptions: { queries: { staleTime: Infinity, retry: false } },
  });
}

beforeEach(() => {
  replace.mockClear();
  push.mockClear();
  layout.compact = false;
  useListDetailSplitStore.setState({ collapsed: true, size: undefined });
  railAgents.splice(0, railAgents.length, agentA, agentB);
});

describe("AgentDetailRoute desktop split layout", () => {
  it("defaults to the collapsed rail strip beside the detail", async () => {
    const qc = newClient();
    renderRoute("ag-1", qc);

    expect(await screen.findByTestId("agent-detail")).toBeInTheDocument();
    expect(screen.getByTestId("agent-detail-rail-collapsed")).toBeInTheDocument();
    expect(screen.queryByTestId("agent-detail-rail-list")).not.toBeInTheDocument();
    qc.clear();
  });

  it("renders a two-column desktop layout: left list rail + right detail", async () => {
    useListDetailSplitStore.setState({ collapsed: false });
    const qc = newClient();
    renderRoute("ag-1", qc);

    expect(await screen.findByTestId("agent-detail")).toBeInTheDocument();
    expect(screen.getByTestId("agent-detail-rail-list")).toBeInTheDocument();
    expect(
      await screen.findByTestId("agent-detail-rail-row-ag-2"),
    ).toBeInTheDocument();
    qc.clear();
  });

  it("keeps the full-width detail without the left rail on compact widths", async () => {
    layout.compact = true;
    useListDetailSplitStore.setState({ collapsed: false });
    const qc = newClient();
    renderRoute("ag-1", qc);

    expect(await screen.findByTestId("agent-detail")).toBeInTheDocument();
    expect(screen.queryByTestId("agent-detail-rail-list")).not.toBeInTheDocument();
    expect(screen.queryByTestId("agent-detail-rail-collapsed")).not.toBeInTheDocument();
    qc.clear();
  });

  it("navigates in place when a rail row is clicked", async () => {
    useListDetailSplitStore.setState({ collapsed: false });
    const qc = newClient();
    renderRoute("ag-1", qc);

    fireEvent.click(await screen.findByTestId("agent-detail-rail-row-ag-2"));

    expect(replace).toHaveBeenCalledWith("/acme/agents/ag-2");
    qc.clear();
  });

  it("collapses and expands the rail via the header toggle, writing the store", async () => {
    useListDetailSplitStore.setState({ collapsed: false });
    const qc = newClient();
    renderRoute("ag-1", qc);

    const toggle = await screen.findByTestId("agent-detail-rail-toggle");
    fireEvent.click(toggle);

    expect(useListDetailSplitStore.getState().collapsed).toBe(true);
    expect(screen.getByTestId("agent-detail-rail-collapsed")).toBeInTheDocument();
    expect(screen.queryByTestId("agent-detail-rail-list")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("agent-detail-rail-toggle"));

    expect(useListDetailSplitStore.getState().collapsed).toBe(false);
    expect(screen.getByTestId("agent-detail-rail-list")).toBeInTheDocument();
    expect(screen.queryByTestId("agent-detail-rail-collapsed")).not.toBeInTheDocument();
    qc.clear();
  });
});
