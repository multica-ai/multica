import { describe, expect, it } from "vitest";
import { PALETTE_PRESETS, PALETTE_SLOTS, labelTextColor, paletteColor, paletteSlot } from "./palette";
import { THEME_VARIABLE_NAMES } from "./theme-variables";

describe("palette", () => {
  it("puts each preset on its own slot", () => {
    for (const slot of PALETTE_SLOTS) {
      expect(paletteSlot(PALETTE_PRESETS[slot])).toBe(slot);
      expect(paletteSlot(PALETTE_PRESETS[slot].toUpperCase())).toBe(slot);
    }
  });

  it("puts any other colour on the nearest slot", () => {
    expect(paletteSlot("#b91c1c")).toBe("red");
    expect(paletteSlot("#16a34a")).toBe("green");
    expect(paletteSlot("#d1d5db")).toBe("gray");
    expect(paletteSlot("#7c3aed")).toBe("purple");
    expect(paletteSlot("fb923c")).toBe("orange");
    expect(paletteSlot("#111827")).toBe("gray");
    expect(paletteSlot("#ffffff")).toBe("gray");
    expect(paletteSlot("#fecaca")).toBe("red");
    expect(paletteSlot("#1e3a8a")).toBe("blue");
    expect(paletteSlot("#92400e")).toBe("orange");
  });

  it("reads a slot's variable with the colour as picked as its fallback", () => {
    expect(paletteColor("#ef4444")).toBe("var(--palette-red, #ef4444)");
    expect(paletteColor("#b91c1c")).toBe("var(--palette-red, #b91c1c)");
  });

  it("passes anything that is not a hex through", () => {
    expect(paletteSlot("red")).toBeNull();
    expect(paletteColor("red")).toBe("red");
    expect(paletteColor(null)).toBeUndefined();
    expect(paletteColor(undefined)).toBeUndefined();
  });

  it("registers every slot as a theme variable", () => {
    for (const slot of PALETTE_SLOTS) expect(THEME_VARIABLE_NAMES.has(`palette-${slot}`)).toBe(true);
    expect(THEME_VARIABLE_NAMES.has("label-chip-color")).toBe(true);
  });

  it("picks dark text on light labels and light text on dark ones", () => {
    expect(labelTextColor("#eab308")).toBe("#111827");
    expect(labelTextColor("#3b82f6")).toBe("#f9fafb");
    expect(labelTextColor("nope")).toBe("#111827");
  });
});
