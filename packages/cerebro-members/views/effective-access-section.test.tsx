// JEH-1067 (Bundle B / PR-B) — EffectiveAccessSection tests.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const mockListGroups = vi.hoisted(() => vi.fn());
const mockListGroupMembers = vi.hoisted(() => vi.fn());
const mockListGroupAgents = vi.hoisted(() => vi.fn());
const mockListGroupRuntimes = vi.hoisted(() => vi.fn());
const mockListAgents = vi.hoisted(() => vi.fn());
const mockListRuntimes = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      listCerebroGroups: mockListGroups,
      listCerebroGroupMembers: mockListGroupMembers,
      listCerebroGroupAgents: mockListGroupAgents,
      listCerebroGroupRuntimes: mockListGroupRuntimes,
      listAgents: mockListAgents,
      listRuntimes: mockListRuntimes,
    },
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({
    queryKey: ["agentList"],
    queryFn: () => mockListAgents(),
  }),
}));

vi.mock("@multica/core/runtimes/queries", () => ({
  runtimeListOptions: () => ({
    queryKey: ["runtimeList"],
    queryFn: () => mockListRuntimes(),
  }),
}));

import { EffectiveAccessSection } from "./effective-access-section";

function renderWith(node: React.ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>{node}</QueryClientProvider>,
  );
}

describe("EffectiveAccessSection", () => {
  beforeEach(() => vi.clearAllMocks());

  it("short-circuits to admin copy when the role is owner", () => {
    renderWith(<EffectiveAccessSection userId="user-42" role="owner" />);
    expect(
      screen.getByTestId("effective-access-admin"),
    ).toBeInTheDocument();
    expect(screen.getByText(/Adgang: Alt/i)).toBeInTheDocument();
  });

  it("aggregates agents + runtimes via group membership with source chips", async () => {
    mockListGroups.mockResolvedValue([
      {
        id: "g-1",
        workspace_id: "ws-1",
        name: "ai-team",
        description: null,
        created_by: null,
        created_at: "",
        updated_at: "",
      },
    ]);
    mockListGroupMembers.mockResolvedValue([
      {
        group_id: "g-1",
        user_id: "user-42",
        added_by: null,
        added_at: "",
        user_name: "Anne",
        user_email: "a@x",
        user_avatar_url: null,
      },
    ]);
    mockListGroupAgents.mockResolvedValue([
      { group_id: "g-1", agent_id: "agent-1", granted_by: null, granted_at: "" },
    ]);
    mockListGroupRuntimes.mockResolvedValue([
      { group_id: "g-1", runtime_id: "rt-1", granted_by: null, granted_at: "" },
    ]);
    mockListAgents.mockResolvedValue([
      {
        id: "agent-1",
        workspace_id: "ws-1",
        runtime_id: "rt-1",
        name: "Kristian",
        description: "Sales",
        instructions: "",
        avatar_url: null,
        runtime_mode: "cloud",
        runtime_config: {},
        custom_env: {},
        custom_args: [],
        custom_env_redacted: false,
        visibility: "workspace",
        status: "idle",
        max_concurrent_tasks: 1,
        model: "claude",
        owner_id: null,
        skills: [],
        persona_sandbox: "",
        created_at: "",
        updated_at: "",
        archived_at: null,
        archived_by: null,
      },
    ]);
    mockListRuntimes.mockResolvedValue([
      {
        id: "rt-1",
        workspace_id: "ws-1",
        daemon_id: null,
        name: "Claude Dev",
        runtime_mode: "cloud",
        provider: "anthropic",
        launch_header: "",
        status: "online",
        device_info: "",
        metadata: {},
        owner_id: null,
        sandbox_enabled: null,
        persona_sandbox: "",
        capabilities: {},
        last_seen_at: null,
        created_at: "",
        updated_at: "",
      },
    ]);

    renderWith(<EffectiveAccessSection userId="user-42" role="member" />);

    await waitFor(() =>
      expect(
        screen.getByTestId("effective-access-agent-row"),
      ).toBeInTheDocument(),
    );
    expect(
      screen.getByTestId("effective-access-runtime-row"),
    ).toBeInTheDocument();
    expect(
      screen.getAllByTestId("effective-access-source-chip")[0]?.textContent,
    ).toMatch(/via gruppe: ai-team/);
  });
});
