import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";

const route = vi.hoisted(() => ({ pathname: null as string | null }));

vi.mock("next/navigation", () => ({
  usePathname: () => route.pathname,
}));

import { useUserLocaleSyncEnabled } from "./user-locale-sync";

describe("useUserLocaleSyncEnabled", () => {
  beforeEach(() => {
    route.pathname = null;
  });

  it("waits for the callback to finish before resuming locale synchronization", () => {
    const { result, rerender } = renderHook(useUserLocaleSyncEnabled);
    expect(result.current).toBe(false);

    route.pathname = "/auth/callback";
    rerender();
    expect(result.current).toBe(false);

    route.pathname = "/onboarding";
    rerender();
    expect(result.current).toBe(true);
  });

  it("also pauses on a callback path with a trailing slash", () => {
    route.pathname = "/auth/callback/";

    const { result } = renderHook(useUserLocaleSyncEnabled);

    expect(result.current).toBe(false);
  });
});
