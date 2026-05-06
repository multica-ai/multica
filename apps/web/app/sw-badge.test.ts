import { describe, expect, it, vi } from "vitest";
import { applyAppBadge } from "./sw-badge";

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
