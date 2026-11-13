import { describe, expect, it } from "vitest";
import {
  BEAR_DATA_URIS,
  BEAR_PUBLIC_PATHS,
  BEAR_VARIANTS,
  FOX_DATA_URIS,
  FOX_PUBLIC_PATHS,
  FOX_VARIANTS,
  ANDROID_DATA_URIS,
  ANDROID_PUBLIC_PATHS,
  ANDROID_VARIANTS,
  DRAGON_DATA_URIS,
  DRAGON_PUBLIC_PATHS,
  DRAGON_VARIANTS,
  OWL_DATA_URIS,
  OWL_PUBLIC_PATHS,
  OWL_VARIANTS,
  getBearAvatarAsset,
  getFoxAvatarAsset,
  getIllustratedAvatarAsset,
  getAndroidAvatarAsset,
  getDragonAvatarAsset,
  hasIllustratedAvatarAsset,
  getOwlAvatarAsset,
  type AvatarSizeTier,
} from "@/lib/avatar-catalog";

const TIERS: readonly AvatarSizeTier[] = [32, 48, 96];

function expectCompleteCatalog(
  archetype: "fox" | "bear" | "owl" | "dragon" | "android",
  variants: readonly ("general" | "developer" | "auditor")[],
  data: typeof FOX_DATA_URIS,
  paths: typeof FOX_PUBLIC_PATHS,
) {
  expect(variants).toEqual(["general", "developer", "auditor"]);
  for (const variant of variants) {
    for (const tier of TIERS) {
      expect(data[variant][tier]).toMatch(/^data:image\/webp;base64,[A-Za-z0-9+/=]+$/);
      expect(paths[variant][tier]).toBe(
        `/avatars/characters/${archetype}/${tier}/${variant}.webp`,
      );
    }
  }
}

describe("mobile illustrated avatar catalog", () => {
  it("contains every Fox, Bear, Owl, Dragon, and Android variant at every optimized tier", () => {
    expectCompleteCatalog("fox", FOX_VARIANTS, FOX_DATA_URIS, FOX_PUBLIC_PATHS);
    expectCompleteCatalog("bear", BEAR_VARIANTS, BEAR_DATA_URIS, BEAR_PUBLIC_PATHS);
    expectCompleteCatalog("owl", OWL_VARIANTS, OWL_DATA_URIS, OWL_PUBLIC_PATHS);
    expectCompleteCatalog("dragon", DRAGON_VARIANTS, DRAGON_DATA_URIS, DRAGON_PUBLIC_PATHS);
    expectCompleteCatalog("android", ANDROID_VARIANTS, ANDROID_DATA_URIS, ANDROID_PUBLIC_PATHS);
  });

  it("selects matching pixel tiers for high-density displays", () => {
    expect(getFoxAvatarAsset("general", 20).src).toBe(FOX_DATA_URIS.general[32]);
    expect(getBearAvatarAsset("developer", 32).src).toBe(BEAR_DATA_URIS.developer[48]);
    expect(getOwlAvatarAsset("general", 20).src).toBe(OWL_DATA_URIS.general[32]);
    expect(getAndroidAvatarAsset("developer", 48).src).toBe(ANDROID_DATA_URIS.developer[48]);
    expect(getAndroidAvatarAsset("auditor", 96).src).toBe(ANDROID_DATA_URIS.auditor[96]);
    expect(getDragonAvatarAsset("developer", 48).src).toBe(DRAGON_DATA_URIS.developer[48]);
    expect(getDragonAvatarAsset("auditor", 96).src).toBe(DRAGON_DATA_URIS.auditor[96]);
    expect(getBearAvatarAsset("auditor", 56).src).toBe(BEAR_DATA_URIS.auditor[96]);
    expect(getIllustratedAvatarAsset("bear", "general", 48).archetype).toBe("bear");
  });

  it("falls back to the general variant for unknown runtime input", () => {
    const asset = getBearAvatarAsset("unknown" as never, 32);
    expect(asset.variant).toBe("general");
    expect(asset.src).toBe(BEAR_DATA_URIS.general[48]);
  });

  it("reports asset availability so renderers can fall back to procedural SVG", () => {
    for (const a of ["fox", "bear", "owl", "dragon", "android"]) {
      for (const v of ["general", "developer", "auditor"]) {
        expect(hasIllustratedAvatarAsset(a, v), `${a}:${v}`).toBe(true);
      }
    }
    // Unsupported archetype or unknown variant => no illustrated asset.
    expect(hasIllustratedAvatarAsset("kraken", "general")).toBe(false);
    expect(hasIllustratedAvatarAsset("android", "janitor")).toBe(false);
  });
});
