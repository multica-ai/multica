// @vitest-environment node
import { describe, expect, it } from "vitest";
import en from "../locales/en/agents.json";
import zhHans from "../locales/zh-Hans/agents.json";
import ja from "../locales/ja/agents.json";
import ko from "../locales/ko/agents.json";

const LOCALES = { en, "zh-Hans": zhHans, ja, ko } as const;

/**
 * Verify new access-scope / bulk-access keys are present in every locale
 * with the same key set. This prevents silent regressions where one locale
 * gets a key added while the others lag (the i18next parity bug the
 * learnings researcher flagged).
 */
describe("access-scope i18n parity across all 4 locales", () => {
  const accessScopeKeys = [
    "access.scope_labels.workspace",
    "access.scope_labels.specific_people",
    "access.scope_labels.owner_only",
  ];

  const bulkKeys = [
    "row_actions.set_access",
    "row_actions.set_access_dialog_title",
    "row_actions.set_access_applies_to",
    "row_actions.set_access_skipped",
    "row_actions.set_access_dialog_confirm",
    "row_actions.set_access_bulk_partial",
  ];

  const toolbarKeys = ["toolbar.section_access"];

  const ALL_NEW_KEYS = [...accessScopeKeys, ...bulkKeys, ...toolbarKeys];

  it("all new keys are present in all 4 locales", () => {
    for (const [name, loc] of Object.entries(LOCALES)) {
      for (const key of ALL_NEW_KEYS) {
        const parts = key.split(".");
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        let node: any = loc as any;
        for (const p of parts) {
          node = node?.[p];
        }
        expect(node, `${name}: ${key} missing`).toBeDefined();
        expect(typeof node, `${name}: ${key} not a string`).toBe("string");
        expect(String(node).length > 0, `${name}: ${key} is empty`).toBe(true);
      }
    }
  });

  it("interpolation tokens use double-brace {{count}} everywhere", () => {
    for (const [name, loc] of Object.entries(LOCALES)) {
      for (const key of ["row_actions.set_access_applies_to", "row_actions.set_access_skipped", "row_actions.set_access_bulk_partial"]) {
        const parts = key.split(".");
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        let node: any = loc as any;
        for (const p of parts) {
          node = node?.[p];
        }
        if (typeof node === "string" && /\{count\}/.test(node) && !/\{\{count\}\}/.test(node)) {
          throw new Error(`${name}: ${key} uses {count} instead of {{count}}`);
        }
      }
    }
  });
});

/**
 * The transcript's multi-file patch summary is handed the number of files
 * *beyond* the named one. A translation that phrases this as a total silently
 * under-reports by one — "a.go 等 2 个文件" reads as two files including a.go
 * when three changed. English hides the distinction ("+2 more"), so it has to
 * be pinned per locale.
 */
describe("transcript patch summary i18n", () => {
  const KEY = "transcript.patch_summary_more";

  const read = (loc: object): unknown =>
    KEY.split(".").reduce<unknown>(
      (node, part) =>
        node !== null && typeof node === "object"
          ? (node as Record<string, unknown>)[part]
          : undefined,
      loc,
    );

  const phraseOf = (loc: object): string => {
    const node = read(loc);
    return typeof node === "string" ? node : "";
  };

  it("is present in all 4 locales", () => {
    for (const [name, loc] of Object.entries(LOCALES)) {
      expect(typeof read(loc), `${name}: ${KEY} missing`).toBe("string");
      expect(phraseOf(loc).length > 0, `${name}: ${KEY} is empty`).toBe(true);
    }
  });

  it("interpolates {{path}} and {{extra}} in every locale", () => {
    for (const [name, loc] of Object.entries(LOCALES)) {
      const phrase = phraseOf(loc);
      expect(phrase.includes("{{path}}"), `${name}: ${KEY} lost {{path}}`).toBe(true);
      expect(phrase.includes("{{extra}}"), `${name}: ${KEY} lost {{extra}}`).toBe(true);
    }
  });

  // `count` is i18next's plural selector — this namespace already uses it that
  // way for events_one/events_other. Reusing it here for a plain number ties
  // the string to plural resolution it does not want.
  it("does not use the reserved {{count}} token", () => {
    for (const [name, loc] of Object.entries(LOCALES)) {
      expect(phraseOf(loc).includes("{{count}}"), `${name}: ${KEY} must use {{extra}}`).toBe(false);
    }
  });
});
