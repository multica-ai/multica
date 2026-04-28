"use client";

import { type Locale, useDict } from "@multica/core/i18n";
import { createEnDict } from "./en";
import { createZhTwDict } from "./zh-TW";
import type { InboxDict } from "./types";

const factories: Record<Locale, () => InboxDict> = {
  en: createEnDict,
  "zh-TW": createZhTwDict,
};

export function useInboxT(): InboxDict {
  return useDict<InboxDict>(factories);
}

export type { InboxDict } from "./types";
