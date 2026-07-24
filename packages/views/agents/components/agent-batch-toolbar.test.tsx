// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const updateAgentSpy = vi.hoisted(() =>
  vi.fn(async (_id: string, _patch: Record<string, unknown>) => ({})),
);
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
  const ui = (nextRows: AgentListRow[]) => (
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <AgentBatchToolbar
          rows={nextRows}
          members={[]}
          currentUserId="user-1"
          onClear={() => {}}
        />
      </I18nProvider>
    </QueryClientProvider>
  );
  const view = render(ui(rows));
  return {
    ...view,
    rerenderRows: (nextRows: AgentListRow[]) => view.rerender(ui(nextRows)),
  };
}

beforeEach(() => {
  updateAgentSpy.mockClear();
  updateAgentSpy.mockResolvedValue({});
});

describe("AgentBatchToolbar — action order", () => {
  it("renders Archive last, after the other batch actions", () => {
    // One archived + one active owned row surfaces all three actions at once.
    renderToolbar([
      makeRow("a", "user-1", { archived_at: "2026-01-01T00:00:00Z" }),
      makeRow("b", "user-1"),
    ]);

    // Labelled buttons in DOM order; the clear-selection button is icon-only.
    const actions = screen
      .getAllByRole("button")
      .map((b) => b.textContent?.trim())
      .filter((text): text is string => !!text);

    expect(actions).toEqual(["Restore", "Set access scope", "Archive"]);
  });
});

describe("AgentBatchToolbar — presence", () => {
  it("closes dialogs and removes the toolbar when selection becomes empty", async () => {
    const view = renderToolbar([makeRow("a", "user-1")]);

    fireEvent.click(screen.getByRole("button", { name: "Set access scope" }));
    await screen.findByText(/Applies to 1 agents/);

    view.rerenderRows([]);

    await waitFor(() => {
      expect(screen.queryByText(/Applies to 1 agents/)).not.toBeInTheDocument();
      expect(
        screen.queryByRole("button", { name: "Set access scope" }),
      ).not.toBeInTheDocument();
    });
  });
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

  it("keeps Apply disabled until a scope is picked", async () => {
    renderToolbar([makeRow("a", "user-1")]);

    fireEvent.click(
      screen.getByRole("button", { name: "Set access scope" }),
    );
    await screen.findByText(/Applies to 1 agents/);

    // No radio clicked yet — Apply must be disabled
    expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();
  });

  // Regression: AgentBatchToolbar <-> AccessPicker used to notify each other on
  // every render (unstable onReadyChange + a change object rebuilt per render,
  // plus a cleanup that cleared the parent on each dependency change). Picking a
  // scope never settled, so this whole flow could not complete.
  it("settles after picking a scope: Apply enables and stays enabled", async () => {
    renderToolbar([makeRow("a", "user-1")]);

    fireEvent.click(screen.getByRole("button", { name: "Set access scope" }));
    await screen.findByText(/Applies to 1 agents/);

    fireEvent.click(screen.getByRole("radio", { name: /Entire workspace/ }));

    const apply = screen.getByRole("button", { name: "Apply" });
    await waitFor(() => expect(apply).toBeEnabled());

    // Reaching a fixed point is the point: further flushes must not flip the
    // button back to disabled or re-enter the notify effect.
    for (let i = 0; i < 5; i++) {
      await act(async () => {});
      expect(apply).toBeEnabled();
      expect(
        screen.getByRole("radio", { name: /Entire workspace/ }),
      ).toBeChecked();
    }
  });

  it("applies Entire workspace to each owned agent exactly once", async () => {
    renderToolbar([
      makeRow("a", "user-1"),
      makeRow("b", "user-1"),
      makeRow("c", "user-2"), // not owned → must be skipped
    ]);

    fireEvent.click(screen.getByRole("button", { name: "Set access scope" }));
    await screen.findByText(/Applies to 2 agents/);

    fireEvent.click(screen.getByRole("radio", { name: /Entire workspace/ }));

    const apply = screen.getByRole("button", { name: "Apply" });
    await waitFor(() => expect(apply).toBeEnabled());

    fireEvent.click(apply);

    await waitFor(() => expect(updateAgentSpy).toHaveBeenCalledTimes(2));
    expect(updateAgentSpy.mock.calls.map((call) => call[0]).sort()).toEqual([
      "a",
      "b",
    ]);
    for (const call of updateAgentSpy.mock.calls) {
      expect(call[1]).toEqual({
        permission_mode: "public_to",
        invocation_targets: [{ target_type: "workspace" }],
      });
    }

    // One click, one write per owned agent — the dialog is gone and nothing
    // re-fires afterwards.
    await act(async () => {});
    expect(updateAgentSpy).toHaveBeenCalledTimes(2);
  });

  it("does not retain the previous selection after close and reopen", async () => {
    renderToolbar([makeRow("a", "user-1")]);

    fireEvent.click(screen.getByRole("button", { name: "Set access scope" }));
    await screen.findByText(/Applies to 1 agents/);
    fireEvent.click(screen.getByRole("radio", { name: /Entire workspace/ }));
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Apply" })).toBeEnabled(),
    );

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() =>
      expect(
        screen.queryByRole("button", { name: "Apply" }),
      ).not.toBeInTheDocument(),
    );

    fireEvent.click(screen.getByRole("button", { name: "Set access scope" }));
    await screen.findByText(/Applies to 1 agents/);

    expect(screen.getByRole("button", { name: "Apply" })).toBeDisabled();
    expect(
      screen.getByRole("radio", { name: /Entire workspace/ }),
    ).not.toBeChecked();
    expect(updateAgentSpy).not.toHaveBeenCalled();
  });
});
