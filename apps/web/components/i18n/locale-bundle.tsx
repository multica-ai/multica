"use client";

import type { ComponentType, ReactNode } from "react";
import dynamic from "next/dynamic";
import type { LocaleResources, SupportedLocale } from "@multica/core/i18n";

export interface LocaleBundleProps {
  children: (resources: Record<string, LocaleResources>) => ReactNode;
}

// Keep SSR enabled: the same bundle must be ready for server rendering and
// hydration. The resource callback stays within the client component tree.
const BUNDLES: Record<SupportedLocale, ComponentType<LocaleBundleProps>> = {
  en: dynamic<LocaleBundleProps>(() => import("./bundles/en")),
  "zh-Hans": dynamic<LocaleBundleProps>(() => import("./bundles/zh-Hans")),
  "zh-Hant": dynamic<LocaleBundleProps>(() => import("./bundles/zh-Hant")),
  ko: dynamic<LocaleBundleProps>(() => import("./bundles/ko")),
  ja: dynamic<LocaleBundleProps>(() => import("./bundles/ja")),
  fr: dynamic<LocaleBundleProps>(() => import("./bundles/fr")),
  es: dynamic<LocaleBundleProps>(() => import("./bundles/es")),
  ru: dynamic<LocaleBundleProps>(() => import("./bundles/ru")),
};

export function LocaleBundle({
  locale,
  children,
}: { locale: SupportedLocale } & LocaleBundleProps) {
  const Bundle = BUNDLES[locale];
  return <Bundle>{children}</Bundle>;
}
