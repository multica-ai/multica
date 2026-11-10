import { createEnDict } from "./en";
import { createJaDict } from "./ja";
import { createKoDict } from "./ko";
import { createZhDict } from "./zh";
import {
  toLandingDictionaryLocale,
  type LandingDict,
  type LandingDictionaryLocale,
  type Locale,
} from "./types";

const dictionaryFactories: Record<
  LandingDictionaryLocale,
  () => LandingDict
> = {
  en: createEnDict,
  ja: createJaDict,
  ko: createKoDict,
  zh: createZhDict,
};

// Locales without their own landing copy (e.g. French) reuse another dictionary.
export function createLandingDict(locale: Locale): LandingDict {
  return dictionaryFactories[toLandingDictionaryLocale(locale)]();
}
