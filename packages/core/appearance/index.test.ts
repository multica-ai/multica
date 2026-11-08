// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest";
import { APPEARANCE_STORAGE_KEY, appearanceAttributes, useAppearanceStore } from ".";

describe("appearance store", () => {
  beforeEach(() => {
    localStorage.clear();
    useAppearanceStore.setState({ style: "default", frame: "neutral" });
  });

  it("persists the style and frame on this device", () => {
    useAppearanceStore.getState().setStyle("pop");
    useAppearanceStore.getState().setFrame("mist");
    expect(JSON.parse(localStorage.getItem(APPEARANCE_STORAGE_KEY)!).state).toEqual({
      style: "pop",
      frame: "mist",
    });
  });

  it("falls back to the defaults for unknown or missing stored values", async () => {
    localStorage.setItem(
      APPEARANCE_STORAGE_KEY,
      JSON.stringify({ state: { style: "brutalist", frame: 42 }, version: 0 }),
    );
    await useAppearanceStore.persist.rehydrate();
    expect(useAppearanceStore.getState()).toMatchObject({ style: "default", frame: "neutral" });
  });

  it("maps the defaults to no attribute so the base tokens apply", () => {
    expect(appearanceAttributes({ style: "default", frame: "neutral" })).toEqual({
      "data-style": null,
      "data-frame": null,
    });
    expect(appearanceAttributes({ style: "pop", frame: "sand" })).toEqual({
      "data-style": "pop",
      "data-frame": "sand",
    });
  });
});
