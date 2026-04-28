"use client";

import { type Locale, useDict } from "@multica/core/i18n";
import { createEnDict } from "./en";
import { createZhTwDict } from "./zh-TW";
import type { AuthDict } from "./types";

const factories: Record<Locale, () => AuthDict> = {
  en: createEnDict,
  "zh-TW": createZhTwDict,
};

export function useAuthT(): AuthDict {
  return useDict<AuthDict>(factories);
}

export type { AuthDict } from "./types";
