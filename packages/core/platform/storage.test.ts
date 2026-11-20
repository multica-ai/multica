// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { defaultStorage } from "./storage";

describe("defaultStorage.subscribe", () => {
  it("reports keys changed by other tabs until unsubscribed", () => {
    const listener = vi.fn();
    const unsubscribe = defaultStorage.subscribe!(listener);

    window.dispatchEvent(new StorageEvent("storage", { key: "some_key" }));
    window.dispatchEvent(new StorageEvent("storage", { key: null }));
    expect(listener.mock.calls).toEqual([["some_key"], [null]]);

    unsubscribe();
    window.dispatchEvent(new StorageEvent("storage", { key: "some_key" }));
    expect(listener).toHaveBeenCalledTimes(2);
  });
});

describe("defaultStorage.update", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    localStorage.clear();
  });

  it("reads and writes inside one Web Lock named after the key", async () => {
    const events: string[] = [];
    vi.stubGlobal("navigator", {
      locks: {
        request: async (name: string, callback: () => void) => {
          events.push(`lock ${name}`);
          callback();
          events.push(`unlock ${name}`);
        },
      },
    });
    localStorage.setItem("k", "1");

    await defaultStorage.update!("k", (current) => {
      events.push(`read ${current}`);
      return "2";
    });

    expect(events).toEqual(["lock multica_storage:k", "read 1", "unlock multica_storage:k"]);
    expect(localStorage.getItem("k")).toBe("2");
  });

  it("leaves the key alone when the updater returns null", async () => {
    vi.stubGlobal("navigator", {});
    localStorage.setItem("k", "1");
    const setItem = vi.spyOn(localStorage, "setItem");

    await defaultStorage.update!("k", () => null);

    expect(setItem).not.toHaveBeenCalled();
    expect(localStorage.getItem("k")).toBe("1");
  });

  it("falls back to a plain read and write without Web Locks", async () => {
    vi.stubGlobal("navigator", {});

    await defaultStorage.update!("k", (current) => `${current}!`);

    expect(localStorage.getItem("k")).toBe("null!");
  });

  it("rejects when the write fails", async () => {
    vi.stubGlobal("navigator", {});
    vi.spyOn(localStorage, "setItem").mockImplementation(() => {
      throw new DOMException("The quota has been exceeded.", "QuotaExceededError");
    });

    await expect(defaultStorage.update!("k", () => "x")).rejects.toThrow("quota");
  });
});
