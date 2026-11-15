import assert from "node:assert/strict";
import { test } from "node:test";
import { Linter } from "eslint";
import tseslint from "typescript-eslint";
import { NO_ROUND_CONTROLS } from "./control-shape.js";

const linter = new Linter();

function flagged(code) {
  const messages = linter.verify(
    code,
    [
      {
        files: ["**/*.tsx"],
        languageOptions: {
          parser: tseslint.parser,
          parserOptions: { ecmaFeatures: { jsx: true } },
        },
        rules: { "no-restricted-syntax": ["error", NO_ROUND_CONTROLS] },
      },
    ],
    "fixture.tsx",
  );
  const fatal = messages.find((m) => m.fatal);
  if (fatal) throw new Error(`fixture failed to parse: ${fatal.message}`);
  return messages.length > 0;
}

const FLAGGED = [
  ["native button", '<button className="rounded-full border px-2" />'],
  ["Button", '<Button size="sm" className="rounded-full" />'],
  ["IconButton", '<IconButton className="text-muted-foreground rounded-full" />'],
  ["link", '<a className="rounded-full px-3" />'],
  ["cn() argument", '<button className={cn("inline-flex rounded-full", x && "ring-2")} />'],
  ["conditional branch", '<Button className={open ? "rounded-full" : "rounded-md"} />'],
  ["template literal", "<button className={`rounded-full ${tone}`} />"],
];

const ALLOWED = [
  ["rectangle", '<Button className="rounded-md" />'],
  ["variant-prefixed", '<button className="after:rounded-full" />'],
  ["badge inside a button", '<button className="rounded-md"><span className="rounded-full" /></button>'],
  ["non-control element", '<span className="rounded-full bg-muted px-2" />'],
  ["look-alike class", '<button className="rounded-full-ish" />'],
];

for (const [name, code] of FLAGGED) {
  test(`flags rounded-full on a ${name}`, () => assert.equal(flagged(code), true));
}

for (const [name, code] of ALLOWED) {
  test(`allows ${name}`, () => assert.equal(flagged(code), false));
}
