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
import { AppLink } from "../navigation/app-link";
import { NavigationProvider } from "../navigation/context";
import type { NavigationAdapter } from "../navigation/types";
import { DeferredPopup } from "./deferred-popup";

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

function PickerWrapper({ children }: { children: React.ReactNode }) {
  const stop = (event: React.SyntheticEvent) => {
    event.stopPropagation();
    event.preventDefault();
  };
  return (
    <div onClick={stop} onMouseDown={stop} onPointerDown={stop}>
      {children}
    </div>
  );
}

function renderCardPicker(adapter: NavigationAdapter) {
  return render(
    <NavigationProvider value={adapter}>
      <AppLink href="/acme/issues/issue-1">
        <PickerWrapper>
          <DeferredPopup
            trigger={
              <span>
                <ActorAvatar
                  actorType="member"
                  actorId={MEMBER_ID}
                  enableHoverCard
                />
                <span>Change assignee</span>
              </span>
            }
          >
            {(open) => (
              <span data-testid="mounted-picker" data-open={String(open)}>
                Picker mounted
              </span>
            )}
          </DeferredPopup>
        </PickerWrapper>
      </AppLink>
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

  it("opens the deferred picker while cancelling the enclosing card link", () => {
    const push = vi.fn();
    renderCardPicker(makeAdapter({ push }));

    expect(fireEvent.click(screen.getByText("Change assignee"))).toBe(false);
    expect(screen.getByTestId("mounted-picker")).toHaveAttribute("data-open", "true");
    expect(push).not.toHaveBeenCalled();
  });

  it("yields to the picker by default without leaking native card navigation", () => {
    const push = vi.fn();
    const { container } = renderCardPicker(makeAdapter({ push }));
    const avatar = container.querySelector('[data-slot="avatar"]');

    expect(avatar).not.toBeNull();
    expect(fireEvent.click(avatar!)).toBe(false);
    expect(screen.getByTestId("mounted-picker")).toHaveAttribute("data-open", "true");
    expect(push).not.toHaveBeenCalled();
  });

  it("uses openInNewTab for cmd/ctrl click when available (desktop)", () => {
    const push = vi.fn();
    const openInNewTab = vi.fn();
    const open = vi.spyOn(window, "open").mockReturnValue(null);

    renderAvatar(makeAdapter({ push, openInNewTab }));
    fireEvent.click(screen.getByRole("link"), { metaKey: true });

    expect(openInNewTab).toHaveBeenCalledWith(HREF, undefined);
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
