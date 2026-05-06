import { describe, expect, it, vi } from "vitest";
import { applyAppBadge, resolveBadgeCount } from "./sw-badge";

describe("applyAppBadge", () => {
  it("calls setAppBadge with the count when the count is positive", async () => {
    const nav = {
      setAppBadge: vi.fn(() => Promise.resolve()),
      clearAppBadge: vi.fn(() => Promise.resolve()),
    };
    await applyAppBadge(nav, 7);
    expect(nav.setAppBadge).toHaveBeenCalledWith(7);
    expect(nav.clearAppBadge).not.toHaveBeenCalled();
  });

  it("calls clearAppBadge when the count is zero", async () => {
    const nav = {
      setAppBadge: vi.fn(() => Promise.resolve()),
      clearAppBadge: vi.fn(() => Promise.resolve()),
    };
    await applyAppBadge(nav, 0);
    expect(nav.clearAppBadge).toHaveBeenCalled();
    expect(nav.setAppBadge).not.toHaveBeenCalled();
  });

  it("calls clearAppBadge for negative counts (defensive)", async () => {
    const nav = {
      setAppBadge: vi.fn(() => Promise.resolve()),
      clearAppBadge: vi.fn(() => Promise.resolve()),
    };
    await applyAppBadge(nav, -3);
    expect(nav.clearAppBadge).toHaveBeenCalled();
    expect(nav.setAppBadge).not.toHaveBeenCalled();
  });

  it("is a no-op when count is undefined (server didn't stamp the field)", () => {
    const nav = {
      setAppBadge: vi.fn(() => Promise.resolve()),
      clearAppBadge: vi.fn(() => Promise.resolve()),
    };
    const result = applyAppBadge(nav, undefined);
    expect(result).toBeUndefined();
    expect(nav.setAppBadge).not.toHaveBeenCalled();
    expect(nav.clearAppBadge).not.toHaveBeenCalled();
  });

  it("is a no-op when count is NaN (defensive against malformed payload)", () => {
    const nav = {
      setAppBadge: vi.fn(() => Promise.resolve()),
      clearAppBadge: vi.fn(() => Promise.resolve()),
    };
    const result = applyAppBadge(nav, Number.NaN);
    expect(result).toBeUndefined();
    expect(nav.setAppBadge).not.toHaveBeenCalled();
    expect(nav.clearAppBadge).not.toHaveBeenCalled();
  });

  it("does not throw when the navigator is missing the Badging API", () => {
    // Older Safari, Firefox: setAppBadge / clearAppBadge are undefined.
    expect(applyAppBadge({}, 5)).toBeUndefined();
    expect(applyAppBadge({}, 0)).toBeUndefined();
  });

  it("swallows rejected setAppBadge promises so the SW push handler isn't broken", async () => {
    const nav = {
      setAppBadge: vi.fn(() => Promise.reject(new Error("bad"))),
      clearAppBadge: vi.fn(() => Promise.resolve()),
    };
    await expect(applyAppBadge(nav, 4)).resolves.toBeUndefined();
  });
});

describe("resolveBadgeCount", () => {
  function jsonResponse(body: unknown, ok = true): Response {
    return {
      ok,
      json: () => Promise.resolve(body),
    } as Response;
  }

  it("trusts the payload count when it is a finite number", async () => {
    const fetchImpl = vi.fn();
    await expect(resolveBadgeCount(7, fetchImpl)).resolves.toBe(7);
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("trusts payload count of 0 (no fallback fetch)", async () => {
    const fetchImpl = vi.fn();
    await expect(resolveBadgeCount(0, fetchImpl)).resolves.toBe(0);
    expect(fetchImpl).not.toHaveBeenCalled();
  });

  it("falls back to /api/me/inbox/unread-count when payload count is undefined", async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(jsonResponse({ count: 4 })),
    ) as unknown as typeof fetch;
    await expect(resolveBadgeCount(undefined, fetchImpl)).resolves.toBe(4);
    expect(fetchImpl).toHaveBeenCalledWith("/api/me/inbox/unread-count", {
      credentials: "same-origin",
    });
  });

  it("falls back when payload count is NaN", async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(jsonResponse({ count: 2 })),
    ) as unknown as typeof fetch;
    await expect(resolveBadgeCount(Number.NaN, fetchImpl)).resolves.toBe(2);
  });

  it("returns undefined when the fallback fetch errors", async () => {
    const fetchImpl = vi.fn(() =>
      Promise.reject(new Error("offline")),
    ) as unknown as typeof fetch;
    await expect(resolveBadgeCount(undefined, fetchImpl)).resolves.toBeUndefined();
  });

  it("returns undefined when the fallback responds non-OK", async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(jsonResponse({ count: 9 }, false)),
    ) as unknown as typeof fetch;
    await expect(resolveBadgeCount(undefined, fetchImpl)).resolves.toBeUndefined();
  });

  it("returns undefined when the fallback body has no numeric count", async () => {
    const fetchImpl = vi.fn(() =>
      Promise.resolve(jsonResponse({ count: "five" })),
    ) as unknown as typeof fetch;
    await expect(resolveBadgeCount(undefined, fetchImpl)).resolves.toBeUndefined();
  });
});
