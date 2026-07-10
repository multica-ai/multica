// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

// Agent Office (FIR-1775): the MCP tab no longer writes the agent row directly
// — it proposes a versioned change request. We mock the api surface the tab and
// its embedded Agent Office panels read, and assert the proposal payload.
const mockCreateCR = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector?: (s: { user: { id: string } | null }) => unknown) => {
    const state = { user: null };
    return selector ? selector(state) : state;
  },
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listMembers: vi.fn().mockResolvedValue([]),
    listAgentContextVersions: vi.fn().mockResolvedValue([]),
    listAgentContextChangeRequests: vi.fn().mockResolvedValue([]),
    createAgentContextChangeRequest: (...args: unknown[]) =>
      mockCreateCR(...args),
  },
}));

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
  persona_sandbox: "",
  skills: [],
  created_at: "2026-05-28T00:00:00Z",
  updated_at: "2026-05-28T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function renderTab(overrides: Partial<Agent> = {}) {
  const agent = { ...baseAgent, ...overrides };
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const result = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <McpConfigTab agent={agent} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return result;
}

// Open the propose dialog and submit it.
async function propose(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: /propose change/i }));
  await user.click(screen.getByRole("button", { name: /submit proposal/i }));
}

describe("McpConfigTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCreateCR.mockResolvedValue({ id: "cr-1" });
  });

  it("renders a read-only redacted state when the server omitted the value", () => {
    // mcp_config_redacted means the server knows there IS a config but hid it
    // from this caller. The tab must NOT expose the editor or a propose action.
    renderTab({ mcp_config: null, mcp_config_redacted: true });

    expect(screen.getByText(/hidden from your view/i)).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /propose change/i }),
    ).not.toBeInTheDocument();
  });

  it("shows the editor empty when no config is set, and Propose stays disabled", () => {
    renderTab({ mcp_config: null });

    const editor = screen.getByLabelText(/MCP config JSON editor/i) as HTMLTextAreaElement;
    expect(editor.value).toBe("");

    expect(
      screen.getByRole("button", { name: /propose change/i }),
    ).toBeDisabled();
  });

  it("pretty-prints the existing config and proposes a parsed object", async () => {
    const user = userEvent.setup();
    const stored = { mcpServers: { fetch: { command: "uvx" } } };
    renderTab({ mcp_config: stored });

    const editor = screen.getByLabelText(/MCP config JSON editor/i) as HTMLTextAreaElement;
    expect(editor.value).toBe(JSON.stringify(stored, null, 2));

    // userEvent.type treats `{`/`[` as modifiers, so a raw JSON paste goes
    // through fireEvent.change — the same path the browser uses on paste.
    const replacement = JSON.stringify({
      mcpServers: { fetch: { command: "npx" } },
    });
    fireEvent.change(editor, { target: { value: replacement } });

    await propose(user);

    await waitFor(() => expect(mockCreateCR).toHaveBeenCalledTimes(1));
    // We pass the parsed object so the backend gets real JSON, not a string.
    const payload = mockCreateCR.mock.calls[0]?.[1];
    expect(payload.mcp_config).toEqual({
      mcpServers: { fetch: { command: "npx" } },
    });
    expect(payload.proposed_version).toBe("1.0.1");
  });

  it("clearing the editor proposes null to wipe the column", async () => {
    const user = userEvent.setup();
    renderTab({ mcp_config: { mcpServers: {} } });

    const editor = screen.getByLabelText(/MCP config JSON editor/i) as HTMLTextAreaElement;
    await user.clear(editor);

    await propose(user);

    // null is the explicit "clear this column" signal — the server tells it
    // apart from an omitted field by the key being present.
    await waitFor(() => expect(mockCreateCR).toHaveBeenCalledTimes(1));
    const payload = mockCreateCR.mock.calls[0]?.[1];
    expect(payload.mcp_config).toBeNull();
  });

  it("disables Propose and surfaces an inline error on invalid JSON", () => {
    renderTab({ mcp_config: null });

    const editor = screen.getByLabelText(/MCP config JSON editor/i);
    fireEvent.change(editor, { target: { value: "{ not json" } });

    expect(screen.getByText(/Invalid JSON/i)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /propose change/i }),
    ).toBeDisabled();
    expect(mockCreateCR).not.toHaveBeenCalled();
  });

  it("rejects top-level arrays and primitives", () => {
    renderTab({ mcp_config: null });

    const editor = screen.getByLabelText(/MCP config JSON editor/i);
    fireEvent.change(editor, { target: { value: "[1,2,3]" } });

    expect(
      screen.getByText(/MCP config must be a JSON object/i),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /propose change/i }),
    ).toBeDisabled();
  });

  it("syncs the editor to a refreshed agent prop when the user hasn't edited", () => {
    // A background refetch / WS event swaps in a newer agent.mcp_config; the
    // editor must follow it so the next proposal carries the new value.
    const initial = { mcpServers: { fetch: { command: "uvx" } } };
    const updated = { mcpServers: { fetch: { command: "npx" } } };
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const agent = { ...baseAgent, mcp_config: initial };

    const { rerender } = render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={queryClient}>
          <McpConfigTab agent={agent} />
        </QueryClientProvider>
      </I18nProvider>,
    );

    const editor = screen.getByLabelText(
      /MCP config JSON editor/i,
    ) as HTMLTextAreaElement;
    expect(editor.value).toBe(JSON.stringify(initial, null, 2));

    rerender(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={queryClient}>
          <McpConfigTab agent={{ ...agent, mcp_config: updated }} />
        </QueryClientProvider>
      </I18nProvider>,
    );

    expect(editor.value).toBe(JSON.stringify(updated, null, 2));
    expect(screen.queryByText(/unsaved changes/i)).not.toBeInTheDocument();
  });

  it("preserves an in-flight edit when the agent prop is refreshed underneath", () => {
    const initial = { mcpServers: { fetch: { command: "uvx" } } };
    const updated = { mcpServers: { fetch: { command: "npx" } } };
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const agent = { ...baseAgent, mcp_config: initial };

    const { rerender } = render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={queryClient}>
          <McpConfigTab agent={agent} />
        </QueryClientProvider>
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
        <QueryClientProvider client={queryClient}>
          <McpConfigTab agent={{ ...agent, mcp_config: updated }} />
        </QueryClientProvider>
      </I18nProvider>,
    );

    expect(editor.value).toBe(draft);
  });
});
