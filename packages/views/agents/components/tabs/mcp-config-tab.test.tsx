// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import { McpConfigTab } from "./mcp-config-tab";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

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
) {
  const result = render(
    <TestShell>
      <McpConfigTab
        agent={{ ...baseAgent, ...overrides }}
        runtime={null}
        onSave={onSave}
      />
    </TestShell>,
  );
  return { ...result, onSave };
}

describe("McpConfigTab", () => {
  beforeEach(() => vi.clearAllMocks());

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
});
