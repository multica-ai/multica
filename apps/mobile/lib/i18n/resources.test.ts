// @vitest-environment node
import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { resources } from "./resources";

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

function strings(value: unknown, prefix = ""): [string, string][] {
  if (typeof value === "string") return [[prefix, value]];
  if (!value || typeof value !== "object") return [];
  return Object.entries(value).flatMap(([key, child]) =>
    strings(child, prefix ? `${prefix}.${key}` : key),
  );
}

const placeholders = (text: string) => [...text.matchAll(/{{\s*([^}]+)\s*}}/g)]
  .map((match) => match[1]?.trim()).sort();

describe("mobile i18n resources", () => {
  it.each(["en", "zh-Hans", "zh-Hant"])("%s: overlays retain live upstream keys, interpolation and sentence meaning", (locale) => {
    const upstream = Object.fromEntries(readLocale(locale).map(([file, value]) => [file.replace(/\.json$/, ""), value]));
    const original = new Map(strings(upstream));
    const overlay = JSON.parse(fs.readFileSync(path.join(LOCALES_ROOT, "brand", `${locale}.json`), "utf8"));
    for (const [key, text] of strings(overlay)) {
      const source = original.get(key);
      expect(source, `stale override: ${locale}.${key}`).toBeTypeOf("string");
      expect(placeholders(text), `${locale}.${key}`).toEqual(placeholders(source!));
      expect(text, `review upstream copy change: ${locale}.${key}`).toBe(
        source!.replace(/\bMultica\b/g, "Labrastro").replace(/\bMika\b/g, "Mizuki"),
      );
    }
  });

  it("applies the fork overlay to every effective mobile locale", () => {
    for (const bundle of Object.values(resources)) {
      expect(JSON.stringify(bundle)).not.toMatch(/\b(?:Multica|Mika)\b/);
      expect(bundle.auth.login.title).toContain("Labrastro");
    }
  });

  describe.each(["zh-Hans", "zh-Hant"])("%s", (locale) => {
    it("has the same namespaces as English", () => {
      const enNamespaces = fs
        .readdirSync(path.join(LOCALES_ROOT, "en"))
        .filter((file) => file.endsWith(".json"))
        .sort();
      const zhNamespaces = fs
        .readdirSync(path.join(LOCALES_ROOT, locale))
        .filter((file) => file.endsWith(".json"))
        .sort();
      expect(zhNamespaces).toEqual(enNamespaces);
    });

    it("keeps keys aligned with English", () => {
      const enResources = readLocale("en");
      const zhResources = new Map(readLocale(locale));

      for (const [namespace, value] of enResources) {
        const zhValue = zhResources.get(namespace);
        expect(zhValue, `missing ${locale} namespace: ${namespace}`).toBeDefined();
        const enKeys = flatten(value);
        const zhKeys = flatten(zhValue);
        // i18next plural rules use `_one` / `_other`; Chinese has only `_other`.
        const missing = [...enKeys].filter((key) => {
          if (zhKeys.has(key)) return false;
          if (key.endsWith("_one") && zhKeys.has(`${key.slice(0, -4)}_other`)) {
            return false;
          }
          return true;
        });
        expect(
          missing,
          `missing ${locale} keys in ${namespace}`,
        ).toEqual([]);
        expect(
          [...zhKeys].filter((key) => !enKeys.has(key)),
          `extra ${locale} keys in ${namespace}`,
        ).toEqual([]);
      }
    });
  });
});
