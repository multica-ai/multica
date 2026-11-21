import { vi, it, expect, beforeEach, afterEach, describe } from "vitest";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { isManagedClaudeRuntime } from "@multica/core/runtimes/claude-managed";
import type { AgentRuntime } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enLayout from "../../locales/en/layout.json";
import enRuntimes from "../../locales/en/runtimes.json";
import {
  ClaudeManagedPage,
  isClaudeManagedLocator,
} from "./claude-managed-page";

const { ApiError, mocks, nav } = vi.hoisted(() => {
  class ApiError extends Error {
    status: number;
    body: unknown;
    constructor(message: string, status: number, body: unknown) {
      super(message);
      this.status = status;
      this.body = body;
    }
  }
  return {
    ApiError,
    mocks: {
      getClaudeConnection: vi.fn(),
      listMembers: vi.fn(),
      listRuntimes: vi.fn(),
      listAgents: vi.fn(),
      stopClaudeConnection: vi.fn(),
      saveClaudeConnection: vi.fn(),
      resumeClaudeConnection: vi.fn(),
      disconnectClaudeConnection: vi.fn(),
      listClaudeEnvironments: vi.fn(),
      checkClaudeConnection: vi.fn(),
    },
    nav: { push: vi.fn(), replace: vi.fn() },
  };
});
vi.mock("@multica/core/api", () => ({ api: mocks, ApiError }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));
vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "owner" } };
  return {
    useAuthStore: Object.assign(
      (selector: (s: typeof state) => unknown) => selector(state),
      { getState: () => state },
    ),
  };
});
vi.mock("@multica/core/agents", () => ({
  useWorkspacePresenceMap: () => ({ byAgent: new Map() }),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    runtimes: () => "/acme/runtimes",
    newAgentManual: () => "/acme/agents/new/manual",
    agentDetail: (id: string) => `/acme/agents/${id}`,
  }),
}));
vi.mock("../../navigation", () => ({
  useNavigation: () => nav,
  AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => (
    <a href={href}>{children}</a>
  ),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("./runtime-detail", () => ({
  ServingAgentList: ({ agents }: { agents: { id: string; name: string }[] }) => (
    <ul aria-label="serving agents">
      {agents.map((agent) => (
        <li key={agent.id}>{agent.name}</li>
      ))}
    </ul>
  ),
}));

const connection = {
  available: true,
  configured: true,
  name: "Claude Managed Agents",
  environmentId: "env_one",
  hasToken: true,
  repositories: ["https://github.com/acme/web"],
  vaultIds: ["vlt_one"],
  enabled: true,
  status: "online",
  lastError: "",
  maxConcurrency: 3,
  runTimeoutMinutes: 60,
  activeRuns: 1,
  queuedRuns: 0,
};

const managedRuntime = {
  id: "rt_claude",
  workspace_id: "ws-test",
  provider: "claude_managed",
  daemon_id: "claude-managed-ws-test",
  status: "online",
  visibility: "public",
};

beforeEach(() => {
  mocks.getClaudeConnection.mockResolvedValue(connection);
  mocks.listMembers.mockResolvedValue([{ user_id: "owner", role: "owner" }]);
  mocks.listRuntimes.mockResolvedValue([managedRuntime]);
  mocks.listAgents.mockResolvedValue([
    { id: "a1", name: "Reviewer", runtime_id: "rt_claude", archived_at: null },
    { id: "a2", name: "Local helper", runtime_id: "rt_local", archived_at: null },
  ]);
  mocks.listClaudeEnvironments.mockResolvedValue([
    { id: "env_one", name: "Web environment" },
    { id: "env_two", name: "Build environment" },
  ]);
  mocks.checkClaudeConnection.mockResolvedValue(undefined);
  mocks.saveClaudeConnection.mockResolvedValue(connection);
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <I18nProvider
      locale="en"
      resources={{ en: { common: enCommon, layout: enLayout, runtimes: enRuntimes } }}
    >
      <QueryClientProvider client={client}>
        <ClaudeManagedPage />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return client;
}

describe("Claude Managed Agents runtime locators", () => {
  const runtimes = [
    managedRuntime,
    {
      id: "rt_standalone",
      workspace_id: "ws-test",
      provider: "claude_managed",
      daemon_id: "bridge-on-my-server",
    },
    { id: "rt_local", workspace_id: "ws-test", provider: "claude", daemon_id: "laptop" },
  ] as AgentRuntime[];

  it("recognises only the runtime the server manages", () => {
    expect(isManagedClaudeRuntime(runtimes[0]!)).toBe(true);
    expect(isManagedClaudeRuntime(runtimes[1]!)).toBe(false);
    expect(isManagedClaudeRuntime(runtimes[2]!)).toBe(false);
  });

  it("accepts the page locator and the managed runtime, not a standalone bridge", () => {
    expect(isClaudeManagedLocator("claude-managed", runtimes)).toBe(true);
    expect(isClaudeManagedLocator("rt_claude", runtimes)).toBe(true);
    expect(isClaudeManagedLocator("rt_standalone", runtimes)).toBe(false);
    expect(isClaudeManagedLocator("rt_local", runtimes)).toBe(false);
    expect(isClaudeManagedLocator("local:daemon-1", runtimes)).toBe(false);
  });
});

it("shows the connection with its agents and no machine-level controls", async () => {
  const client = mount();
  expect(
    await screen.findByRole("heading", { name: "Claude Managed Agents" }),
  ).toBeInTheDocument();
  const serving = await screen.findByRole("list", { name: "serving agents" });
  expect(within(serving).getByText("Reviewer")).toBeInTheDocument();
  expect(within(serving).queryByText("Local helper")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Delete runtime" }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Create an agent" }));
  expect(nav.push).toHaveBeenCalledWith(
    "/acme/agents/new/manual?runtime=rt_claude",
  );
  client.clear();
});

it("explains when the server cannot run Claude Managed Agents", async () => {
  mocks.getClaudeConnection.mockResolvedValue({ ...connection, available: false });
  const client = mount();
  expect(
    await screen.findByText(
      "Set MULTICA_VCS_SECRET_KEY on the server to connect Claude Managed Agents.",
    ),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Create an agent" })).toBeNull();
  client.clear();
});

it("pauses the connection", async () => {
  mocks.stopClaudeConnection.mockResolvedValue({ ...connection, enabled: false });
  const client = mount();
  fireEvent.click(await screen.findByRole("button", { name: "Pause" }));
  await waitFor(() =>
    expect(mocks.stopClaudeConnection).toHaveBeenCalledWith("ws-test"),
  );
  client.clear();
});

it("resumes with the saved key and repository bindings", async () => {
  mocks.getClaudeConnection.mockResolvedValue({
    ...connection,
    enabled: false,
    status: "offline",
  });
  mocks.resumeClaudeConnection.mockResolvedValue(connection);
  const client = mount();
  fireEvent.click(await screen.findByRole("button", { name: "Resume" }));
  await waitFor(() => expect(mocks.resumeClaudeConnection).toHaveBeenCalledTimes(1));
  expect(mocks.saveClaudeConnection).not.toHaveBeenCalled();
  const input = mocks.resumeClaudeConnection.mock.calls[0]![1];
  expect(input).toMatchObject({
    environmentId: "env_one",
    anthropicToken: "",
    vaultIds: ["vlt_one"],
    maxConcurrency: 3,
    runTimeoutMinutes: 60,
  });
  expect(input).not.toHaveProperty("githubTokens");
  client.clear();
});

it("disconnects after confirming the agents to unbind", async () => {
  mocks.disconnectClaudeConnection.mockResolvedValue({
    ...connection,
    configured: false,
  });
  const client = mount();
  fireEvent.click(await screen.findByRole("button", { name: "Disconnect" }));
  const dialog = await screen.findByRole("alertdialog");
  expect(within(dialog).getByText("Reviewer")).toBeInTheDocument();
  expect(within(dialog).queryByText("Local helper")).not.toBeInTheDocument();
  expect(mocks.disconnectClaudeConnection).not.toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole("button", { name: "Disconnect" }));
  await waitFor(() =>
    expect(mocks.disconnectClaudeConnection).toHaveBeenCalledWith("ws-test", ["a1"]),
  );
  await waitFor(() => expect(nav.replace).toHaveBeenCalledWith("/acme/runtimes"));
  client.clear();
});

it("shows members the connection without management controls", async () => {
  mocks.listMembers.mockResolvedValue([{ user_id: "owner", role: "member" }]);
  const client = mount();
  expect(
    await screen.findByRole("heading", { name: "Claude Managed Agents" }),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Pause" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Disconnect" })).toBeNull();
  expect(screen.queryByLabelText("Anthropic API key")).toBeNull();
  client.clear();
});

