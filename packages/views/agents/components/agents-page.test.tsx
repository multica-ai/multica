import type { ReactNode } from "react";
import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";

const h = vi.hoisted(() => ({
  agents: [] as Array<Record<string, unknown>>,
  members: [] as Array<Record<string, unknown>>,
  selectedAgentIds: [] as string[],
  dialogPayload: {} as Record<string, unknown>,
  dialogProps: null as null | Record<string, unknown>,
  scope: "all" as "all" | "mine",
  bulkUpdateAgents: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (sel?: (s: unknown) => unknown) => {
    const s = { user: { id: "user-1", name: "Me", email: "me@x.io" } };
    return sel ? sel(s) : s;
  },
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ agentDetail: (id: string) => `/a/${id}` }),
}));
vi.mock("../../navigation", () => ({ useNavigation: () => ({ push: vi.fn() }) }));
vi.mock("@multica/core/permissions", () => ({ canAssignAgentToIssue: () => ({ allowed: true }) }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey?: unknown[] }) => {
    const tag = opts?.queryKey?.[0];
    if (tag === "agents") return { data: h.agents, isLoading: false, error: null, refetch: vi.fn() };
    if (tag === "members") return { data: h.members, isLoading: false, error: null, refetch: vi.fn() };
    if (tag === "runtimes") return { data: [{ id: "rt-1", name: "Codex Runtime", provider: "codex" }], isLoading: false, error: null, refetch: vi.fn() };
    return { data: [], isLoading: false, error: null, refetch: vi.fn() };
  },
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
  workspaceKeys: { agents: (id: string) => ["workspaces", id, "agents"] },
}));
vi.mock("@multica/core/runtimes", () => ({ runtimeListOptions: () => ({ queryKey: ["runtimes"] }) }));
vi.mock("@multica/core/agents", async () => {
  const actual = await vi.importActual<Record<string, unknown>>("@multica/core/agents");
  return {
    ...actual,
    summarizeActivityWindow: () => ({ totalRuns: 0 }),
    agentRunCounts30dOptions: () => ({ queryKey: ["runcounts"] }),
    useWorkspaceActivityMap: () => ({ byAgent: new Map() }),
    useWorkspacePresenceMap: () => ({ byAgent: new Map() }),
  };
});
vi.mock("@multica/core/agents/stores", () => ({
  useAgentsViewStore: (sel: (s: unknown) => unknown) => sel({ scope: h.scope, setScope: vi.fn() }),
}));
vi.mock("@tanstack/react-table", () => ({
  useReactTable: (options: { data?: Array<{ agent: { id: string } }> }) => {
    const rows = (options.data ?? []).map((original) => ({ original }));
    return {
      getRowModel: () => ({ rows }),
      getFilteredSelectedRowModel: () => ({
        rows: rows.filter((row) => h.selectedAgentIds.includes(row.original.agent.id)),
      }),
      getHeaderGroups: () => [],
    };
  },
  getCoreRowModel: () => vi.fn(),
}));
vi.mock("@multica/ui/components/ui/data-table", () => ({
  DataTable: ({ actionBar }: { actionBar?: ReactNode }) => (
    <div data-testid="agents-table">{actionBar}</div>
  ),
}));
vi.mock("./agent-columns", () => ({ createAgentColumns: () => [] }));
vi.mock("./create-agent-dialog", () => ({ CreateAgentDialog: () => null }));
vi.mock("./bulk-edit-agents-dialog", () => ({
  BulkEditAgentsDialog: (props: { affects?: number; onApply: (payload: Record<string, unknown>) => Promise<void> }) => {
    h.dialogProps = props as unknown as Record<string, unknown>;
    return (
      <div role="dialog" aria-label="Bulk edit agents">
        <p>Dialog affects {props.affects}</p>
        <button type="button" onClick={() => void props.onApply(h.dialogPayload).catch(() => undefined)}>
          Apply bulk edit
        </button>
      </div>
    );
  },
}));
vi.mock("./runtime-machine-filter-dropdown", () => ({ RuntimeMachineFilterDropdown: () => null }));
vi.mock("../../runtimes/components/runtime-machines", () => ({ buildRuntimeMachines: () => [] }));
vi.mock("@multica/core/api", () => ({
  api: { bulkUpdateAgents: h.bulkUpdateAgents },
}));
vi.mock("sonner", () => ({ toast: { success: h.toastSuccess, error: h.toastError } }));

import { AgentsPage } from "./agents-page";

const RES = { en: { common: enCommon, agents: enAgents } };
function Wrap({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={RES}>
      {children}
    </I18nProvider>
  );
}

function makeAgent(id: string): Record<string, unknown> {
  return {
    id,
    name: id,
    archived_at: null,
    owner_id: "user-1",
    runtime_id: null,
    runtime_config: {},
    model: "",
    custom_args: [],
    created_at: "2026-01-01T00:00:00Z",
    visibility: "workspace",
  };
}

