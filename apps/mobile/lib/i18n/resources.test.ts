// @vitest-environment node
import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

const LOCALES_ROOT = path.resolve(__dirname, "../../locales");

function readLocale(locale: string): [string, unknown][] {
  return fs
    .readdirSync(path.join(LOCALES_ROOT, locale))
    .filter((file) => file.endsWith(".json"))
    .map((file): [string, unknown] => [
      file,
      JSON.parse(
        fs.readFileSync(path.join(LOCALES_ROOT, locale, file), "utf8"),
      ),
    ]);
}

function flatten(value: unknown, prefix = ""): Set<string> {
  const keys = new Set<string>();
  if (typeof value !== "object" || value === null) {
    keys.add(prefix);
    return keys;
  }
  for (const [key, child] of Object.entries(value)) {
    const nextKey = prefix ? `${prefix}.${key}` : key;
    for (const childKey of flatten(child, nextKey)) keys.add(childKey);
  }
  return keys;
}

const TRANSLATED_LOCALES = fs
  .readdirSync(LOCALES_ROOT)
  .filter((entry) => entry !== "en")
  .filter((entry) =>
    fs.statSync(path.join(LOCALES_ROOT, entry)).isDirectory(),
  )
  .sort();

describe("mobile i18n resources", () => {
  const enNamespaces = fs
    .readdirSync(path.join(LOCALES_ROOT, "en"))
    .filter((file) => file.endsWith(".json"))
    .sort();

  for (const locale of TRANSLATED_LOCALES) {
    it(`has an English resource for every supported namespace in ${locale}`, () => {
      const localeNamespaces = fs
        .readdirSync(path.join(LOCALES_ROOT, locale))
        .filter((file) => file.endsWith(".json"))
        .sort();
      expect(localeNamespaces).toEqual(enNamespaces);
    });

    it(`keeps ${locale} keys aligned with English`, () => {
      const enResources = readLocale("en");
      const localeResources = new Map(readLocale(locale));

      for (const [namespace, value] of enResources) {
        const localeValue = localeResources.get(namespace);
        expect(
          localeValue,
          `missing ${locale} namespace: ${namespace}`,
        ).toBeDefined();
        const enKeys = flatten(value);
        const localeKeys = flatten(localeValue);
        // i18next plural rules use `_one` / `_other`; locales with no
        // grammatical number (Chinese, Indonesian) only ship `_other`.
        const missing = [...enKeys].filter((key) => {
          if (localeKeys.has(key)) return false;
          if (
            key.endsWith("_one") &&
            localeKeys.has(`${key.slice(0, -4)}_other`)
          ) {
            return false;
          }
          return true;
        });
        expect(missing, `missing ${locale} keys in ${namespace}`).toEqual([]);
        expect(
          [...localeKeys].filter((key) => !enKeys.has(key)),
          `extra ${locale} keys in ${namespace}`,
        ).toEqual([]);
      }
    });
  }
});
