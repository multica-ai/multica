// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { renderWithI18n } from "../../test/i18n";

const mockApi = vi.hoisted(() => ({
  makeAgentGlobal: vi.fn(),
  archiveAgent: vi.fn(),
  restoreAgent: vi.fn(),
  cancelAgentTasks: vi.fn(),
}));
const mockToast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));

vi.mock("@multica/core/api", () => ({ api: mockApi }));
vi.mock("sonner", () => ({ toast: mockToast }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    agentDetail: (id: string) => `/acme/agents/${id}`,
  }),
}));
vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "user-1" } };
  const useAuthStore = Object.assign(
    (selector?: (s: typeof state) => unknown) =>
      selector ? selector(state) : state,
    { getState: () => state },
  );
  return { useAuthStore };
});

import { AgentRowActions } from "./agent-row-actions";

const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Lambda",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_args: [],
  visibility: "private",
  permission_mode: "private",
  invocation_targets: [],
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-05-28T00:00:00Z",
  updated_at: "2026-05-28T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function renderActions(agent: Agent) {
  const navigation: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/agents",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
  };
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return renderWithI18n(
    <NavigationProvider value={navigation}>
      <QueryClientProvider client={queryClient}>
        <AgentRowActions
          agent={agent}
          presence={null}
          canManage
          duplicateHref="/acme/agents/new?from=agent-1"
        />
      </QueryClientProvider>
    </NavigationProvider>,
  );
}

async function openMenu() {
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Row actions" }));
  return user;
}

describe("AgentRowActions Make global", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("lets the owner turn their agent into a global agent", async () => {
    mockApi.makeAgentGlobal.mockResolvedValue({ id: "ga-1" });
    renderActions(baseAgent);

    const user = await openMenu();
    await user.click(await screen.findByRole("menuitem", { name: "Make global" }));

    await waitFor(() =>
      expect(mockApi.makeAgentGlobal).toHaveBeenCalledWith("agent-1"),
    );
    await waitFor(() => expect(mockToast.success).toHaveBeenCalled());
  });

  it.each([
    ["someone else's agent", { owner_id: "user-2" }],
    ["an agent that is already linked", { global_agent_id: "ga-1" }],
    ["an archived agent", { archived_at: "2026-06-01T00:00:00Z" }],
    ["a built-in agent", { system_key: "mika" }],
  ])("is not offered for %s", async (_label, overrides) => {
    renderActions({ ...baseAgent, ...overrides });

    await openMenu();
    await screen.findByRole("menuitem", { name: "Open in new tab" });
    expect(
      screen.queryByRole("menuitem", { name: "Make global" }),
    ).not.toBeInTheDocument();
  });
});
