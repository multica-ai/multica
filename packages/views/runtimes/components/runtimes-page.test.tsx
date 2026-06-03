import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AgentRuntime } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { RuntimesPage } from "./runtimes-page";

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };
const queryState = vi.hoisted(() => ({
  runtimes: [] as AgentRuntime[],
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: unknown[] }) => {
    const key = options.queryKey?.[0];
    if (key === "runtimeList") {
      return { data: queryState.runtimes, isLoading: false };
    }
    if (key === "latestCliVersion") {
      return { data: null, isLoading: false };
    }
    return { data: [], isLoading: false };
  },
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));

vi.mock("@multica/core/runtimes", () => ({
  deriveRuntimeHealth: (runtime: AgentRuntime) =>
    runtime.status === "online" ? "online" : "offline",
  latestCliVersionOptions: () => ({ queryKey: ["latestCliVersion"] }),
  runtimeUsageOptions: () => ({ queryKey: ["runtimeUsage"] }),
}));

vi.mock("@multica/core/runtimes/queries", () => ({
  runtimeKeys: { all: (wsId: string) => ["runtimeList", wsId] },
  runtimeListOptions: () => ({ queryKey: ["runtimeList"] }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
}));

vi.mock("@multica/core/agents", () => ({
  deriveWorkload: ({
    runningCount,
    queuedCount,
  }: {
    runningCount: number;
    queuedCount: number;
  }) => (runningCount > 0 ? "working" : queuedCount > 0 ? "queued" : "idle"),
  agentTaskSnapshotOptions: () => ({ queryKey: ["taskSnapshot"] }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ isLoading: false, user: { id: "user-test" } }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/realtime", () => ({
  useWSEvent: vi.fn(),
}));

vi.mock("@multica/core/runtimes/hooks", () => ({
  useUpdatableRuntimeIds: () => new Set<string>(),
}));

vi.mock("@multica/core/runtimes/mutations", () => ({
  useAddCustomRuntime: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
  }),
  useUpdateCustomRuntime: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
  }),
  useDeleteCustomRuntime: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
  }),
}));

vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => false,
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: vi.fn() }),
}));

vi.mock("react-resizable-panels", async () => {
  const React = await import("react");
  const Passthrough = ({ children }: { children?: React.ReactNode }) =>
    React.createElement("div", null, children);
  return {
    Group: Passthrough,
    Panel: Passthrough,
    PanelResizeHandle: Passthrough,
    Separator: Passthrough,
    useDefaultLayout: () => ({
      defaultLayout: undefined,
      onLayoutChanged: vi.fn(),
    }),
  };
});

function renderPage() {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <RuntimesPage />
    </I18nProvider>,
  );
}

function makeRuntime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "rt-test",
    workspace_id: "ws-test",
    daemon_id: "daemon-test",
    name: "Claude (test-machine)",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: "user-test",
    visibility: "private",
    last_seen_at: "2026-06-02T00:00:00Z",
    created_at: "2026-06-02T00:00:00Z",
    updated_at: "2026-06-02T00:00:00Z",
    ...overrides,
  };
}

describe("RuntimesPage custom runtime entry", () => {
  it("does not offer custom runtime setup before a computer exists", () => {
    queryState.runtimes = [];

    renderPage();

    expect(
      screen.queryByRole("button", { name: "Custom runtime" }),
    ).not.toBeInTheDocument();
  });

  it("opens custom runtime setup from the selected machine header once a computer exists", () => {
    queryState.runtimes = [makeRuntime()];

    renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Custom runtime" }));

    const dialog = screen.getByRole("dialog");
    expect(dialog).toHaveTextContent("Custom runtime");
    expect(dialog).toHaveTextContent("Add runtime");
    expect(dialog).not.toHaveTextContent("MULTICA_CUSTOM_AGENTS");
  });
});
