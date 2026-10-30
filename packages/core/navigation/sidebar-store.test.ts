// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";

const storage = vi.hoisted(() => new Map<string, string>());
vi.mock("../platform/storage", () => ({
  defaultStorage: {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
    removeItem: (key: string) => storage.delete(key),
  },
}));

import { SIDEBAR_ITEMS, useSidebarPreferencesStore } from "./sidebar-store";

beforeEach(() => {
  storage.clear();
  useSidebarPreferencesStore.setState({ hiddenItems: [] });
});

describe("sidebar visibility preferences", () => {
  it("shows every item by default", () => {
    expect(useSidebarPreferencesStore.getState().hiddenItems).toEqual([]);
  });

  it.each(SIDEBAR_ITEMS)("can hide and restore %s independently", (item) => {
    const { setItemVisible } = useSidebarPreferencesStore.getState();
    setItemVisible(item, false);
    setItemVisible(item, false);
    expect(useSidebarPreferencesStore.getState().hiddenItems).toEqual([item]);
    setItemVisible(item, true);
    expect(useSidebarPreferencesStore.getState().hiddenItems).toEqual([]);
  });

  it("restores the saved choices after rehydration", async () => {
    const { setItemVisible } = useSidebarPreferencesStore.getState();
    setItemVisible("my_issues", false);
    setItemVisible("squads", false);
    const saved = storage.get("multica_sidebar_preferences")!;
    setItemVisible("my_issues", true);
    setItemVisible("squads", true);
    storage.set("multica_sidebar_preferences", saved);
    await useSidebarPreferencesStore.persist.rehydrate();
    expect(useSidebarPreferencesStore.getState().hiddenItems).toEqual(["my_issues", "squads"]);
  });

  it("ignores unknown entries and keeps Settings reachable", async () => {
    storage.set("multica_sidebar_preferences", JSON.stringify({
      state: { hiddenItems: ["squads", "settings", "unknown", "squads", null] },
      version: 0,
    }));
    await useSidebarPreferencesStore.persist.rehydrate();
    expect(useSidebarPreferencesStore.getState().hiddenItems).toEqual(["squads"]);
  });

  it.each([null, {}, { hiddenItems: null }, { hiddenItems: "squads" }])(
    "shows all items for invalid stored preferences: %j",
    async (state) => {
      useSidebarPreferencesStore.getState().setItemVisible("squads", false);
      storage.set("multica_sidebar_preferences", JSON.stringify({ state, version: 0 }));
      await useSidebarPreferencesStore.persist.rehydrate();
      expect(useSidebarPreferencesStore.getState().hiddenItems).toEqual([]);
    },
  );
});
