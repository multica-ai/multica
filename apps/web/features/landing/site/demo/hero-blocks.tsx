import type { ReactNode } from "react";
import { Check, CircleCheck, GitPullRequest, MessageCircle } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";

/*
 * Rich blocks the agents post in the hero's issue peek — charts, previews and
 * lists that stand in for paragraphs, so the story reads at a glance. They
 * animate in once when their comment appears.
 */

const GROW = "motion-reduce:animate-none";

function Block({ title, aside, children }: { title: string; aside?: ReactNode; children: ReactNode }) {
  return (
    <div className="mt-2.5 rounded-lg border border-border bg-muted/30 p-3">
      <div className="mb-2.5 flex items-center justify-between text-caption">
        <span className="font-medium">{title}</span>
        {aside ? <span className="text-muted-foreground">{aside}</span> : null}
      </div>
      {children}
    </div>
  );
}

export function Checklist({ title, items }: { title: string; items: string[] }) {
  return (
    <Block title={title} aside={`${items.length} steps`}>
      <ul className="flex flex-col gap-1.5 text-caption">
        {items.map((item, index) => (
          <li
            key={item}
            className="flex items-center gap-2 animate-in fade-in slide-in-from-left-1 duration-300 fill-mode-both motion-reduce:animate-none"
            style={{ animationDelay: `${200 + index * 180}ms` }}
          >
            <span className="flex size-3.5 shrink-0 items-center justify-center rounded-[4px] border border-border bg-background" />
            {item}
          </li>
        ))}
      </ul>
    </Block>
  );
}

export function PullRequest({
  number,
  title,
  added,
  removed,
  checks,
}: {
  number: number;
  title: string;
  added: number;
  removed: number;
  checks: string[];
}) {
  return (
    <div className="mt-2.5 rounded-lg border border-border">
      <div className="flex items-center gap-2 px-3 py-2 text-caption">
        <GitPullRequest className="size-3.5 shrink-0 text-success" />
        <span className="min-w-0 flex-1 truncate font-medium">
          #{number} {title}
        </span>
        <span className="text-success tabular-nums">+{added}</span>
        <span className="text-destructive tabular-nums">−{removed}</span>
      </div>
      <div className="flex items-center gap-3 border-t border-border px-3 py-1.5 text-micro text-muted-foreground">
        {checks.map((check, index) => (
          <span
            key={check}
            className="flex items-center gap-1 animate-in fade-in duration-300 fill-mode-both motion-reduce:animate-none"
            style={{ animationDelay: `${300 + index * 250}ms` }}
          >
            <CircleCheck className="size-3 text-success" />
            {check}
          </span>
        ))}
      </div>
    </div>
  );
}

/** Stacked bars: requests the limiter let through, and the 429s above the line. */
export function LoadChart() {
  const offered = [60, 80, 120, 180, 260, 340, 420, 480, 500, 500, 460, 380];
  const limit = 100;
  const max = 500;
  return (
    <Block
      title="Requests per minute on staging"
      aside={
        <span className="flex items-center gap-3">
          <Legend className="bg-brand">served</Legend>
          <Legend className="bg-destructive/60">429</Legend>
        </span>
      }
    >
      <div className="relative flex h-24 items-end gap-1.5">
        <div
          className="absolute inset-x-0 border-t border-dashed border-foreground/40"
          style={{ bottom: `${(limit / max) * 100}%` }}
        >
          <span className="absolute -top-4 left-0 text-micro text-muted-foreground">limit 100</span>
        </div>
        {offered.map((value, index) => {
          const served = Math.min(value, limit);
          return (
            <div
              key={index}
              className={cn("flex flex-1 origin-bottom flex-col justify-end animate-site-grow-y", GROW)}
              style={{ height: `${(value / max) * 100}%`, animationDelay: `${index * 60}ms` }}
            >
              {value > limit ? <div className="rounded-t-[2px] bg-destructive/60" style={{ flexGrow: value - limit }} /> : null}
              <div className={cn("bg-brand", value > limit ? "" : "rounded-t-[2px]")} style={{ flexGrow: served }} />
            </div>
          );
        })}
      </div>
    </Block>
  );
}

export function BarList({ title, aside, bars }: { title: string; aside: string; bars: { label: string; value: number }[] }) {
  const max = Math.max(...bars.map((bar) => bar.value));
  return (
    <Block title={title} aside={aside}>
      <div className="flex flex-col gap-2">
        {bars.map((bar, index) => (
          <div key={bar.label} className="grid grid-cols-[128px_1fr_24px] items-center gap-2 text-caption">
            <span className="truncate">{bar.label}</span>
            <span className="h-2 overflow-hidden rounded-full bg-muted">
              <span
                className={cn("block h-full origin-left rounded-full bg-brand animate-site-grow-x", GROW)}
                style={{ width: `${(bar.value / max) * 100}%`, animationDelay: `${200 + index * 150}ms` }}
              />
            </span>
            <span className="text-right text-muted-foreground tabular-nums">{bar.value}</span>
          </div>
        ))}
      </div>
    </Block>
  );
}

