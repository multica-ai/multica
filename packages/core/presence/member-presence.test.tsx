/**
 * @vitest-environment jsdom
 */
// CEREBRO-PATCH(member-presence-core): TECH-3583 — covers the workspace-wide
// member presence backbone that drives the red/green online dot on human
// avatars (pickers, @-mentions, assignees).
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi, beforeEach } from "vitest";

// Seeded online set the mocked snapshot/ping return. Hoisted so the vi.mock
// factory can close over it without a temporal-dead-zone error.
const presence = vi.hoisted(() => ({ online: new Set<string>() }));

vi.mock("./presence-api", () => ({
  fetchPresenceSnapshot: async () => ({
    online_user_ids: Array.from(presence.online),
  }),
  pingPresence: async () => ({
    online_user_ids: Array.from(presence.online),
  }),
}));

vi.mock("../realtime", () => ({
  // No-op subscription — these tests drive presence via the snapshot, not WS.
  useWSEvent: () => {},
}));

import { MemberPresenceProvider, useMemberOnline } from "./member-presence";

describe("member presence", () => {
  beforeEach(() => {
    presence.online = new Set();
  });

  it("returns null when no provider is mounted (presence unknown → no dot)", () => {
    const { result } = renderHook(() => useMemberOnline("alice"));
    expect(result.current).toBeNull();
  });

  it("reports online=true for a seeded member and false for an absent one", async () => {
    presence.online = new Set(["alice"]);
    const wrapper = ({ children }: { children: ReactNode }) => (
      <MemberPresenceProvider wsId="ws-1">{children}</MemberPresenceProvider>
    );

    const { result: alice } = renderHook(() => useMemberOnline("alice"), {
      wrapper,
    });
    const { result: bob } = renderHook(() => useMemberOnline("bob"), {
      wrapper,
    });

    await waitFor(() => expect(alice.current).toBe(true));
    expect(bob.current).toBe(false);
  });
});
