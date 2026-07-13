#!/usr/bin/env node
// Regenerates server/cmd/multica/cerebro_feature_catalog.json from
// packages/cerebro-feature-flags/registry.ts (the single source of truth).
//
// Run via `pnpm generate:feature-catalog` (wraps node --experimental-strip-types
// so the TypeScript registry can be imported directly; requires Node >= 22.6).
// Drift is caught twice: CI re-runs the generator and `git diff --exit-code`s
// the output, and packages/cerebro-feature-flags/catalog-sync.test.ts compares
// the checked-in JSON against the live registry. The Go CLI embeds the JSON
// (see server/cmd/multica/cerebro_feature.go), so `multica feature list`
// always describes the same flags as the settings UI.

import { writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  CEREBRO_FLAGS,
  CEREBRO_FLAG_GROUPS,
  CEREBRO_FLAG_DEFAULTS,
} from "../packages/cerebro-feature-flags/registry.ts";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const outPath = resolve(repoRoot, "server/cmd/multica/cerebro_feature_catalog.json");

const groupKeys = new Set(CEREBRO_FLAG_GROUPS.map((g) => g.key));
if (groupKeys.size !== CEREBRO_FLAG_GROUPS.length) {
  throw new Error("registry.ts: duplicate group keys in CEREBRO_FLAG_GROUPS");
}

const seen = new Set();
for (const flag of CEREBRO_FLAGS) {
  if (seen.has(flag.key)) throw new Error(`registry.ts: duplicate flag key ${flag.key}`);
  seen.add(flag.key);
  if (!groupKeys.has(flag.group)) {
    throw new Error(`registry.ts: flag ${flag.key} references unknown group ${flag.group}`);
  }
  if (!(flag.key in CEREBRO_FLAG_DEFAULTS)) {
    throw new Error(`registry.ts: flag ${flag.key} has no entry in CEREBRO_FLAG_DEFAULTS`);
  }
}

const catalog = {
  groups: CEREBRO_FLAG_GROUPS.map((g) => ({
    key: g.key,
    label: g.label,
    description: g.description,
  })),
  flags: CEREBRO_FLAGS.map((f) => ({
    key: f.key,
    label: f.label,
    description: f.description,
    group: f.group,
    default: CEREBRO_FLAG_DEFAULTS[f.key],
  })),
};

writeFileSync(outPath, JSON.stringify(catalog, null, 2) + "\n");
console.log(
  `wrote ${outPath}: ${catalog.flags.length} flags in ${catalog.groups.length} groups`,
);
