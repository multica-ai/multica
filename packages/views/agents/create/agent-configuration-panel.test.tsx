// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { EMPTY_AGENT_DRAFT, type AgentDraft } from "@multica/core/agents";
import type { MemberWithUser, WorkspaceFeatureState } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import enBilling from "../../locales/en/billing.json";
import { AgentConfigurationPanel } from "./agent-configuration-panel";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../../common/avatar-upload-control", () => ({ AvatarUploadControl: () => null }));
vi.mock("../components/runtime-picker", () => ({ RuntimePicker: () => null }));
vi.mock("../components/skill-multi-select", () => ({ SkillMultiSelect: () => null }));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useWorkspacePaths: () => ({ settings: () => "/acme/settings" }),
}));
vi.mock("../../navigation", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}));

const MEMBERS = [
  { user_id: "me", name: "Me" },
  { user_id: "u1", name: "Alice" },
  { user_id: "u2", name: "Bob" },
] as unknown as MemberWithUser[];

let feature: WorkspaceFeatureState;

function renderPanel(draft: Partial<AgentDraft> = {}) {
  const onChange = vi.fn();
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <AgentConfigurationPanel
        draft={{ ...EMPTY_AGENT_DRAFT, name: "New agent", ...draft }}
        onChange={onChange}
        runtimes={[]}
        runtimesLoading={false}
        members={MEMBERS}
        currentUserId="me"
        nameError={null}
        onNameChange={vi.fn()}
      />
    </QueryClientProvider>,
  );
  return { onChange };
}

const BADGE = enBilling.paid_feature.badge_label;

function membersOption() {
  return screen.getByRole("radio", { name: /Specific people/ });
}

describe("AgentConfigurationPanel member access (MUL-9491)", () => {
  beforeEach(() => {
    feature = "available";
    setApiInstance({
      getWorkspaceFeatures: vi.fn(async () => ({ features: { agent_invocation_members: feature } })),
    } as unknown as ApiClient);
  });
  afterEach(cleanup);

  it("offers Specific people when the plan includes it", async () => {
    const { onChange } = renderPanel();
    await waitFor(() => expect(membersOption()).toBeEnabled());
    fireEvent.click(membersOption());
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ permissionScope: "members" }));
    expect(screen.queryByText("View plans")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: BADGE })).not.toBeInTheDocument();
  });

  it("disables Specific people on a locked plan", async () => {
    feature = "locked";
    const { onChange } = renderPanel();
    expect(await screen.findByText("Not included in this workspace's plan.")).toBeInTheDocument();
    expect(membersOption()).toBeDisabled();
    fireEvent.click(membersOption());
    expect(onChange).not.toHaveBeenCalled();
    // The badge sits beside the option, not inside it.
    const badge = screen.getByRole("button", { name: BADGE });
    expect(badge).toBeEnabled();
    expect(membersOption()).not.toContainElement(badge);
  });

  it("lets a draft that already picked people drop them on a locked plan, but not add more", async () => {
    feature = "locked";
    renderPanel({ permissionScope: "members", memberIds: new Set(["u1"]) });
    await screen.findByText("Not included in this workspace's plan.");
    expect(membersOption()).toBeEnabled();
    const checkboxes = screen.getAllByRole("checkbox");
    // Alice (picked) can be unchecked; Bob cannot be added.
    expect(checkboxes[0]).not.toHaveAttribute("aria-disabled");
    expect(checkboxes[1]).toHaveAttribute("aria-disabled", "true");
  });
});
