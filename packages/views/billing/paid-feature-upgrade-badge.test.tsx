import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import type { MemberRole } from "@multica/core/types";
import enBilling from "../locales/en/billing.json";
import { PaidFeatureUpgradeBadge, type UpgradeBadgeFeature } from "./paid-feature-upgrade-badge";

const member = vi.hoisted(() => ({ role: "member" as MemberRole | null }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ userId: "user-1", role: member.role, member: null, isLoading: false }),
}));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useWorkspacePaths: () => ({ settings: () => "/acme/settings" }),
}));
vi.mock("../navigation", async (importOriginal) => ({
  ...(await importOriginal<object>()),
  AppLink: ({ href, children, ...props }: React.ComponentProps<"a"> & { href: string }) => (
    <a href={href} {...props}>{children}</a>
  ),
}));

const card = enBilling.paid_feature.upgrade_card;

function renderBadge(feature: UpgradeBadgeFeature = "agent_invocation_members") {
  render(
    <I18nProvider locale="en" resources={{ en: { billing: enBilling } }}>
      <PaidFeatureUpgradeBadge feature={feature} />
    </I18nProvider>,
  );
  return screen.getByRole("button", { name: enBilling.paid_feature.badge_label });
}

afterEach(() => {
  cleanup();
  member.role = "member";
});

describe("PaidFeatureUpgradeBadge", () => {
  it("is a named button showing Business, with the card closed", () => {
    const badge = renderBadge();
    expect(badge).toHaveTextContent("Business");
    expect(screen.queryByText(card.agent_invocation_members.title)).not.toBeInTheDocument();
  });

  it("opens the card on click", async () => {
    const user = userEvent.setup();
    await user.click(renderBadge("runtime_member_sharing"));
    expect(await screen.findByText(card.runtime_member_sharing.title)).toBeVisible();
    expect(screen.getByText(card.runtime_member_sharing.description)).toBeVisible();
  });

  it.each(["{Enter}", " "])("opens the card from the keyboard (%s)", async (key) => {
    const user = userEvent.setup();
    const badge = renderBadge();
    badge.focus();
    expect(badge).toHaveFocus();
    await user.keyboard(key);
    expect(await screen.findByText(card.agent_invocation_members.title)).toBeVisible();
    expect(screen.getByText(card.agent_invocation_members.description)).toBeVisible();
  });

  it.each(["owner", "admin"] as const)("sends a workspace %s to Billing", async (role) => {
    member.role = role;
    const user = userEvent.setup();
    await user.click(renderBadge());
    const link = await screen.findByRole("link", { name: enBilling.paid_feature.upgrade_to_business });
    expect(link).toHaveAttribute("href", "/acme/settings?tab=billing");
    expect(screen.queryByText(enBilling.paid_feature.ask_admin)).not.toBeInTheDocument();
  });

  it("tells a member to ask an admin, without an upgrade action", async () => {
    member.role = "member";
    const user = userEvent.setup();
    await user.click(renderBadge());
    expect(await screen.findByText(enBilling.paid_feature.ask_admin)).toBeVisible();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });
});
