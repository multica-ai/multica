import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { api } from "@multica/core/api";
import { createAuthStore, registerAuthStore } from "@multica/core/auth";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import type { Agent, AgentRuntime, AgentTask } from "@multica/core/types";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import {
  agentListOptions,
  memberListOptions,
  skillListOptions,
  squadListOptions,
  workspaceListOptions,
} from "@multica/core/workspace/queries";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { renderWithI18n } from "../../test/i18n";
import { TeamPanel } from "./team-panel";

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { getIssue: vi.fn(() => new Promise(() => {})), getBaseUrl: () => "http://localhost" },
}));

const WS = "ws-test";
const NOW = Date.parse("2026-11-04T08:00:00Z");
const adapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/runtimes",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => path,
};

function runtime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "rt-online",
    workspace_id: WS,
    daemon_id: "daemon-1",
    name: "Claude (dev.local)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "dev.local",
    metadata: {},
    owner_id: "user-1",
    visibility: "private",
    last_seen_at: new Date(NOW - 42 * 60_000).toISOString(),
    created_at: new Date(NOW - 60 * 60_000).toISOString(),
    updated_at: new Date(NOW - 60 * 60_000).toISOString(),
    ...overrides,
  };
}

let qc: QueryClient;

beforeEach(() => {
  vi.mocked(api.getIssue).mockClear();
  useSidebarTreeStore.setState({ workingCollapsed: false, needsAttentionCollapsed: false });
  vi.spyOn(Date, "now").mockReturnValue(NOW);
  registerAuthStore(
    createAuthStore({
      api,
      storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
    }),
  );
  qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  qc.setQueryData(workspaceListOptions().queryKey, [{
    id: WS,
    name: "Acme",
    slug: "acme",
    description: null,
    context: null,
    settings: {},
    repos: [],
    issue_prefix: "ACM",
    avatar_url: null,
    created_at: new Date(NOW).toISOString(),
    updated_at: new Date(NOW).toISOString(),
  }]);
  for (const options of [
    agentListOptions(WS),
    agentTaskSnapshotOptions(WS),
    memberListOptions(WS),
    skillListOptions(WS),
    squadListOptions(WS),
  ]) {
    qc.setQueryData(options.queryKey, []);
  }
});

afterEach(() => {
  qc.clear();
  vi.restoreAllMocks();
});

function renderPanel(runtimes: AgentRuntime[]) {
  qc.setQueryData(runtimeListOptions(WS).queryKey, runtimes);
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <WorkspaceSlugProvider slug="acme">
        <NavigationProvider value={adapter}>
          <TeamPanel />
        </NavigationProvider>
      </WorkspaceSlugProvider>
    </QueryClientProvider>,
  );
}

function seedAgents(count: number, status: "running" | "queued" = "running") {
  const agents: Agent[] = Array.from({ length: count }, (_, index) => ({
    id: `${status}-${index}`,
    name: `${status} agent ${index + 1}`,
    workspace_id: WS,
    runtime_id: status === "running" ? "rt-online" : "rt-offline",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_args: [],
    visibility: "workspace",
    permission_mode: "public_to",
    invocation_targets: [],
    status: "idle",
    max_concurrent_tasks: 6,
    model: "",
    owner_id: null,
    skills: [],
    created_at: new Date(NOW).toISOString(),
    updated_at: new Date(NOW).toISOString(),
    archived_at: null,
    archived_by: null,
  }));
  const tasks: AgentTask[] = agents.map((agent) => ({
    id: `task-${agent.id}`,
    agent_id: agent.id,
    runtime_id: agent.runtime_id,
    issue_id: `issue-${agent.id}`,
    status,
    priority: 0,
    dispatched_at: null,
    started_at: status === "running" ? new Date(NOW).toISOString() : null,
    completed_at: null,
    result: null,
    error: null,
    created_at: new Date(NOW).toISOString(),
  }));
  qc.setQueryData<Agent[]>(agentListOptions(WS).queryKey, (current = []) => [...current, ...agents]);
  qc.setQueryData<AgentTask[]>(agentTaskSnapshotOptions(WS).queryKey, (current = []) => [...current, ...tasks]);
}

