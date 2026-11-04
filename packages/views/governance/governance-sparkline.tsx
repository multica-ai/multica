"use client";

/**
 * A key result's readings as a line, oldest on the left. It draws nothing for
 * fewer than two points: one reading has no direction to show.
 */
export function sparklinePoints(values: number[], width: number, height: number): string {
  if (values.length < 2) return "";
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min || 1;
  return values
    .map((v, i) => {
      const x = (i / (values.length - 1)) * width;
      const y = height - ((v - min) / span) * height;
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");
}

export function GovernanceSparkline({ values, label }: { values: number[]; label: string }) {
  const points = sparklinePoints(values, 96, 24);
  if (!points) return <span aria-hidden="true" className="inline-block h-6 w-24" />;
  return (
    <svg role="img" aria-label={label} viewBox="0 0 96 24" className="h-6 w-24 text-primary" preserveAspectRatio="none">
      <polyline points={points} fill="none" stroke="currentColor" strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
    </svg>
  );
}
