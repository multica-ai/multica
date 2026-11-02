import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import {
  EMPTY_AUTOPILOT_FILTERS,
  useAutopilotsViewStore,
} from "@multica/core/autopilots/stores";
import type { Autopilot } from "@multica/core/types";
import { NavigationProvider } from "../../navigation";
import { renderWithI18n } from "../../test/i18n";
import { AutopilotsPage } from "./autopilots-page";

vi.mock("@multica/core/api", () => ({ api: { listAutopilots: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    autopilots: () => "/ws/autopilots",
    autopilotDetail: (id: string) => `/ws/autopilots/${id}`,
  }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Lambda" }),
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("./autopilot-list-actions", () => ({
  AutopilotBatchToolbar: () => null,
  AutopilotRowActions: () => null,
}));
vi.mock("./workspace-wakeups", () => ({
  WorkspaceWakeups: () => <div>Wakeup inventory</div>,
}));
vi.mock("./autopilot-dialog", () => ({
  AutopilotDialog: ({ initial }: { initial?: { title: string } }) => (
    <div role="dialog">{initial?.title ?? "Blank autopilot"}</div>
  ),
}));
// jsdom has no layout, so the virtualizer would otherwise render no rows.
vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: ({ count }: { count: number }) => ({
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({
        index,
        key: index,
        start: index * 44,
        end: (index + 1) * 44,
        size: 44,
      })),
    getTotalSize: () => count * 44,
  }),
}));

function autopilot(id: string, title: string, status: Autopilot["status"]): Autopilot {
  return {
    id,
    title,
    status,
    workspace_id: "ws",
    description: "",
    assignee_type: "agent",
    assignee_id: "agent-1",
    execution_mode: "create_issue",
    created_by_type: "member",
    created_by_id: "member-1",
    trigger_kinds: ["schedule"],
    last_run_at: null,
    next_run_at: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  } as unknown as Autopilot;
}

let currentPath = "";

function Harness({ initialPath }: { initialPath: string }) {
  const [path, setPath] = useState(initialPath);
  currentPath = path;
  const url = new URL(path, "https://example.test");
  return (
    <NavigationProvider
      value={{
        pathname: url.pathname,
        searchParams: url.searchParams,
        hash: "",
        push: setPath,
        replace: setPath,
        back: () => {},
        getShareableUrl: (v) => v,
      }}
    >
      <AutopilotsPage />
    </NavigationProvider>
  );
}

function mount(initialPath = "/ws/autopilots") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <Harness initialPath={initialPath} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  useAutopilotsViewStore.setState({ scope: "all", filters: EMPTY_AUTOPILOT_FILTERS });
  vi.mocked(api.listAutopilots).mockReset().mockResolvedValue({
    autopilots: [
      autopilot("a1", "Daily sync", "active"),
      autopilot("a2", "Bug triage", "active"),
      autopilot("a3", "Weekly report", "paused"),
      autopilot("a4", "Old digest", "archived"),
    ],
    total: 4,
  });
});

describe("AutopilotsPage", () => {
  it("shows the workspace's issue wakeups on ?tab=wakeups instead of the list", async () => {
    mount("/ws/autopilots?tab=wakeups");

    expect(await screen.findByText("Wakeup inventory")).toBeVisible();
    expect(screen.queryByRole("button", { name: "New autopilot" })).toBeNull();
    expect(api.listAutopilots).not.toHaveBeenCalled();
  });

  it("titles the list with its scope and switches scope from the title menu", async () => {
    const user = userEvent.setup();
    mount();
    await screen.findByText("Weekly report");
    expect(screen.getByText("Daily sync")).toBeVisible();
    expect(screen.queryByText("Old digest")).toBeNull();

    await user.click(screen.getByRole("button", { name: "All autopilots" }));
    expect(await screen.findByRole("menuitemradio", { name: /All autopilots\s*3/ })).toBeChecked();
    expect(screen.getByRole("menuitemradio", { name: /Active\s*2/ })).toBeInTheDocument();
    await user.click(screen.getByRole("menuitemradio", { name: /Paused\s*1/ }));

    expect(await screen.findByRole("button", { name: "Paused" })).toBeVisible();
    expect(screen.getByText("Weekly report")).toBeVisible();
    expect(screen.queryByText("Daily sync")).toBeNull();
    expect(useAutopilotsViewStore.getState().scope).toBe("paused");
  });

  it("has no search, and Reset clears the filters", async () => {
    const user = userEvent.setup();
    useAutopilotsViewStore.setState({
      filters: { ...EMPTY_AUTOPILOT_FILTERS, triggerKinds: ["webhook"] },
    });
    mount();
    await screen.findByRole("button", { name: "Reset" });
    expect(screen.queryByRole("button", { name: /search/i })).toBeNull();
    expect(screen.queryByText("Daily sync")).toBeNull();

    await user.click(screen.getByRole("button", { name: "Reset" }));
    expect(await screen.findByText("Daily sync")).toBeVisible();
    expect(screen.queryByRole("button", { name: "Reset" })).toBeNull();
  });

  it("opens the create dialog from the header", async () => {
    const user = userEvent.setup();
    mount();
    await screen.findByText("Daily sync");

    await user.click(screen.getByRole("button", { name: "New autopilot" }));
    expect(await screen.findByRole("dialog")).toHaveTextContent("Blank autopilot");
  });

  it("consumes ?template once and keeps the other params", async () => {
    mount("/ws/autopilots?template=bug_triage&keep=yes");

    expect(await screen.findByRole("dialog")).toHaveTextContent("Bug triage");
    await waitFor(() => expect(currentPath).toBe("/ws/autopilots?keep=yes"));
  });

  it("keeps create reachable when the list fails to load", async () => {
    vi.mocked(api.listAutopilots).mockRejectedValue(new Error("Autopilots unavailable"));
    mount();

    expect(await screen.findByText("Autopilots unavailable")).toBeVisible();
    expect(screen.getByRole("button", { name: "New autopilot" })).toBeVisible();
  });
});
