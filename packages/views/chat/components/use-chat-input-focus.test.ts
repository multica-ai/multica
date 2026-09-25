import { describe, expect, it } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useChatInputFocus } from "./use-chat-input-focus";

describe("useChatInputFocus", () => {
  it("stays inert on mount, whether the window starts closed or open", () => {
    expect(renderHook(() => useChatInputFocus(false)).result.current.focusRequest).toBe(0);
    // A persisted "open" preference must not steal focus from the page the user
    // just loaded — ChatWindow is mounted (hidden) even while closed.
    expect(renderHook(() => useChatInputFocus(true)).result.current.focusRequest).toBe(0);
  });

  it("requests focus on every closed → open transition", () => {
    const { result, rerender } = renderHook(
      ({ isOpen }: { isOpen: boolean }) => useChatInputFocus(isOpen),
      { initialProps: { isOpen: false } },
    );

    rerender({ isOpen: true });
    expect(result.current.focusRequest).toBe(1);

    // Re-renders that don't change `isOpen` must not re-focus: the user may
    // have clicked into the message list or the agent picker since.
    rerender({ isOpen: true });
    expect(result.current.focusRequest).toBe(1);

    rerender({ isOpen: false });
    expect(result.current.focusRequest).toBe(1);

    rerender({ isOpen: true });
    expect(result.current.focusRequest).toBe(2);
  });

  it("lets callers bump the nonce for new chats and agent switches", () => {
    const { result } = renderHook(() => useChatInputFocus(true));

    act(() => result.current.requestInputFocus());
    expect(result.current.focusRequest).toBe(1);
    act(() => result.current.requestInputFocus());
    expect(result.current.focusRequest).toBe(2);
  });
});
