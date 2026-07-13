import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

// FIR-3091 slice 3 — the relocated isolation cards must persist to the SAME
// workspace.settings.agent_capabilities shape the retired "Agent capabilities"
// tab used, because agent claim (applyCapabilityPolicyToClaim → ApplyClaimPolicy,
// FIR-458) reads that exact blob. These tests pin the write shape so the UI move
// can never silently drift claim behaviour.

const mockUpdateWorkspace = vi.hoisted(() => vi.fn());
const workspace = {
  id: "ws-1",
  settings: { tasks_paused: true } as Record<string, unknown>,
};

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>("@multica/core/api");
  return {
    ...actual,
    api: {
      updateWorkspace: mockUpdateWorkspace,
    },
  };
});

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => workspace,
}));

vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role: "owner", isLoading: false }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: (wsId: string) => ({
    queryKey: ["agents", wsId],
    queryFn: async () => [{ id: "agent-1", name: "Sara" }],
  }),
  workspaceKeys: { list: () => ["workspaces"] },
}));

vi.mock("@multica/core/runtimes/queries", () => ({
  runtimeListOptions: (wsId: string) => ({
    queryKey: ["runtimes", wsId],
    queryFn: async () => [{ id: "rt-1", name: "sara.local", runtime_mode: "local", status: "online" }],
  }),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { CapabilityIsolationSections } from "./capability-isolation-sections";

function renderSections() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <CapabilityIsolationSections />
    </QueryClientProvider>,
  );
}

describe("CapabilityIsolationSections", () => {
  beforeEach(() => {
    mockUpdateWorkspace.mockReset();
    mockUpdateWorkspace.mockResolvedValue(workspace);
    workspace.settings = { tasks_paused: true };
  });

  it("renders the sandbox and blocked-MCP surfaces", async () => {
    renderSections();
    expect(await screen.findByText("Sandbox profiles")).toBeInTheDocument();
    expect(screen.getByText("Runtime overrides")).toBeInTheDocument();
    expect(screen.getByText("Blocked MCP servers")).toBeInTheDocument();
  });

  it("saves blocked MCP servers into the agent_capabilities blob, preserving unrelated settings", async () => {
    const user = userEvent.setup();
    renderSections();

    const input = await screen.findByPlaceholderText("github, browser");
    await user.clear(input);
    // Paste the full CSV in one change event — csvToList normalizes on every
    // onChange, so char-by-char typing would strip separators mid-entry (the
    // preserved behaviour of the original tab). Paste mirrors real usage.
    await user.click(input);
    await user.paste("github, browser");
    await user.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(mockUpdateWorkspace).toHaveBeenCalledTimes(1));
    const [wsId, payload] = mockUpdateWorkspace.mock.calls[0]!;
    expect(wsId).toBe("ws-1");
    // Unrelated workspace settings survive the merge.
    expect(payload.settings.tasks_paused).toBe(true);
    // Same storage key + per-agent shape the claim path reads (FIR-458).
    expect(payload.settings.agent_capabilities.agent_permissions["agent-1"]).toEqual({
      mcp_denied_servers: ["github", "browser"],
    });
  });

  it("saves the workspace sandbox default profile", async () => {
    const user = userEvent.setup();
    renderSections();

    await screen.findByText("Sandbox profiles");
    await user.click(screen.getByRole("button", { name: /use strict/i }));
    await user.click(screen.getByRole("button", { name: /save/i }));

    await waitFor(() => expect(mockUpdateWorkspace).toHaveBeenCalledTimes(1));
    const [, payload] = mockUpdateWorkspace.mock.calls[0]!;
    expect(payload.settings.agent_capabilities.default_profile_id).toBe("strict");
  });
});
