import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { OPEN_REVEAL_DEADLINE_MS, useChatOpenReveal } from "./use-chat-open-reveal";

describe("useChatOpenReveal", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  function renderReveal(initialOpen: boolean) {
    return renderHook(
      ({ isOpen }: { isOpen: boolean }) => useChatOpenReveal(isOpen),
      { initialProps: { isOpen: initialOpen } },
    );
  }

  it("holds an open window until its message area reports ready", () => {
    const { result, rerender } = renderReveal(false);
    rerender({ isOpen: true });
    expect(result.current.contentReady).toBe(false);

    act(() => result.current.markContentReady());
    expect(result.current.contentReady).toBe(true);
  });

  // MUL-8417: readiness belongs to one open. Carrying it over would let the
  // next open fade in around a list that has not landed yet.
  it("waits again on every open", () => {
    const { result, rerender } = renderReveal(true);
    act(() => result.current.markContentReady());

    rerender({ isOpen: false });
    expect(result.current.contentReady).toBe(false);

    rerender({ isOpen: true });
    expect(result.current.contentReady).toBe(false);
    act(() => result.current.markContentReady());
    expect(result.current.contentReady).toBe(true);
  });

  it("opens anyway once the deadline passes", () => {
    const { result } = renderReveal(true);

    act(() => vi.advanceTimersByTime(OPEN_REVEAL_DEADLINE_MS - 1));
    expect(result.current.contentReady).toBe(false);
    act(() => vi.advanceTimersByTime(1));
    expect(result.current.contentReady).toBe(true);
  });

  it("does not let a closed window's deadline reveal the next open", () => {
    const { result, rerender } = renderReveal(true);
    act(() => vi.advanceTimersByTime(OPEN_REVEAL_DEADLINE_MS - 50));
    rerender({ isOpen: false });
    rerender({ isOpen: true });

    act(() => vi.advanceTimersByTime(50));
    expect(result.current.contentReady).toBe(false);
  });
});
