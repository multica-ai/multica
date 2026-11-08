// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { APPEARANCE_STORAGE_KEY, appearanceStyleAttribute, useAppearanceStore } from ".";

describe("appearance store", () => {
  beforeEach(() => {
    localStorage.clear();
    useAppearanceStore.setState({ style: "default" });
  });

  it("persists the style on this device", () => {
    useAppearanceStore.getState().setStyle("pop");
    expect(JSON.parse(localStorage.getItem(APPEARANCE_STORAGE_KEY)!).state).toEqual({
      style: "pop",
    });
  });

  it("falls back to the default for an unknown stored style", async () => {
    localStorage.setItem(
      APPEARANCE_STORAGE_KEY,
      JSON.stringify({ state: { style: "brutalist" }, version: 0 }),
    );
    await useAppearanceStore.persist.rehydrate();
    expect(useAppearanceStore.getState().style).toBe("default");
  });

  it("maps the default style to no attribute so the base tokens apply", () => {
    expect(appearanceStyleAttribute("default")).toBeNull();
    expect(appearanceStyleAttribute("pop")).toBe("pop");
  });
});
