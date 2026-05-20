import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act } from "@testing-library/react";

const userState = { user: null as { preferences?: Record<string, unknown> } | null };

vi.mock("@multica/core/auth", () => {
  const useAuthStore = (selector: (s: typeof userState) => unknown) =>
    selector(userState);
  Object.assign(useAuthStore, { getState: () => userState });
  return { useAuthStore };
});

import {
  useApplyStartPage,
  getPreferredStartPage,
} from "../views/components/use-apply-start-page";

beforeEach(() => {
  userState.user = null;
});

describe("getPreferredStartPage", () => {
  it("returns null when user is null", () => {
    expect(getPreferredStartPage(null, "desktop")).toBeNull();
  });

  it("returns null when preference is missing", () => {
    expect(getPreferredStartPage({ preferences: {} }, "desktop")).toBeNull();
  });

  it("returns the desktop preference when set", () => {
    expect(
      getPreferredStartPage(
        { preferences: { start_page_desktop: "inbox" } },
        "desktop",
      ),
    ).toBe("inbox");
  });

  it("returns the mobile preference when set", () => {
    expect(
      getPreferredStartPage(
        { preferences: { start_page_mobile: "my-issues" } },
        "mobile",
      ),
    ).toBe("my-issues");
  });

  it("rejects invalid preference values", () => {
    expect(
      getPreferredStartPage(
        { preferences: { start_page_desktop: "evil-route" } },
        "desktop",
      ),
    ).toBeNull();
  });
});

describe("useApplyStartPage", () => {
  it("does not navigate when not ready", () => {
    userState.user = { preferences: { start_page_desktop: "inbox" } };
    const navigate = vi.fn();
    renderHook(() =>
      useApplyStartPage({ platform: "desktop", ready: false, navigate }),
    );
    expect(navigate).not.toHaveBeenCalled();
  });

  it("does not navigate when no user", () => {
    userState.user = null;
    const navigate = vi.fn();
    renderHook(() =>
      useApplyStartPage({ platform: "desktop", ready: true, navigate }),
    );
    expect(navigate).not.toHaveBeenCalled();
  });

  it("navigates once when user + ready + preference set", () => {
    userState.user = { preferences: { start_page_desktop: "inbox" } };
    const navigate = vi.fn();
    const { rerender } = renderHook(() =>
      useApplyStartPage({ platform: "desktop", ready: true, navigate }),
    );
    expect(navigate).toHaveBeenCalledTimes(1);
    expect(navigate).toHaveBeenCalledWith("inbox");
    rerender();
    expect(navigate).toHaveBeenCalledTimes(1);
  });

  it("does not navigate when preference is missing", () => {
    userState.user = { preferences: {} };
    const navigate = vi.fn();
    renderHook(() =>
      useApplyStartPage({ platform: "desktop", ready: true, navigate }),
    );
    expect(navigate).not.toHaveBeenCalled();
  });

  it("resets latch on logout so next login re-applies", () => {
    userState.user = { preferences: { start_page_mobile: "documents" } };
    const navigate = vi.fn();
    const { rerender } = renderHook(() =>
      useApplyStartPage({ platform: "mobile", ready: true, navigate }),
    );
    expect(navigate).toHaveBeenCalledTimes(1);

    act(() => {
      userState.user = null;
    });
    rerender();

    act(() => {
      userState.user = { preferences: { start_page_mobile: "documents" } };
    });
    rerender();

    expect(navigate).toHaveBeenCalledTimes(2);
  });
});
