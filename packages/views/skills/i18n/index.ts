"use client";

import { type Locale, useDict } from "@multica/core/i18n";
import { createEnDict } from "./en";
import { createZhTwDict } from "./zh-TW";
import type { SkillsDict } from "./types";

const factories: Record<Locale, () => SkillsDict> = {
  en: createEnDict,
  "zh-TW": createZhTwDict,
};

export function useSkillsT(): SkillsDict {
  return useDict<SkillsDict>(factories);
}

export type { SkillsDict } from "./types";