describe("connection form", () => {
  it("connects with the only environment the new key can see", async () => {
    mocks.listClaudeEnvironments.mockResolvedValue([
      { id: "env_only", name: "Only environment" },
    ]);
    mocks.getClaudeConnection.mockResolvedValue({
      ...connection,
      configured: false,
      hasToken: false,
      environmentId: "",
      repositories: [],
      vaultIds: [],
    });
    const client = mount();
    const save = await screen.findByRole("button", { name: "Connect" });
    expect(save).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Anthropic API key"), {
      target: { value: "sk-ant-secret" },
    });
    await waitFor(
      () => expect(mocks.listClaudeEnvironments).toHaveBeenCalled(),
      { timeout: 3000 },
    );
    expect(mocks.listClaudeEnvironments.mock.calls[0]![1]).toBe("sk-ant-secret");
    await waitFor(() => expect(save).toBeEnabled());
    fireEvent.click(save);
    await waitFor(() => expect(mocks.saveClaudeConnection).toHaveBeenCalledTimes(1));
    expect(mocks.saveClaudeConnection.mock.calls[0]![1]).toMatchObject({
      environmentId: "env_only",
      anthropicToken: "sk-ant-secret",
      vaultIds: [],
    });
    client.clear();
  });

  it("tests the connection without saving it", async () => {
    const client = mount();
    const check = await screen.findByRole("button", { name: "Test connection" });
    await waitFor(() => expect(check).toBeEnabled());
    fireEvent.click(check);
    await waitFor(() => expect(mocks.checkClaudeConnection).toHaveBeenCalledTimes(1));
    expect(mocks.saveClaudeConnection).not.toHaveBeenCalled();
    expect(await screen.findByText("Connection works")).toBeInTheDocument();
    client.clear();
  });

  it("keeps the saved key when only the limits change", async () => {
    const client = mount();
    const concurrency = await screen.findByLabelText("Concurrent runs");
    fireEvent.change(concurrency, { target: { value: "5" } });
    const save = screen.getByRole("button", { name: "Save" });
    await waitFor(() => expect(save).toBeEnabled());
    fireEvent.click(save);
    await waitFor(() => expect(mocks.saveClaudeConnection).toHaveBeenCalledTimes(1));
    expect(mocks.saveClaudeConnection.mock.calls[0]![1]).toMatchObject({
      anthropicToken: "",
      maxConcurrency: 5,
      environmentId: "env_one",
    });
    client.clear();
  });

  it("splits vault IDs on commas", async () => {
    const client = mount();
    const vaults = await screen.findByLabelText("Vault IDs");
    fireEvent.change(vaults, { target: { value: "vlt_a, vlt_b ,," } });
    const save = screen.getByRole("button", { name: "Save" });
    await waitFor(() => expect(save).toBeEnabled());
    fireEvent.click(save);
    await waitFor(() => expect(mocks.saveClaudeConnection).toHaveBeenCalledTimes(1));
    expect(mocks.saveClaudeConnection.mock.calls[0]![1].vaultIds).toEqual([
      "vlt_a",
      "vlt_b",
    ]);
    client.clear();
  });

  it("requires a token for a repository it has not stored one for", async () => {
    const client = mount();
    fireEvent.click(await screen.findByRole("button", { name: "Add repository" }));
    const save = screen.getByRole("button", { name: "Save" });
    const urls = screen.getAllByLabelText("Repository URL");
    fireEvent.change(urls[urls.length - 1]!, {
      target: { value: "https://github.com/acme/new" },
    });
    expect(save).toBeDisabled();
    const tokens = screen.getAllByLabelText("Token");
    fireEvent.change(tokens[tokens.length - 1]!, { target: { value: "ghp_secret" } });
    await waitFor(() => expect(save).toBeEnabled());
    client.clear();
  });
});
