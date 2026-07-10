// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactElement } from "react";
import { renderWithI18n } from "../test/i18n";

// Stub the profile cards so the test asserts dispatch without triggering their
// data fetching. Each surfaces the id it received so we can confirm the right
// card rendered for the right actor type.
vi.mock("../agents/components/agent-profile-card", () => ({
  AgentProfileCard: ({ agentId }: { agentId: string }) => (
    <span data-testid="agent-profile-card">{agentId}</span>
  ),
}));
vi.mock("../members/member-profile-card", () => ({
  MemberProfileCard: ({ userId }: { userId: string }) => (
    <span data-testid="member-profile-card">{userId}</span>
  ),
}));
vi.mock("../squads/components/squad-profile-card", () => ({
  SquadProfileCard: ({ squadId }: { squadId: string }) => (
    <span data-testid="squad-profile-card">{squadId}</span>
  ),
}));

import { MentionHoverCard } from "./mention-hover-card";

afterEach(cleanup);

// MentionHoverCard opens on hover OR keyboard focus (Base UI PreviewCard
// triggerFocus). In jsdom we drive it with focus, then assert the portaled
// popup content.
async function openHover(ui: ReactElement) {
  const { container } = renderWithI18n(ui);
  fireEvent.focus(container.querySelector('[data-slot="hover-card-trigger"]')!);
}

describe("MentionHoverCard", () => {
  it("renders the agent profile card for an agent mention", async () => {
    await openHover(
      <MentionHoverCard type="agent" id="a-1">
        <span>chip</span>
      </MentionHoverCard>,
    );
    const card = await waitFor(() => screen.getByTestId("agent-profile-card"));
    expect(card.textContent).toBe("a-1");
  });

  it("renders the member profile card for a member mention", async () => {
    await openHover(
      <MentionHoverCard type="member" id="u-1">
        <span>chip</span>
      </MentionHoverCard>,
    );
    const card = await waitFor(() => screen.getByTestId("member-profile-card"));
    expect(card.textContent).toBe("u-1");
  });

  it("renders the squad profile card for a squad mention", async () => {
    await openHover(
      <MentionHoverCard type="squad" id="s-1">
        <span>chip</span>
      </MentionHoverCard>,
    );
    const card = await waitFor(() => screen.getByTestId("squad-profile-card"));
    expect(card.textContent).toBe("s-1");
  });

  it("renders the All members card for @all (no profile card)", async () => {
    await openHover(
      <MentionHoverCard type="all" id="all">
        <span>chip</span>
      </MentionHoverCard>,
    );
    await waitFor(() => {
      expect(screen.getByText("All members")).toBeInTheDocument();
    });
    expect(screen.queryByTestId("agent-profile-card")).not.toBeInTheDocument();
  });
});
