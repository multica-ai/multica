// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { ApiError } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import { McpConfigTab } from "./mcp-config-tab";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const mockRuntimeCapabilities = vi.hoisted(() => vi.fn());
const mockAddServer = vi.hoisted(() => vi.fn());
const mockSetEnabled = vi.hoisted(() => vi.fn());
const mockRemoveServer = vi.hoisted(() => vi.fn());

const workspaceMcp = vi.hoisted(() => ({
  // Servers ASSIGNED to this agent, and the workspace library to pick from.
  assigned: [] as Array<Record<string, unknown>>,
  library: [] as Array<Record<string, unknown>>,
}));

// The workspace section reads the agent's assignments plus the library.
// Stubbing the query options keeps this a pure render test — the real ones
// would hit fetch.
vi.mock("@multica/core/workspace/queries", () => ({
  agentMcpServersOptions: (agentId: string) => ({
    queryKey: ["agents", agentId, "mcp-servers"],
    queryFn: () => Promise.resolve(workspaceMcp.assigned),
    enabled: agentId !== "",
  }),
  workspaceMcpServersOptions: (wsId: string) => ({
    queryKey: ["workspaces", wsId, "mcp-servers"],
    queryFn: () => Promise.resolve(workspaceMcp.library),
    enabled: wsId !== "",
  }),
}));

vi.mock("@multica/core/workspace/mutations", () => ({
  useAddAgentMcpServer: () => ({ mutateAsync: mockAddServer, isPending: false }),
  useSetAgentMcpServerEnabled: () => ({ mutateAsync: mockSetEnabled, isPending: false }),
  useRemoveAgentMcpServer: () => ({ mutateAsync: mockRemoveServer, isPending: false }),
}));

const wsServer = (over: Record<string, unknown>) => ({
  id: "srv-1",
  workspace_id: "ws-1",
  name: "shared-linear",
  transport: "http",
  created_at: "2026-08-14T00:00:00Z",
  updated_at: "2026-08-14T00:00:00Z",
  ...over,
});

// The tab reads discovery through runtimeCapabilitiesOptions; existing tests
// render with runtime={null} so the query stays disabled and never fires.
vi.mock("@multica/core/runtimes", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/runtimes")>(
      "@multica/core/runtimes",
    );
  return {
    ...actual,
    runtimeCapabilitiesOptions: (runtimeId: string | null) => ({
      queryKey: ["runtime-capabilities", runtimeId],
      queryFn: () => mockRuntimeCapabilities(runtimeId),
      enabled: Boolean(runtimeId),
      retry: false,
    }),
  };
});

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_args: [],
  visibility: "workspace",
  permission_mode: "public_to",
  invocation_targets: [{ target_type: "workspace", target_id: null }],
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-05-28T00:00:00Z",
  updated_at: "2026-05-28T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function TestShell({ children }: { children: React.ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    </I18nProvider>
  );
}

function renderTab(
  overrides: Partial<Agent> = {},
  onSave = vi.fn().mockResolvedValue(undefined),
  runtime: AgentRuntime | null = null,
) {
  const result = render(
    <TestShell>
      <McpConfigTab
        agent={{ ...baseAgent, ...overrides }}
        runtime={runtime}
        onSave={onSave}
      />
    </TestShell>,
  );
  return { ...result, onSave };
}

const onlineRuntime: AgentRuntime = {
  id: "runtime-1",
  workspace_id: "ws-1",
  daemon_id: "daemon-1",
  name: "Claude (Mac)",
  runtime_mode: "local",
  provider: "claude",
  launch_header: "",
  status: "online",
  device_info: "Mac",
  metadata: {},
  owner_id: "user-1",
  visibility: "private",
  last_seen_at: null,
  created_at: "2026-07-11T00:00:00Z",
  updated_at: "2026-07-11T00:00:00Z",
};

