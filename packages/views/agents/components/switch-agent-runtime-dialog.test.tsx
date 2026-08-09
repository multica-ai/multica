// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent, AgentRuntime } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const migrateSpy = vi.hoisted(() => vi.fn());
const mutateAsyncSpy = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: { migrateAgentsToRuntime: migrateSpy },
}));
vi.mock("@multica/core/runtimes/mutations", () => ({
  useMigrateAgentsToRuntime: () => ({
    mutateAsync: mutateAsyncSpy,
    isPending: false,
  }),
}));
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <div>avatar</div>,
}));
// The pickers' own behaviour is covered by their component tests; here we only
// need deterministic ways to choose a target / model / thinking level.
vi.mock("./inspector/runtime-picker", () => ({
  RuntimePicker: ({ onChange }: { onChange: (id: string) => void }) => (
    <button type="button" onClick={() => onChange("rt-target")}>
      pick-target
    </button>
  ),
}));
vi.mock("./inspector/model-picker", () => ({
  ModelPicker: ({
    value,
    onChange,
  }: {
    value: string;
    onChange: (id: string) => void;
  }) => (
    <button type="button" onClick={() => onChange("model-x")}>
      pick-model:{value || "none"}
    </button>
  ),
}));
vi.mock("./inspector/thinking-prop-row", () => ({
  ThinkingSettingField: ({
    model,
    onChange,
  }: {
    model: string;
    onChange: (level: string) => void;
  }) =>
    model ? (
      <button type="button" onClick={() => onChange("high")}>
        pick-thinking
      </button>
    ) : null,
}));
vi.mock("./inspector/service-tier-setting-field", () => ({
  ServiceTierSettingField: () => null,
}));
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
}));

import { SwitchAgentRuntimeDialog } from "./switch-agent-runtime-dialog";

function makeAgent(id: string, overrides: Partial<Agent> = {}): Agent {
  return {
    id,
    workspace_id: "ws-1",
    runtime_id: "rt-source",
    name: `Agent ${id}`,
    description: "",
    model: "",
    archived_at: null,
    custom_env_key_count: 0,
    ...overrides,
  } as Agent;
}

const RUNTIMES = [
  { id: "rt-target", name: "Target", status: "online" },
] as unknown as AgentRuntime[];

function renderDialog(agents: Agent[], extraProps: Record<string, unknown> = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <SwitchAgentRuntimeDialog
          open
          onOpenChange={() => {}}
          agents={agents}
          runtimes={RUNTIMES}
          members={[]}
          currentUserId="user-1"
          wsId="ws-1"
          {...extraProps}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

const PREVIEW = {
  target_runtime_id: "rt-target",
  dry_run: true,
  migrated: [{ agent_id: "a", name: "Agent a" }],
  skipped: [],
  tasks_migrated: 2,
  tasks_staying_active: 3,
};

beforeEach(() => {
  migrateSpy.mockReset();
  migrateSpy.mockResolvedValue(PREVIEW);
  mutateAsyncSpy.mockReset();
  mutateAsyncSpy.mockResolvedValue({
    ...PREVIEW,
    dry_run: false,
    tasks_migrated: 2,
  });
});

describe("SwitchAgentRuntimeDialog — one dialog for one agent and for many", () => {
  it("names the agent when a single agent is passed and counts them when several are", () => {
    const single = renderDialog([makeAgent("a")]);
    expect(
      screen.getByText(/Switch runtime or model for "Agent a"/),
    ).toBeTruthy();
    single.unmount();

    renderDialog([makeAgent("a"), makeAgent("b")]);
    // The bulk title also feeds the sr-only dialog description, so it appears
    // twice; the visible heading is what matters here.
    expect(
      screen.getAllByText(/Switch runtime or model for 2 agents/).length,
    ).toBeGreaterThan(0);
  });

  it("submits the same request shape for one agent and for many", async () => {
    const single = renderDialog([makeAgent("a")]);
    fireEvent.click(screen.getByText("pick-target"));
    await screen.findByText(/2 queued tasks move to the new runtime/);
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() => expect(mutateAsyncSpy).toHaveBeenCalled());
    expect(mutateAsyncSpy.mock.calls[0]?.[0]).toMatchObject({
      targetRuntimeId: "rt-target",
      agentIds: ["a"],
    });
    single.unmount();

    mutateAsyncSpy.mockClear();
    renderDialog([makeAgent("a"), makeAgent("b")]);
    fireEvent.click(screen.getByText("pick-target"));
    await screen.findByText(/2 queued tasks move to the new runtime/);
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() => expect(mutateAsyncSpy).toHaveBeenCalled());
    expect(mutateAsyncSpy.mock.calls[0]?.[0]).toMatchObject({
      agentIds: ["a", "b"],
    });
  });
});

