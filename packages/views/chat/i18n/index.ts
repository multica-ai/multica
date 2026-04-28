"use client";

import { type Locale, useDict } from "@multica/core/i18n";
import { createEnDict } from "./en";
import { createZhTwDict } from "./zh-TW";
import type { ChatDict } from "./types";

const factories: Record<Locale, () => ChatDict> = {
  en: createEnDict,
  "zh-TW": createZhTwDict,
};

export function useChatT(): ChatDict {
  return useDict<ChatDict>(factories);
}

export type { ChatDict } from "./types";
