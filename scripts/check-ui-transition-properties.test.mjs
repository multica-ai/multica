import assert from "node:assert/strict";
import test from "node:test";
import { resolve } from "node:path";

import {
  classGroupViolations,
  transitionViolations,
} from "./check-ui-transition-properties.mjs";

const probe = resolve("packages/views/zz-transition-probe.tsx");
const tokens = (...texts) =>
  classGroupViolations(texts.map((text) => ({ text }))).map((finding) => finding.token);
const findings = (source) =>
  transitionViolations(source, probe).map(({ line, token }) => `${line}:${token}`);

test("rejects transform lists next to v4 translate, scale and rotate", () => {
  assert.deepEqual(tokens("transition-[transform,width] translate-x-2"), ["translate-x-2"]);
  assert.deepEqual(
    tokens("transition-[opacity,transform] active:not-aria-[haspopup]:translate-y-px"),
    ["active:not-aria-[haspopup]:translate-y-px"],
  );
  assert.deepEqual(
    tokens("transition-[transform] ltr:-translate-x-1/2 hover:scale-[1.02] data-open:rotate-90"),
    ["ltr:-translate-x-1/2", "hover:scale-[1.02]", "data-open:rotate-90"],
  );
  assert.deepEqual(tokens("transition-[transform,translate] scale-95"), ["scale-95"]);
});

test("rejects rotate lists next to 3D rotates, which still compose transform", () => {
  assert.deepEqual(tokens("transition-[rotate] rotate-x-45"), ["rotate-x-45"]);
  assert.deepEqual(tokens("transition-[transform] rotate-x-45"), []);
});

test("accepts matching lists, built-in transitions and static positioning", () => {
  assert.deepEqual(tokens("transition-[translate,width] translate-x-2"), []);
  assert.deepEqual(tokens("transition-transform rotate-90"), []);
  assert.deepEqual(tokens("transition rotate-90 scale-95"), []);
  assert.deepEqual(tokens("transition-[color,background-color,scale] active:scale-[0.96]"), []);
  assert.deepEqual(tokens("transition-[width] group-data-[side=right]:rotate-180"), []);
  assert.deepEqual(tokens("transition-[opacity] left-1/2 -translate-x-1/2"), []);
});

test("reads underscores in arbitrary lists as spaces", () => {
  assert.deepEqual(tokens("transition-[transform,_translate] translate-x-2"), []);
  assert.deepEqual(tokens("transition-[opacity,_transform] translate-x-2"), ["translate-x-2"]);
});

test("groups the string arguments of one cn() or clsx() call", () => {
  assert.deepEqual(
    findings(`
      const chevron = cn(
        "transition-[transform,opacity] duration-150",
        collapsed
          ? "-rotate-90"
          : "opacity-0",
      );
      const tab = clsx(["transition-[transform,width]"], ready && \`translate-x-(--left) \${x}\`);
      const fine = cn("transition-[translate,opacity]", open ?? "translate-y-1");
    `),
    ["5:-rotate-90", "8:translate-x-(--left)"],
  );
});

test("does not follow classes passed through variables or other calls", () => {
  assert.deepEqual(
    findings(`
      const jsx = <div className="transition-[transform] translate-y-1" />;
      const template = \`transition-[transform] \${size} scale-95\`;
      const moved = "translate-x-0";
      const split = cn("transition-[transform]", moved, other("rotate-90"));
    `),
    ["2:translate-y-1", "3:scale-95"],
  );
});
