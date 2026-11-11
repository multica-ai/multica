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
import type { AgentRuntime } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enLayout from "../../locales/en/layout.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { isQoderLocator, QoderDetailPage } from "./qoder-detail-page";

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
      getQoderConnection: vi.fn(),
      listMembers: vi.fn(),
      listRuntimes: vi.fn(),
      listAgents: vi.fn(),
      stopQoderConnection: vi.fn(),
      saveQoderConnection: vi.fn(),
      resumeQoderConnection: vi.fn(),
      disconnectQoderConnection: vi.fn(),
      listQoderEnvironments: vi.fn(),
      checkQoderConnection: vi.fn(),
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
vi.mock("../../platform", () => ({ openExternal: vi.fn() }));
vi.mock("./qoder-configuration", () => ({
  QoderConfiguration: () => <div>configuration form</div>,
}));
vi.mock("./usage-section", () => ({ UsageSection: () => null }));
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
  name: "QCA",
  baseUrl: "https://api.qoder.com/api/v1/cloud",
  environmentId: "env_one",
  hasToken: true,
  repositories: ["https://github.com/acme/web"],
  enabled: true,
  status: "online",
  lastError: "",
  maxConcurrency: 3,
  runTimeoutMinutes: 60,
  activeRuns: 1,
  queuedRuns: 0,
};

