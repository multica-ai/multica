// @vitest-environment jsdom

// JEH-1066 — pin the visibility/trigger split for the autopilot agent
// picker. Workspace members must SEE every agent in the workspace; group-
// locked agents (`can_trigger === false`) render with a lock icon and a
// disabled row instead of disappearing from the picker.

import { describe, it, expect, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAutopilots from "../../../locales/en/autopilots.json";
import enIssues from "../../../locales/en/issues.json";
import enAgents from "../../../locales/en/agents.json";

const TEST_RESOURCES = {
  en: {
    common: enCommon,
    autopilots: enAutopilots,
    issues: enIssues,
    agents: enAgents,
  },
};

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("../../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid={`avatar-${actorId}`} />
  ),
}));

import { AgentPicker } from "./agent-picker";

function makeAgent(overrides: Partial<Agent> & { id: string; name: string }): Agent {
  return {
    workspace_id: "ws-1",
    runtime_id: "runtime-1",
    description: "",
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
    model: "",
    owner_id: "user-1",
    skills: [],
    created_at: "2026-05-12T00:00:00Z",
    updated_at: "2026-05-12T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

function renderPicker(agents: Agent[], onChange = vi.fn()) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(["workspaces", "ws-1", "agents"], agents);
  const utils = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <AgentPicker agentId={null} onChange={onChange} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { ...utils, onChange };
}

describe("AgentPicker — group-locked agents", () => {
  it("renders every agent (locked + unlocked) and marks the locked one disabled", async () => {
    const user = userEvent.setup();
    const allowed = makeAgent({ id: "agent-allowed", name: "Allowed Agent", can_trigger: true });
    const locked = makeAgent({ id: "agent-locked", name: "Drop Tables Agent", can_trigger: false });
    renderPicker([allowed, locked]);

    // Open the picker.
    await user.click(screen.getByRole("button"));

    // Both agents must appear — the whole point of the visibility/trigger
    // split is that the locked agent is *visible* in the picker.
    expect(screen.getByText("Allowed Agent")).toBeInTheDocument();
    expect(screen.getByText("Drop Tables Agent")).toBeInTheDocument();

    // The allowed row is a clickable button; the locked row is rendered as
    // a disabled button so a click is a no-op.
    const allowedRow = screen.getByText("Allowed Agent").closest("button");
    const lockedRow = screen.getByText("Drop Tables Agent").closest("button");
    expect(allowedRow).not.toBeNull();
    expect(lockedRow).not.toBeNull();
    expect(allowedRow).not.toBeDisabled();
    expect(lockedRow).toBeDisabled();
  });

  it("does not invoke onChange when the locked agent is clicked", async () => {
    const user = userEvent.setup();
    const locked = makeAgent({ id: "agent-locked", name: "Drop Tables Agent", can_trigger: false });
    const { onChange } = renderPicker([locked]);

    await user.click(screen.getByRole("button"));
    const lockedRow = screen.getByText("Drop Tables Agent").closest("button");
    // userEvent respects the disabled attribute and refuses to click — that
    // is exactly the assertion we want (the click is suppressed by the DOM,
    // not by our event handler).
    expect(lockedRow).toBeDisabled();
    expect(onChange).not.toHaveBeenCalled();
  });

  it("renders no lock when can_trigger is true (or missing for older servers)", async () => {
    const user = userEvent.setup();
    const allowed = makeAgent({ id: "agent-1", name: "Allowed Agent", can_trigger: true });
    // `can_trigger` missing on the wire (older servers) — UI must treat it
    // as triggerable so we never silently lock the entire picker on legacy
    // deployments.
    const undefinedFlag = makeAgent({ id: "agent-2", name: "Legacy Agent" });
    renderPicker([allowed, undefinedFlag]);

    await user.click(screen.getByRole("button"));
    const allowedRow = screen.getByText("Allowed Agent").closest("button");
    const legacyRow = screen.getByText("Legacy Agent").closest("button");
    expect(allowedRow).not.toBeDisabled();
    expect(legacyRow).not.toBeDisabled();
  });
});
