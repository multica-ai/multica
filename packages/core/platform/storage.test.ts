// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
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