beforeEach(() => {
  mocks.getQoderConnection.mockResolvedValue(connection);
  mocks.listMembers.mockResolvedValue([{ user_id: "owner", role: "owner" }]);
  mocks.listRuntimes.mockResolvedValue([
    { id: "rt_qoder", provider: "qoder_cloud", status: "online", visibility: "public" },
  ]);
  mocks.listAgents.mockResolvedValue([
    { id: "a1", name: "Reviewer", runtime_id: "rt_qoder", archived_at: null },
    { id: "a2", name: "Local helper", runtime_id: "rt_local", archived_at: null },
  ]);
  mocks.listQoderEnvironments.mockResolvedValue([
    { id: "env_one", name: "Web environment" },
    { id: "env_two", name: "Build environment" },
  ]);
  mocks.checkQoderConnection.mockResolvedValue(undefined);
  mocks.saveQoderConnection.mockResolvedValue(connection);
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
        <QoderDetailPage />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return client;
}

describe("isQoderLocator", () => {
  const runtimes = [
    { id: "rt_qoder", provider: "qoder_cloud" },
    { id: "rt_local", provider: "claude" },
  ] as AgentRuntime[];
  it("accepts the page locator, the managed machine id and a Qoder runtime id", () => {
    expect(isQoderLocator("qoder", runtimes)).toBe(true);
    expect(isQoderLocator("cloud:qoder-managed-ws-test", runtimes)).toBe(true);
    expect(isQoderLocator("rt_qoder", runtimes)).toBe(true);
    expect(isQoderLocator("rt_local", runtimes)).toBe(false);
    expect(isQoderLocator("local:daemon-1", runtimes)).toBe(false);
  });
});

it("shows the connection with its agents and no machine-level controls", async () => {
  const client = mount();
  expect(
    await screen.findByRole("heading", { name: "Qoder Cloud Agents" }),
  ).toBeInTheDocument();
  expect(await screen.findByText("Web environment")).toBeInTheDocument();
  expect(screen.getByText("acme/web")).toBeInTheDocument();
  expect(screen.queryByText("configuration form")).not.toBeInTheDocument();
  const serving = await screen.findByRole("list", { name: "serving agents" });
  expect(within(serving).getByText("Reviewer")).toBeInTheDocument();
  expect(within(serving).queryByText("Local helper")).not.toBeInTheDocument();
  expect(screen.getByText("1 of 3 running · 0 queued")).toBeInTheDocument();
  expect(screen.queryByText("Visibility")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Delete runtime" }),
  ).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Create an agent with it" }));
  expect(nav.push).toHaveBeenCalledWith(
    "/acme/agents/new/manual?runtime=rt_qoder",
  );
  client.clear();
});

it("asks before pausing and names the agents it strands", async () => {
  mocks.stopQoderConnection.mockResolvedValue({ ...connection, enabled: false });
  const client = mount();
  fireEvent.click(await screen.findByRole("button", { name: "Pause" }));
  const dialog = await screen.findByRole("alertdialog");
  expect(within(dialog).getByText("Pause Qoder Cloud Agents?")).toBeInTheDocument();
  expect(within(dialog).getByText("Reviewer")).toBeInTheDocument();
  expect(mocks.stopQoderConnection).not.toHaveBeenCalled();
  fireEvent.click(within(dialog).getByRole("button", { name: "Pause" }));
  await waitFor(() => expect(mocks.stopQoderConnection).toHaveBeenCalledWith("ws-test"));
  client.clear();
});

it("resumes with the saved token and repository bindings", async () => {
  mocks.getQoderConnection.mockResolvedValue({
    ...connection,
    enabled: false,
    status: "offline",
  });
  mocks.resumeQoderConnection.mockResolvedValue(connection);
  const client = mount();
  fireEvent.click(await screen.findByRole("button", { name: "Resume" }));
  await waitFor(() => expect(mocks.resumeQoderConnection).toHaveBeenCalledTimes(1));
  expect(mocks.saveQoderConnection).not.toHaveBeenCalled();
  const input = mocks.resumeQoderConnection.mock.calls[0]![1];
  expect(input).toMatchObject({
    environmentId: "env_one",
    qoderToken: "",
    maxConcurrency: 3,
    runTimeoutMinutes: 60,
  });
  expect(input).not.toHaveProperty("githubTokens");
  client.clear();
});

it("disconnects after confirming the latest agents to unbind", async () => {
  mocks.disconnectQoderConnection
    .mockRejectedValueOnce(
      new ApiError("plan changed", 409, {
        code: "runtime_delete_plan_changed",
        active_agents: [
          { id: "a1", name: "Reviewer" },
          { id: "a3", name: "Triage" },
        ],
      }),
    )
    .mockResolvedValueOnce({ ...connection, configured: false });
  const client = mount();
  fireEvent.click(await screen.findByRole("button", { name: "Disconnect" }));
  const dialog = await screen.findByRole("alertdialog");
  expect(within(dialog).getByText("Reviewer")).toBeInTheDocument();

  fireEvent.click(within(dialog).getByRole("button", { name: "Disconnect" }));
  expect(
    await within(dialog).findByText(
      "The agents on Qoder changed. Review the list and confirm again.",
    ),
  ).toBeInTheDocument();
  expect(within(dialog).getByText("Triage")).toBeInTheDocument();
  expect(mocks.disconnectQoderConnection).toHaveBeenLastCalledWith("ws-test", ["a1"]);
  expect(nav.replace).not.toHaveBeenCalled();

  fireEvent.click(within(dialog).getByRole("button", { name: "Disconnect" }));
  await waitFor(() =>
    expect(mocks.disconnectQoderConnection).toHaveBeenLastCalledWith("ws-test", [
      "a1",
      "a3",
    ]),
  );
  await waitFor(() => expect(nav.replace).toHaveBeenCalledWith("/acme/runtimes"));
  client.clear();
});

it("shows members the connection read-only", async () => {
  mocks.listMembers.mockResolvedValue([{ user_id: "owner", role: "member" }]);
  const client = mount();
  expect(await screen.findByText("acme/web")).toBeInTheDocument();
  await waitFor(() => expect(mocks.listMembers).toHaveBeenCalled());
  expect(screen.getByText("env_one")).toBeInTheDocument();
  expect(mocks.listQoderEnvironments).not.toHaveBeenCalled();
  for (const name of ["Disconnect", "Pause", "Edit", "Test connection", "Add repository"]) {
    expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
  }
  expect(
    screen.queryByRole("button", { name: "Actions for acme/web" }),
  ).not.toBeInTheDocument();
  client.clear();
});

it("connects through the form while Qoder is not configured", async () => {
  mocks.getQoderConnection.mockResolvedValue({
    ...connection,
    configured: false,
    hasToken: false,
    repositories: [],
  });
  const client = mount();
  expect(await screen.findByText("configuration form")).toBeInTheDocument();
  expect(screen.queryByText("Connection")).not.toBeInTheDocument();
  client.clear();
});

it("tests the saved connection without sending secrets", async () => {
  const client = mount();
  fireEvent.click(await screen.findByRole("button", { name: "Test connection" }));
  await waitFor(() => expect(mocks.checkQoderConnection).toHaveBeenCalledTimes(1));
  const input = mocks.checkQoderConnection.mock.calls[0]![1];
  expect(input).toMatchObject({ environmentId: "env_one", qoderToken: "" });
  expect(input).not.toHaveProperty("githubTokens");
  client.clear();
});

it("tests an edited connection before saving it", async () => {
  const client = mount();
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  const dialog = await screen.findByRole("dialog");
  const environment = within(dialog).getByRole("combobox", { name: "Environment" });
  await waitFor(() => expect(environment).toHaveTextContent("Web environment"));
  fireEvent.click(within(dialog).getByRole("button", { name: "Test and save" }));
  await waitFor(() => expect(mocks.saveQoderConnection).toHaveBeenCalledTimes(1));
  expect(mocks.checkQoderConnection).toHaveBeenCalledTimes(1);
  const input = mocks.saveQoderConnection.mock.calls[0]![1];
  expect(input).toMatchObject({
    name: "QCA",
    baseUrl: "https://api.qoder.com/api/v1/cloud",
    environmentId: "env_one",
    qoderToken: "",
  });
  expect(input).not.toHaveProperty("githubTokens");
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  client.clear();
});

it("asks for a new token when the region changes", async () => {
  const client = mount();
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  const dialog = await screen.findByRole("dialog");
  await waitFor(() =>
    expect(
      within(dialog).getByRole("combobox", { name: "Environment" }),
    ).toHaveTextContent("Web environment"),
  );
  fireEvent.click(within(dialog).getByRole("radio", { name: "China (qoder.com.cn)" }));
  expect(
    within(dialog).getByText("Environments load once the token is verified"),
  ).toBeInTheDocument();
  expect(within(dialog).getByRole("button", { name: "Test and save" })).toBeDisabled();
  client.clear();
});

it("adds a repository and keeps the saved tokens", async () => {
  const client = mount();
  fireEvent.click(await screen.findByRole("button", { name: "Add repository" }));
  const dialog = await screen.findByRole("dialog");
  fireEvent.change(within(dialog).getByLabelText("GitHub URL"), {
    target: { value: "https://github.com/acme/api" },
  });
  fireEvent.change(within(dialog).getByLabelText("GitHub token"), {
    target: { value: "ghp_new" },
  });
  fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(mocks.saveQoderConnection).toHaveBeenCalledTimes(1));
  expect(mocks.saveQoderConnection.mock.calls[0]![1]).toMatchObject({
    qoderToken: "",
    githubTokens: {
      "https://github.com/acme/web": "",
      "https://github.com/acme/api": "ghp_new",
    },
  });
  client.clear();
});

