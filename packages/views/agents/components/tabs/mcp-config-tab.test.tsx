// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent, RuntimeDevice } from "@multica/core/types";
import { renderWithI18n } from "../../../test/i18n";

const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());

vi.mock("sonner", () => ({
  toast: {
    success: mockToastSuccess,
    error: mockToastError,
  },
}));

import { McpConfigTab } from "./mcp-config-tab";

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "agent-1",
    workspace_id: "ws-1",
    runtime_id: "runtime-1",
    name: "Agent",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_env: {},
    custom_args: [],
    custom_env_redacted: false,
    mcp_config: null,
    mcp_config_redacted: false,
    visibility: "workspace",
    status: "idle",
    max_concurrent_tasks: 1,
    model: "",
    owner_id: "user-1",
    skills: [],
    created_at: "2026-04-16T00:00:00Z",
    updated_at: "2026-04-16T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

function makeRuntime(overrides: Partial<RuntimeDevice> = {}): RuntimeDevice {
  return {
    id: "runtime-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Claude Code",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "claude",
    status: "online",
    device_info: "host",
    metadata: {},
    owner_id: "user-1",
    last_seen_at: "2026-04-16T00:00:00Z",
    created_at: "2026-04-16T00:00:00Z",
    updated_at: "2026-04-16T00:00:00Z",
    ...overrides,
  };
}

function renderTab({
  agent = makeAgent(),
  runtimeDevice = makeRuntime(),
  onSave = vi.fn(),
}: {
  agent?: Agent;
  runtimeDevice?: RuntimeDevice;
  onSave?: (updates: Partial<Agent>) => Promise<void>;
} = {}) {
  renderWithI18n(
    <McpConfigTab agent={agent} runtimeDevice={runtimeDevice} onSave={onSave} />,
  );
  return { onSave };
}

describe("McpConfigTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders a redacted read-only state without the editor", () => {
    const onSave = vi.fn();

    renderWithI18n(
      <McpConfigTab
        agent={makeAgent({ mcp_config_redacted: true })}
        runtimeDevice={makeRuntime()}
        readOnly
        onSave={onSave}
      />,
    );

    expect(screen.getByText("MCP config is hidden")).toBeInTheDocument();
    expect(
      screen.queryByRole("textbox", { name: "MCP config JSON" }),
    ).not.toBeInTheDocument();
    expect(onSave).not.toHaveBeenCalled();
  });

  it("warns when the selected runtime does not consume MCP config", () => {
    renderTab({ runtimeDevice: makeRuntime({ provider: "codex" }) });

    expect(
      screen.getByText("This runtime ignores MCP config"),
    ).toBeInTheDocument();
    expect(screen.getByText(/Only Claude Code currently consumes MCP config/))
      .toBeInTheDocument();
  });

  it("saves a parsed MCP config object", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn().mockResolvedValue(undefined);
    renderTab({ onSave });

    await user.click(screen.getByRole("textbox", { name: "MCP config JSON" }));
    await user.paste(
      '{"mcpServers":{"github":{"command":"npx","args":["-y","@modelcontextprotocol/server-github"]}}}',
    );
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({
        mcp_config: {
          mcpServers: {
            github: {
              command: "npx",
              args: ["-y", "@modelcontextprotocol/server-github"],
            },
          },
        },
      }),
    );
    expect(mockToastSuccess).toHaveBeenCalledWith("MCP config saved");
  });

  it("does not mark equivalent JSON with reordered keys as dirty", async () => {
    const user = userEvent.setup();
    renderTab({
      agent: makeAgent({
        mcp_config: {
          b: 2,
          a: {
            y: true,
            x: "value",
          },
        },
      }),
    });

    const editor = screen.getByRole("textbox", { name: "MCP config JSON" });
    await user.clear(editor);
    await user.click(editor);
    await user.paste('{"a":{"x":"value","y":true},"b":2}');

    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("rejects invalid JSON", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn().mockResolvedValue(undefined);
    renderTab({ onSave });

    await user.click(screen.getByRole("textbox", { name: "MCP config JSON" }));
    await user.paste('{"mcpServers":');
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(onSave).not.toHaveBeenCalled();
    expect(mockToastError).toHaveBeenCalledWith(
      "MCP config must be a JSON object",
    );
  });

  it("clears existing MCP config when the editor is empty", async () => {
    const user = userEvent.setup();
    const onSave = vi.fn().mockResolvedValue(undefined);
    renderTab({
      agent: makeAgent({
        mcp_config: {
          mcpServers: {
            filesystem: {
              command: "npx",
              args: ["-y", "@modelcontextprotocol/server-filesystem"],
            },
          },
        },
      }),
      onSave,
    });

    await user.click(screen.getByRole("button", { name: "Clear" }));
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith({ mcp_config: null }),
    );
  });
});
