// @vitest-environment jsdom
/**
 * The presence dot's accessible name (#7411).
 *
 * `AgentStatusDot` renders a coloured dot and nothing else, so its
 * `aria-label` is the entire payload for a screen-reader user — colour is not
 * available to them. That label used to be built as `Status: ${label}` from
 * `availabilityConfig`, a table of English strings sitting inside the
 * translated package: a Chinese workspace announced "Status: Offline". Both
 * halves — the prefix and the state word — now come from the `agents` bundle.
 */
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import type { AgentAvailability } from "@multica/core/agents";
import { renderWithI18n } from "../test/i18n";

const presence = vi.hoisted(() => ({
  availability: "offline" as AgentAvailability,
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: () => "Ada Lovelace",
    getActorInitials: () => "AL",
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    memberDetail: (id: string) => `/acme/members/${id}`,
    agentDetail: (id: string) => `/acme/agents/${id}`,
    squadDetail: (id: string) => `/acme/squads/${id}`,
  }),
  useCurrentWorkspace: () => ({ id: "ws1", slug: "acme" }),
}));

vi.mock("@multica/core/agents", () => ({
  useAgentPresenceDetail: () => ({
    availability: presence.availability,
    workload: null,
  }),
}));

vi.mock("../agents/components/agent-profile-card", () => ({
  AgentProfileCard: () => null,
}));
vi.mock("../agents/components/agent-live-peek-card", () => ({
  AgentLivePeekCard: () => null,
}));
vi.mock("../members/member-profile-card", () => ({
  MemberProfileCard: () => null,
}));
vi.mock("../squads/components/squad-profile-card", () => ({
  SquadProfileCard: () => null,
}));

import { AgentStatusDot } from "./actor-avatar";

describe("AgentStatusDot accessible name", () => {
  it("names the status in English by default", () => {
    presence.availability = "offline";
    renderWithI18n(<AgentStatusDot agentId="agent-1" />);

    expect(screen.getByLabelText("Status: Offline")).toBeInTheDocument();
  });

  it("names the status in the active locale, prefix included", () => {
    presence.availability = "offline";
    renderWithI18n(<AgentStatusDot agentId="agent-1" />, { locale: "zh-Hans" });

    expect(screen.getByLabelText("状态：离线")).toBeInTheDocument();
    expect(screen.queryByLabelText(/Status:/)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/Offline/)).not.toBeInTheDocument();
  });

  it("covers every availability value the visual config knows", () => {
    // `archived` is a lifecycle state the dot also renders; it was in the same
    // English table, so it needs the same coverage as the runtime states.
    for (const [availability, expected] of [
      ["online", "状态：在线"],
      ["unstable", "状态：不稳定"],
      ["offline", "状态：离线"],
      ["archived", "状态：已归档"],
    ] as const) {
      presence.availability = availability;
      const { unmount } = renderWithI18n(<AgentStatusDot agentId="agent-1" />, {
        locale: "zh-Hans",
      });
      expect(screen.getByLabelText(expected)).toBeInTheDocument();
      unmount();
    }
  });
});
