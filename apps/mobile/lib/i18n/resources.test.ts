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

function flattenEntries(
  value: unknown,
  prefix = "",
): Map<string, string> {
  const entries = new Map<string, string>();
  if (typeof value === "string") {
    entries.set(prefix, value);
    return entries;
  }
  if (typeof value !== "object" || value === null) {
    return entries;
  }
  for (const [key, child] of Object.entries(value)) {
    const nextKey = prefix ? `${prefix}.${key}` : key;
    for (const [k, v] of flattenEntries(child, nextKey)) entries.set(k, v);
  }
  return entries;
}

function placeholdersOf(text: string): string[] {
  return [...new Set([...text.matchAll(/\{\{\s*(\w+)\s*\}\}/g)].map((m) => m[1]!))].sort();
}

describe("mobile i18n resources", () => {
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

    it("keeps keys and interpolation placeholders aligned with English", () => {
      const enResources = readLocale("en");
      const zhResources = new Map(readLocale(locale));

      for (const [namespace, value] of enResources) {
        const zhValue = zhResources.get(namespace);
        expect(zhValue, `missing ${locale} namespace: ${namespace}`).toBeDefined();
        const enEntries = flattenEntries(value);
        const zhEntries = flattenEntries(zhValue);
        const enKeys = new Set(enEntries.keys());
        const zhKeys = new Set(zhEntries.keys());
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

        const placeholderMismatches: string[] = [];
        for (const [key, zhText] of zhEntries) {
          const enText = enEntries.get(key);
          if (enText === undefined) continue;
          const enPh = placeholdersOf(enText);
          const zhPh = placeholdersOf(zhText);
          if (JSON.stringify(zhPh) !== JSON.stringify(enPh)) {
            placeholderMismatches.push(
              `${locale}/${namespace}:${key} expected [${enPh.join(", ")}] got [${zhPh.join(", ")}]`,
            );
          }
        }
        expect(
          placeholderMismatches,
          `placeholder mismatches in ${locale}/${namespace}`,
        ).toEqual([]);
      }
    });

    it("contains no stray top-level dotted keys or raw English status enums", () => {
      const RAW_STATUS_ENUMS = new Set([
        "backlog",
        "todo",
        "in_progress",
        "in_review",
        "done",
        "blocked",
        "cancelled",
        "unstarted",
        "started",
        "closed",
      ]);
      for (const [namespace, value] of readLocale(locale)) {
        const record = value as Record<string, unknown>;
        const dotted = Object.keys(record).filter((k) => k.includes("."));
        expect(dotted, `${locale}/${namespace} has flat dotted keys`).toEqual([]);
      }
      const issues = Object.fromEntries(readLocale(locale))["issues.json"] as {
        status: Record<string, string>;
        category: Record<string, string>;
      };
      for (const [k, v] of Object.entries(issues.status)) {
        expect(RAW_STATUS_ENUMS.has(v), `${locale}/issues.json status.${k} is raw enum "${v}"`).toBe(false);
      }
      for (const [k, v] of Object.entries(issues.category)) {
        expect(RAW_STATUS_ENUMS.has(v), `${locale}/issues.json category.${k} is raw enum "${v}"`).toBe(false);
      }
      const inbox = Object.fromEntries(readLocale(locale))["inbox.json"] as {
        detail: Record<string, string>;
      };
      for (const k of ["status_todo", "status_in_progress", "status_done", "status_blocked"]) {
        expect(
          RAW_STATUS_ENUMS.has(inbox.detail[k] ?? ""),
          `${locale}/inbox.json detail.${k} is raw enum "${inbox.detail[k]}"`,
        ).toBe(false);
      }
    });
  });

  it("en has no stray top-level dotted keys", () => {
    for (const [namespace, value] of readLocale("en")) {
      const dotted = Object.keys(value as Record<string, unknown>).filter((k) => k.includes("."));
      expect(dotted, `en/${namespace} has flat dotted keys`).toEqual([]);
    }
  });
});
