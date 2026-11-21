// @vitest-environment node
import fs from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { SUPPORTED_LOCALES } from "@multica/core/i18n";
import { RESOURCES } from "./index";

type LeafEntry = { key: string; value: string };

function collectLeaves(
  obj: unknown,
  prefix = "",
  leaves: LeafEntry[] = [],
  emptyObjects: string[] = [],
): { leaves: LeafEntry[]; emptyObjects: string[] } {
  if (typeof obj === "string") {
    leaves.push({ key: prefix, value: obj });
    return { leaves, emptyObjects };
  }
  if (obj && typeof obj === "object" && !Array.isArray(obj)) {
    const entries = Object.entries(obj as Record<string, unknown>);
    if (entries.length === 0 && prefix) {
      emptyObjects.push(prefix);
    }
    for (const [k, v] of entries) {
      collectLeaves(v, prefix ? `${prefix}.${k}` : k, leaves, emptyObjects);
    }
  }
  return { leaves, emptyObjects };
}

function placeholdersOf(text: string): string[] {
  return [...new Set([...text.matchAll(/\{\{\s*(\w+)\s*\}\}/g)].map((m) => m[1]!))].sort();
}

const PLURAL_SUFFIX_RE = /_(zero|one|two|few|many|other)$/;
const FORBIDDEN_INTERNAL_TERMS = /\b(Composio|Bohan|PersonalAgent)\b|Богдан/i;
const RAW_SNAKE_STATUS_RE = /\b(in_progress|in_review)\b/;
const RAW_CJK_RU_STATUS_RE = /\b(backlog|todo|in_progress|in_review|blocked|cancelled|unstarted)\b/;
const CJK_AND_RU_LOCALES = new Set<string>(["zh-Hans", "zh-Hant", "ja", "ko", "ru"]);

// Known pre-existing plural-variant nuances where a locale's `_one` idiom
// legitimately omits or retains `{{count}}`/`{{updated}}` differently from EN
// (e.g. Russian CLDR `_one` matches 1, 21, 31... and requires `{{count}}`).
const ALLOWED_PLACEHOLDER_DIFFS = new Set<string>([
  "fr/issues:batch.update_partial_one",
  "fr/issues:deliverables.view_all_one",
  "fr/issues:workflows.mapping.unchanged_one",
  "es/issues:batch.update_partial_one",
  "ru/issues:batch.update_partial_one",
  "ru/issues:batch.delete_dialog_desc_one",
]);

