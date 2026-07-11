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

  it("renders explicit access choices for the owner", () => {
    renderPicker({ canEdit: true });
    expect(
      screen.getByRole("radio", { name: /^Private/i }),
    ).toBeChecked();
    expect(
      screen.getByRole("radio", { name: /^Shared/i }),
    ).not.toBeChecked();
  });

  it("defaults a newly shared agent to workspace access", () => {
    const { onChange } = renderPicker({ canEdit: true });
    fireEvent.click(screen.getByRole("radio", { name: /^Shared/i }));
    expect(onChange).not.toHaveBeenCalled();
    expect(
      screen.getByRole("checkbox", { name: /Public to workspace/i }),
    ).toBeChecked();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(onChange).toHaveBeenCalledWith({
      permission_mode: "public_to",
      invocation_targets: [{ target_type: "workspace" }],
    });
  });

  it("reports an unsaved draft until the owner returns to the persisted mode", () => {
    const onDirtyChange = vi.fn();
    renderPicker({ canEdit: true, onDirtyChange });

    fireEvent.click(screen.getByRole("radio", { name: /^Shared/i }));
    expect(onDirtyChange).toHaveBeenLastCalledWith(true);

    fireEvent.click(screen.getByRole("radio", { name: /^Private/i }));
    expect(onDirtyChange).toHaveBeenLastCalledWith(false);
  });

  it("owner can pick a specific member, emitting a public_to member target", () => {
    const { onChange } = renderPicker({
      canEdit: true,
      permissionMode: "public_to",
      invocationTargets: [],
    });
    fireEvent.click(screen.getByRole("checkbox", { name: /Alice/i }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
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
    fireEvent.click(
      screen.getByRole("checkbox", { name: /Public to workspace/i }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(onChange).toHaveBeenCalledWith({
      permission_mode: "public_to",
      invocation_targets: [
        { target_type: "workspace" },
        { target_type: "member", target_id: "u1" },
      ],
    });
  });

  // Regression: GH #4915. Older self-host backends / stale caches may return
  // an agent without `invocation_targets` even though the modern type says
  // required-array. The picker must degrade gracefully to the "Private" /
  // empty-allowlist summary instead of crashing the whole agent detail
  // route with "Cannot read properties of undefined (reading 'some')".
  it("renders without crashing when invocationTargets is undefined", () => {
    expect(() =>
      renderPicker({
        // Force the runtime shape produced by a legacy backend response.
        invocationTargets: undefined as unknown as AgentInvocationTarget[],
      }),
    ).not.toThrow();
    expect(
      screen.getByRole("radio", { name: /^Private/i }),
    ).toBeChecked();
  });

  it("read-only mode: undefined invocationTargets does not crash for non-owners", () => {
    expect(() =>
      renderPicker({
        canEdit: false,
        permissionMode: "public_to",
        invocationTargets: undefined as unknown as AgentInvocationTarget[],
        visibility: "workspace",
      }),
    ).not.toThrow();
    // No workspace target in the (empty) list ⇒ shows the "no members" state
    // rather than the workspace one, which is the safe degradation.
    expect(screen.getByTestId("access-readonly")).toBeInTheDocument();
  });
});
