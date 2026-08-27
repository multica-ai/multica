import { createEnDict } from "./en";
import { createJaDict } from "./ja";
import { createKoDict } from "./ko";
import { createZhDict } from "./zh";
import {
  toLandingDictionaryLocale,
  type LandingDict,
  type Locale,
} from "./types";

/** Server-only: static imports stay out of the landing client graph. */
export function createLandingDict(locale: Locale, allowSignup: boolean): LandingDict {
  switch (toLandingDictionaryLocale(locale)) {
    case "ja":
      return createJaDict(allowSignup);
    case "ko":
      return createKoDict(allowSignup);
    case "zh":
      return createZhDict(allowSignup);
    default:
      return createEnDict(allowSignup);
  }
}