describe("SwitchAgentRuntimeDialog — consequence summary", () => {
  it("asks the server for the task split instead of deriving it locally", async () => {
    renderDialog([makeAgent("a")]);
    // Nothing is knowable before a target is chosen, so no preview until then.
    expect(migrateSpy).not.toHaveBeenCalled();

    fireEvent.click(screen.getByText("pick-target"));
    await waitFor(() => expect(migrateSpy).toHaveBeenCalled());
    expect(migrateSpy.mock.calls[0]?.[0]).toBe("rt-target");
    expect(migrateSpy.mock.calls[0]?.[1]).toMatchObject({ dry_run: true });
  });

  it("separates tasks that move from tasks that stay with a running daemon", async () => {
    renderDialog([makeAgent("a")]);
    fireEvent.click(screen.getByText("pick-target"));

    // The two groups are stated separately and never summed: 'queued' and
    // 'deferred' travel, while 'dispatched' / 'running' /
    // 'waiting_local_directory' finish where they are.
    expect(
      await screen.findByText(/2 queued tasks move to the new runtime/),
    ).toBeTruthy();
    expect(
      screen.getByText(/3 running tasks stay on their current runtime/),
    ).toBeTruthy();
  });

  it("hides the zero-task line instead of stating '0 tasks will move'", async () => {
    migrateSpy.mockResolvedValue({
      ...PREVIEW,
      tasks_migrated: 0,
      tasks_staying_active: 0,
    });
    renderDialog([makeAgent("a")]);
    fireEvent.click(screen.getByText("pick-target"));

    await waitFor(() => expect(migrateSpy).toHaveBeenCalled());
    await waitFor(() =>
      expect(screen.queryByText(/queued tasks? move/)).toBeNull(),
    );
  });
});

describe("SwitchAgentRuntimeDialog — optional model", () => {
  it("shows the model section only after a target is picked", async () => {
    renderDialog([makeAgent("a")]);
    expect(screen.queryByText("pick-model:none")).toBeNull();

    fireEvent.click(screen.getByText("pick-target"));
    expect(await screen.findByText("pick-model:none")).toBeTruthy();
  });

  it("re-asks the preview with the chosen model and submits model + thinking", async () => {
    renderDialog([makeAgent("a")]);
    fireEvent.click(screen.getByText("pick-target"));
    await waitFor(() => expect(migrateSpy).toHaveBeenCalledTimes(1));

    // Picking a model changes the server-side classification (in-place
    // updates become eligible), so the dry run must be re-asked with it.
    fireEvent.click(screen.getByText("pick-model:none"));
    await waitFor(() => expect(migrateSpy).toHaveBeenCalledTimes(2));
    expect(migrateSpy.mock.calls[1]?.[1]).toMatchObject({ model: "model-x" });

    fireEvent.click(await screen.findByText("pick-thinking"));
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await waitFor(() => expect(mutateAsyncSpy).toHaveBeenCalled());
    expect(mutateAsyncSpy.mock.calls[0]?.[0]).toMatchObject({
      model: "model-x",
      thinkingLevel: "high",
    });
  });

  it("never sends thinking without a model", async () => {
    renderDialog([makeAgent("a")]);
    fireEvent.click(screen.getByText("pick-target"));
    await screen.findByText(/2 queued tasks move to the new runtime/);
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() => expect(mutateAsyncSpy).toHaveBeenCalled());
    const body = mutateAsyncSpy.mock.calls[0]?.[0];
    expect(body.model).toBeUndefined();
    expect(body.thinkingLevel).toBeUndefined();
    expect(body.serviceTier).toBeUndefined();
  });
});

describe("SwitchAgentRuntimeDialog — confirm stays clickable and explains itself", () => {
  it("answers a click without a target with the blocker instead of submitting", async () => {
    renderDialog([makeAgent("a")]);

    const apply = screen.getByRole("button", { name: "Apply" });
    expect(apply.hasAttribute("disabled")).toBe(false);
    fireEvent.click(apply);

    expect(
      await screen.findByText(/Pick a target runtime first/),
    ).toBeTruthy();
    expect(mutateAsyncSpy).not.toHaveBeenCalled();
  });

  it("clears the blocker once the user fixes the input", async () => {
    renderDialog([makeAgent("a")]);
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    await screen.findByText(/Pick a target runtime first/);

    fireEvent.click(screen.getByText("pick-target"));
    await waitFor(() =>
      expect(screen.queryByText(/Pick a target runtime first/)).toBeNull(),
    );
  });
});

describe("SwitchAgentRuntimeDialog — Runtime detail entry point", () => {
  it("forwards the source runtime so the server can refuse a drifted plan", async () => {
    renderDialog([makeAgent("a")], { expectedSourceRuntimeId: "rt-source" });
    fireEvent.click(screen.getByText("pick-target"));
    await screen.findByText(/2 queued tasks move to the new runtime/);
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() => expect(mutateAsyncSpy).toHaveBeenCalled());
    expect(mutateAsyncSpy.mock.calls[0]?.[0]).toMatchObject({
      expectedSourceRuntimeId: "rt-source",
    });
  });

  it("re-asks the server when the user excludes an agent from the selection", async () => {
    renderDialog([makeAgent("a"), makeAgent("b")]);
    fireEvent.click(screen.getByText("pick-target"));
    await waitFor(() => expect(migrateSpy).toHaveBeenCalledTimes(1));

    // Unchecking must invalidate the previous counts rather than leaving the
    // dialog describing a set the user has since changed.
    fireEvent.click(screen.getByText("Agent b"));
    await waitFor(() => expect(migrateSpy).toHaveBeenCalledTimes(2));
    expect(migrateSpy.mock.calls[1]?.[1]).toMatchObject({ agent_ids: ["a"] });
  });
});
