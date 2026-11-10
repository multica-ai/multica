import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type {
  Agent,
  AgentRuntime,
  AgentTask,
  IssueEnvironment,
  IssueEnvironmentActionRecord,
  IssueEnvironmentConfig,
} from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

let mockEnvironments: IssueEnvironment[] = [];
let mockConfigs: IssueEnvironmentConfig[] = [];
let mockPendingStarts: IssueEnvironmentActionRecord[] = [];
let mockRuntimes: AgentRuntime[] = [];
let mockTasks: AgentTask[] = [];
let mockAgents: Agent[] = [];
let mockLocalDaemonId: string | null = null;

vi.mock("@multica/core/issues/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/queries")>()),
  issueEnvironmentsOptions: (wsId: string, issueId: string) => ({
    queryKey: ["issues", wsId, "environments", issueId],
    queryFn: async () => ({ environments: mockEnvironments, configs: mockConfigs, pending_starts: mockPendingStarts }),
  }),
  issueTasksOptions: (issueId: string) => ({
    queryKey: ["issues", "tasks", issueId],
    queryFn: async () => mockTasks,
  }),
}));
vi.mock("@multica/core/runtimes/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/runtimes/queries")>()),
  runtimeListOptions: (wsId: string) => ({
    queryKey: ["runtimes", wsId, "list"],
    queryFn: async () => mockRuntimes,
  }),
}));
vi.mock("@multica/core/workspace/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/workspace/queries")>()),
  agentListOptions: (wsId: string) => ({
    queryKey: ["workspaces", wsId, "agents"],
    queryFn: async () => mockAgents,
  }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (type: string, id: string) =>
      type === "member" ? (id === "user-1" ? "Jiayuan" : "Unknown") : mockAgents.find((a) => a.id === id)?.name ?? "Unknown Agent",
  }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (state: { user: { id: string } }) => unknown) => select({ user: { id: "user-1" } }),
}));
vi.mock("../../platform/use-local-daemon-status", () => ({
  useLocalDaemonStatus: () => ({ daemonId: mockLocalDaemonId, deviceName: null, running: !!mockLocalDaemonId }),
}));
const control = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/issues/environments", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/environments")>()),
  useControlIssueEnvironment: () => ({ mutate: control }),
}));
const createComment = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/issues/mutations", () => ({
  useCreateComment: () => ({ mutate: createComment }),
}));
const apiMock = vi.hoisted(() => ({
  readIssueEnvironmentLogs: vi.fn(),
  getIssueEnvironmentCommand: vi.fn(),
}));
vi.mock("@multica/core/api", () => ({ api: apiMock }));
const copyText = vi.hoisted(() => vi.fn(async () => true));
vi.mock("@multica/ui/lib/clipboard", () => ({ copyText }));
const toastError = vi.hoisted(() => vi.fn());
vi.mock("sonner", () => ({ toast: { error: toastError, success: vi.fn() } }));
const focusEnvironment = vi.fn();

import { IssueEnvironmentsSection } from "./issue-environments-section";

const HOUR = 3_600_000;

function runtime(overrides: Partial<AgentRuntime>): AgentRuntime {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Claude (studio.local)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "studio.local · claude 1.0.0",
    metadata: {},
    owner_id: "user-1",
    visibility: "private",
    last_seen_at: new Date().toISOString(),
    created_at: "2026-11-10T00:00:00Z",
    updated_at: "2026-11-10T00:00:00Z",
    ...overrides,
  };
}

function environment(overrides: Partial<IssueEnvironment>): IssueEnvironment {
  return {
    id: "env-1",
    issue_id: "issue-1",
    task_id: "task-1",
    runtime_id: "rt-1",
    agent_id: "agent-1",
    local_key: "config:abc",
    source: "config",
    kind: "server",
    name: "web",
    label: "Web app",
    url: "http://localhost:41000",
    port: 41000,
    state: "running",
    detail: "",
    excerpt: "",
    started_at: new Date(Date.now() - HOUR).toISOString(),
    expires_at: new Date(Date.now() + 23 * HOUR).toISOString(),
    last_seen_at: new Date().toISOString(),
    ended_at: null,
    controllable: true,
    can_control: true,
    pending_action: null,
    last_action: null,
    origin: null,
    ...overrides,
  };
}

