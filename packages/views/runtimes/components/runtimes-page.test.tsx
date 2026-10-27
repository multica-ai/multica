import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import { runtimeProfileListOptions } from "@multica/core/runtimes";
import { qoderOptions, type QoderConnection } from "@multica/core/runtimes/qoder";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import type { AgentRuntime } from "@multica/core/types";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { RuntimesPage } from "./runtimes-page";

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };
const WS = "ws-test";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (state: unknown) => unknown) =>
    select({ isLoading: false, user: { id: "user-1" } }),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    runtimeDetail: (id: string) => `/runtimes/${id}`,
  }),
  useRequiredWorkspaceSlug: () => "workspace-test",
}));
vi.mock("@multica/core/realtime", () => ({ useWSEvent: () => {} }));
vi.mock("@multica/core/onboarding", () => ({
  memberNeedsMikaSetup: () => false,
  useBootstrapMika: () => ({ isPending: false, mutateAsync: vi.fn() }),
}));
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, ...props }: { href: string; children: ReactNode }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
  rowLinkInteractiveProps: {},
  useNavigation: () => ({ push: vi.fn() }),
  useRowLink: () => () => ({}),
}));

const NOW = Date.now();

function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "rt-claude",
    workspace_id: WS,
    daemon_id: "daemon-1",
    name: "Claude (dev-machine.local)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "dev-machine.local · claude 1.0.0",
    metadata: {},
    owner_id: "user-1",
    visibility: "private",
    last_seen_at: new Date(NOW - 5_000).toISOString(),
    created_at: new Date(NOW - 60_000).toISOString(),
    updated_at: new Date(NOW - 60_000).toISOString(),
    ...overrides,
  };
}

function qoder(overrides: Partial<QoderConnection> = {}): QoderConnection {
  return {
    configured: false,
    available: true,
    name: "",
    baseUrl: "",
    environmentId: "",
    hasToken: false,
    repositories: [],
    enabled: false,
    status: "",
    lastError: "",
    maxConcurrency: 3,
    runTimeoutMinutes: 60,
    activeRuns: 0,
    queuedRuns: 0,
    ...overrides,
  };
}

function renderPage({
  runtimes = [makeRuntime()],
  connection = qoder(),
  desktop = false,
}: {
  runtimes?: AgentRuntime[];
  connection?: QoderConnection;
  desktop?: boolean;
} = {}) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  qc.setQueryData(runtimeListOptions(WS).queryKey, runtimes);
  qc.setQueryData(runtimeProfileListOptions(WS).queryKey, []);
  qc.setQueryData(agentListOptions(WS).queryKey, []);
  qc.setQueryData(memberListOptions(WS).queryKey, []);
  qc.setQueryData(agentTaskSnapshotOptions(WS).queryKey, []);
  qc.setQueryData(chatSessionsOptions(WS).queryKey, []);
  qc.setQueryData(qoderOptions(WS).queryKey, connection);
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <RuntimesPage
          {...(desktop
            ? {
                localDaemonId: "daemon-1",
                localMachineName: "Jiayuan's MacBook",
                hasLocalMachine: true,
              }
            : {})}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("RuntimesPage", () => {
  it("lists machines and cloud services in one table", () => {
    renderPage();

    expect(screen.getByText("Machines")).toBeInTheDocument();
    expect(screen.getByText("Cloud services")).toBeInTheDocument();
    expect(screen.getByText("dev-machine.local")).toBeInTheDocument();
    expect(screen.getByText("Not connected")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Connect" })).toBeInTheDocument();
    expect(screen.getByText("Cursor Cloud Agents")).toBeInTheDocument();
    expect(screen.getByText("Claude Managed Agents")).toBeInTheDocument();
  });

  it("offers the desktop download on web when no machine is connected", () => {
    renderPage({ runtimes: [] });

    expect(screen.getByText("Run on this computer")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Download desktop/ })).toHaveAttribute(
      "href",
      "/download",
    );
    expect(screen.getByText("Add another machine or server")).toBeInTheDocument();
  });

  it("pins this device on desktop and marks it connected in the add menu", async () => {
    const user = userEvent.setup();
    renderPage({ desktop: true });

    expect(screen.getByText("This device")).toBeInTheDocument();
    expect(screen.queryByText("Run on this computer")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /^Add/ }));
    const menu = await screen.findByRole("menu");
    expect(within(menu).getByText("Connected · Jiayuan's MacBook")).toBeInTheDocument();
    expect(within(menu).getByText("Other machine or server")).toBeInTheDocument();
    expect(within(menu).getByText("Qoder Cloud Agents")).toBeInTheDocument();
    expect(within(menu).getByText("By Qoder")).toBeInTheDocument();
  });

  it("filters rows by search and status", async () => {
    const user = userEvent.setup();
    renderPage({
      runtimes: [
        makeRuntime(),
        makeRuntime({
          id: "rt-codex",
          daemon_id: "daemon-2",
          name: "Codex (build-server)",
          provider: "codex",
          device_info: "build-server · codex 0.5.0",
          status: "offline",
          last_seen_at: new Date(NOW - 3 * 60 * 60_000).toISOString(),
        }),
      ],
      connection: qoder({ configured: true, enabled: true, status: "online" }),
    });

    await user.click(screen.getByRole("button", { name: /^Offline/ }));
    expect(screen.getByText("build-server")).toBeInTheDocument();
    expect(screen.queryByText("dev-machine.local")).not.toBeInTheDocument();
    expect(screen.queryByText("Qoder Cloud Agents")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /^All/ }));
    await user.type(screen.getByRole("textbox"), "zzz");
    expect(
      screen.getByText("No machines or cloud services match"),
    ).toBeInTheDocument();
  });

  it("never lists the Qoder runtime as a machine", () => {
    const qoderRuntime = makeRuntime({
      id: "rt-qoder",
      daemon_id: "qoder-managed-ws-test",
      name: "Qoder Cloud Agents",
      provider: "qoder_cloud",
      runtime_mode: "cloud",
      device_info: "",
    });
    for (const connection of [
      qoder({ configured: true, enabled: true, status: "online" }),
      qoder({ available: false }),
    ]) {
      const { unmount } = renderPage({
        runtimes: [makeRuntime(), qoderRuntime],
        connection,
      });
      expect(screen.getByText("dev-machine.local")).toBeInTheDocument();
      expect(screen.queryAllByText("Qoder Cloud Agents")).toHaveLength(
        1,
      );
      expect(screen.getByRole("button", { name: /^All/ })).toHaveTextContent(
        connection.configured ? "2" : "1",
      );
      unmount();
    }
  });
});