describe("TeamPanel agent previews", () => {
  it("previews five of 100 working agents, retaining the total and only fetching visible issue labels", async () => {
    const getIssue = vi.mocked(api.getIssue);
    seedAgents(100);
    renderPanel([runtime()]);

    const working = within(screen.getByRole("region", { name: "Working now" }));
    expect(working.getByText("100")).toBeInTheDocument();
    expect(working.getAllByRole("link")).toHaveLength(5);
    expect(working.queryByText("running agent 6")).not.toBeInTheDocument();
    await waitFor(() => expect(getIssue).toHaveBeenCalledTimes(5));
    expect(screen.getByRole("link", { name: /^All agents/ })).toBeInTheDocument();

    fireEvent.click(working.getByRole("button", { name: "Show 95 more…" }));
    expect(working.getAllByRole("link")).toHaveLength(100);
    expect(working.getByRole("button", { name: "Show fewer" })).toHaveAttribute("aria-expanded", "true");

    fireEvent.click(working.getByRole("button", { name: "Show fewer" }));
    expect(working.getAllByRole("link")).toHaveLength(5);
  });

  it("previews and collapses each status independently, retaining collapse state on remount", () => {
    seedAgents(7);
    seedAgents(8, "queued");
    const runtimes = [runtime(), runtime({ id: "rt-offline", status: "offline" })];
    const panel = renderPanel(runtimes);
    const working = within(screen.getByRole("region", { name: "Working now" }));
    const attention = within(screen.getByRole("region", { name: "Needs attention" }));

    expect(working.getAllByRole("link")).toHaveLength(5);
    expect(attention.getAllByRole("link")).toHaveLength(5);
    fireEvent.click(attention.getByRole("button", { name: "Show 3 more…" }));
    expect(attention.getAllByRole("link")).toHaveLength(8);
    expect(working.getAllByRole("link")).toHaveLength(5);

    fireEvent.click(working.getByRole("button", { name: "Working now" }));
    expect(working.queryAllByRole("link")).toHaveLength(0);
    expect(working.getByText("7")).toBeInTheDocument();
    expect(attention.getAllByRole("link")).toHaveLength(8);
    fireEvent.click(attention.getByRole("button", { name: "Needs attention" }));
    panel.unmount();
    renderPanel(runtimes);
    expect(screen.getByRole("button", { name: "Working now" })).toHaveAttribute("aria-expanded", "false");
    expect(screen.getByRole("button", { name: "Needs attention" })).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("running agent 1")).not.toBeInTheDocument();
    expect(screen.queryByText("queued agent 1")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Working now" }));
    expect(within(screen.getByRole("region", { name: "Working now" })).getAllByRole("link")).toHaveLength(5);
  });

  it("omits preview controls for lists at the limit and hides empty sections", () => {
    seedAgents(5);
    renderPanel([runtime()]);
    expect(screen.queryByRole("region", { name: "Needs attention" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Show/ })).not.toBeInTheDocument();
    expect(within(screen.getByRole("region", { name: "Working now" })).getAllByRole("link")).toHaveLength(5);
  });
});

describe("TeamPanel agent menu", () => {
  it("opens the agents page's row menu on right-click", async () => {
    seedAgents(1);
    renderPanel([runtime()]);

    fireEvent.contextMenu(screen.getByText("running agent 1"), { clientX: 20, clientY: 40 });
    const menu = await screen.findByRole("menu");
    expect(
      Array.from(menu.querySelectorAll('[role="menuitem"]'), (el) => el.textContent?.trim()),
    ).toEqual(["Open in new tab", "Settings", "Duplicate"]);
  });
});

describe("TeamPanel runtime badge", () => {
  it("shows a green online machine count without a total or duplicate providers", () => {
    renderPanel([
      ...Array.from({ length: 4 }, (_, index) => [
        runtime({ id: `online-${index}`, daemon_id: `daemon-${index}` }),
        runtime({ id: `online-codex-${index}`, daemon_id: `daemon-${index}`, provider: "codex" }),
      ]).flat(),
      ...Array.from({ length: 4 }, (_, index) =>
        runtime({
          id: `stale-${index}`,
          daemon_id: `daemon-${index}`,
          provider: "copilot",
          status: "offline",
        }),
      ),
      runtime({ id: "offline-claude", daemon_id: "daemon-offline", status: "offline" }),
      runtime({
        id: "offline-codex",
        daemon_id: "daemon-offline",
        provider: "codex",
        status: "offline",
      }),
    ]);

    const link = screen.getByRole("link", { name: /^Runtimes/ });
    expect(within(link).getByText("4 online")).toHaveClass("text-success");
    expect(link).not.toHaveTextContent(/offline|\//);
  });

  it("updates the count and warning when the last online provider disconnects and reconnects", async () => {
    const online = runtime();
    const stale = runtime({ id: "rt-stale", provider: "codex", status: "offline" });
    renderPanel([online, stale]);
    const link = screen.getByRole("link", { name: /^Runtimes/ });

    expect(within(link).getByText("1 online")).toHaveClass("text-success");

    act(() => {
      qc.setQueryData(runtimeListOptions(WS).queryKey, [{ ...online, status: "offline" }, stale]);
    });
    await waitFor(() => {
      expect(within(link).getByText("0 online")).toHaveClass("text-warning");
    });

    act(() => {
      qc.setQueryData(runtimeListOptions(WS).queryKey, [online, stale]);
    });
    await waitFor(() => {
      expect(within(link).getByText("1 online")).toHaveClass("text-success");
    });
  });

  it("does not count Qoder cloud services as machines", () => {
    renderPanel([
      runtime(),
      runtime({
        id: "rt-qoder-cloud",
        daemon_id: "qoder-daemon",
        provider: "qoder_cloud",
        runtime_mode: "cloud",
      }),
    ]);

    const link = screen.getByRole("link", { name: /^Runtimes/ });
    expect(within(link).getByText("1 online")).toHaveClass("text-success");
  });

  it("hides the badge when there are no machines", () => {
    renderPanel([]);

    expect(screen.getByRole("link", { name: "Runtimes" })).toBeInTheDocument();
    expect(screen.queryByText(/online|offline/)).not.toBeInTheDocument();
  });
});
