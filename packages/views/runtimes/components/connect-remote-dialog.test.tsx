import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { configStore } from "@multica/core/config";
import type { DaemonConfigStatus, DaemonSetupTarget } from "@multica/core/runtimes";
import { runtimeKeys } from "@multica/core/runtimes/queries";
import type { AgentRuntime } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { ConnectRemoteDialog } from "./connect-remote-dialog";

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };

const WS_ID = "11111111-1111-4111-8111-111111111111";
const USER_ID = "user-test";
const workspaceState = vi.hoisted(() => ({
  id: "11111111-1111-4111-8111-111111111111",
  slug: "workspace-test",
}));
const authState = vi.hoisted(() => ({ user: { id: "user-test" } as { id: string } | null }));
const runtimeApi = vi.hoisted(() => ({ listRuntimes: vi.fn<() => Promise<AgentRuntime[]>>() }));
vi.mock("@multica/core/api", () => ({ api: runtimeApi }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (select: (state: typeof authState) => unknown) => select(authState),
    { getState: () => authState },
  ),
}));

// Keep connection assertions independent of exit animation timing.
vi.mock("motion/react", async (importOriginal) => ({
  ...await importOriginal<typeof import("motion/react")>(),
  AnimatePresence: ({ children }: { children: ReactNode }) => children,
  motion: {
    div: ({ children, className }: { children: ReactNode; className?: string }) => <div className={className}>{children}</div>,
  },
}));


// Mocked at the module boundary rather than through navigator.clipboard: jsdom
// exposes no clipboard, and user-event installs a getter-only stub of its own
// that would swallow the assertion. Assert what the button hands to copyText,
// not what the browser then does with it.
const clipboard = vi.hoisted(() => ({
  copyText: vi.fn<(text: string) => Promise<boolean>>(),
}));

vi.mock("@multica/ui/lib/clipboard", () => ({ copyText: clipboard.copyText }));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => workspaceState.id,
}));

vi.mock("@multica/core/paths", () => ({
  paths: {
    workspace: () => ({
      agents: () => "/agents",
      runtimeDetail: (id: string) => `/runtimes/${id}`,
    }),
  },
  useWorkspaceSlug: () => workspaceState.slug,
}));

const wsEventState = vi.hoisted(() => ({
  handler: null as ((payload: unknown) => void) | null,
  reconnect: null as (() => void) | null,
}));

const WINDOWS_CMD =
  "irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex";

vi.mock("@multica/core/realtime", () => ({
  useWSEvent: (_event: string, handler: (payload: unknown) => void) => {
    wsEventState.handler = handler;
  },
  useWSReconnect: (handler: () => void) => { wsEventState.reconnect = handler; },
}));

const navigationState = vi.hoisted(() => ({ push: vi.fn() }));
vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: navigationState.push }),
}));

function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "rt-new",
    workspace_id: WS_ID,
    daemon_id: "daemon-new",
    name: "Claude (new-machine)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "new-machine",
    metadata: {},
    owner_id: USER_ID,
    visibility: "private",
    last_seen_at: null,
    created_at: "2026-10-29T00:00:00Z",
    updated_at: "2026-10-29T00:00:00Z",
    ...overrides,
  };
}

async function refreshRuntimes(qc: QueryClient, payload: unknown) {
  const calls = runtimeApi.listRuntimes.mock.calls.length;
  await act(async () => {
    await wsEventState.handler?.(payload);
  });
  await waitFor(() => expect(runtimeApi.listRuntimes.mock.calls.length).toBeGreaterThan(calls));
  await waitFor(() => expect(qc.isFetching()).toBe(0));
}

function resetConfigStore() {
  configStore.setState({
    cdnDomain: "",
    allowSignup: true,
    googleClientId: "",
    daemonServerUrl: "",
    daemonAppUrl: "",
    daemonConfigStatus: "loading",
    daemonSetupTarget: "unknown",
    workspaceCreationDisabled: false,
  });
}

