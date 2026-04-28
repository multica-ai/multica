"use client";

import { type Locale, useDict } from "@multica/core/i18n";
import { createEnDict } from "./en";
import { createZhTwDict } from "./zh-TW";
import type { RuntimesDict } from "./types";

const factories: Record<Locale, () => RuntimesDict> = {
  en: createEnDict,
  "zh-TW": createZhTwDict,
};

export function useRuntimesT(): RuntimesDict {
  return useDict<RuntimesDict>(factories);
}

export type { RuntimesDict } from "./types";
