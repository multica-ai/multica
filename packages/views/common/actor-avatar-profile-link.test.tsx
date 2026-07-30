/**
 * Avatar profile-link modifier-click (MUL-5456).
 *
 * The trigger is a `<span role="link">`, not an anchor — deliberately, so it
 * can sit inside rows and menus without nesting interactive elements. That
 * also means the browser has nothing native to fall back on: whatever the
 * handler does IS the behaviour. On web (no `openInNewTab` adapter) a
 * cmd/ctrl-click used to fall through to `push()` and navigate in place,
 * throwing away the user's "keep me here" intent.
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { NavigationProvider } from "../navigation/context";
import type { NavigationAdapter } from "../navigation/types";

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
  useAgentPresenceDetail: () => ({ availability: "offline", workload: null }),
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

const MEMBER_ID = "8f14e45f-ceea-4d0e-a1a2-9b1c0d3e4f5a";
const HREF = `/acme/members/${MEMBER_ID}`;

function makeAdapter(overrides: Partial<NavigationAdapter> = {}): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/",
    searchParams: new URLSearchParams(),
    getShareableUrl: (p) => `https://app.example${p}`,
    ...overrides,
  };
}

function renderAvatar(adapter: NavigationAdapter) {
  return render(
    <NavigationProvider value={adapter}>
      <ActorAvatar actorType="member" actorId={MEMBER_ID} />
    </NavigationProvider>,
  );
}

describe("ActorAvatar profile link", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("pushes on plain click", () => {
    const push = vi.fn();
    const open = vi.spyOn(window, "open").mockReturnValue(null);

    renderAvatar(makeAdapter({ push }));
    fireEvent.click(screen.getByRole("link"));

    expect(push).toHaveBeenCalledWith(HREF);
    expect(open).not.toHaveBeenCalled();
  });

  it("uses openInNewTab for cmd/ctrl click when available (desktop)", () => {
    const push = vi.fn();
    const openInNewTab = vi.fn();
    const open = vi.spyOn(window, "open").mockReturnValue(null);

    renderAvatar(makeAdapter({ push, openInNewTab }));
    fireEvent.click(screen.getByRole("link"), { metaKey: true });

    expect(openInNewTab).toHaveBeenCalledWith(HREF);
    expect(push).not.toHaveBeenCalled();
    expect(open).not.toHaveBeenCalled();
  });

  it("opens a browser tab against the shareable URL when openInNewTab is absent (web)", () => {
    const push = vi.fn();
    const open = vi.spyOn(window, "open").mockReturnValue(null);

    renderAvatar(makeAdapter({ push }));
    fireEvent.click(screen.getByRole("link"), { metaKey: true });
    fireEvent.click(screen.getByRole("link"), { ctrlKey: true });

    expect(open).toHaveBeenCalledTimes(2);
    expect(open).toHaveBeenNthCalledWith(
      1,
      `https://app.example${HREF}`,
      "_blank",
      "noopener,noreferrer",
    );
    expect(push).not.toHaveBeenCalled();
  });
});
