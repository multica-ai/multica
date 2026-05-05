import { describe, it, expect } from "vitest";
import { interpolate, getAllKeys } from "./core";
import { en } from "./dict/en";
import { zh } from "./dict/zh";

describe("interpolate", () => {
  it("returns template unchanged when no params", () => {
    expect(interpolate("Hello")).toBe("Hello");
  });

  it("replaces named params", () => {
    expect(interpolate("Hello {name}", { name: "World" })).toBe("Hello World");
  });

  it("replaces multiple params", () => {
    expect(interpolate("{a} + {b}", { a: 1, b: 2 })).toBe("1 + 2");
  });

  it("leaves unreferenced params in template", () => {
    expect(interpolate("{missing}")).toBe("{missing}");
  });
});

describe("dictionary completeness", () => {
  it("zh has every key that en has", () => {
    const enKeys = getAllKeys(en);
    const zhKeys = getAllKeys(zh);
    const missing = [...enKeys].filter((k) => !zhKeys.has(k));
    expect(missing, `Missing zh keys: ${missing.join(", ")}`).toEqual([]);
  });

  it("en has every key that zh has", () => {
    const enKeys = getAllKeys(en);
    const zhKeys = getAllKeys(zh);
    const extra = [...zhKeys].filter((k) => !enKeys.has(k));
    expect(extra, `Extra zh keys: ${extra.join(", ")}`).toEqual([]);
  });
});
