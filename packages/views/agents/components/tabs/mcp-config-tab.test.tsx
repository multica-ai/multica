// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
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
const workspaceMcp = vi.hoisted(() => ({
  data: { workspace_id: "ws-1", servers: [] as Array<Record<string, unknown>>, server_count: 0 },
}));

// The inherited section reads the workspace's shared servers. Stubbing the
// query options keeps this a pure render test — the real one would hit fetch.
vi.mock("@multica/core/workspace/queries", () => ({
  workspaceMcpConfigOptions: (wsId: string) => ({
    queryKey: ["workspaces", wsId, "mcp-config"],
    queryFn: () => Promise.resolve(workspaceMcp.data),
    enabled: wsId !== "",
  }),
}));

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
    workspaceMcp.data = { workspace_id: "ws-1", servers: [], server_count: 0 };
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

// The workspace layer (GH #6062) is what an agent runs with on top of its own
// servers, so the tab has to make it visible — including which shared servers
// this agent overrides, and when it inherits nothing at all.
describe("McpConfigTab workspace inheritance", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    workspaceMcp.data = { workspace_id: "ws-1", servers: [], server_count: 0 };
  });

  it("lists the servers inherited from the workspace", async () => {
    workspaceMcp.data = {
      workspace_id: "ws-1",
      servers: [{ name: "shared-linear", transport: "http", enabled: true }],
      server_count: 1,
    };
    renderTab({ mcp_config: null });

    expect(await screen.findByText("shared-linear")).toBeInTheDocument();
    expect(screen.getByText(/Inherited from workspace/)).toBeInTheDocument();
  });

  it("marks a shared server the agent overrides by name", async () => {
    workspaceMcp.data = {
      workspace_id: "ws-1",
      servers: [{ name: "linear", transport: "http", enabled: true }],
      server_count: 1,
    };
    renderTab({
      mcp_config: { mcpServers: { linear: { url: "https://agent.example" } } },
    });

    expect(await screen.findByText("Overridden")).toBeInTheDocument();
  });

  it("says an agent with a managed-but-empty config inherits nothing", async () => {
    workspaceMcp.data = {
      workspace_id: "ws-1",
      servers: [{ name: "shared-linear", transport: "http", enabled: true }],
      server_count: 1,
    };
    renderTab({ mcp_config: {} });

    expect(
      await screen.findByText(/inherits none of the workspace/i),
    ).toBeInTheDocument();
    // The shared server must NOT be listed as inherited: this agent opted out.
    expect(screen.queryByText("shared-linear")).toBeNull();
  });

  it("renders an empty state when the workspace shares nothing", async () => {
    renderTab({ mcp_config: null });

    expect(
      await screen.findByText(/workspace shares no MCP servers/),
    ).toBeInTheDocument();
  });
});

// The runtime section's Overridden badge has to reflect the REAL precedence —
// runtime < workspace < agent — and the whole inheritance story has to go
// quiet when the agent's own config is hidden from this viewer.
describe("McpConfigTab effective set", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    workspaceMcp.data = { workspace_id: "ws-1", servers: [], server_count: 0 };
  });

  it("marks a runtime server shadowed by a workspace server as overridden", async () => {
    workspaceMcp.data = {
      workspace_id: "ws-1",
      servers: [{ name: "fetch", transport: "http", enabled: true }],
      server_count: 1,
    };
    mockRuntimeCapabilities.mockResolvedValue({
      mcpSupported: true,
      mcpServers: [{ name: "fetch", transport: "stdio", enabled: true, source: "runtime" }],
    });
    renderTab({ mcp_config: null }, vi.fn(), onlineRuntime);

    // The shared server hides the runtime's same-named one, even though the
    // agent itself declares nothing.
    expect(
      await screen.findByText("Overridden by Multica"),
    ).toBeInTheDocument();
  });

  it("does not mark a runtime server overridden when the agent opted out", async () => {
    workspaceMcp.data = {
      workspace_id: "ws-1",
      servers: [{ name: "fetch", transport: "http", enabled: true }],
      server_count: 1,
    };
    mockRuntimeCapabilities.mockResolvedValue({
      mcpSupported: true,
      mcpServers: [{ name: "fetch", transport: "stdio", enabled: true, source: "runtime" }],
    });
    renderTab({ mcp_config: {} }, vi.fn(), onlineRuntime);

    expect(
      await screen.findByText(/inherits none of the workspace/i),
    ).toBeInTheDocument();
    expect(screen.queryByText("Overridden by Multica")).toBeNull();
  });

  // Same transport hazard on the agent side, reached through the SAVED config
  // rather than a summary: the form emits `type: "http"`, so neither entry may
  // be editable through it.
  //
  // `websocket` is the one that catches using mcpTransport() for this: that
  // display classifier buckets any entry with a `url` as http, so the unknown
  // type would look form-editable and be rewritten on save.
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

  it("says inheritance cannot be determined when mcp_config is redacted", async () => {
    workspaceMcp.data = {
      workspace_id: "ws-1",
      servers: [{ name: "shared-linear", transport: "http", enabled: true }],
      server_count: 1,
    };
    renderTab({ mcp_config: null, mcp_config_redacted: true });

    expect(
      await screen.findByText(/cannot be determined/i),
    ).toBeInTheDocument();
    // Claiming the agent inherits these would be a guess: a hidden config may
    // be an explicit empty document, or may override some of these names.
    expect(screen.queryByText("shared-linear")).toBeNull();
  });
});
