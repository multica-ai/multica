import { readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";
import { describe, expect, it } from "vitest";
// Relative import, not the "@/" tsconfig alias: vitest.config.ts has no
// path-alias resolution wired up (confirmed by running this suite — the
// alias throws ERR_MODULE_NOT_FOUND), and no other lib/**/*.test.ts file
// in this app uses "@/" either. Runtime code (lib/i18n/index.ts) still
// uses "@/locales" since Metro/tsc do resolve the alias for app code.
import { RESOURCES } from "../../locales";

// Schema-level guard: every key in the EN bundle must have a counterpart
// in the zh-Hans bundle and vice-versa. Catches retrofit drift where a
// new EN key lands without its translation, which would silently fall
// back to the English string in production. Mirrors
// packages/views/locales/parity.test.ts (web's equivalent guard).
const LOCALES_DIR = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "../../locales",
);

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

function keySet(bundle: Record<string, unknown>): Set<string> {
  return new Set(flattenKeys(bundle));
}

const en = RESOURCES.en;
const zhHans = RESOURCES["zh-Hans"];

describe("mobile locale bundle parity", () => {
  it("registers every JSON file in RESOURCES (en)", () => {
    expect(Object.keys(en).sort()).toEqual(jsonNamespacesIn("en"));
  });

  it("declares the same namespaces in en and zh-Hans", () => {
    expect(Object.keys(zhHans).sort()).toEqual(Object.keys(en).sort());
  });

  it("registers every JSON file in RESOURCES (zh-Hans)", () => {
    expect(Object.keys(zhHans).sort()).toEqual(jsonNamespacesIn("zh-Hans"));
  });

  for (const ns of Object.keys(en)) {
    it(`${ns}: zh-Hans covers every en key`, () => {
      const enKeys = keySet(en[ns as keyof typeof en] ?? {});
      const zhKeys = keySet(zhHans[ns as keyof typeof zhHans] ?? {});
      const missing = [...enKeys].filter((k) => !zhKeys.has(k));
      expect(missing).toEqual([]);
    });

    it(`${ns}: en covers every zh-Hans key`, () => {
      const enKeys = keySet(en[ns as keyof typeof en] ?? {});
      const zhKeys = keySet(zhHans[ns as keyof typeof zhHans] ?? {});
      const extra = [...zhKeys].filter((k) => !enKeys.has(k));
      expect(extra).toEqual([]);
    });
  }
});
