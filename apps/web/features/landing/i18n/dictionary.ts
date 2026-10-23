import { docsHrefForLocale } from "@/lib/docs-href";
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
  (allowSignup: boolean, docsHref: string) => LandingDict
> = {
  en: createEnDict,
  fr: createFrDict,
  ja: createJaDict,
  ko: createKoDict,
  zh: createZhDict,
};

// Locales without their own landing copy reuse another dictionary, but docs
// links still follow the viewer's locale.
export function createLandingDict(
  locale: Locale,
  allowSignup: boolean,
): LandingDict {
  return dictionaryFactories[toLandingDictionaryLocale(locale)](
    allowSignup,
    docsHrefForLocale(locale),
  );
}
