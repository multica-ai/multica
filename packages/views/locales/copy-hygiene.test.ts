// @vitest-environment node
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
  return [...text.matchAll(/\{\{(\w+)\}\}/g)].map((m) => m[1]!).sort();
}

const PLURAL_SUFFIX_RE = /_(zero|one|two|few|many|other)$/;
const FORBIDDEN_INTERNAL_TERMS = /\b(Composio|Bohan|PersonalAgent)\b/;
const RAW_ZH_STATUS_LEAKS = [
  { ns: "issues", key: "backlog_hint.title" },
  { ns: "issues", key: "backlog_hint.description" },
  { ns: "issues", key: "backlog_hint.row_backlog_label" },
  { ns: "issues", key: "backlog_hint.row_todo_label" },
  { ns: "issues", key: "wakeups.create.other_blocked" },
  { ns: "issues", key: "wakeups.cond.other_blocked" },
  { ns: "issues", key: "wakeups.wait.other_blocked" },
  { ns: "issues", key: "workflows.editor.steps_hint" },
  { ns: "modals", key: "run_confirm.create_parked" },
  { ns: "settings", key: "notifications.groups.status_changes.description" },
] as const;

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
    for (const ns of enNamespaces) {
      const enLeaves = collectLeaves((RESOURCES.en as Record<string, unknown>)[ns]).leaves;
      const enBaseToPlaceholders = new Map<string, string[]>();
      for (const { key, value } of enLeaves) {
        const base = key.replace(PLURAL_SUFFIX_RE, "");
        if (!enBaseToPlaceholders.has(base)) {
          enBaseToPlaceholders.set(base, placeholdersOf(value));
        }
      }

      for (const locale of SUPPORTED_LOCALES) {
        if (locale === "en") continue;
        const locLeaves = collectLeaves((RESOURCES[locale] as Record<string, unknown>)[ns]).leaves;
        const locBases = new Set(locLeaves.map(({ key }) => key.replace(PLURAL_SUFFIX_RE, "")));

        const missing = [...enBaseToPlaceholders.keys()].filter((k) => !locBases.has(k));
        expect(missing, `${locale}/${ns} missing keys`).toEqual([]);

        const extra = [...locBases].filter((k) => !enBaseToPlaceholders.has(k));
        expect(extra, `${locale}/${ns} extra keys`).toEqual([]);
      }
    }
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

  it("enforces English style rules (US organization, Sign out, Unicode ellipsis)", () => {
    const violations: string[] = [];
    for (const ns of enNamespaces) {
      for (const { key, value } of collectLeaves((RESOURCES.en as Record<string, unknown>)[ns]).leaves) {
        if (/\borganisation\b/i.test(value)) {
          violations.push(`UK spelling in en/${ns}:${key}`);
        }
        if (value === "Log out") {
          violations.push(`Legacy "Log out" in en/${ns}:${key}`);
        }
        if (!value.includes("`") && value.includes("...")) {
          violations.push(`ASCII ellipsis in en/${ns}:${key}`);
        }
      }
    }
    expect(violations).toEqual([]);
  });

  it("enforces zh-Hans hygiene (no raw status enums, no ASCII ellipsis, no straight quotes in prose)", () => {
    const violations: string[] = [];
    const zh = RESOURCES["zh-Hans"] as Record<string, unknown>;
    const rawEnumPattern = /\b(backlog|todo|in_progress|in_review|blocked)\b/i;

    for (const { ns, key } of RAW_ZH_STATUS_LEAKS) {
      const leaf = collectLeaves(zh[ns]).leaves.find((l) => l.key === key);
      expect(leaf, `missing zh-Hans/${ns}:${key}`).toBeDefined();
      if (leaf && rawEnumPattern.test(leaf.value)) {
        violations.push(`Raw enum in zh-Hans/${ns}:${key}: ${leaf.value}`);
      }
    }

    for (const ns of enNamespaces) {
      for (const { key, value } of collectLeaves(zh[ns]).leaves) {
        if (!value.includes("`") && (value.includes("...") || value.includes("……"))) {
          violations.push(`Non-standard ellipsis in zh-Hans/${ns}:${key}`);
        }
        if (!value.includes("`") && !value.replaceAll("{{", "").replaceAll("}}", "").includes("{") && value.includes('"')) {
          violations.push(`Straight ASCII quote in zh-Hans/${ns}:${key}: ${value}`);
        }
      }
    }
    expect(violations).toEqual([]);
  });
});
