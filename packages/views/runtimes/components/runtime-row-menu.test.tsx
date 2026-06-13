// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import type { AgentRuntime } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";
import enAgents from "../../locales/en/agents.json";

const TEST_RESOURCES = {
  en: { common: enCommon, runtimes: enRuntimes, agents: enAgents },
};

// Stub the workspace queries the columns reach into. None of them feed the
// row menu directly, but `createRuntimeColumns` wires CliCell + CostCell
// against the same query client, so we still need useQuery to resolve.
vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return {
    ...actual,
    useQuery: vi.fn(() => ({ data: [], isLoading: false })),
  };
});

vi.mock("@multica/core/runtimes/mutations", () => ({
  useDeleteRuntime: () => ({ mutate: vi.fn(), isPending: false, mutateAsync: vi.fn() }),
  useArchiveAgentsAndDeleteRuntime: () => ({
    mutate: vi.fn(),
    isPending: false,
    mutateAsync: vi.fn(),
  }),
}));

vi.mock("@multica/core/runtimes", () => ({
  deriveRuntimeHealth: () => "online",
  runtimeUsageOptions: () => ({ kind: "usage" }),
}));

vi.mock("@multica/core/agents", () => ({
  deriveWorkload: () => "idle",
  useWorkspacePresenceMap: () => ({ byAgent: new Map(), loading: false }),
}));

// The unified DeleteRuntimeDialog the kebab now opens reaches into auth +
// the api singleton. The dialog never renders in these tests (`open=false`
// throughout) but its hooks still mount; stub them so module init is clean.
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (sel: (s: { user: { id: string } }) => unknown) =>
    sel({ user: { id: "user-me" } }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    deleteRuntime: vi.fn(),
    archiveAgentsAndDeleteRuntime: vi.fn(),
  },
  ApiError: class ApiError extends Error {},
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

vi.mock("../../common/use-viewing-timezone", () => ({
  useViewingTimezone: () => "UTC",
}));

vi.mock("./provider-logo", () => ({ ProviderLogo: () => null }));
vi.mock("./shared", () => ({
  HealthIcon: () => null,
  useHealthLabel: () => () => "Online",
}));

import { createRuntimeColumns, type RuntimeRow } from "./runtime-columns";
import { useT } from "../../i18n";

function makeRuntime(overrides: Partial<AgentRuntime>): AgentRuntime {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    daemon_id: null,
    name: "rt",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: "user-1",
    visibility: "private",
    last_seen_at: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function makeRow(runtime: AgentRuntime, canDelete = true): RuntimeRow {
  return {
    runtime,
    ownerMember: null,
    workload: { agentIds: [], runningCount: 0, queuedCount: 0 },
    canDelete,
  };
}

// The row menu lives inside the "actions" column cell. To exercise it
// without rendering the entire DataTable, we resolve the cell renderer from
// createRuntimeColumns and render its output directly inside a minimal table
// row (the cell expects React table context, but our shape — `row.original`
// — is the only field RowMenu reads, so a hand-built shim suffices).
function renderActionsCell(row: RuntimeRow) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  function Harness() {
    const { t } = useT("runtimes");
    const columns = createRuntimeColumns({
      showOwner: false,
      wsId: "ws-1",
      now: Date.now(),
      t,
    });
    const actions = columns.find((c) => c.id === "actions");
    if (!actions || typeof actions.cell !== "function") {
      throw new Error("actions column missing or has no cell renderer");
    }
    // The cell renderer only reads `row.original`. Casting through unknown
    // keeps us honest about not implementing the full tanstack-table cell
    // context.
    const cell = actions.cell({
      row: { original: row },
    } as unknown as Parameters<typeof actions.cell>[0]);
    return <>{cell}</>;
  }

  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <Harness />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("runtime list row menu", () => {
  beforeEach(() => vi.clearAllMocks());

  it("hides the kebab menu for an online local runtime (self-healing)", () => {
    // Deleting an online local runtime is a no-op (daemon re-registers in
    // seconds), so the row menu drops the only action — Delete — entirely.
    renderActionsCell(
      makeRow(makeRuntime({ runtime_mode: "local", status: "online" })),
    );
    expect(screen.queryByLabelText("Row actions")).not.toBeInTheDocument();
  });

  it("renders the kebab menu for an offline local runtime", () => {
    renderActionsCell(
      makeRow(makeRuntime({ runtime_mode: "local", status: "offline" })),
    );
    expect(screen.getByLabelText("Row actions")).toBeInTheDocument();
  });

  it("renders the kebab menu for a cloud runtime regardless of status", () => {
    renderActionsCell(
      makeRow(makeRuntime({ runtime_mode: "cloud", status: "online" })),
    );
    expect(screen.getByLabelText("Row actions")).toBeInTheDocument();
  });

  it("hides the kebab menu when the caller lacks delete permission", () => {
    // Pre-existing behavior — re-asserted so the new self-healing guard
    // doesn't accidentally regress it (both paths return the same empty
    // span).
    renderActionsCell(
      makeRow(
        makeRuntime({ runtime_mode: "local", status: "offline" }),
        /* canDelete */ false,
      ),
    );
    expect(screen.queryByLabelText("Row actions")).not.toBeInTheDocument();
  });
});

// The CLI column lives in its own cell renderer; resolve it from
// createRuntimeColumns and render it in isolation, mirroring renderActionsCell.
function renderCliCell(row: RuntimeRow) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  function Harness() {
    const { t } = useT("runtimes");
    const columns = createRuntimeColumns({
      showOwner: false,
      wsId: "ws-1",
      now: Date.now(),
      t,
    });
    const cli = columns.find((c) => c.id === "cli");
    if (!cli || typeof cli.cell !== "function") {
      throw new Error("cli column missing or has no cell renderer");
    }
    const cell = cli.cell({
      row: { original: row },
    } as unknown as Parameters<typeof cli.cell>[0]);
    return <>{cell}</>;
  }

  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <Harness />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("runtime list CLI column", () => {
  beforeEach(() => vi.clearAllMocks());

  // #3838: every agent showed the same number because the column rendered the
  // shared multica daemon `cli_version`. It must instead show the agent's own
  // tool version from `metadata.version`.
  it("shows the agent's own CLI tool version, not the shared daemon version", () => {
    renderCliCell(
      makeRow(
        makeRuntime({
          runtime_mode: "local",
          metadata: { version: "2.1.5 (Claude Code)", cli_version: "0.3.17" },
        }),
      ),
    );
    expect(screen.getByText("2.1.5 (Claude Code)")).toBeInTheDocument();
    expect(screen.queryByText("0.3.17")).not.toBeInTheDocument();
  });

  it("falls back to an em dash when the agent version is missing", () => {
    renderCliCell(
      makeRow(
        makeRuntime({
          runtime_mode: "local",
          metadata: { cli_version: "0.3.17" },
        }),
      ),
    );
    expect(screen.queryByText("0.3.17")).not.toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("renders an em dash for cloud runtimes", () => {
    renderCliCell(
      makeRow(
        makeRuntime({
          runtime_mode: "cloud",
          metadata: { version: "2.1.5 (Claude Code)" },
        }),
      ),
    );
    expect(screen.getByText("—")).toBeInTheDocument();
  });
});
