// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, beforeAll } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { Autopilot } from "@multica/core/types";
import { useListDetailSplitStore } from "@multica/core/list-detail/stores";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";
import { AutopilotDetailRoute } from "./autopilot-detail-route";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    autopilotDetail: (id: string) => `/acme/autopilots/${id}`,
  }),
}));

// Desktop / compact layout switch for the two-column detail route.
const layout = vi.hoisted(() => ({ compact: false }));
vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => layout.compact,
  useIsCompact: () => layout.compact,
}));

const railAutopilots = vi.hoisted(() => [] as unknown[]);
vi.mock("@multica/core/autopilots/queries", () => ({
  autopilotListOptions: () => ({
    queryKey: ["autopilots", "ws-1", "list"],
    queryFn: () => Promise.resolve(railAutopilots),
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

// The detail pane is not under test here; stub it so the rail/layout is what
// renders and the detail's own queries never fire.
vi.mock("./autopilot-detail-page", () => ({
  AutopilotDetailPage: () => <div data-testid="autopilot-detail" />,
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

function makeAutopilot(id: string, title: string): Autopilot {
  return {
    id,
    workspace_id: "ws-1",
    title,
    description: null,
    project_id: null,
    assignee_type: "agent",
    assignee_id: "agent-1",
    status: "active",
    execution_mode: "create_issue",
    issue_title_template: null,
    created_by_type: "member",
    created_by_id: "user-1",
    last_run_at: null,
    created_at: "2026-08-01T00:00:00Z",
    updated_at: "2026-08-01T00:00:00Z",
  };
}

const autopilotA = makeAutopilot("a-1", "Nightly digest");
const autopilotB = makeAutopilot("a-2", "PR review");

function renderRoute(routeId: string, qc: QueryClient) {
  const adapter: NavigationAdapter = {
    push,
    replace,
    back: vi.fn(),
    pathname: `/acme/autopilots/${routeId}`,
    searchParams: new URLSearchParams(),
    getShareableUrl: (p: string) => `https://app.multica.com${p}`,
  };
  return render(
    <QueryClientProvider client={qc}>
      <NavigationProvider value={adapter}>
        <AutopilotDetailRoute autopilotId={routeId} />
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
  railAutopilots.splice(0, railAutopilots.length, autopilotA, autopilotB);
});

describe("AutopilotDetailRoute desktop split layout", () => {
  it("defaults to the collapsed rail strip beside the detail", async () => {
    const qc = newClient();
    renderRoute("a-1", qc);

    expect(await screen.findByTestId("autopilot-detail")).toBeInTheDocument();
    expect(
      screen.getByTestId("autopilot-detail-rail-collapsed"),
    ).toBeInTheDocument();
    expect(screen.queryByTestId("autopilot-detail-rail-list")).not.toBeInTheDocument();
    qc.clear();
  });

  it("renders a two-column desktop layout: left list rail + right detail", async () => {
    useListDetailSplitStore.setState({ collapsed: false });
    const qc = newClient();
    renderRoute("a-1", qc);

    expect(await screen.findByTestId("autopilot-detail")).toBeInTheDocument();
    expect(screen.getByTestId("autopilot-detail-rail-list")).toBeInTheDocument();
    expect(
      await screen.findByTestId("autopilot-detail-rail-row-a-2"),
    ).toBeInTheDocument();
    qc.clear();
  });

  it("keeps the full-width detail without the left rail on compact widths", async () => {
    layout.compact = true;
    useListDetailSplitStore.setState({ collapsed: false });
    const qc = newClient();
    renderRoute("a-1", qc);

    expect(await screen.findByTestId("autopilot-detail")).toBeInTheDocument();
    expect(screen.queryByTestId("autopilot-detail-rail-list")).not.toBeInTheDocument();
    expect(screen.queryByTestId("autopilot-detail-rail-collapsed")).not.toBeInTheDocument();
    qc.clear();
  });

  it("navigates in place when a rail row is clicked", async () => {
    useListDetailSplitStore.setState({ collapsed: false });
    const qc = newClient();
    renderRoute("a-1", qc);

    fireEvent.click(await screen.findByTestId("autopilot-detail-rail-row-a-2"));

    expect(replace).toHaveBeenCalledWith("/acme/autopilots/a-2");
    qc.clear();
  });

  it("collapses and expands the rail via the header toggle, writing the store", async () => {
    useListDetailSplitStore.setState({ collapsed: false });
    const qc = newClient();
    renderRoute("a-1", qc);

    const toggle = await screen.findByTestId("autopilot-detail-rail-toggle");
    fireEvent.click(toggle);

    expect(useListDetailSplitStore.getState().collapsed).toBe(true);
    expect(screen.getByTestId("autopilot-detail-rail-collapsed")).toBeInTheDocument();
    expect(screen.queryByTestId("autopilot-detail-rail-list")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("autopilot-detail-rail-toggle"));

    expect(useListDetailSplitStore.getState().collapsed).toBe(false);
    expect(screen.getByTestId("autopilot-detail-rail-list")).toBeInTheDocument();
    expect(screen.queryByTestId("autopilot-detail-rail-collapsed")).not.toBeInTheDocument();
    qc.clear();
  });
});
