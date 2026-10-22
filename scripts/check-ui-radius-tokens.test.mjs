import assert from "node:assert/strict";
import test from "node:test";
import { resolve } from "node:path";

import {
  radiusScaleViolations,
  radiusViolations,
} from "./check-ui-radius-tokens.mjs";

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

test("keeps the mobile radius scale aligned with resolved web tokens", () => {
  const webTokens = `
    --radius-xs: calc(var(--radius) * 0.375);
    --radius-sm: calc(var(--radius) * 0.5);
    --radius-md: calc(var(--radius) * 0.75);
    --radius-lg: var(--radius);
    --radius-xl: calc(var(--radius) * 1.5);
    --radius-2xl: calc(var(--radius) * 2);
    --radius-3xl: calc(var(--radius) * 2.5);
    --radius-4xl: calc(var(--radius) * 3);
    --radius: 0.5rem;
  `;
  const aligned = { xs: 3, sm: 4, md: 6, lg: 8, xl: 12, "2xl": 16, "3xl": 20, "4xl": 24 };
  assert.deepEqual(radiusScaleViolations(webTokens, aligned), []);
  assert.deepEqual(
    radiusScaleViolations(webTokens, { ...aligned, xl: 10 }),
    ["xl: web=12px mobile=10px"],
  );
});
