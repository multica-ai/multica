// format.ts — display formatters for the eval UI. A run stores cost in cents
// (hundredths of a krone) and latency in milliseconds; people read kroner and
// seconds, so the catalog and See-why screens format through here. Non-finite
// input degrades to zero rather than rendering "NaN".

export function formatCost(cents: number): string {
  const kr = (Number.isFinite(cents) ? cents : 0) / 100;
  return `${kr.toFixed(2)} kr`;
}

export function formatDuration(ms: number): string {
  const value = Number.isFinite(ms) ? ms : 0;
  if (value < 1000) return `${Math.round(value)} ms`;
  return `${(value / 1000).toFixed(1)} s`;
}
