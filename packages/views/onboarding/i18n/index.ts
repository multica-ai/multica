"use client";

import { type Locale, useDict } from "@multica/core/i18n";
import { createEnDict } from "./en";
import { createZhTwDict } from "./zh-TW";
import type { OnboardingDict } from "./types";

const factories: Record<Locale, () => OnboardingDict> = {
  en: createEnDict,
  "zh-TW": createZhTwDict,
};

export function useOnboardingT(): OnboardingDict {
  return useDict<OnboardingDict>(factories);
}

export type { OnboardingDict } from "./types";
