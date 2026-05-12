import { act } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { useInputPin } from "./use-input-pin";
import { usePinInputStore } from "./store";

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => true,
}));

describe("useInputPin", () => {
  beforeEach(() => {
    usePinInputStore.getState().unpinAll();
  });

  it("reports enabled=true when feature flag is on and caller opts in", () => {
    const { result } = renderHook(() => useInputPin("reply:1", true));
    expect(result.current.enabled).toBe(true);
    expect(result.current.isPinned).toBe(false);
  });

  it("is disabled and a no-op when caller opts out (channels/DMs)", () => {
    const { result } = renderHook(() => useInputPin("reply:1", false));
    expect(result.current.enabled).toBe(false);
    act(() => result.current.togglePin());
    expect(usePinInputStore.getState().pinnedKey).toBeNull();
    expect(result.current.isPinned).toBe(false);
  });

  it("togglePin flips state and only one key can be pinned at a time", () => {
    const a = renderHook(() => useInputPin("reply:a", true));
    const b = renderHook(() => useInputPin("reply:b", true));

    act(() => a.result.current.togglePin());
    a.rerender();
    b.rerender();
    expect(a.result.current.isPinned).toBe(true);
    expect(b.result.current.isPinned).toBe(false);

    act(() => b.result.current.togglePin());
    a.rerender();
    b.rerender();
    expect(a.result.current.isPinned).toBe(false);
    expect(b.result.current.isPinned).toBe(true);
  });

  it("unpin only releases the matching key", () => {
    const a = renderHook(() => useInputPin("reply:a", true));
    const b = renderHook(() => useInputPin("reply:b", true));

    act(() => a.result.current.togglePin());
    a.rerender();
    act(() => b.result.current.unpin());
    a.rerender();
    expect(a.result.current.isPinned).toBe(true);

    act(() => a.result.current.unpin());
    a.rerender();
    expect(a.result.current.isPinned).toBe(false);
    expect(usePinInputStore.getState().pinnedKey).toBeNull();
  });
});
