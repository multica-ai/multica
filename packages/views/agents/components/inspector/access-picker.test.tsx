// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import type {
  AgentInvocationTarget,
  MemberWithUser,
} from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import enIssues from "../../../locales/en/issues.json";

import { AccessPicker } from "./access-picker";

// ActorAvatar pulls workspace context (useWorkspaceId) that this unit test
// doesn't provide; stub it — the picker logic under test doesn't depend on it.
vi.mock("../../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, issues: enIssues },
};

const MEMBERS = [
  { user_id: "u1", name: "Alice", role: "member" },
  { user_id: "u2", name: "Bob", role: "member" },
] as unknown as MemberWithUser[];

function renderPicker(
  props: Partial<React.ComponentProps<typeof AccessPicker>> = {},
) {
  const onChange = vi.fn();
  const utils = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <AccessPicker
        permissionMode="private"
        invocationTargets={[]}
        visibility="private"
        members={MEMBERS}
        canEdit
        onChange={onChange}
        {...props}
      />
    </I18nProvider>,
  );
  return { ...utils, onChange };
}

describe("AccessPicker owner-only editing (MUL-3963)", () => {
  beforeEach(() => cleanup());
  afterEach(() => cleanup());

  it("renders a static, non-interactive read-only state for non-owners", () => {
    const targets: AgentInvocationTarget[] = [
      { target_type: "workspace", target_id: "ws-1" },
    ];
    renderPicker({
      canEdit: false,
      permissionMode: "public_to",
      invocationTargets: targets,
      visibility: "workspace",
    });

    // No clickable trigger — a non-owner can never open the picker.
    expect(screen.queryByRole("button")).toBeNull();
    // The current access is still shown…
    expect(screen.getByTestId("access-readonly")).toBeInTheDocument();
    expect(screen.getByText("Workspace")).toBeInTheDocument();
    // …with the owner-only explanation surfaced as the accessible label.
    expect(
      screen.getByLabelText(
        "Only the agent owner can change who can run this agent.",
      ),
    ).toBeInTheDocument();
  });

  it("renders an interactive trigger for the owner", () => {
    renderPicker({ canEdit: true });
    expect(screen.getByRole("button")).toBeInTheDocument();
    // Private is the default summary.
    expect(screen.getAllByText("Only me").length).toBeGreaterThan(0);
  });

  it("owner can pick a specific member, emitting a public_to member target", () => {
    const { onChange } = renderPicker({ canEdit: true });
    fireEvent.click(screen.getByRole("button"));
    // Checkbox order in the open popover: [0] workspace, [1] Alice, [2] Bob.
    const boxes = screen.getAllByRole("checkbox");
    fireEvent.click(boxes[1]!);
    expect(onChange).toHaveBeenCalledWith({
      permission_mode: "public_to",
      invocation_targets: [{ target_type: "member", target_id: "u1" }],
    });
  });

  it("owner can stack workspace + a member (mixed, multi-select)", () => {
    // Start from a member target; toggling the workspace checkbox must ADD a
    // workspace target rather than replacing the member one.
    const { onChange } = renderPicker({
      canEdit: true,
      permissionMode: "public_to",
      invocationTargets: [{ target_type: "member", target_id: "u1" }],
      visibility: "private",
    });
    fireEvent.click(screen.getByRole("button"));
    const boxes = screen.getAllByRole("checkbox");
    // [0] is the "Everyone in workspace" toggle.
    fireEvent.click(boxes[0]!);
    expect(onChange).toHaveBeenCalledWith({
      permission_mode: "public_to",
      invocation_targets: [
        { target_type: "workspace" },
        { target_type: "member", target_id: "u1" },
      ],
    });
  });
});
