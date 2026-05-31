// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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

import { McpConfigTab } from "./mcp-config-tab";

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

function renderTab(
  overrides: Partial<Agent> = {},
  onSave = vi.fn().mockResolvedValue(undefined),
) {
  const agent = { ...baseAgent, ...overrides };
  const result = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <McpConfigTab agent={agent} onSave={onSave} />
    </I18nProvider>,
  );
  return { ...result, onSave };
}

describe("McpConfigTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders a read-only redacted state when the server omitted the value", () => {
    // mcp_config_redacted means the server knows there IS a config but
    // hid it from this caller. The tab must NOT expose the editor or
    // any input — even an empty textarea would let a non-privileged
    // member silently overwrite an admin-owned config on save.
    renderTab({ mcp_config: null, mcp_config_redacted: true });

    expect(screen.getByText(/hidden from your view/i)).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /save/i })).not.toBeInTheDocument();
  });

  it("shows the editor empty when no config is set, and Save stays disabled", () => {
    renderTab({ mcp_config: null });

    const editor = screen.getByLabelText(/MCP config JSON editor/i) as HTMLTextAreaElement;
    expect(editor.value).toBe("");

    expect(screen.getByRole("button", { name: /save/i })).toBeDisabled();
  });

  it("pretty-prints the existing config and saves a parsed object", async () => {
    const user = userEvent.setup();
    const stored = { mcpServers: { fetch: { command: "uvx" } } };
    const { onSave } = renderTab({ mcp_config: stored });

    const editor = screen.getByLabelText(/MCP config JSON editor/i) as HTMLTextAreaElement;
    expect(editor.value).toBe(JSON.stringify(stored, null, 2));

    // userEvent.type interprets `{` / `[` as keyboard modifiers, so a
    // raw JSON paste goes through fireEvent.change instead — the same
    // path the browser uses when the user pastes.
    const replacement = JSON.stringify({
      mcpServers: { fetch: { command: "npx" } },
    });
    fireEvent.change(editor, { target: { value: replacement } });

    const save = screen.getByRole("button", { name: /save/i });
    expect(save).toBeEnabled();
    await user.click(save);

    expect(onSave).toHaveBeenCalledTimes(1);
    // We pass the parsed object, not the raw string, so the backend
    // gets a real JSON shape and not an escaped string.
    expect(onSave).toHaveBeenCalledWith({
      mcp_config: { mcpServers: { fetch: { command: "npx" } } },
    });
  });

  it("clearing the editor saves null to wipe the column", async () => {
    const user = userEvent.setup();
    const { onSave } = renderTab({ mcp_config: { mcpServers: {} } });

    const editor = screen.getByLabelText(/MCP config JSON editor/i) as HTMLTextAreaElement;
    await user.clear(editor);

    const save = screen.getByRole("button", { name: /save/i });
    await user.click(save);

    // null is what the backend reads as "clear this column" — sending
    // an empty string or {} would either fail validation or store an
    // empty object, both of which surprise the user.
    expect(onSave).toHaveBeenCalledWith({ mcp_config: null });
  });

  it("disables Save and surfaces an inline error on invalid JSON", () => {
    const { onSave } = renderTab({ mcp_config: null });

    const editor = screen.getByLabelText(/MCP config JSON editor/i);
    fireEvent.change(editor, { target: { value: "{ not json" } });

    expect(screen.getByText(/Invalid JSON/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /save/i })).toBeDisabled();
    expect(onSave).not.toHaveBeenCalled();
  });

  it("rejects top-level arrays and primitives", () => {
    renderTab({ mcp_config: null });

    const editor = screen.getByLabelText(/MCP config JSON editor/i);
    fireEvent.change(editor, { target: { value: "[1,2,3]" } });

    expect(
      screen.getByText(/MCP config must be a JSON object/i),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /save/i })).toBeDisabled();
  });

  it("syncs the editor to a refreshed agent prop when the user hasn't edited", () => {
    // Reproduces the stale-editor bug: a background refetch / WS event swaps
    // in a newer `agent.mcp_config`, and the editor must follow it (so the
    // next Save writes the new value, not the old one). Comparing the draft
    // against the *previous* original — not the new one — is what makes this
    // work. Without the ref, the effect would self-defeat: on re-render the
    // draft already equals the new original, the equality check is true,
    // but the conditional only re-assigns `original` to itself, so a draft
    // that started life equal to the OLD original is never touched.
    const initial = { mcpServers: { fetch: { command: "uvx" } } };
    const updated = { mcpServers: { fetch: { command: "npx" } } };
    const agent = { ...baseAgent, mcp_config: initial };

    const { rerender } = render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <McpConfigTab agent={agent} onSave={vi.fn()} />
      </I18nProvider>,
    );

    const editor = screen.getByLabelText(
      /MCP config JSON editor/i,
    ) as HTMLTextAreaElement;
    expect(editor.value).toBe(JSON.stringify(initial, null, 2));

    rerender(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <McpConfigTab
          agent={{ ...agent, mcp_config: updated }}
          onSave={vi.fn()}
        />
      </I18nProvider>,
    );

    // Editor follows the new prop and the dirty hint is NOT shown — if it
    // were, the next Save would write the *old* JSON back over the new one.
    expect(editor.value).toBe(JSON.stringify(updated, null, 2));
    expect(screen.queryByText(/unsaved changes/i)).not.toBeInTheDocument();
  });

  it("preserves an in-flight edit when the agent prop is refreshed underneath", () => {
    // The mirror of the test above: if the user IS editing, a background
    // refresh must not clobber their draft.
    const initial = { mcpServers: { fetch: { command: "uvx" } } };
    const updated = { mcpServers: { fetch: { command: "npx" } } };
    const agent = { ...baseAgent, mcp_config: initial };

    const { rerender } = render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <McpConfigTab agent={agent} onSave={vi.fn()} />
      </I18nProvider>,
    );

    const editor = screen.getByLabelText(
      /MCP config JSON editor/i,
    ) as HTMLTextAreaElement;
    const draft = JSON.stringify({ mcpServers: { fetch: { command: "wip" } } });
    fireEvent.change(editor, { target: { value: draft } });
    expect(editor.value).toBe(draft);

    rerender(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <McpConfigTab
          agent={{ ...agent, mcp_config: updated }}
          onSave={vi.fn()}
        />
      </I18nProvider>,
    );

    expect(editor.value).toBe(draft);
  });

});
