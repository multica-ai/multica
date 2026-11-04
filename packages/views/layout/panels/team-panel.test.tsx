import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { api } from "@multica/core/api";
import { createAuthStore, registerAuthStore } from "@multica/core/auth";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import type { AgentRuntime } from "@multica/core/types";
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
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <WorkspaceSlugProvider slug="acme">
        <NavigationProvider value={adapter}>
          <TeamPanel />
        </NavigationProvider>
      </WorkspaceSlugProvider>
    </QueryClientProvider>,
  );
}

describe("TeamPanel runtime badge", () => {
  it("counts one offline machine when six provider runtimes are offline across five machines", () => {
    renderPanel([
      ...Array.from({ length: 4 }, (_, index) =>
        runtime({ id: `online-${index}`, daemon_id: `daemon-${index}` }),
      ),
      ...Array.from({ length: 4 }, (_, index) =>
        runtime({
          id: `stale-${index}`,
          daemon_id: `daemon-${index}`,
          provider: "codex",
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
    expect(within(link).getByText("1 offline")).toBeInTheDocument();
  });

  it("only warns after the last online runtime on a machine goes offline, and clears on reconnect", async () => {
    const online = runtime();
    const stale = runtime({ id: "rt-stale", provider: "codex", status: "offline" });
    renderPanel([online, stale]);
    const link = screen.getByRole("link", { name: /^Runtimes/ });

    expect(within(link).queryByText(/offline/)).not.toBeInTheDocument();

    act(() => {
      qc.setQueryData(runtimeListOptions(WS).queryKey, [{ ...online, status: "offline" }, stale]);
    });
    await waitFor(() => {
      expect(within(link).getByText("1 offline")).toBeInTheDocument();
    });

    act(() => {
      qc.setQueryData(runtimeListOptions(WS).queryKey, [online, stale]);
    });
    await waitFor(() => {
      expect(within(link).queryByText(/offline/)).not.toBeInTheDocument();
    });
  });
});
