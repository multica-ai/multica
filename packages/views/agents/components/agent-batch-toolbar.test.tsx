// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const updateAgentSpy = vi.hoisted(() => vi.fn(async () => ({})));
const archiveSpy = vi.hoisted(() => vi.fn(async () => ({})));
const restoreSpy = vi.hoisted(() => vi.fn(async () => ({})));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/api", () => ({
  api: {
    updateAgent: updateAgentSpy,
    archiveAgent: archiveSpy,
    restoreAgent: restoreSpy,
  },
}));
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <div>avatar</div>,
}));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

import { AgentBatchToolbar } from "./agent-batch-toolbar";
import type { AgentListRow } from "./agents-page";

function makeAgent(
  id: string,
  ownerId: string | null,
  overrides: Record<string, unknown> = {},
) {
  return {
    id,
    workspace_id: "ws-1",
    runtime_id: "rt-1",
    name: `Agent ${id}`,
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local" as const,
    runtime_config: {},
    max_concurrent_tasks: 1,
    owner_id: ownerId,
    archived_at: null,
    custom_args: [] as string[],
    visibility: "private" as const,
    permission_mode: "private" as const,
    invocation_targets: [] as unknown[],
    model: "claude",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    status: "idle" as const,
    skills: [] as unknown[],
    archived_by: null,
    ...overrides,
  };
}

function makeRow(
  id: string,
  ownerId: string | null,
  overrides: Record<string, unknown> = {},
): AgentListRow {
  const agent = makeAgent(id, ownerId, overrides);
  return {
    agent: agent as AgentListRow["agent"],
    runtime: null,
    presence: null,
    activity: null,
    runCount: 0,
    lastActiveDays: null,
    owner: ownerId
      ? ({ user_id: ownerId, name: `Owner ${ownerId}`, email: "" } as AgentListRow["owner"])
      : null,
    isOwnedByMe: ownerId === "user-1",
    canManage: ownerId === "user-1",
  };
}

function renderToolbar(rows: AgentListRow[]) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <AgentBatchToolbar
          rows={rows}
          members={[]}
          currentUserId="user-1"
          onClear={() => {}}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  updateAgentSpy.mockClear();
  updateAgentSpy.mockResolvedValue({});
});

describe("AgentBatchToolbar — bulk Set access scope", () => {
  it("shows the Set access scope button for active owned agents", () => {
    renderToolbar([makeRow("a", "user-1")]);
    expect(
      screen.getByRole("button", { name: "Set access scope" }),
    ).toBeInTheDocument();
  });

  it("renders Applies to N with skip count in the dialog", async () => {
    renderToolbar([
      makeRow("a", "user-1"),
      makeRow("b", "user-1"),
      makeRow("c", "user-2"), // not owned → skipped
    ]);

    fireEvent.click(
      screen.getByRole("button", { name: "Set access scope" }),
    );

    await screen.findByText(/Applies to 2 agents/);
    expect(
      screen.getByText(/1 skipped/),
    ).toBeInTheDocument();
  });

  // The interactive flow (pick Workspace → Apply enabled → updateAgent
  // called per owned row) is covered by access-picker-bulk-commit.test.tsx
  // (picker commit + onReadyChange) + the toolbar integration tests here.

  it("keeps Apply disabled until a scope is picked", async () => {
    renderToolbar([makeRow("a", "user-1")]);

    fireEvent.click(
      screen.getByRole("button", { name: "Set access scope" }),
    );
    await screen.findByText(/Applies to 1 agents/);

    // No radio clicked yet — Apply must be disabled
    expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();
  });
});
