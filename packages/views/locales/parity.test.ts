import { readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { SUPPORTED_LOCALES, type SupportedLocale } from "@multica/core/i18n";
import { RESOURCES } from "./index";

// Schema-level guard: every key in the EN bundle must have a counterpart
// in every other locale and vice-versa. Catches retrofit drift where a
// new EN key lands without its translation, which would silently fall
// back to the English string in production.
//
// i18next plural rule: EN uses `_one` + `_other`; zh and he use only
// `_other` (Chinese has no grammatical number; Hebrew's plural agreement
// isn't expressed via i18next's `_one`/`_other` split for our keys).
// Normalize both forms to `_other` before comparing so `{ key_one, key_other }`
// in EN matches a single `{ key_other }` in the target locale.

// Derive the canonical namespace list from disk so the test fails if a JSON
// file ships without a matching RESOURCES entry. Without this guard the test
// would still pass when EN and a target locale both skip the same namespace
// — that's a tautology, not parity.
const LOCALES_DIR = dirname(fileURLToPath(import.meta.url));

function jsonNamespacesIn(locale: string): string[] {
  return readdirSync(resolve(LOCALES_DIR, locale))
    .filter((name) => name.endsWith(".json"))
    .map((name) => name.replace(/\.json$/, ""))
    .sort();
}

type Json = Record<string, unknown>;

function flattenKeys(obj: unknown, prefix = ""): string[] {
  if (obj === null || typeof obj !== "object") return [prefix];
  const entries = Object.entries(obj as Json);
  if (entries.length === 0) return [];
  return entries.flatMap(([k, v]) =>
    flattenKeys(v, prefix ? `${prefix}.${k}` : k),
  );
}

function normalizePlural(key: string): string {
  return key.replace(/_(one|other)$/, "_count");
}

function keySet(bundle: Record<string, unknown>): Set<string> {
  return new Set(flattenKeys(bundle).map(normalizePlural));
}

const en = RESOURCES.en;
const OTHER_LOCALES: SupportedLocale[] = SUPPORTED_LOCALES.filter(
  (l) => l !== "en",
);

describe("locale bundle parity", () => {
  it("registers every JSON file in RESOURCES (en)", () => {
    expect(Object.keys(en).sort()).toEqual(jsonNamespacesIn("en"));
  });

  for (const locale of OTHER_LOCALES) {
    describe(locale, () => {
      const bundle = RESOURCES[locale];

      it(`declares the same namespaces as en`, () => {
        expect(Object.keys(en).sort()).toEqual(Object.keys(bundle).sort());
      });

      it(`registers every JSON file in RESOURCES`, () => {
        expect(Object.keys(bundle).sort()).toEqual(jsonNamespacesIn(locale));
      });

      for (const ns of Object.keys(en)) {
        it(`${ns}: covers every en key`, () => {
          const enKeys = keySet(en[ns] ?? {});
          const targetKeys = keySet(bundle[ns] ?? {});
          const missing = [...enKeys].filter((k) => !targetKeys.has(k));
          expect(missing).toEqual([]);
        });

        it(`${ns}: en covers every ${locale} key`, () => {
          const enKeys = keySet(en[ns] ?? {});
          const targetKeys = keySet(bundle[ns] ?? {});
          const extra = [...targetKeys].filter((k) => !enKeys.has(k));
          expect(extra).toEqual([]);
        });
      }
    });
  }
});
