#!/usr/bin/env node
// Fetch https://models.dev/api.json (the open-source pricing database
// OpenCode uses internally, MIT-licensed), filter it to the LLM
// providers Multica runtimes actually emit, and write a static pricing
// snapshot to packages/views/runtimes/pricing.generated.ts.
//
// Output is keyed by both `<provider>/<model>` (OpenCode form) and
// bare `<model>` (Claude-direct / legacy daemon form). Only the `cost`
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
// surface in usage data. Add a provider here only if a Multica runtime
// can actually emit a `<provider>/<model>` pair under it — extra
// providers just bloat the snapshot with markup-laden re-hosts (see
// 302ai, helicone, etc., which we deliberately leave out).
const ALLOWED_PROVIDERS = [
  // First-party providers — bare keys win when there is a collision.
  "anthropic",
  "deepseek",
  "google",
  "moonshotai",
  "openai",
  "xai",
  "zai",
  // Aggregators — prefixed keys only; bare keys are skipped if already
  // claimed by a first-party provider above.
  "github-copilot",
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

(async () => {
  const db = await loadModelsDev();

  const skippedProviders = ALLOWED_PROVIDERS.filter((p) => !db[p]);
  if (skippedProviders.length) {
    console.warn(
      "Warning: allowed providers missing from models.dev: " +
        skippedProviders.join(", "),
    );
  }

  const rows = new Map(); // deduplicate by key
  let pricedCount = 0;
  let skippedNoCost = 0;

  // Models not in models.dev but used by Multica runtimes — maintained manually.
  // Source: cursor.com/docs/models-and-pricing (Cursor does not publish a
  // cache-write rate, so cacheWrite stays 0 to avoid inventing spend).
  // Cursor model ids are unprefixed generic names (`auto`, `composer-*`) that
  // collide with other providers. They are keyed as `cursor/<model>` so that
  // provider-qualified lookup resolves them only for Cursor runtimes. The
  // bare `cursor` key equals the provider name itself, so it stays unqualified.
  const extraEntries = [
    { key: "cursor/auto",            input: 1.25, output: 6,    cache_read: 0.25,  cache_write: 0 },
    { key: "cursor/composer-2.5-fast", input: 3,  output: 15,   cache_read: 0.5,   cache_write: 0 },
    { key: "cursor/composer-2.5",   input: 0.5,  output: 2.5,  cache_read: 0.2,   cache_write: 0 },
    { key: "cursor/composer-2-fast", input: 1.5, output: 7.5,  cache_read: 0.35,  cache_write: 0 },
    { key: "cursor/composer-2",     input: 0.5,  output: 2.5,  cache_read: 0.2,   cache_write: 0 },
    { key: "cursor/composer-1.5",   input: 3.5,  output: 17.5, cache_read: 0.35,  cache_write: 0 },
    { key: "cursor/composer-1",     input: 1.25, output: 10,   cache_read: 0.125, cache_write: 0 },
    { key: "cursor",                input: 3,    output: 15,   cache_read: 0.5,   cache_write: 0 },
  ];
  for (const e of extraEntries) {
    if (!rows.has(e.key)) {
      rows.set(e.key, e);
      pricedCount++;
    }
  }

  for (const provider of ALLOWED_PROVIDERS) {
    const prov = db[provider];
    if (!prov) continue;
    const ids = Object.keys(prov.models || {}).sort();
    for (const id of ids) {
      const cost = prov.models[id]?.cost;
      // Skip entries without real per-token pricing — both missing values
      // and explicit zeros. Subscription providers (github-copilot) list
      // their full catalog with cost: {0,0,0,0}; those rows shadow first-
      // party prices once mapped to bare keys (e.g. `claude-opus-4.7`
      // would resolve to $0 instead of Anthropic's $5/$25).
      if (!cost || (!cost.input && !cost.output)) {
        skippedNoCost++;
        continue;
      }
      // Emit both the provider-prefixed key (OpenCode form) and the bare
      // key (legacy daemon form). The resolver is a simple exact match,
      // so both shapes need their own row.
      // First-party providers (anthropic, deepseek, google, moonshotai, openai, xai)
      // win over aggregators (opencode, opencode-go) when a bare key collides.
      const withProvider = `${provider}/${id}`;
      if (!rows.has(withProvider)) {
        rows.set(withProvider, cost);
        pricedCount++;
      }
      if (!rows.has(id)) {
        rows.set(id, cost);
        pricedCount++;
      }
    }
  }

  const today = new Date().toISOString().slice(0, 10);
  const lines = [
    "// AUTO-GENERATED — do not edit by hand.",
    "//",
    "// Source: https://models.dev/api.json (MIT, community-maintained,",
    "// the same dataset OpenCode uses internally).",
    `// Snapshot: ${today}`,
    "//",
    "// Regenerate with: node scripts/generate-pricing.mjs",
    "",
    "// Keys are either `<provider>/<model>` (OpenCode form) or bare `<model>`",
    "// (legacy daemon form). The resolver does a simple exact match, so both",
    "// shapes need their own row.",
    "export const MODEL_PRICING: Readonly<Record<string, {",
    "  input: number;",
    "  output: number;",
    "  cacheRead: number;",
    "  cacheWrite: number;",
    "}>> = {",
  ];

  const sortedKeys = [...rows.keys()].sort();

  // Pre-compute clamped effective pricing for all rows so we can do the
  // redundancy check and the final emit in one pass.
  const effective = new Map();
  for (const [key, c] of rows) {
    // Clamp cacheRead to input so estimateCacheSavings never goes negative.
    // models.dev has a few upstream-quirky rows (e.g. gpt-3.5-turbo) where
    // cache_read > input, which would make "money saved by the cache" a
    // negative number. A discount can never cost more than the full rate.
    const cacheRead = Math.min(c.cache_read ?? c.input, c.input);
    // Clamp cacheWrite to >= input. Prompt-cache writes are billed at or
    // above the input rate everywhere we've checked, so an upstream row
    // with cacheWrite < input (e.g. claude-3-sonnet-20240229 listing
    // cache_write: 0.3 vs input: 3) is almost certainly a data error and
    // would systematically under-bill cache-creation tokens.
    // Exception: explicit 0 is intentional (Cursor does not bill cache writes).
    const cacheWrite = (c.cache_write === 0) ? 0 : Math.max(c.cache_write ?? c.input, c.input);
    effective.set(key, { input: c.input, output: c.output, cacheRead, cacheWrite });
  }

  const pricingId = (p) =>
    `${p.input}|${p.output}|${p.cacheRead}|${p.cacheWrite}`;

  // Fully-canonical form: mirrors the resolver's canonicalCandidates
  // transformations (utils.ts:280-317) that eliminate resolver-equivalent
  // aliases — strip provider prefix, apply claude dot→dash, strip context
  // tag, strip trailing date. Two keys with the same FC and identical
  // effective pricing resolve to the same entry; keep only the FC
  // representative and drop the redundant alias.
  //
  // Critically, canonAnthropic only applies to claude-* IDs — OpenAI
  // dot/dash (gpt-5.4 ≠ gpt-5-4) are NOT normalized and must both stay.
  //
  // github-copilot/* entries with real pricing land here too: their dashed
  // provider counterpart does not exist in models.dev, but stripProvider
  // reaches the identical bare-model entry (e.g. github-copilot/claude-opus-4.7
  // → claude-opus-4-7 — same $5/$25 rate). Safe to drop: the resolver's
  // stripProvider step finds the bare key without the vendor row.
  const fullyCanonical = (key) => {
    const i = key.indexOf("/");
    let s =
      i > 0 && /^[a-z][a-z0-9_-]*$/i.test(key.slice(0, i))
        ? key.slice(i + 1)
        : key;
    if (s.startsWith("claude-")) s = s.replace(/\./g, "-");
    s = s.replace(/\[[^\]]+\]$/, "");
    s = s.replace(/-(20\d{2}-\d{2}-\d{2}|20\d{6}|latest)$/, "");
    return s;
  };

  let skippedRedundant = 0;

  for (const key of sortedKeys) {
    const fc = fullyCanonical(key);
    if (
      fc !== key &&
      effective.has(fc) &&
      pricingId(effective.get(fc)) === pricingId(effective.get(key))
    ) {
      skippedRedundant++;
      continue;
    }
    const eff = effective.get(key);
    lines.push(`  ${JSON.stringify(key)}: { input: ${eff.input}, output: ${eff.output}, cacheRead: ${eff.cacheRead}, cacheWrite: ${eff.cacheWrite} },`);
  }
  lines.push("};");
  lines.push("");

  mkdirSync(dirname(OUTPUT_PATH), { recursive: true });
  writeFileSync(OUTPUT_PATH, lines.join("\n"));

  console.log(`Wrote ${OUTPUT_PATH}`);
  console.log(`  Providers included: ${ALLOWED_PROVIDERS.length - skippedProviders.length}`);
  console.log(`  Models with pricing: ${pricedCount}`);
  console.log(`  Models skipped (no cost data): ${skippedNoCost}`);
  console.log(`  Resolver-equivalent duplicates skipped: ${skippedRedundant}`);
})().catch((err) => {
  console.error(err);
  process.exit(1);
});
