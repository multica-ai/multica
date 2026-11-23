import assert from "node:assert/strict";
import test from "node:test";
import { resolve } from "node:path";

import {
  classStringViolations,
  transitionViolations,
} from "./check-ui-transition-properties.mjs";

const probe = resolve("packages/views/zz-transition-probe.tsx");
const tokens = (text) => classStringViolations(text).map((finding) => finding.token);

test("rejects transition lists that name transform for v4 translate, scale and rotate", () => {
  assert.deepEqual(tokens("transition-[transform,width] translate-x-2"), ["translate-x-2"]);
  assert.deepEqual(
    tokens("transition-[opacity,transform] active:not-aria-[haspopup]:translate-y-px"),
    ["active:not-aria-[haspopup]:translate-y-px"],
  );
  assert.deepEqual(
    tokens("transition-[transform] ltr:-translate-x-1/2 hover:scale-[1.02] data-open:rotate-90"),
    ["ltr:-translate-x-1/2", "hover:scale-[1.02]", "data-open:rotate-90"],
  );
  assert.deepEqual(
    classStringViolations("transition-[opacity] scale-95").map((finding) => finding.property),
    ["scale"],
  );
});

test("requires transform for 3D rotate utilities", () => {
  assert.deepEqual(tokens("transition-[rotate] rotate-x-45"), ["rotate-x-45"]);
  assert.deepEqual(tokens("transition-[transform] rotate-x-45"), []);
});

test("accepts lists that name the property, and the built-in transition utilities", () => {
  assert.deepEqual(tokens("transition-[translate,width] translate-x-2"), []);
  assert.deepEqual(tokens("transition-transform rotate-90"), []);
  assert.deepEqual(tokens("transition rotate-90 scale-95"), []);
  assert.deepEqual(tokens("transition-[color,background-color,scale] active:scale-[0.96]"), []);
  assert.deepEqual(tokens("transition-[all] translate-x-2"), []);
  assert.deepEqual(tokens("transition-[var(--props)] translate-x-2"), []);
  assert.deepEqual(tokens("transition-[width] w-4"), []);
});

test("matches within one string literal, joining a template's static text", () => {
  const findings = transitionViolations(
    `
      const jsx = <div className="transition-[transform] translate-y-1" />;
      const template = \`transition-[transform] \${size} scale-95\`;
      const split = cn("transition-[transform]", open && "translate-x-0");
    `,
    probe,
  );
  assert.deepEqual(
    findings.map(({ line, token }) => `${line}:${token}`),
    ["2:translate-y-1", "3:scale-95"],
  );
});