describe("McpConfigTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    workspaceMcp.assigned = [];
    workspaceMcp.library = [];
    mockAddServer.mockResolvedValue([]);
    mockSetEnabled.mockResolvedValue([]);
    mockRemoveServer.mockResolvedValue([]);
  });

  it("renders redacted managed MCP without exposing add or edit controls", () => {
    renderTab({ mcp_config: null, mcp_config_redacted: true });

    expect(screen.getByText(/hidden from your view/i)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /add mcp/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("projects historical aggregate config into individually managed rows", () => {
    renderTab({
      mcp_config: {
        version: 1,
        mcpServers: {
          fetch: { command: "uvx", args: ["mcp-server-fetch"] },
          docs: { type: "http", url: "https://example.test/mcp" },
        },
      },
    });

    expect(screen.getByText("fetch")).toBeInTheDocument();
    expect(screen.getByText("docs")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: /managed by multica/i })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: /inherited from runtime/i })).toBeInTheDocument();
    expect(screen.queryByLabelText(/MCP config JSON editor/i)).not.toBeInTheDocument();
  });

  it("adds one stdio server through the form and preserves historical top-level data", async () => {
    const user = userEvent.setup();
    const { onSave } = renderTab({ mcp_config: { version: 1 } });

    await user.click(screen.getByRole("button", { name: /add mcp/i }));
    await user.type(screen.getByLabelText("Name"), "fetch");
    await user.type(screen.getByLabelText("Command"), "uvx");
    await user.click(screen.getByRole("button", { name: /add server/i }));

    expect(onSave).toHaveBeenCalledWith({
      mcp_config: {
        version: 1,
        mcpServers: { fetch: { command: "uvx" } },
      },
    });
  });

  it("adds one HTTP server through JSON mode", async () => {
    const user = userEvent.setup();
    const { onSave } = renderTab();

    await user.click(screen.getByRole("button", { name: /add mcp/i }));
    await user.type(screen.getByLabelText("Name"), "weather");
    await user.click(screen.getByRole("tab", { name: "JSON" }));
    fireEvent.change(screen.getByLabelText(/MCP server JSON configuration/i), {
      target: {
        value: JSON.stringify({
          type: "http",
          url: "https://example.invalid/mcp",
        }),
      },
    });
    await user.click(screen.getByRole("button", { name: /add server/i }));

    expect(onSave).toHaveBeenCalledWith({
      mcp_config: {
        mcpServers: {
          weather: {
            type: "http",
            url: "https://example.invalid/mcp",
          },
        },
      },
    });
  });

  it("edits one historical server without replacing its siblings", async () => {
    const user = userEvent.setup();
    const existing = {
      version: 1,
      mcpServers: {
        fetch: {
          command: "uvx",
          timeout: 30,
          tools: { include: ["fetch_url"] },
        },
        docs: { url: "https://example.test/mcp" },
      },
    };
    const { onSave } = renderTab({ mcp_config: existing });

    await user.click(
      screen.getByRole("button", { name: /edit mcp server fetch/i }),
    );
    const command = screen.getByLabelText("Command");
    await user.clear(command);
    await user.type(command, "npx");
    await user.click(screen.getByRole("button", { name: /save changes/i }));

    expect(onSave).toHaveBeenCalledWith({
      mcp_config: {
        version: 1,
        mcpServers: {
          fetch: {
            timeout: 30,
            tools: { include: ["fetch_url"] },
            command: "npx",
          },
          docs: { url: "https://example.test/mcp" },
        },
      },
    });
  });

  it("deletes the last managed server only after confirmation", async () => {
    const user = userEvent.setup();
    const { onSave } = renderTab({
      mcp_config: { mcpServers: { fetch: { command: "uvx" } } },
    });

    await user.click(
      screen.getByRole("button", { name: /delete mcp server fetch/i }),
    );
    expect(screen.getByText(/runtime servers are not affected/i)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /delete server/i }));

    expect(onSave).toHaveBeenCalledWith({ mcp_config: null });
  });

  it("blocks invalid single-server JSON", async () => {
    const user = userEvent.setup();
    const { onSave } = renderTab();

    await user.click(screen.getByRole("button", { name: /add mcp/i }));
    await user.type(screen.getByLabelText("Name"), "broken");
    await user.click(screen.getByRole("tab", { name: "JSON" }));
    fireEvent.change(screen.getByLabelText(/MCP server JSON configuration/i), {
      target: { value: "{not json" },
    });

    expect(screen.getByText(/invalid json/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /add server/i })).toBeDisabled();
    expect(onSave).not.toHaveBeenCalled();
  });

  it("lists inherited MCP servers discovered from the assigned runtime", async () => {
    mockRuntimeCapabilities.mockResolvedValue({
      skills: [],
      supported: true,
      mcpServers: [
        { name: "linear", transport: "http", source: "User config", enabled: true },
      ],
      mcpSupported: true,
    });

    renderTab({}, undefined, onlineRuntime);

    expect(await screen.findByText("linear")).toBeInTheDocument();
  });

  it("shows a permission notice when capability discovery is forbidden", async () => {
    mockRuntimeCapabilities.mockRejectedValue(
      new ApiError("insufficient permissions", 403, "Forbidden"),
    );

    renderTab({}, undefined, onlineRuntime);

    expect(
      await screen.findByText(
        "You don't have permission to view this runtime's MCP servers.",
      ),
    ).toBeInTheDocument();
  });

  it("shows a retry notice when capability discovery fails", async () => {
    mockRuntimeCapabilities.mockRejectedValue(
      new Error("daemon did not respond within 3 minutes"),
    );

    renderTab({}, undefined, onlineRuntime);

    expect(
      await screen.findByText(
        "Couldn't discover runtime MCP servers. Try again.",
      ),
    ).toBeInTheDocument();
  });
});

