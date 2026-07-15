import type { ReactNode } from "react";

export function ControlRoomPanel({
  title,
  meta,
  children,
  className = "",
}: {
  title: string;
  meta?: string;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={`min-w-0 overflow-hidden rounded-lg border bg-card ${className}`}>
      <header className="flex items-start justify-between gap-3 border-b px-4 py-3">
        <div>
          <h2 className="text-xs font-semibold text-foreground">{title}</h2>
          {meta && <p className="mt-0.5 text-[10px] text-muted-foreground">{meta}</p>}
        </div>
      </header>
      {children}
    </section>
  );
}

export function ControlRoomKpi({
  label,
  value,
  note,
  tone = "default",
}: {
  label: string;
  value: string;
  note: string;
  tone?: "default" | "positive" | "warning";
}) {
  const valueClass = tone === "positive"
    ? "text-emerald-600"
    : tone === "warning"
      ? "text-amber-600"
      : "text-foreground";

  return (
    <div className="min-w-0 px-4 py-3">
      <p className="truncate text-[10px] font-medium uppercase tracking-wide text-muted-foreground">{label}</p>
      <p className={`mt-1 font-mono text-xl font-semibold tabular-nums ${valueClass}`}>{value}</p>
      <p className="mt-0.5 truncate text-[10px] text-muted-foreground">{note}</p>
    </div>
  );
}

export function ControlRoomEmpty({ children }: { children: ReactNode }) {
  return <p className="px-4 py-10 text-center text-xs text-muted-foreground">{children}</p>;
}

export function ControlRoomLoading({ rows = 4 }: { rows?: number }) {
  return (
    <div className="space-y-2 p-4" aria-label="Loading panel">
      {Array.from({ length: rows }, (_, index) => (
        <div key={index} className="h-8 animate-pulse rounded bg-muted" />
      ))}
    </div>
  );
}

export function MetricBar({ value, maximum }: { value: number; maximum: number }) {
  const width = maximum > 0 ? Math.max(4, Math.round((value / maximum) * 100)) : 0;
  return (
    <span className="block h-1.5 overflow-hidden rounded-full bg-muted" aria-hidden="true">
      <span className="block h-full rounded-full bg-primary" style={{ width: `${width}%` }} />
    </span>
  );
}

export function formatCompact(value: number): string {
  return value.toLocaleString("en", { notation: "compact", maximumFractionDigits: 1 });
}

export function formatDollars(cents: number): string {
  return `$${(cents / 100).toLocaleString("en", { maximumFractionDigits: 2 })}`;
}