/** Export time against row count, crossing the request timeout. */
export function TimeoutChart() {
  const points: [number, number][] = [
    [0, 0],
    [2, 2],
    [4, 6],
    [6, 11],
    [8, 17],
    [10, 25],
    [12, 34],
  ];
  const w = 300;
  const h = 84;
  const x = (rows: number) => (rows / 12) * w;
  const y = (seconds: number) => h - (seconds / 36) * h;
  const path = points.map(([rows, seconds], index) => `${index ? "L" : "M"}${x(rows).toFixed(1)} ${y(seconds).toFixed(1)}`).join(" ");
  return (
    <Block title="Export time by rows" aside="staging, 12k rows">
      <svg viewBox={`0 0 ${w} ${h + 14}`} className="block w-full overflow-visible">
        <line x1="0" x2={w} y1={y(30)} y2={y(30)} className="stroke-destructive/60" strokeDasharray="4 3" />
        <text x={0} y={y(30) - 4} className="fill-muted-foreground text-[10px]">
          timeout 30s
        </text>
        <path
          d={path}
          pathLength={1}
          strokeDasharray="1"
          className={cn("fill-none stroke-brand animate-site-draw", GROW)}
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        <circle
          cx={x(12)}
          cy={y(34)}
          r="3.5"
          className="fill-destructive animate-in fade-in zoom-in duration-300 fill-mode-both motion-reduce:animate-none"
          style={{ animationDelay: "1100ms" }}
        />
        {[0, 4, 8, 12].map((rows) => (
          <text key={rows} x={x(rows)} y={h + 12} textAnchor={rows === 12 ? "end" : rows === 0 ? "start" : "middle"} className="fill-muted-foreground text-[10px]">
            {rows}k
          </text>
        ))}
      </svg>
    </Block>
  );
}

export function DocPreview({ title, meta }: { title: string; meta: string }) {
  return (
    <div className="mt-2.5 overflow-hidden rounded-lg border border-border">
      <div className="relative h-20 overflow-hidden bg-[linear-gradient(135deg,oklch(0.93_0.05_290),oklch(0.95_0.04_200))]">
        <div className="absolute inset-x-4 top-4 flex flex-col gap-1.5">
          {[72, 54, 64].map((width, index) => (
            <span
              key={index}
              className={cn("block h-1.5 origin-left rounded-full bg-foreground/15 animate-site-grow-x", GROW)}
              style={{ width: `${width}%`, animationDelay: `${200 + index * 120}ms` }}
            />
          ))}
        </div>
        <span className="absolute right-4 bottom-3 text-title-sm font-semibold tracking-tight text-foreground">Autopilots</span>
      </div>
      <div className="flex items-center gap-2 px-3 py-2 text-caption">
        <span className="min-w-0 flex-1 truncate font-medium">{title}</span>
        <span className="text-muted-foreground">{meta}</span>
      </div>
    </div>
  );
}

const SOURCE_TONE = { Intercom: "bg-sky-500", Discord: "bg-indigo-500" } as const;

export function ReportCluster({ reports }: { reports: { source: keyof typeof SOURCE_TONE; from: string; quote: string }[] }) {
  return (
    <Block title="Grouped reports" aside={`${reports.length} merged`}>
      <ul className="flex flex-col gap-1.5">
        {reports.map((report, index) => (
          <li
            key={report.from}
            className="flex items-center gap-2 rounded-md bg-background px-2 py-1.5 text-caption shadow-[var(--surface-shadow)] animate-in fade-in slide-in-from-bottom-1 duration-300 fill-mode-both motion-reduce:animate-none"
            style={{ animationDelay: `${200 + index * 200}ms` }}
          >
            <span className={cn("flex size-4 shrink-0 items-center justify-center rounded-[4px] text-white", SOURCE_TONE[report.source])}>
              <MessageCircle className="size-2.5" />
            </span>
            <span className="min-w-0 flex-1 truncate">“{report.quote}”</span>
            <span className="shrink-0 text-muted-foreground">{report.from}</span>
          </li>
        ))}
      </ul>
    </Block>
  );
}

export function Replies({ people }: { people: { initials: string; name: string }[] }) {
  return (
    <div className="mt-2.5 flex items-center gap-3 rounded-lg border border-border px-3 py-2 text-caption">
      <span className="flex -space-x-1.5">
        {people.map((person, index) => (
          <span
            key={person.name}
            className="flex size-6 items-center justify-center rounded-full bg-muted text-micro font-medium ring-2 ring-background animate-in fade-in zoom-in-75 duration-300 fill-mode-both motion-reduce:animate-none"
            style={{ animationDelay: `${150 + index * 150}ms` }}
          >
            {person.initials}
          </span>
        ))}
      </span>
      <span className="min-w-0 flex-1 truncate text-muted-foreground">{people.map((person) => person.name).join(", ")}</span>
      <span className="flex items-center gap-1 text-success">
        <Check className="size-3.5" />
        Sent
      </span>
    </div>
  );
}

function Legend({ className, children }: { className: string; children: ReactNode }) {
  return (
    <span className="flex items-center gap-1">
      <span className={cn("size-2 rounded-[2px]", className)} />
      {children}
    </span>
  );
}
