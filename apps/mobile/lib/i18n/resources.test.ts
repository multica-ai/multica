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

describe("mobile i18n resources", () => {
  it("has an English resource for every supported namespace and locale", () => {
    const enNamespaces = fs
      .readdirSync(path.join(LOCALES_ROOT, "en"))
      .filter((file) => file.endsWith(".json"))
      .sort();
    const zhNamespaces = fs
      .readdirSync(path.join(LOCALES_ROOT, "zh-Hans"))
      .filter((file) => file.endsWith(".json"))
      .sort();
    expect(zhNamespaces).toEqual(enNamespaces);
  });

  it("keeps Simplified Chinese keys aligned with English", () => {
    const enResources = readLocale("en");
    const zhResources = new Map(readLocale("zh-Hans"));

    for (const [namespace, value] of enResources) {
      const zhValue = zhResources.get(namespace);
      expect(zhValue, `missing zh-Hans namespace: ${namespace}`).toBeDefined();
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
        `missing zh-Hans keys in ${namespace}`,
      ).toEqual([]);
      expect(
        [...zhKeys].filter((key) => !enKeys.has(key)),
        `extra zh-Hans keys in ${namespace}`,
      ).toEqual([]);
    }
  });
});