describe("locale copy hygiene and parity", () => {
  const enNamespaces = Object.keys(RESOURCES.en).sort();

  it("ships identical namespace sets and zero dead empty objects across all 8 locales", () => {
    for (const locale of SUPPORTED_LOCALES) {
      const bundle = RESOURCES[locale] as Record<string, unknown>;
      expect(Object.keys(bundle).sort(), `${locale} namespaces`).toEqual(enNamespaces);
      for (const ns of enNamespaces) {
        const { emptyObjects } = collectLeaves(bundle[ns]);
        expect(emptyObjects, `${locale}/${ns} empty objects`).toEqual([]);
      }
    }
  });

  it("keeps base keys and interpolation placeholders aligned across all 8 locales", () => {
    const placeholderViolations: string[] = [];

    for (const ns of enNamespaces) {
      const enLeaves = collectLeaves((RESOURCES.en as Record<string, unknown>)[ns]).leaves;
      const enExact = new Map<string, string[]>(
        enLeaves.map(({ key, value }) => [key, placeholdersOf(value)]),
      );
      const enBases = new Set(enLeaves.map(({ key }) => key.replace(PLURAL_SUFFIX_RE, "")));

      for (const locale of SUPPORTED_LOCALES) {
        if (locale === "en") continue;
        const locLeaves = collectLeaves((RESOURCES[locale] as Record<string, unknown>)[ns]).leaves;
        const locBases = new Set(locLeaves.map(({ key }) => key.replace(PLURAL_SUFFIX_RE, "")));

        const missing = [...enBases].filter((k) => !locBases.has(k));
        expect(missing, `${locale}/${ns} missing keys`).toEqual([]);

        const extra = [...locBases].filter((k) => !enBases.has(k));
        expect(extra, `${locale}/${ns} extra keys`).toEqual([]);

        for (const { key, value } of locLeaves) {
          if (ALLOWED_PLACEHOLDER_DIFFS.has(`${locale}/${ns}:${key}`)) continue;

          // Match exact key first; for Russian `_few`/`_many` or CJK `_other`,
          // fall back to English `_other` so plural placeholders align cleanly.
          const base = key.replace(PLURAL_SUFFIX_RE, "");
          const expected =
            enExact.get(key) ??
            enExact.get(`${base}_other`) ??
            enExact.get(base);
          if (!expected) continue;

          const actual = placeholdersOf(value);
          // Russian CLDR `_one` is used for 1, 21, 31... so `{{count}}` may appear
          // alongside English `_one`'s placeholders (e.g. `{{name}}` + `{{count}}`).
          if (
            locale === "ru" &&
            key.endsWith("_one") &&
            expected.every((p) => actual.includes(p)) &&
            actual.every((p) => p === "count" || expected.includes(p))
          ) {
            continue;
          }

          if (JSON.stringify(actual) !== JSON.stringify(expected)) {
            placeholderViolations.push(
              `${locale}/${ns}:${key} expected [${expected.join(", ")}] got [${actual.join(", ")}]`,
            );
          }
        }
      }
    }

    expect(placeholderViolations).toEqual([]);
  });

  it("never leaks internal vendor, codename, or teammate names in any locale", () => {
    const violations: string[] = [];
    for (const locale of SUPPORTED_LOCALES) {
      const bundle = RESOURCES[locale] as Record<string, unknown>;
      for (const ns of enNamespaces) {
        for (const { key, value } of collectLeaves(bundle[ns]).leaves) {
          if (FORBIDDEN_INTERNAL_TERMS.test(value)) {
            violations.push(`${locale}/${ns}:${key} = ${value}`);
          }
        }
      }
    }
    expect(violations).toEqual([]);
  });

  it("never leaks raw English status enums in non-English locales", () => {
    const violations: string[] = [];
    for (const locale of SUPPORTED_LOCALES) {
      if (locale === "en") continue;
      const bundle = RESOURCES[locale] as Record<string, unknown>;
      const pattern = CJK_AND_RU_LOCALES.has(locale)
        ? RAW_CJK_RU_STATUS_RE
        : RAW_SNAKE_STATUS_RE;

      for (const ns of enNamespaces) {
        for (const { key, value } of collectLeaves(bundle[ns]).leaves) {
          // Ignore literal CLI commands or backtick code snippets.
          const prose = value.replace(/`[^`]*`/g, "");
          if (pattern.test(prose)) {
            violations.push(`${locale}/${ns}:${key} = ${value}`);
          }
        }
      }
    }
    expect(violations).toEqual([]);
  });

  it("enforces English style rules per conventions.mdx (US organization, Sign in/out, ASCII ellipsis)", () => {
    const violations: string[] = [];
    for (const ns of enNamespaces) {
      for (const { key, value } of collectLeaves((RESOURCES.en as Record<string, unknown>)[ns]).leaves) {
        if (/\borganisation\b/i.test(value)) {
          violations.push(`UK spelling in en/${ns}:${key}`);
        }
        if (/\blog (in|out)\b/i.test(value)) {
          violations.push(`Legacy "Log in/out" in en/${ns}:${key}: ${value}`);
        }
        if (value.includes("…")) {
          violations.push(`Unicode ellipsis in en/${ns}:${key}: ${value}`);
        }
      }
    }
    expect(violations).toEqual([]);
  });

  it("enforces zh-Hans conventions per conventions.zh.mdx (ASCII ellipsis, straight quotes, lowercase skill)", () => {
    const violations: string[] = [];
    const zh = RESOURCES["zh-Hans"] as Record<string, unknown>;

    for (const ns of enNamespaces) {
      for (const { key, value } of collectLeaves(zh[ns]).leaves) {
        if (value.includes("…") || value.includes("……")) {
          violations.push(`Non-standard ellipsis in zh-Hans/${ns}:${key}: ${value}`);
        }
        if (/[「」“”]/.test(value)) {
          violations.push(`Non-straight quote in zh-Hans/${ns}:${key}: ${value}`);
        }
        if (value.includes("技能")) {
          violations.push(`Translated "技能" instead of skill/Skills in zh-Hans/${ns}:${key}: ${value}`);
        }
      }
    }
    expect(violations).toEqual([]);
  });

  it("keeps packages/ee/project-folders locales aligned and hygienic", () => {
    const eeDir = path.resolve(__dirname, "../../ee/project-folders/locales");
    const enJson = JSON.parse(fs.readFileSync(path.join(eeDir, "en.json"), "utf8"));
    const enLeaves = collectLeaves(enJson).leaves;
    const enMap = new Map(enLeaves.map(({ key, value }) => [key, value]));
    const violations: string[] = [];

    for (const locale of SUPPORTED_LOCALES) {
      const locPath = path.join(eeDir, `${locale}.json`);
      expect(fs.existsSync(locPath), `missing ee locale ${locale}.json`).toBe(true);
      const parsed = JSON.parse(fs.readFileSync(locPath, "utf8"));
      const { leaves, emptyObjects } = collectLeaves(parsed);
      expect(emptyObjects, `ee/${locale} empty objects`).toEqual([]);

      const locKeys = new Set(leaves.map((l) => l.key.replace(PLURAL_SUFFIX_RE, "")));
      const enBases = new Set(enLeaves.map((l) => l.key.replace(PLURAL_SUFFIX_RE, "")));
      expect([...enBases].filter((k) => !locKeys.has(k)), `ee/${locale} missing keys`).toEqual([]);
      expect([...locKeys].filter((k) => !enBases.has(k)), `ee/${locale} extra keys`).toEqual([]);

      for (const { key, value } of leaves) {
        const base = key.replace(PLURAL_SUFFIX_RE, "");
        const enVal = enMap.get(key) ?? enMap.get(`${base}_other`) ?? enMap.get(base);
        if (enVal) {
          const expected = placeholdersOf(enVal);
          const actual = placeholdersOf(value);
          if (JSON.stringify(actual) !== JSON.stringify(expected)) {
            violations.push(`ee/${locale}:${key} placeholder mismatch`);
          }
        }
        if (locale === "zh-Hans" && (/[「」“”…]/.test(value) || value.includes("……"))) {
          violations.push(`ee/zh-Hans:${key} violates punctuation conventions: ${value}`);
        }
      }
    }

    expect(violations).toEqual([]);
  });
});
