import {
  toLandingDictionaryLocale,
  type LandingDict,
  type LandingDictionaryLocale,
  type Locale,
} from "./types";

async function loadFactory(
  locale: LandingDictionaryLocale,
): Promise<(allowSignup: boolean) => LandingDict> {
  switch (locale) {
    case "ja":
      return (await import("./ja")).createJaDict;
    case "ko":
      return (await import("./ko")).createKoDict;
    case "zh":
      return (await import("./zh")).createZhDict;
    default:
      return (await import("./en")).createEnDict;
  }
}

export async function loadLandingDict(
  locale: Locale,
  allowSignup: boolean,
): Promise<LandingDict> {
  const factory = await loadFactory(toLandingDictionaryLocale(locale));
  return factory(allowSignup);
}
