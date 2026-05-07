import { describe, it, expect, vi, beforeEach } from "vitest";
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

import { useSubmitOnEnter, SUBMIT_ON_ENTER_KEY } from "./use-submit-on-enter";

beforeEach(() => {
  authState.user = null;
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

  it("returns true when key is exactly boolean true", () => {
    authState.user = { preferences: { submit_on_enter: true } };
    const { result } = renderHook(() => useSubmitOnEnter());
    expect(result.current).toBe(true);
  });

  it("exports the storage key string used by the settings UI", () => {
    expect(SUBMIT_ON_ENTER_KEY).toBe("submit_on_enter");
  });
});
