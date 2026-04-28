"use client";

import { type Locale, useDict } from "@multica/core/i18n";
import { createEnDict } from "./en";
import { createZhTwDict } from "./zh-TW";
import type { SearchDict } from "./types";

const factories: Record<Locale, () => SearchDict> = {
  en: createEnDict,
  "zh-TW": createZhTwDict,
};

export function useSearchT(): SearchDict {
  return useDict<SearchDict>(factories);
}

export type { SearchDict } from "./types";
