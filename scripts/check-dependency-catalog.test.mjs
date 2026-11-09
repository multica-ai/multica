import assert from "node:assert/strict";
import test from "node:test";

import { catalogViolations, parseWorkspaceManifest } from "./check-dependency-catalog.mjs";

const workspace = (dir, manifest) => ({ dir, manifest });

test("parses packages and the default catalog, ignoring comments", () => {
  const { packages, catalog } = parseWorkspaceManifest(`packages:
  - apps/*
  - "packages/*"

catalog:
  '@scope/pkg': ^1.2.0
  # Comment about the next entry.
  typescript: npm:@typescript/typescript6@6.0.2
  react: 19.2.3 # trailing comment

onlyBuiltDependencies:
  - esbuild
`);
  assert.deepEqual(packages, ["apps/*", "packages/*"]);
  assert.deepEqual(
    [...catalog],
    [
      ["@scope/pkg", "^1.2.0"],
      ["typescript", "npm:@typescript/typescript6@6.0.2"],
      ["react", "19.2.3"],
    ],
  );
});

test("rejects a dependency shared by two workspaces without catalog:", () => {
  const violations = catalogViolations({
    catalog: new Map(),
    workspaces: [
      workspace("packages/ui", { dependencies: { sonner: "^2.0.7" } }),
      workspace("packages/views", { dependencies: { sonner: "catalog:" } }),
    ],
  });
  assert.equal(violations.length, 1);
  assert.match(violations[0], /^sonner: declared by 2 workspaces.*packages\/ui dependencies \^2\.0\.7$/);
});

test("rejects a plain range for a dependency the catalog defines, even in one workspace", () => {
  const violations = catalogViolations({
    catalog: new Map([["tailwindcss", "^4"]]),
    workspaces: [workspace("apps/desktop", { devDependencies: { tailwindcss: "^4" } })],
  });
  assert.equal(violations.length, 1);
  assert.match(violations[0], /^tailwindcss: catalog defines \^4/);
});

test("accepts catalog:, workspace:, single-workspace deps, peers, and mobile pins", () => {
  const violations = catalogViolations({
    catalog: new Map([["react", "19.2.3"]]),
    workspaces: [
      workspace("apps/web", {
        dependencies: { react: "catalog:", "@multica/core": "workspace:*", "web-only": "^1.0.0" },
      }),
      workspace("packages/views", {
        peerDependencies: { react: "^19", eslint: "^9.0.0" },
        devDependencies: { eslint: "^9.0.0" },
      }),
      workspace("packages/eslint-config", { peerDependencies: { eslint: "^9.0.0" } }),
      workspace("apps/mobile", { dependencies: { react: "19.2.3", "web-only": "^0.9.0" } }),
    ],
  });
  assert.deepEqual(violations, []);
});

test("allowlisted drift passes until it is gone, then must be removed", () => {
  const allowlist = new Map([["fumadocs-core", "pending upgrade"]]);
  const drifted = [
    workspace("apps/web", { dependencies: { "fumadocs-core": "^15.5.2" } }),
    workspace("apps/docs", { dependencies: { "fumadocs-core": "^16.17.8" } }),
  ];
  assert.deepEqual(catalogViolations({ catalog: new Map(), workspaces: drifted, allowlist }), []);
  assert.deepEqual(
    catalogViolations({
      catalog: new Map([["fumadocs-core", "^16.17.8"]]),
      workspaces: drifted,
      allowlist,
    }),
    [],
  );

  const aligned = catalogViolations({
    catalog: new Map([["fumadocs-core", "^16.17.8"]]),
    workspaces: drifted.map(({ dir }) => workspace(dir, { dependencies: { "fumadocs-core": "catalog:" } })),
    allowlist,
  });
  assert.equal(aligned.length, 1);
  assert.match(aligned[0], /^fumadocs-core: allowlisted but no longer/);
});