function renderDialog(config?: {
  daemonServerUrl?: string;
  daemonAppUrl?: string;
  daemonSetupTarget?: DaemonSetupTarget;
}, status: DaemonConfigStatus = "ready", qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  resetConfigStore();
  configStore.getState().setDaemonConfig(config ?? { daemonSetupTarget: "cloud" });
  configStore.getState().setDaemonConfigStatus(status);
  const dialog = () => (
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ConnectRemoteDialog onClose={vi.fn()} />
      </I18nProvider>
    </QueryClientProvider>
  );
  const view = render(dialog());
  return { ...view, qc, rerenderDialog: () => view.rerender(dialog()) };
}

async function renderListeningDialog(runtimes: AgentRuntime[] = []) {
  runtimeApi.listRuntimes.mockResolvedValue(runtimes);
  const view = renderDialog();
  await waitFor(() => expect(view.qc.getQueryData(runtimeKeys.listMine(WS_ID))).toEqual(runtimes));
  return view;
}

describe("ConnectRemoteDialog", () => {
  beforeEach(() => {
    workspaceState.id = WS_ID;
    workspaceState.slug = "workspace-test";
    authState.user = { id: USER_ID };
    runtimeApi.listRuntimes.mockReset().mockResolvedValue([]);
    navigationState.push.mockReset();
    wsEventState.reconnect = null;
    wsEventState.handler = null;
    clipboard.copyText.mockReset().mockResolvedValue(true);
  });

  it("uses cloud setup commands by default", () => {
    const { baseElement } = renderDialog();

    expect(baseElement).toHaveTextContent("multica setup");
    expect(baseElement).not.toHaveTextContent("multica setup self-host");
    expect(baseElement).toHaveTextContent(
      "multica config set server_url https://api.multica.ai",
    );
    expect(baseElement).toHaveTextContent(
      "multica config set app_url https://multica.ai",
    );
  });

  it("uses self-host daemon URLs from runtime config", () => {
    const { baseElement } = renderDialog({
      daemonSetupTarget: "self_host",
      daemonServerUrl: "https://api.example.com/",
      daemonAppUrl: "https://app.example.com/",
    });

    expect(baseElement).toHaveTextContent(
      "multica setup self-host --server-url https://api.example.com --app-url https://app.example.com",
    );
    expect(baseElement).toHaveTextContent(
      "multica config set server_url https://api.example.com",
    );
    expect(baseElement).toHaveTextContent(
      "multica config set app_url https://app.example.com",
    );
  });

  it("withholds setup and token commands until config recovers", () => {
    const { baseElement } = renderDialog({}, "loading");
    expect(screen.getByRole("status")).toHaveTextContent(
      "Loading connection settings",
    );
    expect(baseElement).not.toHaveTextContent("multica setup");
    expect(baseElement).not.toHaveTextContent("multica config set");
    act(() => configStore.getState().setDaemonConfigStatus("error"));
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Couldn't load connection settings",
    );
    expect(baseElement).not.toHaveTextContent("multica setup");
    expect(baseElement).not.toHaveTextContent("multica config set");
    act(() => configStore.getState().setDaemonConfig({
      daemonSetupTarget: "self_host",
      daemonServerUrl: "https://api.example.com",
      daemonAppUrl: "https://app.example.com",
    }));
    expect(baseElement).toHaveTextContent(
      "multica setup self-host --server-url https://api.example.com --app-url https://app.example.com",
    );
    expect(baseElement).toHaveTextContent(
      "multica config set server_url https://api.example.com",
    );
  });

  it.each([
    { daemonSetupTarget: "unknown" as const },
    { daemonSetupTarget: "self_host" as const, daemonServerUrl: "https://api.example.com" },
  ])("withholds both command paths for unresolved config %j", (config) => {
    const { baseElement } = renderDialog(config);
    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(baseElement).not.toHaveTextContent("multica setup");
    expect(baseElement).not.toHaveTextContent("multica config set");
    expect(screen.getAllByRole("button", { name: "Copy" })).toHaveLength(1);
  });

  // The install command is OS-specific, so the dialog can't hardcode one.
  // Before this switch existed the dialog shipped only the curl command and
  // Windows users had no path at all, despite scripts/install.ps1. The switch
  // itself is covered in common/cli-install-command.test.tsx; this checks it
  // is wired through to step 1's copy button.
  it("copies the installer for the platform picked in step 1", async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(screen.getByRole("tab", { name: "Windows" }));
    await user.click(screen.getAllByRole("button", { name: "Copy" })[0]!);

    await waitFor(() => {
      expect(clipboard.copyText).toHaveBeenCalledWith(WINDOWS_CMD);
    });
  });

  it("confirms a newly registered own machine and opens its verified runtime", async () => {
    const user = userEvent.setup();
    const { baseElement, qc } = await renderListeningDialog();

    expect(baseElement).toHaveTextContent("multica setup");
    const runtime = makeRuntime();
    runtimeApi.listRuntimes.mockResolvedValue([runtime]);
    await refreshRuntimes(qc, { runtimes: [runtime] });

    await waitFor(() => {
      expect(screen.getByText("Machine connected")).toBeInTheDocument();
      expect(
        screen.getByRole("button", { name: "Create an agent" }),
      ).toBeInTheDocument();
    });
    expect(baseElement).not.toHaveTextContent("multica setup");
    expect(runtimeApi.listRuntimes).toHaveBeenCalledWith({ workspace_id: WS_ID, owner: "me" }, "workspace-test");
    await user.click(screen.getByRole("button", { name: "View runtime" }));
    expect(navigationState.push).toHaveBeenCalledWith("/runtimes/rt-new");
  });

  it.each([
    { action: "update" },
    { action: "deregister" },
    { action: "stale_sweep" },
    { action: "heartbeat_recovery" },
    { runtime_profile_id: "existing-profile" },
    { runtime_id: "unverified-runtime" },
    { runtimes: [makeRuntime()] },
    null,
  ])("keeps instructions for an event without a new verified machine: %j", async (payload) => {
    const { qc } = await renderListeningDialog();
    await refreshRuntimes(qc, payload);
    expect(screen.queryByText("Machine connected")).not.toBeInTheDocument();
    expect(screen.getByText("multica setup")).toBeInTheDocument();
  });

  it("does not report success from an offline notification alone", async () => {
    renderDialog();
    await act(async () => { wsEventState.handler?.({ action: "stale_sweep" }); });
    expect(screen.queryByText("Machine connected")).not.toBeInTheDocument();
  });

  it("does not treat an existing machine's recovery or new provider as a new connection", async () => {
    const runtime = makeRuntime({ status: "offline" });
    const { qc } = await renderListeningDialog([runtime]);
    runtimeApi.listRuntimes.mockResolvedValue([
      { ...runtime, status: "online" },
      makeRuntime({ id: "rt-another-provider", provider: "codex" }),
    ]);
    await refreshRuntimes(qc, { runtimes: [runtime] });
    expect(screen.queryByText("Machine connected")).not.toBeInTheDocument();
  });

  it.each([
    { owner_id: "another-member" },
    { owner_id: null },
    { runtime_mode: "cloud" as const },
    { status: "offline" as const },
    { daemon_id: null },
    { workspace_id: "another-workspace" },
    { metadata: { pending_custom_runtime: true } },
    { metadata: { runtime_profile_registration_error: true } },
  ])("does not complete for an ineligible runtime: %j", async (overrides) => {
    const { qc } = await renderListeningDialog();
    const runtime = makeRuntime(overrides);
    runtimeApi.listRuntimes.mockResolvedValue([runtime]);
    await refreshRuntimes(qc, { runtimes: [runtime] });
    expect(screen.queryByText("Machine connected")).not.toBeInTheDocument();
  });

  it("refreshes after reconnect to detect a registration whose event was missed", async () => {
    const { qc } = await renderListeningDialog();
    runtimeApi.listRuntimes.mockResolvedValue([makeRuntime()]);
    await act(async () => {
      await wsEventState.reconnect?.();
    });
    await waitFor(() => expect(qc.isFetching()).toBe(0));
    expect(await screen.findByText("Machine connected")).toBeInTheDocument();
  });

  it.each(["registration", "reconnect"] as const)(
    "replays %s notifications received during the uncached initial fetch",
    async (event) => {
      let finishInitialFetch!: (runtimes: AgentRuntime[]) => void;
      let finishRefresh!: (runtimes: AgentRuntime[]) => void;
      runtimeApi.listRuntimes
        .mockImplementationOnce(() => new Promise((resolve) => { finishInitialFetch = resolve; }))
        .mockImplementationOnce(() => new Promise((resolve) => { finishRefresh = resolve; }));
      const { qc } = renderDialog();
      await waitFor(() => expect(runtimeApi.listRuntimes).toHaveBeenCalledTimes(1));
      expect(qc.getQueryData(runtimeKeys.listMine(WS_ID))).toBeUndefined();

      await act(async () => {
        for (let i = 0; i < 3; i++) {
          if (event === "registration") wsEventState.handler?.({ action: "update" });
          else wsEventState.reconnect?.();
        }
      });
      expect(runtimeApi.listRuntimes).toHaveBeenCalledTimes(1);

      // The in-flight request captured the list before the machine registered.
      await act(async () => { finishInitialFetch([]); });
      await waitFor(() => expect(runtimeApi.listRuntimes).toHaveBeenCalledTimes(2));
      expect(screen.queryByText("Machine connected")).not.toBeInTheDocument();

      await act(async () => { finishRefresh([makeRuntime()]); });
      expect(await screen.findByText("Machine connected")).toBeInTheDocument();
      expect(runtimeApi.listRuntimes).toHaveBeenCalledTimes(2);
      await userEvent.setup().click(screen.getByRole("button", { name: "View runtime" }));
      expect(navigationState.push).toHaveBeenCalledWith("/runtimes/rt-new");
    },
  );

  it("replays a queued refresh after an initial failure and establishes a fresh baseline", async () => {
    let failInitialFetch!: (error: Error) => void;
    const existing = makeRuntime({ id: "rt-existing", daemon_id: "daemon-existing" });
    runtimeApi.listRuntimes
      .mockImplementationOnce(() => new Promise((_resolve, reject) => { failInitialFetch = reject; }))
      .mockResolvedValue([existing]);
    const { qc } = renderDialog();
    await waitFor(() => expect(runtimeApi.listRuntimes).toHaveBeenCalledTimes(1));
    await act(async () => { wsEventState.handler?.({ action: "update" }); });
    await act(async () => { failInitialFetch(new Error("initial query failed")); });

    await waitFor(() => expect(runtimeApi.listRuntimes).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(qc.isFetching()).toBe(0));
    expect(screen.queryByText("Machine connected")).not.toBeInTheDocument();
    runtimeApi.listRuntimes.mockResolvedValue([existing, makeRuntime()]);
    await refreshRuntimes(qc, { action: "update" });
    expect(await screen.findByText("Machine connected")).toBeInTheDocument();
    expect(runtimeApi.listRuntimes).toHaveBeenCalledTimes(3);
  });

  it("waits for a successful initial fetch instead of trusting stale cached runtimes", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    qc.setQueryData(runtimeKeys.listMine(WS_ID), []);
    let finishFetch!: (runtimes: AgentRuntime[]) => void;
    runtimeApi.listRuntimes.mockReturnValue(new Promise((resolve) => { finishFetch = resolve; }));
    renderDialog(undefined, "ready", qc);
    await act(async () => { finishFetch([makeRuntime()]); });
    await waitFor(() => expect(qc.isFetching()).toBe(0));
    expect(screen.queryByText("Machine connected")).not.toBeInTheDocument();
    runtimeApi.listRuntimes.mockResolvedValue([makeRuntime()]);
    await refreshRuntimes(qc, { action: "update" });
    expect(screen.queryByText("Machine connected")).not.toBeInTheDocument();
  });

  it("does not report success when the runtime refresh fails and recovers on a later refresh", async () => {
    const { qc } = await renderListeningDialog();
    runtimeApi.listRuntimes.mockRejectedValue(new Error("runtime query failed"));
    await refreshRuntimes(qc, { runtime_id: "unverified-runtime" });
    expect(screen.queryByText("Machine connected")).not.toBeInTheDocument();
    runtimeApi.listRuntimes.mockResolvedValue([makeRuntime()]);
    await refreshRuntimes(qc, { action: "update" });
    expect(await screen.findByText("Machine connected")).toBeInTheDocument();
  });

  it("establishes its baseline only after the initial query recovers", async () => {
    runtimeApi.listRuntimes.mockRejectedValue(new Error("initial query failed"));
    const { qc } = renderDialog();
    await waitFor(() => expect(qc.getQueryState(runtimeKeys.listMine(WS_ID))?.status).toBe("error"));
    runtimeApi.listRuntimes.mockResolvedValue([makeRuntime()]);
    await refreshRuntimes(qc, { action: "update" });
    expect(screen.queryByText("Machine connected")).not.toBeInTheDocument();
    await refreshRuntimes(qc, { action: "heartbeat_recovery" });
    expect(screen.queryByText("Machine connected")).not.toBeInTheDocument();
  });

  it("completes when a new offline machine is subsequently confirmed online", async () => {
    const { qc } = await renderListeningDialog();
    runtimeApi.listRuntimes.mockResolvedValue([makeRuntime({ status: "offline" })]);
    await refreshRuntimes(qc, { action: "update" });
    expect(screen.queryByText("Machine connected")).not.toBeInTheDocument();
    runtimeApi.listRuntimes.mockResolvedValue([makeRuntime()]);
    await refreshRuntimes(qc, { action: "heartbeat_recovery" });
    expect(await screen.findByText("Machine connected")).toBeInTheDocument();
  });

  it("keeps an empty workspace waiting through repeated background updates", async () => {
    const { qc } = await renderListeningDialog();
    for (const payload of [
      { action: "update" },
      { action: "update" },
      { action: "heartbeat_recovery" },
      { runtime_profile_id: "existing-profile" },
      { runtime_profile_id: "existing-profile" },
      { runtimes: [makeRuntime({ owner_id: "another-member" })] },
    ]) {
      await refreshRuntimes(qc, payload);
      expect(screen.queryByText("Machine connected")).not.toBeInTheDocument();
    }
  });

  it("takes a fresh baseline when an open dialog moves to another workspace", async () => {
    const view = await renderListeningDialog();
    const nextWorkspace = "22222222-2222-4222-8222-222222222222";
    const existing = makeRuntime({ workspace_id: nextWorkspace });
    runtimeApi.listRuntimes.mockResolvedValue([existing]);
    workspaceState.id = nextWorkspace;
    workspaceState.slug = "another-workspace";
    view.rerenderDialog();
    await waitFor(() => expect(view.qc.getQueryData(runtimeKeys.listMine(nextWorkspace))).toEqual([existing]));
    expect(screen.queryByText("Machine connected")).not.toBeInTheDocument();
    expect(runtimeApi.listRuntimes).toHaveBeenLastCalledWith({ workspace_id: nextWorkspace, owner: "me" }, "another-workspace");

    await refreshRuntimes(view.qc, { action: "update" });
    expect(screen.queryByText("Machine connected")).not.toBeInTheDocument();
  });

  it("clears a previous workspace's success when switching to an unregistered workspace", async () => {
    const view = await renderListeningDialog();
    runtimeApi.listRuntimes.mockResolvedValue([makeRuntime()]);
    await refreshRuntimes(view.qc, { runtimes: [makeRuntime()] });
    expect(await screen.findByText("Machine connected")).toBeInTheDocument();

    const nextWorkspace = "22222222-2222-4222-8222-222222222222";
    runtimeApi.listRuntimes.mockResolvedValue([]);
    workspaceState.id = nextWorkspace;
    workspaceState.slug = "empty-workspace";
    view.rerenderDialog();
    await waitFor(() => expect(view.qc.getQueryData(runtimeKeys.listMine(nextWorkspace))).toEqual([]));
    await refreshRuntimes(view.qc, { runtime_profile_id: "existing-profile" });
    expect(screen.queryByText("Machine connected")).not.toBeInTheDocument();
    expect(screen.getByText("multica setup")).toBeInTheDocument();
  });


  it("clears connection success and establishes a new baseline when the account changes", async () => {
    const view = await renderListeningDialog();
    runtimeApi.listRuntimes.mockResolvedValue([makeRuntime()]);
    await refreshRuntimes(view.qc, { action: "update" });
    expect(await screen.findByText("Machine connected")).toBeInTheDocument();

    authState.user = { id: "another-user" };
    const existing = makeRuntime({ id: "rt-other-account", daemon_id: "daemon-other-account", owner_id: "another-user" });
    runtimeApi.listRuntimes.mockResolvedValue([existing]);
    view.rerenderDialog();
    await waitFor(() => expect(view.qc.getQueryData(runtimeKeys.listMine(WS_ID))).toEqual([existing]));
    await refreshRuntimes(view.qc, { runtime_profile_id: "existing-profile" });
    expect(screen.queryByText("Machine connected")).not.toBeInTheDocument();
  });
});
