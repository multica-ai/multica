import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { configStore } from "@multica/core/config";
import type { AgentRuntime } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";
import {
  ConnectRemoteDialog,
  runtimeSetupCommand,
} from "./connect-remote-dialog";

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };
const TOKEN = "mst_0123456789abcdef0123456789abcdef01234567";
const mockSetup = vi.hoisted(() => ({
  status: {
    id: "session-test",
    expires_at: "2026-07-21T12:30:00Z",
    redeemed_at: null as string | null,
    daemon_connected_at: null as string | null,
    daemon_id: null as string | null,
    runtime_count: 0,
  },
  runtimes: [] as AgentRuntime[],
}));

vi.mock("@multica/core/paths", () => ({
  paths: {
    workspace: () => ({
      agents: () => "/agents",
      runtimeDetail: () => "/runtimes/rt-test",
    }),
  },
  useCurrentWorkspace: () => ({ id: "ws-test" }),
  useWorkspaceSlug: () => "workspace-test",
}));
vi.mock("@multica/core/realtime", () => ({ useWSEvent: vi.fn() }));
vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: vi.fn() }),
}));
vi.mock("@multica/core/runtimes", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/runtimes")>(
    "@multica/core/runtimes",
  );
  return {
    ...actual,
    runtimeSetupCreateOptions: () => ({
      queryKey: ["setup", "create"],
      queryFn: async () => ({
        id: "session-test",
        token: "mst_0123456789abcdef0123456789abcdef01234567",
        expires_at: "2026-07-21T12:30:00Z",
        redeemed_at: null,
        daemon_connected_at: null,
        daemon_id: null,
        runtime_count: 0,
      }),
      staleTime: Infinity,
    }),
    runtimeSetupStatusOptions: () => ({
      queryKey: ["setup", "status"],
      queryFn: async () => mockSetup.status,
    }),
    runtimeListOptions: () => ({
      queryKey: ["runtimes", "list"],
      queryFn: async () => mockSetup.runtimes,
    }),
    runtimeKeys: {
      all: (wsId: string) => ["runtimes", wsId],
      list: (wsId: string) => ["runtimes", wsId, "list"],
      setupStatus: (wsId: string, sessionId: string) => [
        "runtimes",
        wsId,
        "setup",
        sessionId,
      ],
    },
  };
});

function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "runtime-claude",
    workspace_id: "ws-test",
    daemon_id: "daemon-test",
    name: "Claude (build-host)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "build-host · claude 1.0.0",
    metadata: {},
    owner_id: "user-test",
    visibility: "private",
    last_seen_at: new Date().toISOString(),
    created_at: "2026-07-21T12:00:00Z",
    updated_at: "2026-07-21T12:00:00Z",
    ...overrides,
  };
}

function resetConfigStore() {
  configStore.setState({
    cdnDomain: "",
    allowSignup: true,
    googleClientId: "",
    daemonServerUrl: "",
    daemonAppUrl: "",
    workspaceCreationDisabled: false,
  });
}

function renderDialog(config?: {
  daemonServerUrl?: string;
  daemonAppUrl?: string;
}) {
  resetConfigStore();
  if (config) configStore.getState().setDaemonConfig(config);
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ConnectRemoteDialog onClose={vi.fn()} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("runtimeSetupCommand", () => {
  it("builds the cloud one-command setup without a browser step", () => {
    const command = runtimeSetupCommand(TOKEN);
    expect(command).toBe(
      `curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash -s -- --token '${TOKEN}'`,
    );
    expect(command).not.toContain("multica setup");
  });

  it("includes both self-host origins in the same command", () => {
    expect(
      runtimeSetupCommand(
        TOKEN,
        "https://api.example.com/",
        "https://app.example.com/",
      ),
    ).toContain(
      `--server-url 'https://api.example.com' --app-url 'https://app.example.com'`,
    );
  });
});

describe("ConnectRemoteDialog", () => {
  beforeEach(() => {
    Object.assign(mockSetup.status, {
      redeemed_at: null,
      daemon_connected_at: null,
      daemon_id: null,
      runtime_count: 0,
    });
    mockSetup.runtimes = [];
  });

  it("renders a one-command headless guide and observable checklist", async () => {
    renderDialog();

    expect(await screen.findByText(/bash -s -- --token/)).toHaveTextContent(TOKEN);
    expect(screen.getByText("Connection progress")).toBeInTheDocument();
    expect(screen.getByText("Setup command accepted")).toBeInTheDocument();
    expect(screen.getByText(/works once/i)).toBeInTheDocument();
    expect(screen.queryByText(/download desktop/i)).not.toBeInTheDocument();
  });

  it("uses configured self-host URLs", async () => {
    renderDialog({
      daemonServerUrl: "https://api.example.com/",
      daemonAppUrl: "https://app.example.com/",
    });

    const command = await screen.findByText(/bash -s -- --token/);
    expect(command).toHaveTextContent("--server-url 'https://api.example.com'");
    expect(command).toHaveTextContent("--app-url 'https://app.example.com'");
  });

  it("distinguishes a connected daemon with no installed agent runtime", async () => {
    Object.assign(mockSetup.status, {
      redeemed_at: "2026-07-21T12:01:00Z",
      daemon_connected_at: "2026-07-21T12:01:05Z",
      daemon_id: "daemon-test",
    });

    renderDialog();

    expect(
      await screen.findByText("Computer connected, but no agent runtime was found"),
    ).toBeInTheDocument();
    expect(screen.getByText(/keeps checking/i)).toBeInTheDocument();
  });

  it("groups multiple runtimes from one daemon as one computer", async () => {
    Object.assign(mockSetup.status, {
      redeemed_at: "2026-07-21T12:01:00Z",
      daemon_connected_at: "2026-07-21T12:01:05Z",
      daemon_id: "daemon-test",
      runtime_count: 2,
    });
    mockSetup.runtimes = [
      makeRuntime(),
      makeRuntime({
        id: "runtime-codex",
        name: "Codex (build-host)",
        provider: "codex",
      }),
    ];

    renderDialog();

    expect(
      await screen.findByText("Connected runtimes: 2 · Computers: 1"),
    ).toBeInTheDocument();
    expect(screen.getByText("build-host")).toBeInTheDocument();
    expect(screen.getByText("claude · codex")).toBeInTheDocument();
  });
});
