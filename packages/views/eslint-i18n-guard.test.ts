// @vitest-environment node
import { describe, expect, it } from "vitest";
import { Linter } from "eslint";
import tsParser from "@typescript-eslint/parser";
import {
  NO_UNTRANSLATED_ATTRIBUTES,
  NO_UNTRANSLATED_TOAST,
} from "./eslint-i18n-guard.mjs";

// These fixtures ARE the specification of the guard. The rule's first
// iteration matched only a bare `Literal` child, so every "indirect" case
// below reached production untranslated while `pnpm lint` stayed green — a
// hand-run probe of the direct cases had reported success. Asserting the
// indirect shapes here is what keeps that hole closed.

const linter = new Linter();

function lint(code: string): string[] {
  const messages = linter.verify(
    code,
    [
      {
        files: ["**/*.tsx"],
        languageOptions: {
          parser: tsParser,
          parserOptions: {
            ecmaVersion: "latest",
            sourceType: "module",
            ecmaFeatures: { jsx: true },
          },
        },
        rules: {
          "no-restricted-syntax": [
            "error",
            NO_UNTRANSLATED_ATTRIBUTES,
            NO_UNTRANSLATED_TOAST,
          ],
        },
      },
    ],
    "fixture.tsx",
  );
  // A fixture that stops parsing would silently "pass" every allow-case.
  const parseError = messages.find((m) => m.fatal);
  if (parseError) {
    throw new Error(`fixture failed to parse: ${parseError.message}`);
  }
  return messages.map((m) => m.message);
}

function flags(code: string): boolean {
  return lint(code).length > 0;
}

describe("no untranslated attribute copy", () => {
  it.each([
    ['direct literal', '<input placeholder="Search issues" />'],
    ['title', '<span title="Copy link" />'],
    ['aria-label', '<input aria-label="Select all sub-issues" />'],
    ['expression container', '<input title={"Expression copy"} />'],
    ['template literal', '<input aria-label={`Select ${id}`} />'],
    ['interpolated template', '<input aria-label={`${label} key ${i}`} />'],
  ])("flags %s", (_name, code) => {
    expect(flags(code)).toBe(true);
  });

  it.each([
    ['translated call', '<input placeholder={t(($) => $.search)} />'],
    ['URL literal', '<a title="https://example.com" />'],
    ['URL template', '<a title={`https://example.com/${id}`} />'],
    // Pure interpolation of already-translated values: no static English.
    ['no static letters', '<span aria-label={`${a}: ${b}`} />'],
    // Only the three copy-bearing attributes are guarded.
    ['unguarded attribute', '<div className="flex items-center" />'],
    ['numeric literal', '<input placeholder="0" />'],
  ])("allows %s", (_name, code) => {
    expect(flags(code)).toBe(false);
  });
});

describe("no untranslated toast copy", () => {
  it.each([
    ['bare toast', 'toast("Saved");'],
    ['toast.error', 'toast.error("Failed to save");'],
    ['toast.success', 'toast.success("Workspace created");'],
    ['template literal', 'toast.error(`Failed to save`);'],
    // The shape that shipped untranslated in source-backfill-modal.tsx.
    [
      'ternary fallback',
      'toast.error(err instanceof Error ? err.message : "Failed to save");',
    ],
    ['logical fallback', 'toast.error(msg || "Something went wrong");'],
    ['nullish fallback', 'toast.error(msg ?? "Something went wrong");'],
    ['guarded literal', 'toast.error(shouldWarn && "Check your input");'],
  ])("flags %s", (_name, code) => {
    expect(flags(code)).toBe(true);
  });

  it.each([
    ['translated call', 'toast.error(t(($) => $.save_failed));'],
    ['translated ternary', 'toast.error(a ? t(($) => $.x) : t(($) => $.y));'],
    // A comparison in the ternary test is not copy.
    ['comparison test', 'toast.error(code === "E_LIMIT" ? t(x) : t(y));'],
    ['non-toast callee', 'logger.error("Failed to save");'],
  ])("allows %s", (_name, code) => {
    expect(flags(code)).toBe(false);
  });
});

it("names the offending door in the message", () => {
  expect(lint('<input aria-label="Copy" />')[0]).toContain("useT()");
  expect(lint('toast.error("Copy");')[0]).toContain("useT()");
});
