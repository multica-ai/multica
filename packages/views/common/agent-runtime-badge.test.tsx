/**
 * Runtime provider badge on agent avatars.
 *
 * The badge and the presence dot are two overlays on one circle, so the rules
 * that keep them from fighting each other are the ones worth pinning: the
 * badge only appears where a provider mark is actually legible, it never
 * displaces the dot, and it stays away from anything it can't resolve.
 */
import type { ReactElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { NavigationProvider } from "../navigation/context";
import type { NavigationAdapter } from "../navigation/types";

const provider = vi.hoisted(() => ({ current: "claude" as string | null }));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: () => "Lambda",
    getActorInitials: () => "L",
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
  useAgentPresenceDetail: () => ({ availability: "online", workload: "idle" }),
  useAgentRuntimeProvider: () => provider.current,
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

import { ActorAvatar } from "./actor-avatar";

const AGENT_ID = "8f14e45f-ceea-4d0e-a1a2-9b1c0d3e4f5a";

// The avatar wraps itself in a profile link, which needs the adapter.
const ADAPTER: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/",
  searchParams: new URLSearchParams(),
  getShareableUrl: (p) => `https://app.example${p}`,
};

function renderAvatar(ui: ReactElement) {
  return render(<NavigationProvider value={ADAPTER}>{ui}</NavigationProvider>);
}

afterEach(() => {
  cleanup();
  provider.current = "claude";
});

describe("AgentRuntimeBadge", () => {
  it("marks the avatar with the runtime's provider", () => {
    renderAvatar(<ActorAvatar actorType="agent" actorId={AGENT_ID} size="2xl" showRuntimeBadge />);
    expect(screen.getByLabelText("Runtime: Claude")).toBeInTheDocument();
  });

  // Provider marks are detailed artwork; at the ~8px a badge gets on a 20px
  // picker row they're a smudge. That size is the status dot's home turf, so
  // the badge has to stay out of it even when a caller asks for one.
  it("stays off avatars too small to render a legible mark", () => {
    renderAvatar(<ActorAvatar actorType="agent" actorId={AGENT_ID} size="sm" showRuntimeBadge />);
    expect(screen.queryByLabelText("Runtime: Claude")).not.toBeInTheDocument();
  });

  it("renders both overlays when a surface asks for the dot too", () => {
    renderAvatar(
      <ActorAvatar
        actorType="agent"
        actorId={AGENT_ID}
        size="lg"
        showStatusDot
        showRuntimeBadge
      />,
    );
    expect(screen.getByLabelText("Runtime: Claude")).toBeInTheDocument();
    expect(screen.getByLabelText("Status: Online")).toBeInTheDocument();
  });

  // Archived agent, deleted runtime, or a backend that omitted the field. A
  // mark that names the wrong runtime is worse than no mark.
  it("renders nothing when the provider cannot be resolved", () => {
    provider.current = null;
    renderAvatar(<ActorAvatar actorType="agent" actorId={AGENT_ID} size="2xl" showRuntimeBadge />);
    expect(screen.queryByLabelText(/^Runtime:/)).not.toBeInTheDocument();
  });

  it("has no effect for non-agent actors", () => {
    renderAvatar(<ActorAvatar actorType="member" actorId={AGENT_ID} size="2xl" showRuntimeBadge />);
    expect(screen.queryByLabelText(/^Runtime:/)).not.toBeInTheDocument();
  });
});
