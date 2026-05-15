import { describe, expect, it } from "vitest";
import type { StorageAdapter } from "../types/storage";
import { createChatStore } from "./store";

function makeStorage(initial: Record<string, string> = {}): StorageAdapter & {
  snapshot: () => Record<string, string>;
} {
  const data = { ...initial };
  return {
    getItem: (key) => data[key] ?? null,
    setItem: (key, value) => {
      data[key] = value;
    },
    removeItem: (key) => {
      delete data[key];
    },
    snapshot: () => ({ ...data }),
  };
}

describe("chat store open state", () => {
  it("defaults the floating chat window to closed for first-time users", () => {
    const storage = makeStorage();
    const store = createChatStore({ storage });

    expect(store.getState().isOpen).toBe(false);
  });

  it("persists an explicit open preference", () => {
    const storage = makeStorage();
    const store = createChatStore({ storage });

    store.getState().setOpen(true);

    expect(storage.snapshot()["multica:chat:isOpen"]).toBe("true");
    expect(createChatStore({ storage }).getState().isOpen).toBe(true);
  });

  it("persists an explicit closed preference", () => {
    const storage = makeStorage({ "multica:chat:isOpen": "true" });
    const store = createChatStore({ storage });

    store.getState().setOpen(false);

    expect(storage.snapshot()["multica:chat:isOpen"]).toBe("false");
    expect(createChatStore({ storage }).getState().isOpen).toBe(false);
  });
});
