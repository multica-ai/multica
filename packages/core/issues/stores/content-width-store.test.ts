// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";

const storage = vi.hoisted(() => new Map<string, string>());
vi.mock("../../platform/storage", () => ({
  defaultStorage: {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
    removeItem: (key: string) => storage.delete(key),
  },
}));

import { useContentWidthStore } from "./content-width-store";

const key = "multica_content_width";

beforeEach(() => {
  storage.clear();
  useContentWidthStore.setState({ contentWidth: "standard" });
});

describe("content width preference", () => {
  it("starts at Standard before hydration and saves a device-wide choice", async () => {
    expect(useContentWidthStore.getState().contentWidth).toBe("standard");
    useContentWidthStore.getState().setContentWidth("full");
    expect(JSON.parse(storage.get(key)!).state).toEqual({ contentWidth: "full" });
    useContentWidthStore.getState().setContentWidth("standard");
    storage.set(key, JSON.stringify({ state: { contentWidth: "full" }, version: 0 }));
    await useContentWidthStore.persist.rehydrate();
    expect(useContentWidthStore.getState().contentWidth).toBe("full");
  });

  it.each([null, {}, { contentWidth: "wide" }, { contentWidth: 1 }, []])(
    "falls back to Standard for invalid saved shape %j",
    async (state) => {
      useContentWidthStore.getState().setContentWidth("full");
      storage.set(key, JSON.stringify({ state, version: 0 }));
      await useContentWidthStore.persist.rehydrate();
      expect(useContentWidthStore.getState().contentWidth).toBe("standard");
    },
  );

  it("falls back to Standard for missing or corrupt JSON", async () => {
    useContentWidthStore.getState().setContentWidth("full");
    storage.delete(key);
    await useContentWidthStore.persist.rehydrate();
    expect(useContentWidthStore.getState().contentWidth).toBe("standard");
    useContentWidthStore.getState().setContentWidth("full");
    storage.set(key, "{broken");
    await useContentWidthStore.persist.rehydrate();
    expect(useContentWidthStore.getState().contentWidth).toBe("standard");
  });
});