// The workspace layer (GH #6062) reaches an agent only when someone assigns
// it, so the tab has to show what is assigned, let an owner assign more, and
// never imply an unassigned library entry is in play.
describe("McpConfigTab workspace servers", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    workspaceMcp.assigned = [];
    workspaceMcp.library = [];
    mockAddServer.mockResolvedValue([]);
    mockSetEnabled.mockResolvedValue([]);
    mockRemoveServer.mockResolvedValue([]);
  });

  it("lists the workspace servers assigned to this agent", async () => {
    workspaceMcp.assigned = [wsServer({ enabled: true })];
    renderTab({ mcp_config: null });

    expect(await screen.findByText("shared-linear")).toBeInTheDocument();
    expect(screen.getByText(/From the workspace library/)).toBeInTheDocument();
  });

  // The defining property of this model: a library entry nobody assigned must
  // not appear as if the agent had it.
  it("does not show unassigned library servers as the agent's", async () => {
    workspaceMcp.library = [wsServer({ name: "not-assigned" })];
    renderTab({ mcp_config: null });

    expect(
      await screen.findByText(/No workspace MCP servers assigned/),
    ).toBeInTheDocument();
    expect(screen.queryByText("not-assigned")).toBeNull();
  });

  it("assigns a library server through the picker", async () => {
    const user = userEvent.setup();
    workspaceMcp.library = [wsServer({ id: "srv-9", name: "pickable" })];
    renderTab({ mcp_config: null });

    await user.click(await screen.findByRole("button", { name: /Add from workspace/ }));
    await user.click(await screen.findByRole("menuitem", { name: /pickable/ }));

    await waitFor(() => expect(mockAddServer).toHaveBeenCalledWith("srv-9"));
  });

  it("toggles an assignment off without dropping it", async () => {
    const user = userEvent.setup();
    workspaceMcp.assigned = [wsServer({ enabled: true })];
    renderTab({ mcp_config: null });

    await user.click(
      await screen.findByRole("switch", { name: /Enable shared-linear/i }),
    );

    await waitFor(() =>
      expect(mockSetEnabled).toHaveBeenCalledWith({ serverId: "srv-1", enabled: false }),
    );
    expect(mockRemoveServer).not.toHaveBeenCalled();
  });

  it("removes an assignment", async () => {
    const user = userEvent.setup();
    workspaceMcp.assigned = [wsServer({ enabled: true })];
    renderTab({ mcp_config: null });

    await user.click(
      await screen.findByRole("button", { name: /Remove shared-linear/i }),
    );

    await waitFor(() => expect(mockRemoveServer).toHaveBeenCalledWith("srv-1"));
  });

  it("marks an assigned server the agent overrides by name", async () => {
    workspaceMcp.assigned = [wsServer({ name: "linear", enabled: true })];
    renderTab({
      mcp_config: { mcpServers: { linear: { url: "https://agent.example" } } },
    });

    expect(await screen.findByText("Overridden")).toBeInTheDocument();
  });

  // A member without edit rights can still read what the agent runs with —
  // the inventory carries no credential material — but every write affordance
  // is hidden rather than left to 403 on click.
  it("hides the assignment controls from a viewer who cannot edit", async () => {
    workspaceMcp.assigned = [wsServer({ enabled: true })];
    workspaceMcp.library = [wsServer({ id: "srv-9", name: "pickable" })];
    render(
      <TestShell>
        <McpConfigTab
          agent={baseAgent}
          runtime={null}
          canEdit={false}
          onSave={vi.fn()}
        />
      </TestShell>,
    );

    expect(await screen.findByText("shared-linear")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Add from workspace/ })).toBeNull();
    expect(screen.queryByRole("switch")).toBeNull();
    expect(screen.queryByRole("button", { name: /Remove shared-linear/i })).toBeNull();
  });

  it("points at workspace Settings when the library is empty", async () => {
    renderTab({ mcp_config: null });

    expect(
      await screen.findByText(/no MCP servers to assign yet/i),
    ).toBeInTheDocument();
  });
});

