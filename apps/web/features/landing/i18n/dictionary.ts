import { createEnDict } from "./en";
import { createFrDict } from "./fr";
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
  fr: createFrDict,
  ja: createJaDict,
  ko: createKoDict,
  zh: createZhDict,
};

// Locales without their own landing copy (e.g. Spanish) reuse another dictionary.
export function createLandingDict(locale: Locale): LandingDict {
  return dictionaryFactories[toLandingDictionaryLocale(locale)]();
}
