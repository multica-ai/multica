// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

import { ConnectorsTab } from "./connectors-tab";

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
};

function renderTab(opts: {
  agent?: Agent;
  readOnly?: boolean;
  onSave?: (u: Partial<Agent>) => Promise<void>;
} = {}) {
  const onSave = opts.onSave ?? vi.fn().mockResolvedValue(undefined);
  const utils = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ConnectorsTab
        agent={opts.agent ?? baseAgent}
        readOnly={opts.readOnly}
        onSave={onSave}
      />
    </I18nProvider>,
  );
  return { ...utils, onSave };
}

describe("ConnectorsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders an empty editor when no config is set", () => {
    renderTab();
    const editor = screen.getByLabelText("MCP config JSON editor") as HTMLTextAreaElement;
    expect(editor.value).toBe("");
    // No clear button when nothing is configured.
    expect(screen.queryByRole("button", { name: /clear connectors/i })).toBeNull();
    // Save is disabled when the form is clean.
    expect(screen.getByRole("button", { name: /^save$/i })).toBeDisabled();
  });

  it("rejects invalid JSON inline and does not call onSave", async () => {
    const { onSave } = renderTab();
    const editor = screen.getByLabelText("MCP config JSON editor");
    fireEvent.change(editor, { target: { value: "{ not json" } });

    expect(await screen.findByRole("alert")).toHaveTextContent(/invalid json/i);
    expect(screen.getByRole("button", { name: /^save$/i })).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));
    expect(onSave).not.toHaveBeenCalled();
  });

  it("rejects JSON missing the mcpServers top-level key", () => {
    renderTab();
    const editor = screen.getByLabelText("MCP config JSON editor");
    fireEvent.change(editor, { target: { value: '{"servers": {}}' } });
    expect(screen.getByRole("alert")).toHaveTextContent(/missing top-level "mcpServers"/i);
    expect(screen.getByRole("button", { name: /^save$/i })).toBeDisabled();
  });

  it("rejects a server entry with the wrong shape", () => {
    renderTab();
    const editor = screen.getByLabelText("MCP config JSON editor");
    fireEvent.change(editor, {
      target: {
        value: JSON.stringify({ mcpServers: { gmail: { args: "not-an-array" } } }),
      },
    });
    expect(screen.getByRole("alert")).toHaveTextContent(/server "gmail" is invalid/i);
    expect(screen.getByRole("button", { name: /^save$/i })).toBeDisabled();
  });

  it("saves a valid config via onSave", async () => {
    const { onSave } = renderTab();
    const editor = screen.getByLabelText("MCP config JSON editor");
    const config = {
      mcpServers: {
        filesystem: {
          command: "npx",
          args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
        },
      },
    };
    fireEvent.change(editor, { target: { value: JSON.stringify(config) } });

    const save = screen.getByRole("button", { name: /^save$/i });
    expect(save).toBeEnabled();
    fireEvent.click(save);

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith({ mcp_config: config });
    });
  });

  it("sends mcp_config: null when the user clears the textarea and saves", async () => {
    // Pre-populated agent so the textarea has content to clear.
    const agent: Agent = {
      ...baseAgent,
      mcp_config: { mcpServers: { gmail: { command: "x" } } },
    };
    const { onSave } = renderTab({ agent });
    const editor = screen.getByLabelText("MCP config JSON editor");
    fireEvent.change(editor, { target: { value: "" } });

    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith({ mcp_config: null });
    });
  });

  it("clear button opens a confirm dialog and sends mcp_config: null when confirmed", async () => {
    const agent: Agent = {
      ...baseAgent,
      mcp_config: { mcpServers: { gmail: { command: "x" } } },
    };
    const { onSave } = renderTab({ agent });

    fireEvent.click(screen.getByRole("button", { name: /clear connectors/i }));
    fireEvent.click(screen.getByRole("button", { name: /^clear$/i }));

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledWith({ mcp_config: null });
    });
  });

  it("renders the redacted state for non-owners with a configured agent", () => {
    const agent: Agent = {
      ...baseAgent,
      mcp_config: null,
      mcp_config_redacted: true,
    };
    renderTab({ agent, readOnly: true });
    expect(screen.getByText(/connectors configured — contents hidden/i)).toBeInTheDocument();
    // No textarea in read-only mode.
    expect(screen.queryByLabelText("MCP config JSON editor")).toBeNull();
  });

  it("renders an empty read-only state when no config and not redacted", () => {
    renderTab({ readOnly: true });
    expect(screen.getByText(/no connectors configured/i)).toBeInTheDocument();
  });
});
