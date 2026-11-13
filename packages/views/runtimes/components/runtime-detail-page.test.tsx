import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { runtimeProfileListOptions } from "@multica/core/runtimes";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import type { AgentRuntime } from "@multica/core/types";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import enCommon from "../../locales/en/common.json";
import enLayout from "../../locales/en/layout.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { RuntimeDetailPage } from "./runtime-detail-page";

const TEST_RESOURCES = {
  en: { common: enCommon, layout: enLayout, runtimes: enRuntimes },
};
const WS = "ws-test";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-test" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (state: unknown) => unknown) =>
    select({ isLoading: false, user: { id: "user-1" } }),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    runtimes: () => "/runtimes",
    runtimeSettings: (machineId: string, runtimeId: string) =>
      `/runtimes/${machineId}/runtime/${runtimeId}`,
  }),
}));
vi.mock("@multica/core/realtime", () => ({ useWSEvent: () => {} }));
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, ...props }: { href: string; children: ReactNode }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("./provider-logo", () => ({ ProviderLogo: () => null }));
vi.mock("./qoder-detail-page", () => ({
  isQoderLocator: () => false,
  QoderDetailPage: () => null,
}));
vi.mock("./runtime-list", () => ({
  buildWorkloadIndex: () => new Map(),
  RuntimeList: ({ runtimes }: { runtimes: AgentRuntime[] }) => (
    <ul aria-label="machine runtimes">
      {runtimes.map((runtime) => (
        <li key={runtime.id}>{runtime.name}</li>
      ))}
    </ul>
  ),
}));
vi.mock("./update-section", () => ({
  UpdateSection: ({ currentVersion }: { currentVersion: string | null }) => (
    <span>CLI version {currentVersion}</span>
  ),
}));
vi.mock("./rename-machine-dialog", () => ({
  RenameMachineDialog: ({ open }: { open: boolean }) =>
    open ? <div role="dialog">Rename machine dialog</div> : null,
}));
vi.mock("./runtime-profiles-dialog", () => ({
  RuntimeProfilesDialog: () => <div role="dialog">Custom runtime dialog</div>,
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
    metadata: { cli_version: "0.3.17" },
    owner_id: "user-1",
    visibility: "private",
    last_seen_at: new Date(NOW - 5_000).toISOString(),
    created_at: new Date(NOW - 60_000).toISOString(),
    updated_at: new Date(NOW - 60_000).toISOString(),
    ...overrides,
  };
}

function renderPage({
  runtimeId = "rt-claude",
  runtimes = [makeRuntime()],
}: {
  runtimeId?: string;
  /** `null` leaves the runtime list unseeded, so the page renders loading. */
  runtimes?: AgentRuntime[] | null;
} = {}) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  if (runtimes) qc.setQueryData(runtimeListOptions(WS).queryKey, runtimes);
  qc.setQueryData(runtimeProfileListOptions(WS).queryKey, []);
  qc.setQueryData(agentListOptions(WS).queryKey, []);
  qc.setQueryData(agentTaskSnapshotOptions(WS).queryKey, []);
  qc.setQueryData(memberListOptions(WS).queryKey, [
    {
      id: "member-1",
      workspace_id: WS,
      user_id: "user-1",
      role: "owner",
      created_at: new Date(NOW).toISOString(),
      name: "Jiayuan",
      email: "jiayuan@example.com",
      avatar_url: null,
    },
  ]);
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <RuntimeDetailPage
          runtimeId={runtimeId}
          localDaemonId="daemon-1"
          localMachineName="Studio Mac"
          hasLocalMachine
          localMachineActions={<button type="button">Start</button>}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

describe("RuntimeDetailPage", () => {
  it("keeps the breadcrumb header in place while the machine loads", () => {
    const { container } = renderPage({ runtimes: null });

    const header = screen.getByRole("banner");
    expect(within(header).getByRole("link", { name: "Runtimes" })).toHaveAttribute(
      "href",
      "/runtimes",
    );
    expect(screen.getAllByRole("banner")).toHaveLength(1);
    expect(container.querySelector('[data-slot="skeleton"]')).not.toBeNull();
  });

  it("lays the machine out as identity, Daemon and Runtimes sections with a properties panel", () => {
    renderPage();

    const header = screen.getByRole("banner");
    expect(screen.getAllByRole("banner")).toHaveLength(1);
    expect(header).toHaveTextContent("Studio Mac");
    expect(within(header).queryByRole("button", { name: "Start" })).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 1, name: "Studio Mac" })).toBeInTheDocument();
    expect(screen.getByText("This device")).toBeInTheDocument();

    const daemon = screen.getByRole("heading", { level: 2, name: "Daemon" }).closest("section")!;
    expect(within(daemon).getByRole("button", { name: "Start" })).toBeInTheDocument();
    expect(within(daemon).getByText("CLI version 0.3.17")).toBeInTheDocument();

    const runtimes = screen.getByRole("heading", { level: 2, name: "Runtimes" }).closest("section")!;
    expect(within(runtimes).getByText("1")).toBeInTheDocument();
    expect(within(runtimes).getByRole("list", { name: "machine runtimes" })).toHaveTextContent(
      "Claude (dev-machine.local)",
    );
    expect(within(runtimes).getByRole("button", { name: /Add custom runtime/ })).toBeInTheDocument();

    expect(screen.getByRole("button", { name: "Properties" })).toBeInTheDocument();
    expect(screen.getByText("Status")).toBeInTheDocument();
    expect(screen.getByText("Owner")).toBeInTheDocument();
    expect(screen.getByText("Jiayuan")).toBeInTheDocument();
    expect(screen.getByText("Daemon ID")).toBeInTheDocument();
  });

  it("moves Rename machine into the ⋯ menu", async () => {
    renderPage();

    expect(screen.queryByRole("button", { name: "Rename machine" })).not.toBeInTheDocument();
    fireEvent.click(
      within(screen.getByRole("banner")).getByRole("button", { name: "More actions" }),
    );
    fireEvent.click(await screen.findByRole("menuitem", { name: "Rename machine" }));
    expect(await screen.findByRole("dialog")).toHaveTextContent("Rename machine dialog");
  });

  it("shows not-found in the body under the same breadcrumb", () => {
    renderPage({ runtimeId: "missing" });

    expect(
      within(screen.getByRole("banner")).getByRole("link", { name: "Runtimes" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Machine not found" })).toBeInTheDocument();
    expect(screen.getByText("All runtimes").closest("a")).toHaveAttribute(
      "href",
      "/runtimes",
    );
  });
});
