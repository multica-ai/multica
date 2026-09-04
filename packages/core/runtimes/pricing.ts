import { z } from "zod";
import pricingJson from "./pricing.json";
import { defaultStorage } from "../platform/storage";

export const modelPricingRowSchema = z.object({
  input: z.number().finite().min(0).max(1e9),
  output: z.number().finite().min(0).max(1e9),
  cacheRead: z.number().finite().min(0).max(1e9),
  cacheWrite: z.number().finite().min(0).max(1e9),
  provider: z.string().optional(),
  model: z.string().optional(),
  source: z.string().optional(),
  sourceUrl: z.string().optional(),
});
export type ModelPricingRow = z.infer<typeof modelPricingRowSchema>;
const modelPricingWireRowSchema = modelPricingRowSchema
  .omit({ cacheRead: true, cacheWrite: true, sourceUrl: true })
  .extend({
    cache_read: modelPricingRowSchema.shape.cacheRead,
    cache_write: modelPricingRowSchema.shape.cacheWrite,
    source_url: z.string().optional(),
  })
  .transform(({ cache_read, cache_write, source_url, ...row }) => ({
    ...row,
    cacheRead: cache_read,
    cacheWrite: cache_write,
    ...(source_url === undefined ? {} : { sourceUrl: source_url }),
  }));

export function modelPricingRowToWire(row: ModelPricingRow) {
  const { cacheRead, cacheWrite, sourceUrl, ...rest } = row;
  return {
    ...rest,
    cache_read: cacheRead,
    cache_write: cacheWrite,
    ...(sourceUrl === undefined ? {} : { source_url: sourceUrl }),
  };
}

export const modelPricingSnapshotSchema = z.object({
  version: z.string().min(1),
  rows: z.record(z.string(), modelPricingWireRowSchema).refine((rows) => Object.keys(rows).length > 0),
  aliases: z.record(z.string(), z.string()),
  overrides: z.record(z.string(), modelPricingWireRowSchema),
  revision: z.number().int().nonnegative(),
  can_manage: z.boolean(),
  checked_at: z.string().nullable(),
  succeeded_at: z.string().nullable(),
  last_error: z.string(),
  timezone: z.string(),
}).transform(({ can_manage, checked_at, succeeded_at, last_error, ...snapshot }) => ({
  ...snapshot,
  canManage: can_manage,
  checkedAt: checked_at,
  succeededAt: succeeded_at,
  lastError: last_error,
}));
export type ModelPricingSnapshot = z.infer<typeof modelPricingSnapshotSchema>;
export type PricingContext = Pick<
  ModelPricingSnapshot,
  "rows" | "aliases" | "overrides"
>;

export const BUNDLED_PRICING: PricingContext = {
  rows: z.record(z.string(), modelPricingRowSchema).parse(pricingJson.rows),
  aliases: pricingJson.aliases,
  overrides: {},
};

export function pricingCandidates(model: string, provider?: string): string[] {
  const base = new Set<string>();
  const add = (key: string) => {
    if (!key) return;
    base.add(key);
    // Keep an exact transport spelling first, then its catalog spelling.
    base.add(key.replace(/^([a-z][a-z0-9_-]*):/, "$1/"));
  };
  let raw = model.trim().toLowerCase();
  while (raw) {
    add(raw);
    let plain = raw.replace(/\[[^\]]+\]$/, "");
    add(plain);
    if (plain.startsWith("claude-")) {
      plain = plain.replace(/(\d)\.(\d)/g, "$1-$2");
      add(plain);
    }
    add(plain.replace(/-(20\d{2}-\d{2}-\d{2}|20\d{6}|latest)$/, ""));
    const prefix = raw.match(/^[a-z][a-z0-9_-]*[/:]/);
    if (!prefix) break;
    raw = raw.slice(prefix[0].length);
  }
  const p = provider?.trim().toLowerCase();
  return p
    ? [...base]
        .map((key) => (key.startsWith(`${p}/`) || key.startsWith(`${p}:`) ? key : `${p}/${key}`))
        .concat([...base])
    : [...base];
}

function aliasTarget(key: string, context: PricingContext): string {
  const seen = new Set<string>();
  while (context.aliases[key] && !seen.has(key)) {
    seen.add(key);
    key = context.aliases[key]!;
  }
  return key;
}

export function resolveModelPricing(
  model: string,
  provider?: string,
  context: PricingContext = BUNDLED_PRICING,
): ModelPricingRow | undefined {
  const keys = pricingCandidates(model, provider);
  for (const key of keys) {
    if (context.overrides[key]) return context.overrides[key];
  }
  for (const key of keys) {
    const target = aliasTarget(key, context);
    if (context.overrides[target]) return context.overrides[target];
    // A plain canonical override also applies to a subscription alias.
    const bare = target.slice(target.lastIndexOf("/") + 1);
    if (context.aliases[bare] === target && context.overrides[bare])
      return context.overrides[bare];
  }
  for (const key of keys) {
    const row = context.rows[aliasTarget(key, context)] ?? context.rows[key];
    if (row) return row;
  }
  return undefined;
}

// Legacy browser prices are available only for explicit, previewed import.
// They are never an active source of workspace prices.
const LEGACY_KEY = "multica_runtime_custom_pricing";
const legacySchema = z
  .object({
    state: z
      .object({ pricings: z.record(z.string(), z.unknown()) })
      .passthrough(),
  })
  .passthrough();
export function readLegacyModelPrices(): Record<string, ModelPricingRow> {
  try {
    const raw = defaultStorage.getItem(LEGACY_KEY);
    const parsed = legacySchema.safeParse(raw ? JSON.parse(raw) : null);
    if (!parsed.success) return {};
    return Object.fromEntries(
      Object.entries(parsed.data.state.pricings).flatMap(([key, value]) => {
        const row = modelPricingRowSchema.safeParse(value);
        return row.success ? [[key, row.data]] : [];
      }),
    );
  } catch {
    return {};
  }
}
export function clearImportedModelPrices(
  imported: Record<string, ModelPricingRow>,
): void {
  if (Object.keys(imported).length === 0) return;
  try {
    const raw = defaultStorage.getItem(LEGACY_KEY);
    const parsed = legacySchema.safeParse(raw ? JSON.parse(raw) : null);
    if (!parsed.success) return;
    const remaining = parsed.data.state.pricings;
    for (const [key, value] of Object.entries(imported)) {
      const row = modelPricingRowSchema.safeParse(remaining[key]);
      if (row.success && JSON.stringify(row.data) === JSON.stringify(value))
        delete remaining[key];
    }
    if (Object.keys(remaining).length === 0)
      defaultStorage.removeItem(LEGACY_KEY);
    else defaultStorage.setItem(LEGACY_KEY, JSON.stringify(parsed.data));
  } catch {
    /* A failed local cleanup must not turn a successful server save into failure. */
  }
}