// The runtime section's Overridden badge has to reflect the REAL precedence:
// runtime < (assigned workspace servers + the agent's own).
describe("McpConfigTab effective set", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    workspaceMcp.assigned = [];
    workspaceMcp.library = [];
  });

  it("marks a runtime server shadowed by an assigned server as overridden", async () => {
    workspaceMcp.assigned = [wsServer({ name: "fetch", enabled: true })];
    mockRuntimeCapabilities.mockResolvedValue({
      mcpSupported: true,
      mcpServers: [{ name: "fetch", transport: "stdio", enabled: true, source: "runtime" }],
    });
    renderTab({ mcp_config: null }, vi.fn(), onlineRuntime);

    expect(
      await screen.findByText("Overridden by Multica"),
    ).toBeInTheDocument();
  });

  // A switched-off assignment is not sent, so it shadows nothing.
  it("does not mark a runtime server overridden by a disabled assignment", async () => {
    workspaceMcp.assigned = [wsServer({ name: "fetch", enabled: false })];
    mockRuntimeCapabilities.mockResolvedValue({
      mcpSupported: true,
      mcpServers: [{ name: "fetch", transport: "stdio", enabled: true, source: "runtime" }],
    });
    renderTab({ mcp_config: null }, vi.fn(), onlineRuntime);

    // "fetch" renders twice — once as the (disabled) assignment, once as the
    // runtime's own server — which is exactly the state under test.
    await waitFor(() => expect(screen.getAllByText("fetch")).toHaveLength(2));
    expect(screen.queryByText("Overridden by Multica")).toBeNull();
  });

  // Same transport hazard as before, reached through the SAVED config: the
  // form emits `type: "http"`, so neither entry may be editable through it.
  // `websocket` is the one that catches using mcpTransport() for this.
  it.each([
    ["sse", { type: "sse", url: "https://sse.example" }],
    ["websocket", { type: "websocket", url: "wss://example.test" }],
  ])("edits an agent's %s server through JSON, not the form", async (_label, entry) => {
    const user = userEvent.setup();
    renderTab({ mcp_config: { mcpServers: { streamy: entry } } });

    await user.click(
      screen.getByRole("button", { name: /edit mcp server streamy/i }),
    );

    expect(screen.getByRole("tab", { name: "JSON" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(screen.getByRole("tab", { name: "Form" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });

  // The common shapes must stay on the form — this guard is about entries the
  // form would CHANGE, not a general retreat to the JSON editor.
  it.each([
    ["stdio", { command: "uvx" }],
    ["typed http", { type: "http", url: "https://mcp.example" }],
    ["untyped url", { url: "https://mcp.example" }],
  ])("keeps editing a %s server on the form", async (_label, entry) => {
    const user = userEvent.setup();
    renderTab({ mcp_config: { mcpServers: { normal: entry } } });

    await user.click(
      screen.getByRole("button", { name: /edit mcp server normal/i }),
    );

    expect(screen.getByRole("tab", { name: "Form" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });
});
