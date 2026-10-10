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

import { useMobileAppearanceStore } from "./mobile-appearance-store";

const KEY = "multica_mobile_appearance";

beforeEach(() => {
  storage.clear();
  useMobileAppearanceStore.setState({ fontSize: "default", contentWidth: "standard" });
});

describe("mobile appearance store", () => {
  it("defaults to the unchanged appearance", () => {
    const { fontSize, contentWidth } = useMobileAppearanceStore.getState();
    expect({ fontSize, contentWidth }).toEqual({
      fontSize: "default",
      contentWidth: "standard",
    });
  });

  it("persists only the two preference values, locally", () => {
    useMobileAppearanceStore.getState().setFontSize("large");
    useMobileAppearanceStore.getState().setContentWidth("full");
    expect(JSON.parse(storage.get(KEY)!).state).toEqual({
      fontSize: "large",
      contentWidth: "full",
    });
  });

  it("rehydrates saved choices", async () => {
    storage.set(
      KEY,
      JSON.stringify({ state: { fontSize: "small", contentWidth: "full" }, version: 0 }),
    );
    await useMobileAppearanceStore.persist.rehydrate();
    const s = useMobileAppearanceStore.getState();
    expect([s.fontSize, s.contentWidth]).toEqual(["small", "full"]);
  });

  it.each([
    [null],
    [{}],
    [{ fontSize: "huge", contentWidth: "wide" }],
    [{ fontSize: 3, contentWidth: false }],
    ["garbage"],
  ])("falls back to defaults for invalid stored state: %j", async (state) => {
    useMobileAppearanceStore.getState().setFontSize("large");
    useMobileAppearanceStore.getState().setContentWidth("full");
    storage.set(KEY, JSON.stringify({ state, version: 0 }));
    await useMobileAppearanceStore.persist.rehydrate();
    const s = useMobileAppearanceStore.getState();
    expect([s.fontSize, s.contentWidth]).toEqual(["default", "standard"]);
  });

  it("keeps a valid field when the other one is invalid", async () => {
    storage.set(
      KEY,
      JSON.stringify({ state: { fontSize: "small", contentWidth: "bogus" }, version: 0 }),
    );
    await useMobileAppearanceStore.persist.rehydrate();
    const s = useMobileAppearanceStore.getState();
    expect([s.fontSize, s.contentWidth]).toEqual(["small", "standard"]);
  });

  it("rejects invalid values passed to setters", () => {
    useMobileAppearanceStore.getState().setFontSize("huge" as never);
    useMobileAppearanceStore.getState().setContentWidth("wide" as never);
    const s = useMobileAppearanceStore.getState();
    expect([s.fontSize, s.contentWidth]).toEqual(["default", "standard"]);
  });
});
