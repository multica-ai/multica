import assert from "node:assert/strict";
import test from "node:test";
import { resolve } from "node:path";

import { radiusViolations } from "./check-ui-radius-tokens.mjs";

const probe = resolve("packages/views/zz-radius-probe.tsx");

test("rejects bare radius utilities, including directions, variants, and templates", () => {
  const source = `
    const plain = "rounded border";
    const variant = \`hover:rounded data-open:rounded-t \${active ? "rounded-bl" : ""}\`;
  `;
  assert.deepEqual(
    radiusViolations(source, probe).map((finding) => finding.token),
    ["rounded", "hover:rounded", "data-open:rounded-t", "rounded-bl"],
  );
});

test("rejects fixed arbitrary product radii regardless of CSS unit", () => {
  const findings = radiusViolations(
    `const classes = "rounded-[4px] data-open:rounded-t-[0.75rem] rounded-b-[1em]";`,
    probe,
  );
  assert.deepEqual(
    findings.map((finding) => finding.token),
    ["rounded-[4px]", "data-open:rounded-t-[0.75rem]", "rounded-b-[1em]"],
  );
});

test("accepts named, computed, and explicitly allowlisted micro-geometry", () => {
  assert.equal(
    radiusViolations(
      `const classes = "rounded-xs rounded-lg rounded-[calc(var(--radius)-3px)]";`,
      probe,
    ).length,
    0,
  );
  assert.equal(
    radiusViolations(
      `const mark = "rounded-[2px]";`,
      resolve("packages/ui/components/ui/chart.tsx"),
    ).length,
    0,
  );
});