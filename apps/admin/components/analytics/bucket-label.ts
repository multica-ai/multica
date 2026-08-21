// Buckets are anchored to the request's `from` timestamp, not wall-clock
// midnight (see bucketStartIsoExpr in lib/queries.ts) — `from` is `now minus
// the window`, so a bucket boundary can fall at any hour/minute regardless
// of granularity. Always show the exact time so the label never implies an
// alignment (e.g. midnight) that isn't guaranteed.
export function bucketLabel(iso: string): string {
  const date = new Date(iso);
  return date.toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "numeric" });
}
