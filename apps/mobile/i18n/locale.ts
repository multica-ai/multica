export type MobileLocale = "en" | "zh-Hans";

export interface DeviceLocale {
  languageCode?: string | null;
  languageTag?: string | null;
  languageScriptCode?: string | null;
  regionCode?: string | null;
}

const TRADITIONAL_CHINESE_REGIONS = new Set(["HK", "MO", "TW"]);

export function resolveMobileLocale(
  locales: readonly DeviceLocale[],
): MobileLocale {
  const primary = locales[0];
  if (!primary || primary.languageCode?.toLowerCase() !== "zh") return "en";

  const tag = primary.languageTag?.toLowerCase() ?? "";
  const script = primary.languageScriptCode?.toLowerCase() ?? "";
  const region = primary.regionCode?.toUpperCase() ?? "";
  const tagRegion = tag.split("-").at(-1)?.toUpperCase() ?? "";
  if (
    script === "hant" ||
    tag.includes("-hant") ||
    TRADITIONAL_CHINESE_REGIONS.has(region) ||
    TRADITIONAL_CHINESE_REGIONS.has(tagRegion)
  ) {
    return "en";
  }

  return "zh-Hans";
}
