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
      tier: null,
    });
  });

  it("reads a brand plus capability tier", () => {
    expect(parseAvatarBrand("brand:grok/gold")).toEqual({
      id: "grok",
      tier: "gold",
    });
  });

  it("rejects unknown brands, unknown tiers, and other avatar shapes", () => {
    expect(parseAvatarBrand("brand:not-a-model")).toBeNull();
    expect(parseAvatarBrand("brand:claude/flagship")).toBeNull();
    expect(parseAvatarBrand("emoji:🚀")).toBeNull();
    expect(parseAvatarBrand("https://cdn.example/a.png")).toBeNull();
    expect(parseAvatarBrand("")).toBeNull();
    expect(parseAvatarBrand(null)).toBeNull();
  });
});

describe("formatAvatarBrand", () => {
  it("round-trips with and without a tier", () => {
    expect(formatAvatarBrand("gpt")).toBe("brand:gpt");
    expect(formatAvatarBrand("gpt", null)).toBe("brand:gpt");
    expect(formatAvatarBrand("gpt", "crown")).toBe("brand:gpt/crown");
    expect(parseAvatarBrand(formatAvatarBrand("devin", "diamond"))).toEqual({
      id: "devin",
      tier: "diamond",
    });
  });

  it("covers every catalog id the picker offers", () => {
    for (const brand of AVATAR_BRANDS) {
      expect(parseAvatarBrand(formatAvatarBrand(brand.id, "gold"))?.id).toBe(
        brand.id,
      );
    }
  });
});
