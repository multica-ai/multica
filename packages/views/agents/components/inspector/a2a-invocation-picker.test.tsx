// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import { A2aInvocationPicker } from "./a2a-invocation-picker";

// ActorAvatar pulls workspace context (useWorkspaceId) that this unit test
// doesn't provide; stub it — the picker logic under test doesn't depend on it.
vi.mock("../../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents },
};

function makeAgent(id: string, name: string, archived = false): Agent {
  return {
    id,
    workspace_id: "ws-1",
    runtime_id: "rt-1",
    name,
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_args: [],
    visibility: "private",
    permission_mode: "private",
    invocation_targets: [],
    status: "idle",
    max_concurrent_tasks: 1,
    model: "claude",
    skills: [],
    owner_id: "user-1",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    archived_at: archived ? "2026-01-02T00:00:00Z" : null,
    archived_by: null,
  };
}

const AGENTS = [
  makeAgent("self-id", "Self"),
  makeAgent("a-1", "Alpha"),
  makeAgent("a-2", "Beta"),
  makeAgent("a-3", "Gamma"),
  makeAgent("a-4", "Zeta Archived", true),
];

function renderPicker(
  props: Partial<React.ComponentProps<typeof A2aInvocationPicker>> = {},
) {
  const onChange = vi.fn();
  const utils = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <A2aInvocationPicker
        mode="default"
        grants={[]}
        agentId="self-id"
        agents={AGENTS}
        canEdit
        onChange={onChange}
        {...props}
      />
    </I18nProvider>,
  );
  return { ...utils, onChange };
}

describe("A2aInvocationPicker owner-only editing (NEX-24)", () => {
  beforeEach(() => cleanup());
  afterEach(() => cleanup());

  it("renders a static, non-interactive read-only state for non-owners", () => {
    renderPicker({
      canEdit: false,
      mode: "specific_agents",
      grants: ["a-1"],
    });
    expect(screen.queryByRole("radio")).toBeNull();
    expect(screen.getByTestId("a2a-readonly")).toBeInTheDocument();
    // Summary shows the current mode ("Specific agents" + owner-only hint).
    expect(screen.getByLabelText(
      "Only the agent owner can change which agents can call this agent.",
    )).toBeInTheDocument();
  });

  it("renders the four mutually exclusive modes for the owner", () => {
    renderPicker({ canEdit: true });
    expect(screen.getByRole("radio", { name: /^Not enabled/i })).toBeChecked();
    expect(
      screen.getByRole("radio", { name: /^Any agent/i }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("radio", { name: /^Squad leaders/i }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("radio", { name: /^Specific agents/i }),
    ).not.toBeChecked();
  });

  it("persists nothing until the owner saves; emits the mode change on save", () => {
    const { onChange } = renderPicker({ canEdit: true });
    fireEvent.click(screen.getByRole("radio", { name: /^Any agent/i }));
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(onChange).toHaveBeenCalledWith({
      a2a_invocation_mode: "any_agent",
      a2a_invocation_grants: [],
    });
  });

  it("maps 'Not enabled' back to the default enum", () => {
    const { onChange } = renderPicker({
      canEdit: true,
      mode: "squad_leaders",
    });
    fireEvent.click(screen.getByRole("radio", { name: /^Not enabled/i }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(onChange).toHaveBeenCalledWith({
      a2a_invocation_mode: "default",
      a2a_invocation_grants: [],
    });
  });

  it("reports an unsaved draft until the owner returns to the persisted mode", () => {
    const onDirtyChange = vi.fn();
    renderPicker({ canEdit: true, onDirtyChange });
    fireEvent.click(screen.getByRole("radio", { name: /^Any agent/i }));
    expect(onDirtyChange).toHaveBeenLastCalledWith(true);
    fireEvent.click(screen.getByRole("radio", { name: /^Not enabled/i }));
    expect(onDirtyChange).toHaveBeenLastCalledWith(false);
  });

  it("lists only non-archived workspace agents (excluding self) for specific_agents", () => {
    renderPicker({ canEdit: true });
    fireEvent.click(screen.getByRole("radio", { name: /^Specific agents/i }));
    expect(screen.queryByText("Self")).toBeNull();
    expect(screen.queryByText("Zeta Archived")).toBeNull();
    expect(screen.getByText("Alpha")).toBeInTheDocument();
    expect(screen.getByText("Beta")).toBeInTheDocument();
    expect(screen.getByText("Gamma")).toBeInTheDocument();
  });

  it("emits the selected whitelist for specific_agents", () => {
    const { onChange } = renderPicker({ canEdit: true });
    fireEvent.click(screen.getByRole("radio", { name: /^Specific agents/i }));
    fireEvent.click(screen.getByRole("checkbox", { name: /Alpha/i }));
    fireEvent.click(screen.getByRole("checkbox", { name: /Beta/i }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(onChange).toHaveBeenCalledWith({
      a2a_invocation_mode: "specific_agents",
      a2a_invocation_grants: ["a-1", "a-2"],
    });
  });

  it("pre-seeds the whitelist from persisted grants", () => {
    renderPicker({
      canEdit: true,
      mode: "specific_agents",
      grants: ["a-1"],
    });
    const alpha = screen.getByRole("checkbox", { name: /Alpha/i });
    expect(alpha).toBeChecked();
  });

  it("disables Save for specific_agents until at least one agent is selected", () => {
    const { onChange } = renderPicker({
      canEdit: true,
      mode: "specific_agents",
      grants: ["a-1"],
    });
    // Deselect the only grant → draft has an empty whitelist.
    fireEvent.click(screen.getByRole("checkbox", { name: /Alpha/i }));
    const save = screen.getByRole("button", { name: "Save" });
    expect(save).toBeDisabled();
    expect(onChange).not.toHaveBeenCalled();
  });

  it("switching away from specific_agents clears the whitelist payload", () => {
    const { onChange } = renderPicker({
      canEdit: true,
      mode: "specific_agents",
      grants: ["a-1"],
    });
    fireEvent.click(screen.getByRole("radio", { name: /^Squad leaders/i }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(onChange).toHaveBeenCalledWith({
      a2a_invocation_mode: "squad_leaders",
      a2a_invocation_grants: [],
    });
  });

  it("read-only state does not crash when grants/mode are undefined", () => {
    expect(() =>
      renderPicker({
        canEdit: false,
        mode: undefined as unknown as never,
        grants: undefined as unknown as never,
      }),
    ).not.toThrow();
    expect(screen.getByTestId("a2a-readonly")).toBeInTheDocument();
  });
});
