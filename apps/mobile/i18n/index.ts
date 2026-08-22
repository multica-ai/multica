import { createI18n } from "@multica/core/i18n/react";
import { EN_ZH_RESOURCES } from "@multica/views/locales/en-zh";
import { getLocales } from "expo-localization";
import type { TOptions } from "i18next";
import enMobile from "./locales/en.json";
import zhHansMobile from "./locales/zh-Hans.json";
import { resolveMobileLocale, type MobileLocale } from "./locale";

type ResourceNode = Record<string, unknown>;

const RESOURCES = {
  en: { ...EN_ZH_RESOURCES.en, mobile: enMobile },
  "zh-Hans": { ...EN_ZH_RESOURCES["zh-Hans"], mobile: zhHansMobile },
};

function walkLeaves(
  node: ResourceNode,
  prefix: string,
  visitor: (path: string, value: string) => void,
) {
  for (const [key, value] of Object.entries(node)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (typeof value === "string") visitor(path, value);
    else if (value && typeof value === "object") {
      walkLeaves(value as ResourceNode, path, visitor);
    }
  }
}

function buildSharedSourceIndex() {
  const candidates = new Map<string, { key: string; zh: string }[]>();
  for (const [namespace, bundle] of Object.entries(EN_ZH_RESOURCES.en)) {
    const zhBundle = EN_ZH_RESOURCES["zh-Hans"][namespace];
    if (!zhBundle) continue;
    const zhByPath = new Map<string, string>();
    walkLeaves(zhBundle, "", (path, value) => zhByPath.set(path, value));
    walkLeaves(bundle as ResourceNode, "", (path, value) => {
      const zh = zhByPath.get(path);
      if (!zh) return;
      const entries = candidates.get(value) ?? [];
      entries.push({ key: `${namespace}:${path}`, zh });
      candidates.set(value, entries);
    });
  }

  const index = new Map<string, string>();
  for (const [source, entries] of candidates) {
    if (new Set(entries.map((entry) => entry.zh)).size === 1) {
      index.set(source, entries[0].key);
    }
  }
  return index;
}

function slugifySource(source: string): string {
  return source
    .replace(/&(?:apos|quot|ldquo|rdquo);/g, " ")
    .replace(/\{\{\s*([a-zA-Z0-9_]+)\s*\}\}/g, " $1 ")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
}

const SHARED_SOURCE_INDEX = buildSharedSourceIndex();
const MOBILE_SOURCE_INDEX = new Map<string, string>();
walkLeaves(enMobile, "", (path, source) => {
  MOBILE_SOURCE_INDEX.set(source, `mobile:${path}`);
});

function safeInterpolate(source: string, options?: TOptions): string {
  return source.replace(/\{\{\s*([a-zA-Z0-9_]+)\s*\}\}/g, (_, name: string) => {
    const value = options?.[name as keyof TOptions];
    return value == null ? "" : String(value);
  });
}

function detectLocale(): MobileLocale {
  try {
    return resolveMobileLocale(getLocales());
  } catch {
    return "en";
  }
}

export const mobileLocale = detectLocale();

let instance: ReturnType<typeof createI18n> | null = null;
try {
  instance = createI18n(mobileLocale, RESOURCES);
} catch (error) {
  console.warn("[i18n] initialization failed; using English fallback", error);
  try {
    instance = createI18n("en", RESOURCES);
  } catch {
    instance = null;
  }
}

export function translationKeyForSource(source: string): string {
  return (
    MOBILE_SOURCE_INDEX.get(source) ??
    SHARED_SOURCE_INDEX.get(source) ??
    `mobile:copy.${slugifySource(source)}`
  );
}

export function hasTranslationForSource(source: string): boolean {
  return MOBILE_SOURCE_INDEX.has(source) || SHARED_SOURCE_INDEX.has(source);
}

export function translate(source: string, options?: TOptions): string {
  if (!instance) return safeInterpolate(source, options);
  return String(
    instance.t(translationKeyForSource(source), {
      ...options,
      defaultValue: source,
    }),
  );
}
