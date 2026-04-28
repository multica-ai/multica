"use client";

import { type Locale, useDict } from "@multica/core/i18n";
import { createEnDict } from "./en";
import { createZhTwDict } from "./zh-TW";
import type { AutopilotsDict } from "./types";

const factories: Record<Locale, () => AutopilotsDict> = {
  en: createEnDict,
  "zh-TW": createZhTwDict,
};

export function useAutopilotsT(): AutopilotsDict {
  return useDict<AutopilotsDict>(factories);
}

export type { AutopilotsDict } from "./types";
