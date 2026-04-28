"use client";

import { type Locale, useDict } from "@multica/core/i18n";
import { createEnDict } from "./en";
import { createZhTwDict } from "./zh-TW";
import type { InviteDict } from "./types";

const factories: Record<Locale, () => InviteDict> = {
  en: createEnDict,
  "zh-TW": createZhTwDict,
};

export function useInviteT(): InviteDict {
  return useDict<InviteDict>(factories);
}

export type { InviteDict } from "./types";