it("replaces the token of a saved repository", async () => {
  const client = mount();
  fireEvent.click(
    await screen.findByRole("button", { name: "Actions for acme/web" }),
  );
  fireEvent.click(await screen.findByText("Replace token"));
  const dialog = await screen.findByRole("dialog");
  expect(within(dialog).queryByLabelText("GitHub URL")).not.toBeInTheDocument();
  fireEvent.change(within(dialog).getByLabelText("GitHub token"), {
    target: { value: "ghp_rotated" },
  });
  fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(mocks.saveQoderConnection).toHaveBeenCalledTimes(1));
  expect(mocks.saveQoderConnection.mock.calls[0]![1].githubTokens).toEqual({
    "https://github.com/acme/web": "ghp_rotated",
  });
  client.clear();
});

it("removes a repository after confirming", async () => {
  mocks.getQoderConnection.mockResolvedValue({
    ...connection,
    repositories: ["https://github.com/acme/api", "https://github.com/acme/web"],
  });
  const client = mount();
  fireEvent.click(
    await screen.findByRole("button", { name: "Actions for acme/web" }),
  );
  fireEvent.click(await screen.findByText("Remove"));
  const dialog = await screen.findByRole("alertdialog");
  expect(within(dialog).getByText("Remove acme/web?")).toBeInTheDocument();
  fireEvent.click(within(dialog).getByRole("button", { name: "Remove" }));
  await waitFor(() => expect(mocks.saveQoderConnection).toHaveBeenCalledTimes(1));
  expect(mocks.saveQoderConnection.mock.calls[0]![1].githubTokens).toEqual({
    "https://github.com/acme/api": "",
  });
  client.clear();
});

it("edits the execution limits", async () => {
  const client = mount();
  fireEvent.click(
    await screen.findByRole("button", { name: "Edit execution settings" }),
  );
  const dialog = await screen.findByRole("dialog");
  fireEvent.change(within(dialog).getByLabelText(/Concurrent runs/), {
    target: { value: "5" },
  });
  fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));
  await waitFor(() => expect(mocks.saveQoderConnection).toHaveBeenCalledTimes(1));
  const input = mocks.saveQoderConnection.mock.calls[0]![1];
  expect(input).toMatchObject({ maxConcurrency: 5, runTimeoutMinutes: 60, qoderToken: "" });
  expect(input).not.toHaveProperty("githubTokens");
  client.clear();
});
