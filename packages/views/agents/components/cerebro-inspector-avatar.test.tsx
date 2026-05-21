// @vitest-environment jsdom

// CEREBRO-PATCH(agent-avatar-generate): JEH-1879 — tests for the sibling that
// wires CerebroAvatarPicker into the agent detail inspector. Asserts the two
// behaviours that matter at this boundary: read-only mode hides the picker,
// and a successful generation flows through onUpdate as avatar_url.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup, waitFor } from "@testing-library/react";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const generateAgentAvatar = vi.fn();
vi.mock("@multica/core/api", () => ({
  api: {
    generateAgentAvatar: (...args: unknown[]) => generateAgentAvatar(...args),
  },
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => true,
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <div data-testid={`actor-avatar-${actorId}`} />
  ),
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

import { CerebroInspectorAvatar } from "./cerebro-inspector-avatar";

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "agent-1",
    workspace_id: "ws-1",
    runtime_id: "rt",
    name: "Existing Agent",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_env: {},
    custom_args: [],
    custom_env_redacted: false,
    visibility: "workspace",
    status: "idle",
    max_concurrent_tasks: 1,
    model: "",
    owner_id: "user-1",
    persona_sandbox: "",
    skills: [],
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

function renderInspectorAvatar(opts: {
  agent?: Agent;
  canEdit: boolean;
}) {
  const onUpdate = vi.fn().mockResolvedValue(undefined);
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <CerebroInspectorAvatar
        agent={opts.agent ?? makeAgent()}
        canEdit={opts.canEdit}
        onUpdate={onUpdate}
      />
    </I18nProvider>,
  );
  return { onUpdate };
}

describe("CerebroInspectorAvatar", () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => {
    cleanup();
    document.body.innerHTML = "";
  });

  it("renders a static actor avatar when canEdit is false", () => {
    renderInspectorAvatar({ canEdit: false });
    expect(screen.getByTestId("actor-avatar-agent-1")).toBeTruthy();
    expect(screen.queryByText(/Generate AI/i)).toBeNull();
  });

  it("exposes the AI generate trigger when canEdit is true", () => {
    renderInspectorAvatar({ canEdit: true });
    expect(screen.getByText(/Generate AI/i)).toBeTruthy();
  });

  it("persists a generated avatar via onUpdate", async () => {
    generateAgentAvatar.mockResolvedValue({ url: "https://cdn/avatar.png" });
    const { onUpdate } = renderInspectorAvatar({ canEdit: true });

    fireEvent.click(screen.getByText(/Generate AI/i));
    fireEvent.click(screen.getByText(/^Generate$/i));

    await waitFor(() => {
      expect(generateAgentAvatar).toHaveBeenCalledWith("Existing Agent", undefined);
    });
    await waitFor(() => {
      expect(onUpdate).toHaveBeenCalledWith({ avatar_url: "https://cdn/avatar.png" });
    });
  });
});
