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
