import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AgentRuntime, IssueEnvironment } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

let mockEnvironments: IssueEnvironment[] = [];
let mockRuntimes: AgentRuntime[] = [];
let mockLocalDaemonId: string | null = null;

vi.mock("@multica/core/issues/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/queries")>()),
  issueEnvironmentsOptions: (wsId: string, issueId: string) => ({
    queryKey: ["issues", wsId, "environments", issueId],
    queryFn: async () => ({ environments: mockEnvironments }),
  }),
}));
vi.mock("@multica/core/runtimes/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/runtimes/queries")>()),
  runtimeListOptions: (wsId: string) => ({
    queryKey: ["runtimes", wsId, "list"],
    queryFn: async () => mockRuntimes,
  }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (state: { user: { id: string } }) => unknown) => select({ user: { id: "user-1" } }),
}));
vi.mock("../../platform/use-local-daemon-status", () => ({
  useLocalDaemonStatus: () => ({ daemonId: mockLocalDaemonId, deviceName: null, running: !!mockLocalDaemonId }),
}));
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
    started_at: new Date(Date.now() - HOUR).toISOString(),
    expires_at: new Date(Date.now() + 23 * HOUR).toISOString(),
    last_seen_at: new Date().toISOString(),
    ended_at: null,
    ...overrides,
  };
}

function renderSection() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        <IssueEnvironmentsSection issueId="issue-1" />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("IssueEnvironmentsSection", () => {
  beforeEach(() => {
    mockEnvironments = [];
    mockRuntimes = [runtime({}), runtime({ id: "rt-2", daemon_id: "daemon-2", name: "Codex (build-02)", device_info: "build-02" })];
    mockLocalDaemonId = null;
    copyText.mockClear();
    toastError.mockClear();
    focusEnvironment.mockReset();
    delete (window as { daemonAPI?: unknown }).daemonAPI;
  });

  it("renders nothing while the issue has no environments", async () => {
    const { container } = renderSection();
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(container).toBeEmptyDOMElement();
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

  it("folds ended environments and shows why one exited", async () => {
    mockEnvironments = [
      environment({}),
      environment({
        id: "env-3",
        name: "api",
        label: "API",
        state: "exited",
        detail: "exit status 1",
        ended_at: new Date().toISOString(),
        expires_at: null,
      }),
    ];
    renderSection();

    const fold = await screen.findByRole("button", { name: /1 ended/ });
    expect(screen.queryByText("API")).not.toBeInTheDocument();
    fireEvent.click(fold);
    expect(screen.getByText("API")).toBeVisible();
    expect(screen.getByText("Exited")).toBeVisible();
    expect(screen.getByText("exit status 1")).toBeVisible();
    const ended = screen.getAllByTestId("environment-row")[1]!;
    expect(within(ended).queryByRole("link")).not.toBeInTheDocument();
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
});
