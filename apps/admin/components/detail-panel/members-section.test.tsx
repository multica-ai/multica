// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { inviteTooltipMessage, MembersSection } from "./members-section";
import type { InviteEligibility, PendingInvitation, WorkspaceMember } from "@/lib/types";

function makeMember(overrides: Partial<WorkspaceMember> = {}): WorkspaceMember {
  return {
    id: "user-1",
    name: "Jane Doe",
    email: "jane@example.com",
    role: "member",
    ...overrides,
  };
}

const ELIGIBLE: InviteEligibility = { eligible: true, botEmail: "agentfarm-bot@g2.com", reason: null };
const INELIGIBLE: InviteEligibility = {
  eligible: false,
  botEmail: "agentfarm-bot@g2.com",
  reason: "not-workspace-admin",
};
const PAT_MISSING: InviteEligibility = { eligible: false, botEmail: "agentfarm-bot@g2.com", reason: "pat-missing" };

function renderSection(overrides: Partial<Parameters<typeof MembersSection>[0]> = {}) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <MembersSection
        workspaceId="ws-1"
        members={[]}
        pendingInvitations={[]}
        inviteEligibility={ELIGIBLE}
        {...overrides}
      />
    </QueryClientProvider>,
  );
}

describe("MembersSection", () => {
  it("renders a chip for each member", () => {
    renderSection({
      members: [makeMember({ id: "user-1", name: "Jane Doe" }), makeMember({ id: "user-2", name: "Bob Roe" })],
    });
    expect(screen.getByText("Jane Doe")).toBeInTheDocument();
    expect(screen.getByText("Bob Roe")).toBeInTheDocument();
  });

  it("derives the avatar fallback from first + last word initials", () => {
    renderSection({ members: [makeMember({ name: "Jane Doe" })] });
    expect(screen.getByText("JD")).toBeInTheDocument();
  });

  it("falls back to the first two letters for a single-word name", () => {
    renderSection({ members: [makeMember({ name: "Cher" })] });
    expect(screen.getByText("CH")).toBeInTheDocument();
  });

  it("falls back to '?' for an empty/whitespace name", () => {
    renderSection({ members: [makeMember({ name: "   " })] });
    expect(screen.getByText("?")).toBeInTheDocument();
  });

  it("renders an empty state when there are no members", () => {
    renderSection({ members: [] });
    expect(screen.getByText("No members.")).toBeInTheDocument();
  });

  it("renders pending invitations as separate chips", () => {
    const pendingInvitations: PendingInvitation[] = [
      { email: "pending@example.com", role: "member", createdAt: "2026-01-01T00:00:00Z", expiresAt: "2026-01-08T00:00:00Z" },
    ];
    renderSection({ pendingInvitations });
    expect(screen.getByText("pending@example.com · pending")).toBeInTheDocument();
  });

  it("marks the Invite trigger aria-disabled (not natively disabled) when the bot is not eligible", () => {
    // aria-disabled, not the native `disabled` attribute, so the button stays
    // focusable/hoverable and the Tooltip explaining why stays reachable —
    // see the comment above the Button in members-section.tsx.
    renderSection({ inviteEligibility: INELIGIBLE });
    const button = screen.getByRole("button", { name: /invite/i });
    expect(button).toHaveAttribute("aria-disabled", "true");
    expect(button).toBeEnabled();
  });

  it("leaves the Invite trigger aria-disabled=false when the bot is eligible", () => {
    renderSection({ inviteEligibility: ELIGIBLE });
    const button = screen.getByRole("button", { name: /invite/i });
    expect(button).toHaveAttribute("aria-disabled", "false");
    expect(button).toBeEnabled();
  });

  it("explains a per-workspace ineligibility distinctly from a missing BOT_PAT", () => {
    expect(inviteTooltipMessage(INELIGIBLE)).toMatch(/isn't an owner\/admin of this workspace/i);
    expect(inviteTooltipMessage(PAT_MISSING)).toMatch(/bot_pat.*isn't configured/i);
    expect(inviteTooltipMessage(ELIGIBLE)).toBe("Invite a user to this workspace");
  });
});
