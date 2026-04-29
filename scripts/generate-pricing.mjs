#!/usr/bin/env node
// Fetch https://models.dev/api.json (the open-source pricing database
// OpenCode uses internally, MIT-licensed), filter it to the LLM
// providers Multica runtimes actually emit, and write a static pricing
// snapshot to packages/views/runtimes/pricing.generated.ts.
//
// Output is keyed by `<provider>/<model>` to match what OpenCode and
// other multi-provider runtimes report on the wire. Only the `cost`
// field of each model entry is preserved.
//
// Usage:
//   node scripts/generate-pricing.mjs                # live fetch
//   MODELS_DEV_PATH=/path/to/api.json node scripts/generate-pricing.mjs
//
// Re-run whenever upstream prices change. The output file is checked in
// so production builds don't depend on `models.dev` being reachable.

import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const __dirname = dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = resolve(__dirname, "..");
const OUTPUT_PATH = resolve(
  REPO_ROOT,
  "packages/views/runtimes/pricing.generated.ts",
);

// LLM providers (as keyed in models.dev) whose models Multica runtimes
// surface in usage data. Each entry is paired with the Multica runtime
// kinds that route to it, so the rationale stays close to the data.
//
// Add a provider here only if a Multica runtime can actually emit a
// `<provider>/<model>` pair under it — extra providers just bloat the
// snapshot with markup-laden re-hosts (see 302ai, helicone, etc., which
// we deliberately leave out).
// Providers: anthropic, openai, google, moonshotai, opencode, opencode-go
const ALLOWED_PROVIDERS = [
  "anthropic",
  "openai",
  "google",
  "moonshotai",
  "opencode",
  "opencode-go",
];

async function loadModelsDev() {
  const path = process.env.MODELS_DEV_PATH;
  if (path) return JSON.parse(readFileSync(path, "utf8"));
  const res = await fetch("https://models.dev/api.json");
  if (!res.ok) throw new Error(`models.dev fetch ${res.status}`);
  return await res.json();
}

// Stable JSON-ish field order so generated diffs only show real price
// changes, not key reshuffles.
const COST_FIELD_ORDER = [
  "input",
  "output",
  "reasoning",
  "cache_read",
  "cache_write",
  "input_audio",
  "output_audio",
];

function serializeCost(cost) {
  // We deliberately emit only the enumerated number fields. models.dev
  // sometimes includes context-tier objects (e.g.
  // `gemini-2.5-pro.cost.context_over_200k`); the Multica usage stream
  // doesn't carry context size, so we always price at the standard
  // tier. If a new flat number field appears upstream, add it to
  // COST_FIELD_ORDER and ModelCost together.
  const entries = [];
  for (const k of COST_FIELD_ORDER) {
    if (typeof cost[k] === "number") entries.push(`${k}: ${cost[k]}`);
  }
  return `{ ${entries.join(", ")} }`;
}

(async () => {
  const db = await loadModelsDev();

  const skippedProviders = ALLOWED_PROVIDERS.filter((p) => !db[p]);
  if (skippedProviders.length) {
    console.warn(
      "Warning: allowed providers missing from models.dev: " +
        skippedProviders.join(", "),
    );
  }

  const rows = [];
  let pricedCount = 0;
  let skippedNoCost = 0;
  for (const provider of ALLOWED_PROVIDERS) {
    const prov = db[provider];
    if (!prov) continue;
    const ids = Object.keys(prov.models || {}).sort();
    for (const id of ids) {
      const cost = prov.models[id]?.cost;
      if (!cost || (cost.input == null && cost.output == null)) {
        skippedNoCost++;
        continue;
      }
      rows.push({ key: `${provider}/${id}`, cost });
      pricedCount++;
    }
  }
  rows.sort((a, b) => a.key.localeCompare(b.key));

  const today = new Date().toISOString().slice(0, 10);
  const lines = [
    "// AUTO-GENERATED — do not edit by hand.",
    "//",
    "// Source: https://models.dev/api.json (MIT, community-maintained,",
    "// the same dataset OpenCode uses internally).",
    `// Snapshot: ${today}`,
    "// Providers: " + ALLOWED_PROVIDERS.join(", "),
    "//",
    "// Regenerate with: node scripts/generate-pricing.mjs",
    "",
    "// Cost values are USD per million tokens, matching the raw",
    "// `cost` shape on models.dev — fields are optional and only",
    "// present when the upstream provider charges separately.",
    "export interface ModelCost {",
    "  input: number;",
    "  output: number;",
    "  reasoning?: number;",
    "  cache_read?: number;",
    "  cache_write?: number;",
    "  input_audio?: number;",
    "  output_audio?: number;",
    "}",
    "",
    "// Keys are `<provider>/<model>` to match what OpenCode and other",
    "// multi-provider runtimes report on the wire.",
    "export const PRICING: Readonly<Record<string, ModelCost>> = {",
  ];

  for (const r of rows) {
    lines.push(`  ${JSON.stringify(r.key)}: ${serializeCost(r.cost)},`);
  }
  lines.push("};");
  lines.push("");

  mkdirSync(dirname(OUTPUT_PATH), { recursive: true });
  writeFileSync(OUTPUT_PATH, lines.join("\n"));

  console.log(`Wrote ${OUTPUT_PATH}`);
  console.log(`  Providers included: ${ALLOWED_PROVIDERS.length - skippedProviders.length}`);
  console.log(`  Models with pricing: ${pricedCount}`);
  console.log(`  Models skipped (no cost data): ${skippedNoCost}`);
})().catch((err) => {
  console.error(err);
  process.exit(1);
});
