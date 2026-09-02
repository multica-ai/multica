import {
  modelPricingRowSchema,
  type ModelPricingRow,
} from "@multica/core/runtimes/pricing";

export type PriceDraft = Record<
  "input" | "output" | "cacheRead" | "cacheWrite",
  string
>;

// Feed unit conversion can leave floating-point noise (for example 0.4 becomes
// 0.39999999999999997). Round only the reference display, never editable rates.
export function formatReferenceRate(value: number): string {
  return String(Number(value.toPrecision(15)));
}

// Keep every editable rate intact so saving existing custom rates is lossless.
export function toPriceDraft(row?: ModelPricingRow): PriceDraft {
  return {
    input: row ? String(row.input) : "",
    output: row ? String(row.output) : "",
    cacheRead: row ? String(row.cacheRead) : "",
    cacheWrite: row ? String(row.cacheWrite) : "",
  };
}

export function parsePriceDrafts(
  drafts: Record<string, PriceDraft>,
): Record<string, ModelPricingRow> | null {
  const rows: Record<string, ModelPricingRow> = {};
  for (const [key, draft] of Object.entries(drafts)) {
    const values = Object.values(draft).map((value) => value.trim());
    if (values.every((value) => !value)) continue;
    if (values.some((value) => !value)) return null;
    const parsed = modelPricingRowSchema.safeParse(
      Object.fromEntries(
        Object.entries(draft).map(([field, value]) => [field, Number(value)]),
      ),
    );
    if (!parsed.success) return null;
    rows[key] = parsed.data;
  }
  return rows;
}

export function hasPriceChanges(
  initial: Record<string, ModelPricingRow>,
  next: Record<string, ModelPricingRow>,
): boolean {
  const keys = Object.keys(initial);
  if (keys.length !== Object.keys(next).length) return true;
  return keys.some((key) => {
    const before = initial[key]!;
    const after = next[key];
    return (
      !after ||
      before.input !== after.input ||
      before.output !== after.output ||
      before.cacheRead !== after.cacheRead ||
      before.cacheWrite !== after.cacheWrite
    );
  });
}

export function previewLegacyPrices(
  legacy: Record<string, ModelPricingRow>,
  overrides: Record<string, ModelPricingRow>,
  drafts: Record<string, PriceDraft>,
): Record<string, ModelPricingRow> {
  return Object.fromEntries(
    Object.entries(legacy).filter(
      ([key]) =>
        !(key in overrides) &&
        (!drafts[key] ||
          Object.values(drafts[key]).every((value) => !value.trim())),
    ),
  );
}
