import assert from "node:assert/strict";
import { test } from "node:test";
import { Linter } from "eslint";
import tseslint from "typescript-eslint";
import { NO_FULL_PAGE_NAVIGATION } from "./full-page-navigation.js";

const linter = new Linter();

function flagged(code) {
  const messages = linter.verify(
    code,
    [
      {
        files: ["**/*.ts"],
        languageOptions: { parser: tseslint.parser },
        rules: { "no-restricted-syntax": ["error", NO_FULL_PAGE_NAVIGATION] },
      },
    ],
    "fixture.ts",
  );
  const fatal = messages.find((m) => m.fatal);
  if (fatal) throw new Error(`fixture failed to parse: ${fatal.message}`);
  return messages.length > 0;
}

const FLAGGED = [
  ["window.location.href assignment", 'window.location.href = "https://example.com";'],
  ["bare location.href assignment", "location.href = url;"],
  ["document.location.href assignment", "document.location.href = url;"],
  ["globalThis.location.href assignment", "globalThis.location.href = url;"],
  ["window.location assignment", "window.location = url;"],
  ["window.location.assign", "window.location.assign(url);"],
  ["window.location.replace", "window.location.replace(url);"],
  ["bare location.assign", "location.assign(url);"],
  ["self.location.replace", "self.location.replace(url);"],
];

const ALLOWED = [
  ["reading location.href", "const here = window.location.href;"],
  ["comparing location.href", 'if (location.href === "x") {}'],
  ["location.reload", "window.location.reload();"],
  ["window.open", 'window.open(url, "_blank", "noopener,noreferrer");'],
  ["href on another object", "link.href = url;"],
  ["assign on another object", "Object.assign(target, source);"],
  ["replace on a string", 'path.replace("/", "");'],
  ["location on a non-global", "props.location.href = url;"],
  ["router replace", "navigation.replace(path);"],
];

for (const [name, code] of FLAGGED) {
  test(`flags ${name}`, () => assert.equal(flagged(code), true));
}

for (const [name, code] of ALLOWED) {
  test(`allows ${name}`, () => assert.equal(flagged(code), false));
}

test("message points to openExternal", () => {
  assert.match(NO_FULL_PAGE_NAVIGATION.message, /openExternal/);
});
