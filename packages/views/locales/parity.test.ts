import { readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { RESOURCES } from "./index";

// Schema-level guard: every key in the EN bundle must have a counterpart
// in every non-EN bundle and vice-versa. Catches retrofit drift where a
// new EN key lands without a translation, which would silently fall back
// to the English string in production.
//
// i18next plural rule: EN uses `_one` + `_other`; zh only uses `_other`
// because Chinese has no grammatical number. ES uses both forms.
// Normalize all plural forms to `_count` before comparing so a
// { key_one, key_other } pair in EN matches a single { key_other } in zh.

// Derive the canonical namespace list from disk so the test fails if a JSON
// file ships without a matching RESOURCES entry. Without this guard the test
// would still pass when both EN and a non-EN locale skip a namespace (e.g.
// issues + agents both unregistered), since the iteration happens over
// RESOURCES.en itself — that's a tautology, not parity.
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
const nonEnLocales = Object.keys(RESOURCES).filter((k) => k !== "en");

describe("locale bundle parity", () => {
  for (const locale of nonEnLocales) {
    const loc = RESOURCES[locale as keyof typeof RESOURCES];

    it(`declares the same namespaces in EN and ${locale}`, () => {
      expect(Object.keys(en).sort()).toEqual(Object.keys(loc).sort());
    });

    it(`registers every JSON file in RESOURCES (${locale})`, () => {
      expect(Object.keys(loc).sort()).toEqual(jsonNamespacesIn(locale));
    });
  }

  it("registers every JSON file in RESOURCES (EN)", () => {
    expect(Object.keys(en).sort()).toEqual(jsonNamespacesIn("en"));
  });

  for (const locale of nonEnLocales) {
    const loc = RESOURCES[locale as keyof typeof RESOURCES];

    for (const ns of Object.keys(en)) {
      it(`${ns}: ${locale} covers every EN key`, () => {
        const enKeys = keySet(en[ns] ?? {});
        const locKeys = keySet(loc[ns] ?? {});
        const missing = [...enKeys].filter((k) => !locKeys.has(k));
        expect(missing).toEqual([]);
      });

      it(`${ns}: EN covers every ${locale} key`, () => {
        const enKeys = keySet(en[ns] ?? {});
        const locKeys = keySet(loc[ns] ?? {});
        const extra = [...locKeys].filter((k) => !enKeys.has(k));
        expect(extra).toEqual([]);
      });
    }
  }
});

