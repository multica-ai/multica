/**
 * @vitest-environment jsdom
 */
// CEREBRO-PATCH(channel-typing-core): TECH-3664 — covers the channel/DM typing
// hook: a `cerebro:typing` event for the watched channel marks that user as
// typing, the user's own events are ignored, and events for other channels are
// ignored.
import { renderHook, act, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";

// Capture the live WS handler so a test can fire typing payloads at will.
const ws = vi.hoisted(() => ({
  handler: null as ((payload: unknown) => void) | null,
}));

vi.mock("@multica/core/realtime", () => ({
  useWSEvent: (_event: string, handler: (payload: unknown) => void) => {
    ws.handler = handler;
  },
}));

vi.mock("@multica/core/auth", () => ({
  // Current user is "me"; the hook must never report our own typing.
  useAuthStore: (selector: (s: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "me" } }),
}));

vi.mock("./presence-api", () => ({ postTyping: vi.fn(async () => {}) }));

import { useChannelTyping } from "./channel-typing";

describe("useChannelTyping", () => {
  beforeEach(() => {
    ws.handler = null;
  });

  it("marks another user typing in the watched channel", async () => {
    const { result } = renderHook(() => useChannelTyping("chan-1"));
    expect(result.current.typingUserIds.size).toBe(0);

    act(() => ws.handler?.({ channel_id: "chan-1", user_id: "alice" }));
    await waitFor(() =>
      expect(result.current.typingUserIds.has("alice")).toBe(true),
    );
  });

  it("ignores our own typing and other channels", async () => {
    const { result } = renderHook(() => useChannelTyping("chan-1"));

    act(() => ws.handler?.({ channel_id: "chan-1", user_id: "me" }));
    act(() => ws.handler?.({ channel_id: "other", user_id: "bob" }));

    expect(result.current.typingUserIds.size).toBe(0);
  });
});
