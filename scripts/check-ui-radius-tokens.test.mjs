import assert from "node:assert/strict";
import test from "node:test";
import { resolve } from "node:path";

import { radiusViolations } from "./check-ui-radius-tokens.mjs";

const probe = resolve("packages/views/zz-radius-probe.tsx");

test("rejects bare radius utilities, including variants and template segments", () => {
  const source = `
    const plain = "rounded border";
    const variant = \`hover:rounded \${active ? "bg-muted" : ""}\`;
  `;
  assert.deepEqual(
    radiusViolations(source, probe).map((finding) => finding.token),
    ["rounded", "hover:rounded"],
  );
});

test("rejects pixel-arbitrary product radii", () => {
  const findings = radiusViolations(
    `const classes = "rounded-[4px] data-open:rounded-t-[12px]";`,
    probe,
  );
  assert.deepEqual(
    findings.map((finding) => finding.token),
    ["rounded-[4px]", "data-open:rounded-t-[12px]"],
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
