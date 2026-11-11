import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent } from "@multica/core/types";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { renderWithI18n } from "../../test/i18n";
import { QuickChatAgentRow } from "./quick-chat-agent-row";

const WS = "ws-test";

const h = vi.hoisted(() => ({
  chat: {
    floatingChatEnabled: true,
    quickChatAgentId: null as string | null,
    setQuickChatAgentId: vi.fn(),
  },
}));

vi.mock("@multica/core/chat", () => ({
  useChatStore: Object.assign(
    (selector: (state: typeof h.chat) => unknown) => selector(h.chat),
    { getState: () => h.chat },
  ),
}));
vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "user-1" } };
  return {
    useAuthStore: Object.assign(
      (selector: (s: typeof state) => unknown) => selector(state),
      { getState: () => state },
    ),
  };
});
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => ({ id: "ws-test" }) }));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: { getBaseUrl: () => "http://localhost" },
}));

function agent(id: string, name: string, overrides: Partial<Agent> = {}): Agent {
  return {
    id,
    name,
    owner_id: "user-1",
    workspace_id: WS,
    runtime_id: "runtime-1",
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
    model: "sonnet",
    skills: [],
    created_at: new Date(0).toISOString(),
    updated_at: new Date(0).toISOString(),
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

function renderRow() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  qc.setQueryData(agentListOptions(WS).queryKey, [
    agent("agent-1", "Kepler"),
    agent("agent-2", "Vega"),
    agent("agent-3", "Retired", { archived_at: new Date(0).toISOString() }),
  ]);
  qc.setQueryData(memberListOptions(WS).queryKey, [
    {
      id: "member-1",
      workspace_id: WS,
      user_id: "user-1",
      role: "owner",
      created_at: new Date(0).toISOString(),
      name: "Dev",
      email: "dev@localhost",
      avatar_url: null,
    },
  ]);
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <QuickChatAgentRow />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  h.chat.floatingChatEnabled = true;
  h.chat.quickChatAgentId = null;
});

describe("QuickChatAgentRow", () => {
  it("is unset by default and saves the agent that is picked", async () => {
    const user = userEvent.setup();
    renderRow();

    const select = screen.getByRole("combobox", { name: "Ask Multica agent" });
    expect(select).toHaveTextContent("Last used");

    await user.click(select);
    expect(screen.queryByRole("option", { name: "Retired" })).not.toBeInTheDocument();
    await user.click(await screen.findByRole("option", { name: "Vega" }));

    expect(h.chat.setQuickChatAgentId).toHaveBeenCalledWith("agent-2");
  });

  it("shows the saved agent and clears it with Last used", async () => {
    h.chat.quickChatAgentId = "agent-1";
    const user = userEvent.setup();
    renderRow();

    const select = screen.getByRole("combobox", { name: "Ask Multica agent" });
    expect(select).toHaveTextContent("Kepler");

    await user.click(select);
    await user.click(await screen.findByRole("option", { name: "Last used" }));

    expect(h.chat.setQuickChatAgentId).toHaveBeenCalledWith(null);
  });

  it("reads as unset when the saved agent is archived", () => {
    h.chat.quickChatAgentId = "agent-3";
    renderRow();

    expect(screen.getByRole("combobox", { name: "Ask Multica agent" })).toHaveTextContent("Last used");
  });

  it("is disabled while the floating chat is off", () => {
    h.chat.floatingChatEnabled = false;
    renderRow();

    expect(screen.getByRole("combobox", { name: "Ask Multica agent" })).toBeDisabled();
  });
});
