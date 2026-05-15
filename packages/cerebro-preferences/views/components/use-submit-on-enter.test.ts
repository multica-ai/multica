import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook } from "@testing-library/react";

// Hoisted holder so tests can mutate the "current user" between renders
// without recreating the store mock.
const authState = vi.hoisted(() => ({
  user: null as { preferences?: Record<string, unknown> } | null,
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (s: typeof authState) => unknown) =>
      selector ? selector(authState) : authState,
    { getState: () => authState },
  ),
}));

import {
  ISSUE_LINK_OPEN_MODE_KEY,
  SUBMIT_ON_ENTER_KEY,
  useIssueLinkOpenMode,
  useSubmitOnEnter,
} from "./use-submit-on-enter";

// matchMedia stub. Driven by `coarsePointer` so each test can pretend to be
// on a touch-primary device or a fine-pointer (mouse / trackpad) device.
const coarsePointer = { current: false };
const originalMatchMedia = window.matchMedia;

beforeEach(() => {
  authState.user = null;
  coarsePointer.current = false;
  window.matchMedia = ((query: string) => {
    const matches = query === "(pointer: coarse)" ? coarsePointer.current : false;
    return {
      matches,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    } as unknown as MediaQueryList;
  }) as typeof window.matchMedia;
});

afterEach(() => {
  window.matchMedia = originalMatchMedia;
});

describe("useSubmitOnEnter", () => {
  it("returns false when no user is loaded (default Cmd/Ctrl+Enter behaviour)", () => {
    const { result } = renderHook(() => useSubmitOnEnter());
    expect(result.current).toBe(false);
  });

  it("returns false when user has no preferences blob", () => {
    authState.user = {};
    const { result } = renderHook(() => useSubmitOnEnter());
    expect(result.current).toBe(false);
  });

  it("returns false when preferences omits the key", () => {
    authState.user = { preferences: { theme: "dark" } };
    const { result } = renderHook(() => useSubmitOnEnter());
    expect(result.current).toBe(false);
  });

  it("returns false when key is explicitly false", () => {
    authState.user = { preferences: { submit_on_enter: false } };
    const { result } = renderHook(() => useSubmitOnEnter());
    expect(result.current).toBe(false);
  });

  it("returns false for truthy non-boolean values (strict === true check)", () => {
    // The hook deliberately uses === true so a stale string value from an
    // older client doesn't accidentally flip behaviour.
    authState.user = { preferences: { submit_on_enter: "true" } };
    const { result } = renderHook(() => useSubmitOnEnter());
    expect(result.current).toBe(false);
  });

  it("returns true when key is exactly boolean true on a fine pointer device", () => {
    authState.user = { preferences: { submit_on_enter: true } };
    const { result } = renderHook(() => useSubmitOnEnter());
    expect(result.current).toBe(true);
  });

  it("returns false on a touch-primary device even when the preference is opted in", () => {
    // On mobile / touch-primary devices there's no easy Shift/Cmd+Enter
    // affordance, so Enter must keep inserting newlines regardless of the
    // user's preference. The submit button stays available.
    authState.user = { preferences: { submit_on_enter: true } };
    coarsePointer.current = true;
    const { result } = renderHook(() => useSubmitOnEnter());
    expect(result.current).toBe(false);
  });

  it("exports the storage key string used by the settings UI", () => {
    expect(SUBMIT_ON_ENTER_KEY).toBe("submit_on_enter");
  });
});

describe("useIssueLinkOpenMode", () => {
  it("defaults to modifier-click when unset", () => {
    authState.user = { preferences: {} };
    const { result } = renderHook(() => useIssueLinkOpenMode());
    expect(result.current).toBe("modifier_click");
  });

  it("returns always-new-tab only for the explicit preference", () => {
    authState.user = { preferences: { [ISSUE_LINK_OPEN_MODE_KEY]: "always_new_tab" } };
    const { result } = renderHook(() => useIssueLinkOpenMode());
    expect(result.current).toBe("always_new_tab");
  });

  it("ignores invalid preference values", () => {
    authState.user = { preferences: { [ISSUE_LINK_OPEN_MODE_KEY]: "new_window" } };
    const { result } = renderHook(() => useIssueLinkOpenMode());
    expect(result.current).toBe("modifier_click");
  });

  it("exports the storage key string used by the settings UI", () => {
    expect(ISSUE_LINK_OPEN_MODE_KEY).toBe("issue_link_open_mode");
  });
});
