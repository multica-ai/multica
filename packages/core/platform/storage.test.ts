import { afterEach, describe, expect, it } from "vitest";
import { defaultStorage, setDefaultStorageAdapter } from "./storage";
import type { StorageAdapter } from "../types/storage";

afterEach(() => {
  setDefaultStorageAdapter(defaultStorage);
});

describe("defaultStorage", () => {
  it("does not recurse when registered as the default adapter", () => {
    setDefaultStorageAdapter(defaultStorage);

    expect(defaultStorage.getItem("missing")).toBeNull();
  });

  it("delegates to a configured adapter", () => {
    const adapter: StorageAdapter = {
      getItem: (key) => (key === "token" ? "value" : null),
      setItem: () => {},
      removeItem: () => {},
    };

    setDefaultStorageAdapter(adapter);

    expect(defaultStorage.getItem("token")).toBe("value");
  });
});
