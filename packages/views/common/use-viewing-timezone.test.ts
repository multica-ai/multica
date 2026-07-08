import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook } from "@testing-library/react";

const userRef = vi.hoisted(
  () => ({ current: null as { timezone?: string | null } | null }),
);

vi.mock("@multica/core/auth", () => {
  type AuthState = { user: typeof userRef.current };
  const useAuthStore = Object.assign(
    (sel: (s: AuthState) => unknown) => sel({ user: userRef.current }),
    { getState: () => ({ user: userRef.current }) },
  );
  return { useAuthStore };
});

vi.mock("./timezone-select", () => ({
  browserTimezone: () => "America/Chicago",
}));

import { useViewingTimezone } from "./use-viewing-timezone";

describe("useViewingTimezone", () => {
  beforeEach(() => {
    userRef.current = null;
  });

  it("returns the stored preference when the user pinned one", () => {
    userRef.current = { timezone: "Asia/Tokyo" };
    const { result } = renderHook(() => useViewingTimezone());
    expect(result.current).toBe("Asia/Tokyo");
  });

  it("falls back to the browser tz when there is no user", () => {
    userRef.current = null;
    const { result } = renderHook(() => useViewingTimezone());
    expect(result.current).toBe("America/Chicago");
  });

  it("falls back to the browser tz when timezone is null", () => {
    userRef.current = { timezone: null };
    const { result } = renderHook(() => useViewingTimezone());
    expect(result.current).toBe("America/Chicago");
  });

  it("falls back to the browser tz when timezone is blank", () => {
    userRef.current = { timezone: "   " };
    const { result } = renderHook(() => useViewingTimezone());
    expect(result.current).toBe("America/Chicago");
  });

  // The preferences clear-flow PATCHes timezone: "" and the server may echo
  // the empty string back before normalising it to null. The hook must
  // treat "" as "no preference" and fall back to the browser tz.
  it("falls back to the browser tz when timezone is an empty string", () => {
    userRef.current = { timezone: "" };
    const { result } = renderHook(() => useViewingTimezone());
    expect(result.current).toBe("America/Chicago");
  });

  // Auth store still initialising: user is undefined, not null.
  it("falls back to the browser tz when the user is undefined", () => {
    userRef.current = undefined as never;
    const { result } = renderHook(() => useViewingTimezone());
    expect(result.current).toBe("America/Chicago");
  });
});
