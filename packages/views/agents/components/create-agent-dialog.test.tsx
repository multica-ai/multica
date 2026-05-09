// @vitest-environment jsdom

import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import type { Agent, RuntimeDevice, MemberWithUser } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

vi.mock("./model-dropdown", () => ({
  ModelDropdown: () => null,
}));

vi.mock("../../runtimes/components/provider-logo", () => ({
  ProviderLogo: () => null,
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

import { CreateAgentDialog } from "./create-agent-dialog";

function makeRuntime(over: Partial<RuntimeDevice>): RuntimeDevice {
  return {
    id: "rt-self",
    workspace_id: "ws-1",
    daemon_id: null,
    name: "Self runtime",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "self.local",
    metadata: {},
    owner_id: "user-self",
    visibility: "workspace",
    last_seen_at: null,
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    ...over,
  };
}

const members: MemberWithUser[] = [
  {
    id: "m1",
    workspace_id: "ws-1",
    user_id: "user-self",
    role: "member",
    name: "Self",
    email: "self@example.com",
    avatar_url: null,
    created_at: "2026-04-01T00:00:00Z",
  },
  {
    id: "m2",
    workspace_id: "ws-1",
    user_id: "user-other",
    role: "member",
    name: "Other",
    email: "other@example.com",
    avatar_url: null,
    created_at: "2026-04-01T00:00:00Z",
  },
];

function renderDialog(props: {
  isWorkspaceAdmin?: boolean;
  runtimes: RuntimeDevice[];
  currentUserId?: string | null;
  template?: Agent | null;
}) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <CreateAgentDialog
        runtimes={props.runtimes}
        members={members}
        currentUserId={
          props.currentUserId === undefined ? "user-self" : props.currentUserId
        }
        isWorkspaceAdmin={props.isWorkspaceAdmin ?? false}
        template={props.template ?? null}
        onClose={vi.fn()}
        onCreate={vi.fn()}
      />
    </I18nProvider>,
  );
}

function makeAgent(over: Partial<Agent>): Agent {
  return {
    id: "agent-template",
    workspace_id: "ws-1",
    runtime_id: "rt-self",
    name: "Template",
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
    owner_id: "user-self",
    skills: [],
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...over,
  };
}

describe("CreateAgentDialog runtime picker — admin gate", () => {
  const ownRuntime = makeRuntime({ id: "rt-self" });
  const otherRuntime = makeRuntime({
    id: "rt-other",
    name: "Other runtime",
    owner_id: "user-other",
    device_info: "other.local",
  });
  const runtimes = [ownRuntime, otherRuntime];

  it("non-admin: hides Mine/All filter tabs even when other runtimes exist", () => {
    renderDialog({ isWorkspaceAdmin: false, runtimes });
    // The "All" tab label comes from the agents namespace; if the tab were
    // rendered, both labels would appear. We assert it isn't.
    const allLabel = enAgents.create_dialog.runtime_filter_all;
    expect(screen.queryByText(allLabel)).toBeNull();
  });

  it("admin: shows Mine/All filter tabs when other runtimes exist", () => {
    renderDialog({ isWorkspaceAdmin: true, runtimes });
    const allLabel = enAgents.create_dialog.runtime_filter_all;
    expect(screen.getByText(allLabel)).toBeInTheDocument();
  });

  it("non-admin: even after toggling state to 'all', other runtimes stay hidden in the popover", async () => {
    renderDialog({ isWorkspaceAdmin: false, runtimes });
    const trigger = screen.getByRole("button", { name: /Self runtime/i });
    fireEvent.click(trigger);
    // The popover is now open. The other-owner runtime must not be visible.
    expect(screen.queryByText(/Other runtime/)).toBeNull();
  });

  it("non-admin with null currentUserId: shows no runtimes (no leak during auth hydration)", () => {
    renderDialog({
      isWorkspaceAdmin: false,
      currentUserId: null,
      runtimes,
    });
    // The trigger label falls back to runtime_none when nothing is selectable.
    expect(screen.queryByText(/Self runtime/)).toBeNull();
    expect(screen.queryByText(/Other runtime/)).toBeNull();
    // The Create button must be disabled — no selectable runtime means no
    // valid form state.
    const create = screen.getByRole("button", { name: /^Create$/i });
    expect(create).toBeDisabled();
  });

  it("non-admin duplicating an agent bound to someone else's runtime: silently rebinds to own runtime", () => {
    const template = makeAgent({
      runtime_id: "rt-other",
      owner_id: "user-other",
    });
    renderDialog({
      isWorkspaceAdmin: false,
      runtimes,
      template,
    });
    // The trigger should show the user's own runtime, not the template's.
    expect(screen.getByText(/Self runtime/)).toBeInTheDocument();
    expect(screen.queryByText(/Other runtime/)).toBeNull();
    // Create must be enabled because a valid runtime is selected.
    const create = screen.getByRole("button", { name: /^Create$/i });
    expect(create).not.toBeDisabled();
  });
});
