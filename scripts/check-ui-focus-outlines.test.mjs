import assert from "node:assert/strict";
import test from "node:test";
import { resolve } from "node:path";

import {
  classGroupViolations,
  focusOutlineViolations,
} from "./check-ui-focus-outlines.mjs";

const probe = resolve("packages/views/zz-outline-probe.tsx");
const tokens = (...texts) =>
  classGroupViolations(texts.map((text) => ({ text }))).map((finding) => finding.token);
const fixes = (...texts) =>
  classGroupViolations(texts.map((text) => ({ text }))).map((finding) => finding.fix);
const findings = (source) =>
  focusOutlineViolations(source, probe).map(({ line, token }) => `${line}:${token}`);

test("rejects widths that outline-none or outline-hidden leave without a style", () => {
  assert.deepEqual(
    tokens("outline-none focus-visible:outline-2 focus-visible:outline-ring"),
    ["focus-visible:outline-2"],
  );
  assert.deepEqual(tokens("outline-hidden focus:outline-1"), ["focus:outline-1"]);
  assert.deepEqual(tokens("focus-visible:outline-none focus-visible:outline-2"), ["focus-visible:outline-2"]);
  assert.deepEqual(
    tokens("outline-none group-focus-visible:outline focus-within:outline-[3px] focus-visible:outline-(length:--ring)"),
    ["group-focus-visible:outline", "focus-within:outline-[3px]", "focus-visible:outline-(length:--ring)"],
  );
  assert.deepEqual(tokens("outline-none! focus-visible:outline-2!"), ["focus-visible:outline-2!"]);
});

test("the fix names the width's own variants", () => {
  assert.deepEqual(fixes("outline-none focus-visible:outline-2"), ["focus-visible:outline-solid"]);
  assert.deepEqual(fixes("outline-none data-open:focus:outline-2"), ["data-open:focus:outline-solid"]);
});

test("accepts a style under the same or fewer variants", () => {
  assert.deepEqual(tokens("outline-none focus-visible:outline-2 focus-visible:outline-solid"), []);
  assert.deepEqual(tokens("outline-hidden focus-visible:outline-2 focus-visible:outline-dashed"), []);
  assert.deepEqual(tokens("outline-none focus-visible:outline-dotted data-open:focus-visible:outline-2"), []);
  assert.deepEqual(tokens("outline-none focus-visible:outline-2 focus:outline-double"), ["focus-visible:outline-2"]);
  assert.deepEqual(tokens("outline-none focus-visible:outline-2 hover:outline-solid"), ["focus-visible:outline-2"]);
});

test("ignores rings on pseudo-elements and children, which the reset never reaches", () => {
  assert.deepEqual(
    tokens("outline-none after:absolute after:inset-0 focus-visible:after:outline-2 focus-visible:after:outline-ring"),
    [],
  );
  assert.deepEqual(tokens("outline-none focus-visible:before:outline-1 *:focus:outline-2 [&_svg]:focus:outline-2"), []);
  assert.deepEqual(tokens("after:outline-none focus-visible:after:outline-2"), ["focus-visible:after:outline-2"]);
  assert.deepEqual(tokens("[&_.recharts-layer]:outline-hidden focus-visible:outline-2"), []);
});

test("ignores colors, offsets, unconditional widths and elements without a reset", () => {
  assert.deepEqual(tokens("focus-visible:outline-2 focus-visible:outline-ring"), []);
  assert.deepEqual(
    tokens("outline-none focus-visible:outline-ring focus-visible:outline-offset-2 focus-visible:outline-[#fff]"),
    [],
  );
  assert.deepEqual(tokens("outline", "outline-none"), []);
});

test("groups the string arguments of one cn() or clsx() call", () => {
  assert.deepEqual(
    findings(`
      const row = cn(
        "flex outline-none",
        active
          ? "focus-visible:outline-2"
          : "opacity-50",
      );
      const chip = clsx(["outline-hidden"], ready && \`focus:outline-1 \${x}\`);
      const fine = cn("outline-none", open ?? "focus-visible:outline-2 focus-visible:outline-solid");
    `),
    ["5:focus-visible:outline-2", "8:focus:outline-1"],
  );
});

test("does not follow classes passed through variables or other calls", () => {
  assert.deepEqual(
    findings(`
      const jsx = <button className="outline-none focus-visible:outline-2" />;
      const template = \`outline-none \${size} focus:outline-2\`;
      const ring = "focus-visible:outline-2";
      const split = cn("outline-none", ring, other("focus-visible:outline-2"));
    `),
    ["2:focus-visible:outline-2", "3:focus:outline-2"],
  );
});
