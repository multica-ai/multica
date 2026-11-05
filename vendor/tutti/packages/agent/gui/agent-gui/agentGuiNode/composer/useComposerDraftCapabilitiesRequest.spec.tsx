import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { emptyAgentComposerDraft } from "../model/agentComposerDraft";
import { useComposerDraftCapabilitiesRequest } from "./useComposerDraftCapabilitiesRequest";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

function idleScheduler() {
  const callbacks = new Map<number, IdleRequestCallback>();
  let nextId = 0;
  vi.stubGlobal("requestIdleCallback", vi.fn((callback: IdleRequestCallback) => {
    callbacks.set(++nextId, callback);
    return nextId;
  }));
  const cancel = vi.fn((id: number) => callbacks.delete(id));
  vi.stubGlobal("cancelIdleCallback", cancel);
  return {
    cancel,
    flush() {
      const pending = [...callbacks.values()];
      callbacks.clear();
      act(() => pending.forEach((callback) => callback({ didTimeout: false, timeRemaining: () => 50 })));
    }
  };
}

function options() {
  return {
    agentSessionId: "session-1",
    draftScopeKey: "project-1",
    isActive: true,
    onDraftContentChange: vi.fn(),
    onRetryComposerOptions: vi.fn(),
    provider: "claude-code"
  };
}

describe("composer capabilities prefetch", () => {
  it("prefetches in an idle slot without holding draft edits", () => {
    const idle = idleScheduler();
    const input = options();
    const { result, unmount } = renderHook(() => useComposerDraftCapabilitiesRequest(input));
    expect(input.onRetryComposerOptions).not.toHaveBeenCalled();
    const draft = emptyAgentComposerDraft();
    draft[0].text = "hello";
    act(() => result.current(draft, "project-1"));
    expect(input.onDraftContentChange).toHaveBeenCalledWith(draft, "project-1");
    expect(input.onRetryComposerOptions).not.toHaveBeenCalled();
    idle.flush();
    expect(input.onRetryComposerOptions).toHaveBeenCalledExactlyOnceWith({ section: "capabilities" });
    unmount();
  });

  it("cancels an obsolete prefetch and warms the next active scope", () => {
    const idle = idleScheduler();
    const first = options();
    const second = { ...first, agentSessionId: "session-2", onRetryComposerOptions: vi.fn() };
    const { rerender, unmount } = renderHook((input) => useComposerDraftCapabilitiesRequest(input), { initialProps: first });
    rerender(second);
    idle.flush();
    expect(idle.cancel).toHaveBeenCalled();
    expect(first.onRetryComposerOptions).not.toHaveBeenCalled();
    expect(second.onRetryComposerOptions).toHaveBeenCalledOnce();
    rerender({ ...second, draftScopeKey: "project-2" });
    unmount();
    idle.flush();
    expect(second.onRetryComposerOptions).toHaveBeenCalledOnce();
  });

  it("defers unsupported idle scheduling and skips inactive composers", () => {
    vi.useFakeTimers();
    vi.stubGlobal("requestIdleCallback", undefined);
    const input = options();
    const { rerender, unmount } = renderHook((value) => useComposerDraftCapabilitiesRequest(value), { initialProps: { ...input, isActive: false } });
    act(() => vi.runOnlyPendingTimers());
    expect(input.onRetryComposerOptions).not.toHaveBeenCalled();
    rerender(input);
    expect(input.onRetryComposerOptions).not.toHaveBeenCalled();
    act(() => vi.runOnlyPendingTimers());
    expect(input.onRetryComposerOptions).toHaveBeenCalledExactlyOnceWith({ section: "capabilities" });
    unmount();
  });
});
