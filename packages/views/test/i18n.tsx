import { readFileSync, readdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import {
  render,
  type RenderOptions,
  type RenderResult,
} from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { ReactElement, ReactNode } from "react";
import type { LocaleResources, SupportedLocale } from "@multica/core/i18n";

// Load the requested locale and its English fallback synchronously so render
// keeps Testing Library's API without importing every language into each file.
// Discover namespaces from disk; the parity test checks production registration.
const localesDir = join(dirname(fileURLToPath(import.meta.url)), "../locales");
const namespaces = readdirSync(join(localesDir, "en"))
  .filter((name) => name.endsWith(".json"));
const localeCache = new Map<SupportedLocale, LocaleResources>();

function loadLocale(locale: SupportedLocale): LocaleResources {
  let resources = localeCache.get(locale);
  if (!resources) {
    resources = Object.fromEntries(namespaces.map((filename) => [
      filename.slice(0, -5),
      JSON.parse(readFileSync(join(localesDir, locale, filename), "utf8")),
    ]));
    localeCache.set(locale, resources);
  }
  return resources;
}

export function getTestResources(
  locale: SupportedLocale = "en",
): Record<string, LocaleResources> {
  return locale === "en"
    ? { en: loadLocale("en") }
    : { en: loadLocale("en"), [locale]: loadLocale(locale) };
}

type RenderArgs = Omit<RenderOptions, "wrapper"> & {
  locale?: SupportedLocale;
};

export function renderWithI18n(
  ui: ReactElement,
  options: RenderArgs = {},
): RenderResult {
  const { locale = "en", ...rest } = options;
  const resources = getTestResources(locale);
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <I18nProvider locale={locale} resources={resources}>
        {children}
      </I18nProvider>
    );
  }
  return render(ui, { wrapper: Wrapper, ...rest });
}
