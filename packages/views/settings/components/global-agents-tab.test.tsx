// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type {
  GlobalAgent,
  GlobalAgentWorkspaceTarget,
} from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockApi = vi.hoisted(() => ({
  listGlobalAgents: vi.fn(),
  listGlobalAgentWorkspaces: vi.fn(),
  createGlobalAgent: vi.fn(),
  updateGlobalAgent: vi.fn(),
  deleteGlobalAgent: vi.fn(),
  enableGlobalAgentInWorkspace: vi.fn(),
  disableGlobalAgentInWorkspace: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({ api: mockApi }));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));
// The avatar control uploads through the workspace file pipeline and the
// starters editor has its own suite; neither is under test here.
vi.mock("../../common/avatar-upload-control", () => ({
  AvatarUploadControl: () => null,
}));
vi.mock("../../agents/components/conversation-starters-editor", () => ({
  ConversationStartersEditor: () => null,
}));

import { GlobalAgentsTab } from "./global-agents-tab";

function globalAgent(overrides: Partial<GlobalAgent> = {}): GlobalAgent {
  return {
    id: "ga-1",
    owner_id: "user-1",
    name: "Reviewer",
    description: "Reviews pull requests",
    instructions: "",
    avatar_url: null,
    conversation_starters: [],
    links: [
      {
        workspace_id: "ws-1",
        workspace_name: "Acme",
        workspace_slug: "acme",
        agent_id: "agent-1",
        archived: false,
        runtime_bound: true,
      },
    ],
    created_at: "2026-09-01T00:00:00Z",
    updated_at: "2026-09-01T00:00:00Z",
    ...overrides,
  };
}

const targets: GlobalAgentWorkspaceTarget[] = [
  {
    workspace_id: "ws-1",
    workspace_name: "Acme",
    workspace_slug: "acme",
    agent: { id: "agent-1", archived: false, runtime_id: "rt-1" },
    runtimes: [
      { id: "rt-1", name: "MacBook", provider: "claude", status: "online", owned_by_me: true },
    ],
    suggested_runtime_id: "rt-1",
  },
  {
    workspace_id: "ws-2",
    workspace_name: "Beta",
    workspace_slug: "beta",
    agent: null,
    runtimes: [
      { id: "rt-2", name: "Build box", provider: "codex", status: "online", owned_by_me: true },
    ],
    suggested_runtime_id: "rt-2",
  },
  {
    workspace_id: "ws-3",
    workspace_name: "Gamma",
    workspace_slug: "gamma",
    agent: { id: "agent-3", archived: true, runtime_id: "rt-3" },
    runtimes: [
      { id: "rt-3", name: "Studio", provider: "claude", status: "offline", owned_by_me: true },
    ],
    suggested_runtime_id: "rt-3",
  },
  {
    workspace_id: "ws-4",
    workspace_name: "Delta",
    workspace_slug: "delta",
    agent: null,
    runtimes: [],
    suggested_runtime_id: "",
  },
];

function renderTab() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <GlobalAgentsTab />
    </QueryClientProvider>,
  );
}

