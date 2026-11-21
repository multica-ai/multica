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
import enBilling from "../../../locales/en/billing.json";

import { AccessPicker } from "./access-picker";

// ActorAvatar pulls workspace context (useWorkspaceId) that this unit test
// doesn't provide; stub it — the picker logic under test doesn't depend on it.
vi.mock("../../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useWorkspacePaths: () => ({ settings: () => "/acme/settings" }),
}));
vi.mock("../../../navigation", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}));

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, issues: enIssues, billing: enBilling },
};

const BADGE = enBilling.paid_feature.badge_label;

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

  it("renders mutually exclusive access scopes for the owner", () => {
    renderPicker({ canEdit: true });
    expect(
      screen.getByRole("radio", { name: /^Only me/i }),
    ).toBeChecked();
    expect(
      screen.getByRole("radio", { name: /^Entire workspace/i }),
    ).not.toBeChecked();
    expect(
      screen.getByRole("radio", { name: /^Specific people/i }),
    ).not.toBeChecked();
  });

  it("lets the owner grant the entire workspace access", () => {
    const { onChange } = renderPicker({ canEdit: true });
    fireEvent.click(
      screen.getByRole("radio", { name: /^Entire workspace/i }),
    );
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(onChange).toHaveBeenCalledWith({
      permission_mode: "public_to",
      invocation_targets: [{ target_type: "workspace" }],
    });
  });

  it("reports an unsaved draft until the owner returns to the persisted mode", () => {
    const onDirtyChange = vi.fn();
    renderPicker({ canEdit: true, onDirtyChange });

    fireEvent.click(
      screen.getByRole("radio", { name: /^Entire workspace/i }),
    );
    expect(onDirtyChange).toHaveBeenLastCalledWith(true);

    fireEvent.click(screen.getByRole("radio", { name: /^Only me/i }));
    expect(onDirtyChange).toHaveBeenLastCalledWith(false);
  });

  it("owner can pick a specific member, emitting a public_to member target", () => {
    const { onChange } = renderPicker({
      canEdit: true,
    });
    fireEvent.click(
      screen.getByRole("radio", { name: /^Specific people/i }),
    );
    fireEvent.click(screen.getByRole("checkbox", { name: /Alice/i }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(onChange).toHaveBeenCalledWith({
      permission_mode: "public_to",
      invocation_targets: [{ target_type: "member", target_id: "u1" }],
    });
  });

  it("workspace scope removes redundant member grants", () => {
    const { onChange } = renderPicker({
      canEdit: true,
      permissionMode: "public_to",
      invocationTargets: [{ target_type: "member", target_id: "u1" }],
      visibility: "private",
    });
    fireEvent.click(screen.getByRole("radio", { name: /^Entire workspace/i }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(onChange).toHaveBeenCalledWith({
      permission_mode: "public_to",
      invocation_targets: [{ target_type: "workspace" }],
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
      screen.getByRole("radio", { name: /^Only me/i }),
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

describe("AccessPicker on a plan without member access (MUL-9491)", () => {
  beforeEach(() => cleanup());
  afterEach(() => cleanup());

  it("offers every member and no notice when the feature is available", () => {
    renderPicker({ membersAccess: { canAdd: true, state: "available" } });
    const members = screen.getByRole("radio", { name: /^Specific people/i });
    expect(members).toBeEnabled();
    fireEvent.click(members);
    expect(screen.getByRole("checkbox", { name: /Alice/i })).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: /Bob/i })).toBeInTheDocument();
    expect(screen.queryByText("View plans")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: BADGE })).not.toBeInTheDocument();
  });

  it("disables Specific people for an agent without a member list", () => {
    renderPicker({ membersAccess: { canAdd: false, state: "unavailable" } });
    expect(screen.getByRole("radio", { name: /^Specific people/i })).toBeDisabled();
    expect(screen.getByRole("radio", { name: /^Entire workspace/i })).toBeEnabled();
    expect(screen.getByText("Not included in this workspace's plan.")).toBeInTheDocument();
  });

  it("puts a Business badge beside the locked option, outside its label", () => {
    renderPicker({ membersAccess: { canAdd: false, state: "unavailable" } });
    const badge = screen.getByRole("button", { name: BADGE });
    expect(badge).toBeEnabled();
    // A control inside the label would select the option it explains.
    expect(badge.closest("label")).toBeNull();
  });

  it("keeps an existing list removable but adds nobody, and requires Only me to drop the last person", () => {
    const { onChange } = renderPicker({
      permissionMode: "public_to",
      invocationTargets: [{ target_type: "member", target_id: "u1" }],
      membersAccess: { canAdd: false, state: "read_only" },
    });
    expect(screen.getByText("Read-only. Upgrade to edit.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: BADGE })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: /^Specific people/i })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: /Alice/i })).toBeChecked();
    expect(screen.queryByRole("checkbox", { name: /Bob/i })).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("checkbox", { name: /Alice/i }));
    expect(screen.getByRole("alert")).toHaveTextContent("Select at least one person before saving.");
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();

    fireEvent.click(screen.getByRole("radio", { name: /^Only me/i }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(onChange).toHaveBeenCalledWith({ permission_mode: "private", invocation_targets: [] });
  });
});

describe("AccessPicker with a locked member pool (bulk edit, MUL-9491)", () => {
  afterEach(() => cleanup());

  it("offers only the pool's people while adding is locked", () => {
    renderPicker({
      membersAccess: { canAdd: false, state: "read_only" },
      lockedMemberPool: ["u1"],
    });
    const members = screen.getByRole("radio", { name: /^Specific people/i });
    expect(members).toBeEnabled();
    fireEvent.click(members);
    expect(screen.getByRole("checkbox", { name: /Alice/i })).not.toBeChecked();
    expect(screen.queryByRole("checkbox", { name: /Bob/i })).not.toBeInTheDocument();
  });

  it("disables Specific people when the pool is empty", () => {
    renderPicker({
      membersAccess: { canAdd: false, state: "unavailable" },
      lockedMemberPool: [],
    });
    expect(screen.getByRole("radio", { name: /^Specific people/i })).toBeDisabled();
  });

  it("ignores the pool when the plan includes the feature", () => {
    renderPicker({
      membersAccess: { canAdd: true, state: "available" },
      lockedMemberPool: [],
    });
    fireEvent.click(screen.getByRole("radio", { name: /^Specific people/i }));
    expect(screen.getByRole("checkbox", { name: /Bob/i })).toBeInTheDocument();
  });
});
