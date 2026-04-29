"use client";

interface ActivityBucketLike {
  total: number;
  failed: number;
}

interface SparklineProps {
  /**
   * Buckets in display order (oldest → newest). One column per bucket;
   * total drives column height, failed renders on top in destructive so
   * the column conveys *throughput + failure share* in one glance — both
   * dimensions live on the same shape, no extra row chrome required.
   */
  buckets: readonly ActivityBucketLike[];
  width: number;
  height: number;
  className?: string;
}

const SUCCESS_FILL = "var(--color-brand)";
const SUCCESS_OPACITY = 0.6;
const FAILED_FILL = "var(--color-destructive)";
const BASELINE_FILL = "var(--color-muted-foreground)";
const BASELINE_OPACITY = 0.25;

/**
 * Stacked bar sparkline — success bottom, failure top. One row, one shape,
 * two dimensions:
 *
 *   - **Column height** = total throughput that day (per-component scaled
 *     so a quiet agent reads "its own shape", not flattened by a noisy
 *     neighbour).
 *   - **Red share** = failure rate. A 100-runs-1-failed agent and a
 *     100-runs-99-failed agent must be told apart at scan speed; the only
 *     way to do that with a single column is to encode the second
 *     dimension *inside* the column.
 *
 * Why not just colour the whole column red on any failure (the "binary"
 * approach we shipped first)? Because it loses the failure-rate dimension
 * — 1/100 and 99/100 paint the same. Splitting the segment keeps the
 * dimension while staying within "one element per cell", which is the
 * scan-speed budget.
 *
 * Pre-life days (the agent didn't exist yet) and real zero days look the
 * same here on purpose — distinguishing them adds row variety the column
 * doesn't earn (Tufte data-ink); the tooltip carries "Created N days ago"
 * where it actually matters.
 */
export function Sparkline({
  buckets,
  width,
  height,
  className,
}: SparklineProps) {
  const n = buckets.length;
  if (n === 0) {
    return (
      <svg
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        className={className}
        aria-hidden
      />
    );
  }

  // Column geometry — gap = 1px, columns share the rest equally. Round to
  // whole pixels so sub-pixel rects don't render fuzzy at this size.
  const gap = 1;
  const colWidth = Math.max(1, Math.floor((width - gap * (n - 1)) / n));
  const usedWidth = colWidth * n + gap * (n - 1);
  const offsetX = Math.floor((width - usedWidth) / 2);

  // Per-component max so a low-volume agent's shape isn't flattened by a
  // single noisy day from a neighbour.
  let maxTotal = 0;
  for (const b of buckets) if (b.total > maxTotal) maxTotal = b.total;
  const scaleDenominator = Math.max(1, maxTotal);

  // Reserve 1px so columns visually sit on something rather than floating.
  const baselineY = height - 1;
  const usableH = height - 1;

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      className={className}
      aria-hidden
    >
      {/* Faint floor — a row with zero history still reads as "a row",
          not a missing cell. */}
      <rect
        x={0}
        y={baselineY}
        width={width}
        height={1}
        fill={BASELINE_FILL}
        fillOpacity={BASELINE_OPACITY}
      />
      {buckets.map((b, i) => {
        if (b.total === 0) return null;
        const x = offsetX + i * (colWidth + gap);
        const totalH = Math.max(
          1,
          Math.round((usableH * b.total) / scaleDenominator),
        );
        // 1px floor on the failed segment so a single failure is still
        // visible; clamp by total so 1-of-1 doesn't make the failed
        // segment taller than the column itself.
        const failedH =
          b.failed > 0
            ? Math.min(
                totalH,
                Math.max(1, Math.round((usableH * b.failed) / scaleDenominator)),
              )
            : 0;
        const successH = totalH - failedH;
        const colTop = baselineY - totalH;
        return (
          <g key={i}>
            {successH > 0 && (
              <rect
                x={x}
                y={colTop + failedH}
                width={colWidth}
                height={successH}
                fill={SUCCESS_FILL}
                fillOpacity={SUCCESS_OPACITY}
              />
            )}
            {failedH > 0 && (
              <rect
                x={x}
                y={colTop}
                width={colWidth}
                height={failedH}
                fill={FAILED_FILL}
              />
            )}
          </g>
        );
      })}
    </svg>
  );
}