describe("GlobalAgentsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockApi.listGlobalAgents.mockResolvedValue([globalAgent()]);
    mockApi.listGlobalAgentWorkspaces.mockResolvedValue(targets);
    mockApi.enableGlobalAgentInWorkspace.mockResolvedValue({
      id: "agent-x",
      workspace_id: "ws-x",
    });
  });

  it("lists global agents with the workspaces they are enabled in", async () => {
    renderTab();

    expect(await screen.findByText("Reviewer")).toBeInTheDocument();
    expect(screen.getByText("Enabled in Acme")).toBeInTheDocument();
  });

  it("offers creation from the empty state", async () => {
    mockApi.listGlobalAgents.mockResolvedValue([]);
    renderTab();

    expect(await screen.findByText("No global agents yet.")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "New global agent" }),
    ).toBeInTheDocument();
  });

  it("creates a global agent and closes only after the server answers", async () => {
    const user = userEvent.setup();
    mockApi.listGlobalAgents.mockResolvedValue([]);
    mockApi.createGlobalAgent.mockResolvedValue(globalAgent({ id: "ga-2" }));
    renderTab();

    await user.click(
      await screen.findByRole("button", { name: "New global agent" }),
    );
    const dialog = await screen.findByRole("dialog");
    const create = within(dialog).getByRole("button", { name: "Create" });
    expect(create).toBeDisabled();

    await user.type(within(dialog).getByLabelText("Name"), "Reviewer");
    await user.click(create);

    await waitFor(() =>
      expect(mockApi.createGlobalAgent).toHaveBeenCalledWith({
        name: "Reviewer",
        description: "",
        instructions: "",
      }),
    );
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });

  it("keeps the dialog open with the input when creation fails", async () => {
    const user = userEvent.setup();
    mockApi.listGlobalAgents.mockResolvedValue([]);
    mockApi.createGlobalAgent.mockRejectedValue(
      new Error("you already have a global agent named \"Reviewer\""),
    );
    renderTab();

    await user.click(
      await screen.findByRole("button", { name: "New global agent" }),
    );
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText("Name"), "Reviewer");
    await user.click(within(dialog).getByRole("button", { name: "Create" }));

    await waitFor(() => expect(mockApi.createGlobalAgent).toHaveBeenCalled());
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Name")).toHaveValue("Reviewer");
  });

  it("shows each workspace's state and enables on the suggested runtime", async () => {
    const user = userEvent.setup();
    renderTab();

    await user.click(await screen.findByRole("button", { name: "Workspaces" }));
    const dialog = await screen.findByRole("dialog");

    const acme = (await within(dialog).findByText("Acme")).closest("li")!;
    expect(within(acme).getByText("Enabled")).toBeInTheDocument();
    expect(within(acme).getByRole("button", { name: "Disable" })).toBeInTheDocument();

    const gamma = within(dialog).getByText("Gamma").closest("li")!;
    expect(within(gamma).getByText("Disabled")).toBeInTheDocument();

    const delta = within(dialog).getByText("Delta").closest("li")!;
    expect(
      within(delta).getByText("No runtime you can use here yet."),
    ).toBeInTheDocument();

    const beta = within(dialog).getByText("Beta").closest("li")!;
    expect(within(beta).getByText("Not added")).toBeInTheDocument();
    await user.click(within(beta).getByRole("button", { name: "Enable" }));

    await waitFor(() =>
      expect(mockApi.enableGlobalAgentInWorkspace).toHaveBeenCalledWith("ga-1", {
        workspace_id: "ws-2",
        runtime_id: "rt-2",
      }),
    );
  });

  it("enables every workspace that has a runtime, one after another", async () => {
    const user = userEvent.setup();
    renderTab();

    await user.click(await screen.findByRole("button", { name: "Workspaces" }));
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByText("Beta");
    await user.click(
      within(dialog).getByRole("button", { name: "Enable in all workspaces" }),
    );

    await waitFor(() =>
      expect(mockApi.enableGlobalAgentInWorkspace).toHaveBeenCalledTimes(2),
    );
    expect(mockApi.enableGlobalAgentInWorkspace.mock.calls).toEqual([
      ["ga-1", { workspace_id: "ws-2", runtime_id: "rt-2" }],
      ["ga-1", { workspace_id: "ws-3", runtime_id: "rt-3" }],
    ]);
  });

  it("disables in a workspace", async () => {
    const user = userEvent.setup();
    mockApi.disableGlobalAgentInWorkspace.mockResolvedValue({
      id: "agent-1",
      workspace_id: "ws-1",
    });
    renderTab();

    await user.click(await screen.findByRole("button", { name: "Workspaces" }));
    const dialog = await screen.findByRole("dialog");
    const acme = (await within(dialog).findByText("Acme")).closest("li")!;
    await user.click(within(acme).getByRole("button", { name: "Disable" }));

    await waitFor(() =>
      expect(mockApi.disableGlobalAgentInWorkspace).toHaveBeenCalledWith(
        "ga-1",
        "ws-1",
      ),
    );
  });

  it("deletes after confirmation", async () => {
    const user = userEvent.setup();
    mockApi.deleteGlobalAgent.mockResolvedValue(undefined);
    renderTab();

    await user.click(
      await screen.findByRole("button", { name: "Actions for Reviewer" }),
    );
    await user.click(await screen.findByRole("menuitem", { name: "Delete" }));
    const confirm = await screen.findByRole("alertdialog");
    expect(
      within(confirm).getByText(/kept as regular agents/),
    ).toBeInTheDocument();
    await user.click(within(confirm).getByRole("button", { name: "Delete" }));

    await waitFor(() =>
      expect(mockApi.deleteGlobalAgent).toHaveBeenCalledWith("ga-1"),
    );
    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
  });
});
