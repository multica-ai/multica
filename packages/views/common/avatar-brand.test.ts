import { describe, expect, it } from "vitest";
import {
  AVATAR_BRANDS,
  formatAvatarBrand,
  parseAvatarBrand,
} from "@multica/ui/lib/avatar-brand";

describe("parseAvatarBrand", () => {
  it("reads a bare brand marker", () => {
    expect(parseAvatarBrand("brand:claude")).toEqual({
      id: "claude",
      ring: null,
    });
  });

  it("reads a brand plus capability ring", () => {
    expect(parseAvatarBrand("brand:grok/flagship")).toEqual({
      id: "grok",
      ring: "flagship",
    });
  });

  it("rejects unknown brands, unknown rings, and other avatar shapes", () => {
    expect(parseAvatarBrand("brand:not-a-model")).toBeNull();
    expect(parseAvatarBrand("brand:claude/gold")).toBeNull();
    expect(parseAvatarBrand("emoji:🚀")).toBeNull();
    expect(parseAvatarBrand("https://cdn.example/a.png")).toBeNull();
    expect(parseAvatarBrand("")).toBeNull();
    expect(parseAvatarBrand(null)).toBeNull();
  });
});

describe("formatAvatarBrand", () => {
  it("round-trips with and without a ring", () => {
    expect(formatAvatarBrand("gpt")).toBe("brand:gpt");
    expect(formatAvatarBrand("gpt", null)).toBe("brand:gpt");
    expect(formatAvatarBrand("gpt", "fast")).toBe("brand:gpt/fast");
    expect(parseAvatarBrand(formatAvatarBrand("devin", "standard"))).toEqual({
      id: "devin",
      ring: "standard",
    });
  });

  it("covers every catalog id the picker offers", () => {
    for (const brand of AVATAR_BRANDS) {
      expect(parseAvatarBrand(formatAvatarBrand(brand.id, "flagship"))?.id).toBe(
        brand.id,
      );
    }
  });
});