describe("AgentsPage — bulk edit agents", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    h.agents = [makeAgent("alpha"), makeAgent("beta")];
    h.members = [{ user_id: "user-1", role: "owner" }];
    h.selectedAgentIds = [];
    h.dialogProps = null;
    h.dialogPayload = { runtime_id: "rt-1", model: "gpt-5.5" };
    h.scope = "all";
    h.bulkUpdateAgents.mockResolvedValue({ updated: 3 });
  });

  it("admin: bulk edits all active agents and surfaces the SERVER-returned count", async () => {
    const user = userEvent.setup();
    render(<AgentsPage />, { wrapper: Wrap });

    await user.click(screen.getByRole("button", { name: "Bulk edit agents" }));
    expect(screen.getByText("Dialog affects 2")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Apply bulk edit" }));

    await waitFor(() =>
      expect(h.bulkUpdateAgents).toHaveBeenCalledWith("ws-1", { runtime_id: "rt-1", model: "gpt-5.5" }),
    );
    await waitFor(() => expect(h.toastSuccess).toHaveBeenCalledWith("Updated 3 agents"));
  });

  it("admin: bulk edits selected agents only", async () => {
    const user = userEvent.setup();
    h.selectedAgentIds = ["alpha", "beta"];
    h.dialogPayload = { model: "gpt-5.5" };
    render(<AgentsPage />, { wrapper: Wrap });

    await user.click(screen.getByRole("button", { name: "Bulk edit selected" }));
    expect(screen.getByText("Dialog affects 2")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Apply bulk edit" }));

    await waitFor(() =>
      expect(h.bulkUpdateAgents).toHaveBeenCalledWith("ws-1", {
        agent_ids: ["alpha", "beta"],
        model: "gpt-5.5",
      }),
    );
  });

  it("passes custom arg suggestions for the targeted selected agents", async () => {
    const user = userEvent.setup();
    h.agents = [
      { ...makeAgent("alpha"), custom_args: ["--permission-mode", "acceptEdits"] },
      { ...makeAgent("beta"), custom_args: ["--permission-mode"] },
      { ...makeAgent("gamma"), custom_args: ["--other"], archived_at: null },
      { ...makeAgent("archived"), custom_args: ["--archived"], archived_at: "2026-01-01T00:00:00Z" },
    ];
    h.selectedAgentIds = ["alpha", "beta"];
    render(<AgentsPage />, { wrapper: Wrap });

    await user.click(screen.getByRole("button", { name: "Bulk edit selected" }));

    expect(h.dialogProps?.customArgOptions).toEqual([
      { value: "--permission-mode", agentCount: 2 },
      { value: "acceptEdits", agentCount: 1 },
    ]);
  });

  it("passes custom arg suggestions for all active agents", async () => {
    const user = userEvent.setup();
    h.agents = [
      { ...makeAgent("alpha"), custom_args: ["--permission-mode", "acceptEdits"] },
      { ...makeAgent("beta"), custom_args: ["--permission-mode", "--verbose"] },
      { ...makeAgent("archived"), custom_args: ["--archived"], archived_at: "2026-01-01T00:00:00Z" },
    ];
    render(<AgentsPage />, { wrapper: Wrap });

    await user.click(screen.getByRole("button", { name: "Bulk edit agents" }));

    expect(h.dialogProps?.customArgOptions).toEqual([
      { value: "--permission-mode", agentCount: 2 },
      { value: "--verbose", agentCount: 1 },
      { value: "acceptEdits", agentCount: 1 },
    ]);
  });

  it("passes custom arg suggestions for every active agent when all-active bulk edit ignores the current scope", async () => {
    const user = userEvent.setup();
    h.scope = "mine";
    h.agents = [
      { ...makeAgent("alpha"), owner_id: "user-1", custom_args: ["--mine"] },
      { ...makeAgent("beta"), owner_id: "user-2", custom_args: ["--other"] },
      { ...makeAgent("archived"), owner_id: "user-2", custom_args: ["--archived"], archived_at: "2026-01-01T00:00:00Z" },
    ];
    render(<AgentsPage />, { wrapper: Wrap });

    await user.click(screen.getByRole("button", { name: "Bulk edit agents" }));

    expect(h.dialogProps?.customArgOptions).toEqual([
      { value: "--mine", agentCount: 1 },
      { value: "--other", agentCount: 1 },
    ]);
  });

  it("non-admin: hides the bulk edit action", () => {
    h.members = [{ user_id: "user-1", role: "member" }];
    render(<AgentsPage />, { wrapper: Wrap });
    expect(screen.queryByRole("button", { name: "Bulk edit agents" })).toBeNull();
  });

  it("on failure: shows an error toast and keeps the dialog open", async () => {
    const user = userEvent.setup();
    h.bulkUpdateAgents.mockRejectedValueOnce(new Error("server boom"));
    render(<AgentsPage />, { wrapper: Wrap });

    await user.click(screen.getByRole("button", { name: "Bulk edit agents" }));
    await user.click(screen.getByRole("button", { name: "Apply bulk edit" }));

    await waitFor(() => expect(h.toastError).toHaveBeenCalledWith("server boom"));
    expect(screen.getByRole("button", { name: "Apply bulk edit" })).toBeInTheDocument();
    expect(h.toastSuccess).not.toHaveBeenCalled();
  });
});