function action(overrides: Partial<IssueEnvironmentActionRecord>): IssueEnvironmentActionRecord {
  return {
    id: "cmd-1",
    action: "stop",
    status: "pending",
    error: "",
    environment_id: "env-1",
    runtime_id: "rt-1",
    repo: "app",
    name: "web",
    actor_type: "member",
    actor_id: "user-1",
    created_at: new Date().toISOString(),
    completed_at: null,
    ...overrides,
  };
}

function task(overrides: Partial<AgentTask>): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "rt-1",
    issue_id: "issue-1",
    status: "completed",
    priority: 0,
    dispatched_at: null,
    started_at: null,
    completed_at: null,
    result: null,
    error: null,
    created_at: new Date(Date.now() - 2 * HOUR).toISOString(),
    ...overrides,
  } as AgentTask;
}

const lambda = { id: "agent-1", name: "Lambda", archived_at: null } as Agent;

function renderSection(props: { assigneeType?: "agent" | "member" | null; assigneeId?: string | null } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        <IssueEnvironmentsSection issueId="issue-1" {...props} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

async function openRowMenu(row: HTMLElement) {
  fireEvent.click(within(row).getByRole("button", { name: "More actions" }));
  return screen.findByRole("menu");
}

describe("IssueEnvironmentsSection", () => {
  beforeEach(() => {
    mockEnvironments = [];
    mockConfigs = [];
    mockPendingStarts = [];
    mockRuntimes = [runtime({}), runtime({ id: "rt-2", daemon_id: "daemon-2", name: "Codex (build-02)", device_info: "build-02" })];
    mockTasks = [task({})];
    mockAgents = [lambda];
    mockLocalDaemonId = null;
    copyText.mockClear();
    toastError.mockClear();
    focusEnvironment.mockReset();
    delete (window as { daemonAPI?: unknown }).daemonAPI;
    control.mockReset();
    createComment.mockReset();
    apiMock.readIssueEnvironmentLogs.mockReset();
    apiMock.getIssueEnvironmentCommand.mockReset();
  });

  it("keeps the header and its start menu while the issue has no environments", async () => {
    mockTasks = [];
    renderSection();
    expect(await screen.findByText("Environments")).toBeVisible();
    expect(screen.queryByTestId("environment-row")).not.toBeInTheDocument();
  });

  it("groups live environments by machine and links the running ones", async () => {
    mockLocalDaemonId = "daemon-1";
    mockEnvironments = [
      environment({}),
      environment({ id: "env-2", runtime_id: "rt-2", source: "detected", name: "node", label: "", url: "http://localhost:5173", port: 5173, expires_at: null }),
    ];
    renderSection();

    expect(await screen.findByText("Web app")).toBeVisible();
    expect(screen.getByText("2 running")).toBeVisible();
    expect(screen.getByText("This machine")).toBeVisible();
    expect(screen.getByText("studio.local")).toBeVisible();
    expect(screen.getByText("build-02")).toBeVisible();

    const [local, remote] = screen.getAllByTestId("environment-row");
    expect(within(local!).getByText("localhost:41000")).toBeVisible();
    expect(within(local!).getByText("Expires in 23h")).toBeVisible();
    expect(within(local!).getAllByRole("link")[0]).toHaveAttribute("href", "http://localhost:41000");
    expect(within(remote!).getByText("localhost:5173")).toBeVisible();
    expect(remote).toHaveAttribute("title", expect.stringContaining("Runs on build-02"));
    expect(local).not.toHaveAttribute("title");

    fireEvent.click(within(local!).getByRole("button", { name: "Copy URL" }));
    expect(copyText).toHaveBeenCalledWith("http://localhost:41000");
  });

  it("tells a viewer who may be on another machine where each localhost URL opens", async () => {
    mockEnvironments = [
      environment({ runtime_id: "rt-2" }),
      environment({ id: "env-2", runtime_id: "rt-gone", name: "api", label: "API", url: "http://127.0.0.1:41010", port: 41010 }),
      environment({ id: "env-3", runtime_id: "rt-2", name: "docs", label: "Docs", url: "https://docs.example.com", port: 0 }),
    ];
    renderSection();

    await screen.findByText("Web app");
    const rows = screen.getAllByTestId("environment-row");
    const row = (label: string) => rows.find((r) => within(r).queryByText(label))!;
    expect(row("Web app")).toHaveAttribute("title", "Runs on build-02. A localhost URL opens only on that machine.");
    expect(row("API")).toHaveAttribute("title", "A localhost URL opens only on the machine running it.");
    expect(row("Docs")).not.toHaveAttribute("title");
  });

  it("keeps a crashed declared environment in view with its last lines", async () => {
    mockEnvironments = [
      environment({}),
      environment({
        id: "env-3",
        name: "api",
        label: "API",
        state: "exited",
        detail: "exit status 1",
        excerpt: "ERROR failed to connect to database\ndial tcp [::1]:5432: connect: connection refused",
        ended_at: new Date().toISOString(),
        expires_at: null,
      }),
    ];
    renderSection();

    const crashed = (await screen.findByText("API")).closest<HTMLElement>("[data-testid=environment-row]")!;
    expect(within(crashed).getByText("Exited")).toBeVisible();
    expect(within(crashed).getByText(/Exit code 1/)).toBeVisible();
    expect(within(crashed).getByText("ERROR failed to connect to database")).toBeVisible();
    expect(screen.queryByRole("button", { name: /ended/ })).not.toBeInTheDocument();

    fireEvent.click(within(crashed).getByRole("button", { name: "Restart" }));
    expect(control).toHaveBeenCalledWith({ action: "restart", environmentId: "env-3" }, expect.anything());
  });

  it("folds ended environments and says who stopped them", async () => {
    mockEnvironments = [
      environment({}),
      environment({
        id: "env-3",
        name: "storybook",
        label: "Storybook",
        state: "stopped",
        ended_at: new Date(Date.now() - 2 * HOUR).toISOString(),
        expires_at: null,
        last_action: action({ id: "cmd-9", environment_id: "env-3", status: "succeeded" }),
      }),
    ];
    renderSection();

    const fold = await screen.findByRole("button", { name: /1 ended/ });
    expect(screen.queryByText("Storybook")).not.toBeInTheDocument();
    fireEvent.click(fold);
    const ended = screen.getByText("Storybook").closest<HTMLElement>("[data-testid=environment-row]")!;
    expect(within(ended).getByText(/Stopped by Jiayuan/)).toBeVisible();
    expect(within(ended).queryByRole("link")).not.toBeInTheDocument();

    fireEvent.click(within(ended).getByRole("button", { name: "Restart" }));
    expect(control).toHaveBeenCalledWith({ action: "restart", environmentId: "env-3" }, expect.anything());
  });

  describe("desktop apps", () => {
    const app = (overrides: Partial<IssueEnvironment> = {}) =>
      environment({
        id: "env-app",
        local_key: "detected:app",
        source: "detected",
        kind: "app",
        name: "Multica Canary multica-501",
        label: "",
        url: "",
        port: null,
        detail: "Remote debugging on localhost:9724",
        expires_at: null,
        ...overrides,
      });
    const onDesktop = () => {
      mockLocalDaemonId = "daemon-1";
      (window as { daemonAPI?: unknown }).daemonAPI = { focusEnvironment };
    };

    it("switches to the app's window on the machine whose daemon found it", async () => {
      onDesktop();
      focusEnvironment.mockResolvedValue({ ok: true });
      mockEnvironments = [app()];
      renderSection();

      const row = await screen.findByTestId("environment-row");
      expect(within(row).getByText("Multica Canary multica-501")).toBeVisible();
      expect(within(row).queryByText(/localhost/)).not.toBeInTheDocument();
      expect(within(row).queryByRole("link")).not.toBeInTheDocument();
      expect(row).toHaveAttribute("title", expect.stringContaining("Remote debugging on localhost:9724"));

      fireEvent.click(within(row).getByText("Multica Canary multica-501"));
      fireEvent.click(within(row).getByRole("button", { name: "Switch to window" }));
      await waitFor(() => expect(focusEnvironment).toHaveBeenCalledTimes(2));
      expect(focusEnvironment).toHaveBeenCalledWith("detected:app");
      expect(toastError).not.toHaveBeenCalled();
    });

    it("offers no switch for an app whose key did not arrive", async () => {
      onDesktop();
      mockEnvironments = [app({ local_key: "" })];
      renderSection();

      const row = await screen.findByTestId("environment-row");
      expect(within(row).queryByRole("button")).not.toBeInTheDocument();
      fireEvent.click(within(row).getByText("Multica Canary multica-501"));
      expect(focusEnvironment).not.toHaveBeenCalled();
    });

    it("says so when the app has exited instead of opening anything", async () => {
      onDesktop();
      focusEnvironment.mockResolvedValue({ ok: false, reason: "gone" });
      mockEnvironments = [app()];
      renderSection();

      fireEvent.click(await screen.findByRole("button", { name: "Switch to window" }));
      await waitFor(() => expect(toastError).toHaveBeenCalledWith("Multica Canary multica-501 is no longer running"));
    });

    it("cannot be switched to from another machine", async () => {
      onDesktop();
      mockEnvironments = [app({ runtime_id: "rt-2" })];
      renderSection();

      const row = await screen.findByTestId("environment-row");
      expect(within(row).queryByRole("button")).not.toBeInTheDocument();
      expect(row).toHaveAttribute("title", expect.stringContaining("Runs on build-02. Switch to its window from Multica Desktop on that machine."));
    });

    it("cannot be switched to on web", async () => {
      mockEnvironments = [app(), app({ id: "env-app-2", runtime_id: "rt-gone", name: "Other App" })];
      renderSection();

      await screen.findByText("Multica Canary multica-501");
      const rows = screen.getAllByTestId("environment-row");
      for (const row of rows) {
        expect(within(row).queryByRole("button")).not.toBeInTheDocument();
        expect(within(row).queryByRole("link")).not.toBeInTheDocument();
      }
      const row = (name: string) => rows.find((r) => within(r).queryByText(name))!;
      expect(row("Multica Canary multica-501")).toHaveAttribute("title", expect.stringContaining("Runs on studio.local. Switch to its window from Multica Desktop on that machine."));
      expect(row("Other App")).toHaveAttribute("title", expect.stringContaining("Switch to its window from Multica Desktop on the machine running it."));
      expect(focusEnvironment).not.toHaveBeenCalled();
    });

    it("cannot be switched to once it has ended", async () => {
      onDesktop();
      mockEnvironments = [environment({}), app({ state: "stopped", ended_at: new Date().toISOString() })];
      renderSection();

      fireEvent.click(await screen.findByRole("button", { name: /1 ended/ }));
      const ended = screen.getAllByTestId("environment-row")[1]!;
      expect(within(ended).getByText("Multica Canary multica-501")).toBeVisible();
      expect(within(ended).queryByRole("button")).not.toBeInTheDocument();
    });
  });

  it("offers no link for an environment whose daemon went quiet", async () => {
    mockEnvironments = [environment({ state: "unreachable" })];
    renderSection();
    const row = await screen.findByTestId("environment-row");
    expect(within(row).getByText("Not responding")).toBeVisible();
    expect(within(row).queryByRole("link")).not.toBeInTheDocument();
    expect(within(row).getByText(/Last seen/)).toBeVisible();
  });

  it("stops a declared environment from its menu and shows it pending", async () => {
    mockEnvironments = [environment({})];
    const { rerender } = renderSection();
    const row = await screen.findByTestId("environment-row");
    const menu = await openRowMenu(row);
    expect(within(menu).getByText("Started by Lambda in the first run")).toBeInTheDocument();
    fireEvent.click(within(menu).getByRole("menuitem", { name: "Stop" }));
    expect(control).toHaveBeenCalledWith({ action: "stop", environmentId: "env-1" }, expect.anything());

    mockEnvironments = [environment({ pending_action: action({}) })];
    rerender(<></>);
    renderSection();
    const pending = await screen.findByText("Stopping");
    const pendingRow = pending.closest<HTMLElement>("[data-testid=environment-row]")!;
    expect(within(pendingRow).getByText("Waiting for the machine")).toBeVisible();
    expect(within(pendingRow).queryByRole("link")).not.toBeInTheDocument();
  });

  it("says when the machine never answered", async () => {
    mockEnvironments = [environment({ last_action: action({ status: "expired" }) })];
    renderSection();
    const row = await screen.findByTestId("environment-row");
    expect(within(row).getByText("No response")).toBeVisible();
  });

  it("asks before stopping a detected service and offers no logs for it", async () => {
    mockEnvironments = [
      environment({ source: "detected", name: "vite", label: "", url: "http://localhost:5173", port: 5173, expires_at: null }),
    ];
    renderSection();
    const menu = await openRowMenu(await screen.findByTestId("environment-row"));
    expect(within(menu).getByText("Detected service · vite")).toBeInTheDocument();
    expect(within(menu).getByRole("menuitem", { name: "View logs" })).toHaveAttribute("aria-disabled", "true");
    fireEvent.click(within(menu).getByRole("menuitem", { name: "Stop…" }));

    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText("Stop localhost:5173?")).toBeVisible();
    expect(control).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole("button", { name: "Stop" }));
    expect(control).toHaveBeenCalledWith({ action: "stop", environmentId: "env-1" }, expect.anything());
  });

  it("disables every action while the machine is offline", async () => {
    mockRuntimes = [runtime({ status: "offline" })];
    mockEnvironments = [environment({ state: "unreachable" })];
    renderSection();
    await screen.findByText("Offline");
    const menu = await openRowMenu(await screen.findByTestId("environment-row"));
    expect(within(menu).getByText("Available once this machine is back online.")).toBeInTheDocument();
    for (const name of ["View logs", "Restart", "Stop"]) {
      expect(within(menu).getByRole("menuitem", { name })).toHaveAttribute("aria-disabled", "true");
    }
  });

  it("leaves a member who cannot use the machine only the logs", async () => {
    mockEnvironments = [environment({ can_control: false })];
    renderSection();
    const menu = await openRowMenu(await screen.findByTestId("environment-row"));
    expect(within(menu).getByRole("menuitem", { name: "View logs" })).not.toHaveAttribute("aria-disabled");
    expect(within(menu).queryByRole("menuitem", { name: "Restart" })).not.toBeInTheDocument();
    expect(within(menu).queryByRole("menuitem", { name: "Stop" })).not.toBeInTheDocument();
    expect(within(menu).getByText("Only members who can use this machine can stop or restart it.")).toBeInTheDocument();
  });

  it("reads the log from the machine and marks errors", async () => {
    mockEnvironments = [environment({})];
    apiMock.readIssueEnvironmentLogs.mockResolvedValue({ id: "log-1", status: "pending" });
    apiMock.getIssueEnvironmentCommand.mockResolvedValue({
      id: "log-1",
      action: "logs",
      status: "succeeded",
      completed_at: new Date().toISOString(),
      output: "> make server\nWARN database not ready\nERROR failed to connect\n    err=\"dial tcp\"\ndone\n",
    });
    renderSection();
    const menu = await openRowMenu(await screen.findByTestId("environment-row"));
    fireEvent.click(within(menu).getByRole("menuitem", { name: "View logs" }));

    const log = await screen.findByTestId("environment-log");
    await waitFor(() => expect(within(log).getByText("ERROR failed to connect")).toBeInTheDocument());
    expect(apiMock.readIssueEnvironmentLogs).toHaveBeenCalledWith("issue-1", "env-1");
    const levelOf = (text: string) => within(log).getByText(text).closest("[data-level]")?.getAttribute("data-level") ?? null;
    expect(levelOf("ERROR failed to connect")).toBe("error");
    expect(levelOf("err=\"dial tcp\"", )).toBe("error");
    expect(levelOf("WARN database not ready")).toBe("warn");
    expect(levelOf("done")).toBeNull();
    expect(screen.getByText(/Showing the last 5 lines/)).toBeInTheDocument();

    fireEvent.change(screen.getByRole("textbox", { name: "Filter logs" }), { target: { value: "warn" } });
    expect(within(log).queryByText("done")).not.toBeInTheDocument();
    expect(within(log).getByText("WARN database not ready")).toBeInTheDocument();
  });

  it("starts a declared environment from the + menu", async () => {
    mockConfigs = [{
      runtime_id: "rt-1",
      task_id: "task-1",
      reported_at: new Date().toISOString(),
      can_control: true,
      environments: [
        { repo: "app", name: "web", label: "Web app", start: "pnpm dev:web" },
        { repo: "app", name: "api", label: "API", start: "make server" },
      ],
    }];
    mockEnvironments = [environment({})];
    renderSection();
    await screen.findByTestId("environment-row");
    fireEvent.click(screen.getByRole("button", { name: "Start an environment" }));
    const menu = await screen.findByRole("menu");
    expect(within(menu).getByText("studio.local · the first run")).toBeInTheDocument();
    expect(within(menu).getByRole("menuitem", { name: /Web app/ })).toHaveAttribute("aria-disabled", "true");
    expect(within(menu).getByText("make server")).toBeInTheDocument();
    fireEvent.click(within(menu).getByRole("menuitem", { name: /API/ }));
    expect(control).toHaveBeenCalledWith(
      { action: "start", body: { task_id: "task-1", repo: "app", name: "api" } },
      expect.anything(),
    );
  });

  it("asks the agent to start it when nothing can be started directly", async () => {
    mockTasks = [];
    renderSection({ assigneeType: "agent", assigneeId: "agent-1" });
    fireEvent.click(await screen.findByRole("button", { name: "Start an environment" }));
    const menu = await screen.findByRole("menu");
    expect(within(menu).getByText(/hasn't run yet/)).toBeInTheDocument();
    fireEvent.click(within(menu).getByRole("menuitem", { name: /Ask Lambda to start it/ }));
    expect(createComment).toHaveBeenCalledTimes(1);
    const [{ content }] = createComment.mock.calls[0]!;
    expect(content).toMatch(/^\[@Lambda\]\(mention:\/\/agent\/agent-1\) /);
    expect(content).toContain("multica env start");
  });

  it("shows the agent preparing the environment while its run is under way", async () => {
    mockTasks = [
      task({}),
      task({ id: "task-2", status: "running", created_at: new Date().toISOString(), trigger_summary: "@Lambda Start the environment: get this issue's code ready, then run multica env start" }),
    ];
    renderSection();
    expect(await screen.findByText("Lambda is preparing the environment")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Start an environment" }));
    const menu = await screen.findByRole("menu");
    expect(within(menu).getByRole("menuitem", { name: /Ask Lambda to start it/ })).toHaveAttribute("aria-disabled", "true");
  });
});
