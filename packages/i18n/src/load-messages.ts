import type { Locale } from "./locales";
import zhCommon from "./messages/zh-CN/common.json";
import enCommon from "./messages/en/common.json";

// Eager registry — keeps PR #1 simple. Lazy / per-namespace loading lands in
// a follow-up once the codemod produces real per-domain bundles. The structure
// here mirrors the "namespace" concept (top-level keys), not the file split.

export type Messages = Record<string, unknown>;

const REGISTRY: Record<Locale, Messages> = {
  "zh-CN": {
    common: zhCommon,
  },
  en: {
    common: enCommon,
  },
};

export function loadMessages(locale: Locale): Messages {
  return REGISTRY[locale];
}
