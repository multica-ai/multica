"use client";

import { useQuery } from "@tanstack/react-query";
import { X } from "lucide-react";
import { analyticsQueryOptions } from "@multica/cerebro-usage";
import type { AnalyticsFilter } from "../../core/analytics";

export type RunRow = Record<string, unknown>;

function str(value: unknown): string | null {
  if (value == null || value === "") return null;
  return String(value);
}
function num(value: unknown): number {
  return typeof value === "number" ? value : Number(value) || 0;
}
function dollars(cents: unknown): string {
  return `$${(num(cents) / 100).toLocaleString("en", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}
function integer(value: unknown): string {
  return num(value).toLocaleString("en");
}
function duration(seconds: unknown): string {
  const total = Math.round(num(seconds));
  if (total < 60) return `${total}s`;
  const minutes = Math.floor(total / 60);
  return `${minutes}m ${total % 60}s`;
}
function isUrl(value: string | null): value is string {
  return typeof value === "string" && (value.startsWith("http") || value.startsWith("/"));
}

// Run & debug context rail. Opens when a row in the Matching runs table is
// clicked, so any run can be traced end to end: source -> person -> outcome,
// the issue/thread it came from, execution economics, the skills it invoked,
// and a link to the full trace. Closes the FIR-2996 trace drill-down gap.
export function RunDetailDrawer({
  workspaceId,
  run,
  timezone,
  onClose,
}: {
  workspaceId: string;
  run: RunRow;
  timezone: string;
  onClose: () => void;
}) {
  const runId = str(run.run) ?? str(run.run_id) ?? "";
  const skillFilter: AnalyticsFilter[] = runId
    ? [{ dimension: "run", operator: "in", values: [runId] }]
    : [];
  const skills = useQuery({
    ...analyticsQueryOptions(workspaceId, {
      population: "all",
      metrics: ["skill_invocations"],
      dimensions: ["skill"],
      grain: "none",
      filters: skillFilter,
      page: { limit: 20 },
      timezone,
    }),
    enabled: workspaceId.length > 0 && runId.length > 0,
  });

  const debugLink = str(run.debug_link);
  const trace = str(run.trace);
  const reference = str(run.reference_label) ?? str(run.reference) ?? "Run";
  const status = str(run.status) ?? "unknown";
  const person = str(run.person) ?? "Unknown";
  const source = str(run.source) ?? "—";
  const skillRows = skills.data?.rows ?? [];

  return (
    <aside
      aria-label="Run and debug context"
      className="flex h-full w-[350px] shrink-0 flex-col overflow-y-auto border-l bg-card"
    >
      <header className="flex items-start justify-between border-b px-4 py-3">
        <div>
          <h2 className="text-sm font-semibold">Run &amp; debug context</h2>
          <p className="font-mono text-[11px] text-muted-foreground">{runId || "—"}</p>
        </div>
        <button
          type="button"
          aria-label="Close run context"
          onClick={onClose}
          className="rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <X className="size-4" />
        </button>
      </header>

      <div className="space-y-4 p-4">
        <div className="grid grid-cols-[1fr_auto_1fr_auto_1fr] items-center gap-1 rounded-lg border bg-muted/30 p-3 text-center">
          <RibbonNode label="Source" value={source} />
          <span className="text-muted-foreground">›</span>
          <RibbonNode label="Person" value={person} />
          <span className="text-muted-foreground">›</span>
          <RibbonNode label="Outcome" value={status} accent={status.toLowerCase() === "completed"} />
        </div>

        <Section title="Debug links">
          {debugLink ? (
            <a
              href={debugLink}
              className="flex items-center justify-between rounded-md border bg-muted/30 px-3 py-2 hover:border-primary hover:text-primary"
            >
              <span className="min-w-0">
                <span className="block truncate text-xs font-medium">{reference}</span>
                <span className="block text-[10px] text-muted-foreground">Issue / thread context</span>
              </span>
              <span className="text-xs font-medium">Open →</span>
            </a>
          ) : (
            <p className="text-xs text-muted-foreground">No linked issue or thread.</p>
          )}
        </Section>

        <Section title="Execution">
          <Dl>
            <Row label="Provider" value={str(run.provider) ?? "—"} />
            <Row label="Model" value={str(run.model) ?? "—"} />
            <Row label="Runtime" value={str(run.runtime) ?? "—"} />
            <Row label="Duration" value={duration(run.duration_seconds)} />
          </Dl>
        </Section>

        <Section title="Usage &amp; economics">
          <Dl>
            <Row label="Input tokens" value={integer(run.input_tokens)} mono />
            <Row label="Output tokens" value={integer(run.output_tokens)} mono />
            <Row label="Gateway cost" value={dollars(run.cost_cents)} mono />
            <Row label="Measured savings" value={dollars(run.saved_cents)} mono accent />
          </Dl>
        </Section>

        <Section title="Skills used">
          {skills.isLoading ? (
            <p className="text-xs text-muted-foreground">Loading…</p>
          ) : skillRows.length === 0 ? (
            <p className="text-xs text-muted-foreground">No skills recorded for this run.</p>
          ) : (
            <Dl>
              {skillRows.map((row, index) => (
                <Row
                  key={`${str(row.skill) ?? index}`}
                  label={str(row.skill) ?? "Unknown"}
                  value={`${integer(row.skill_invocations)} calls`}
                />
              ))}
            </Dl>
          )}
        </Section>

        <Section title="Trace preview">
          <ol className="space-y-2 border-l border-primary/30 pl-3 text-[11px] text-muted-foreground">
            <li>Run context loaded</li>
            {debugLink && <li>Source reference linked</li>}
            <li>Usage and economics measured</li>
            <li className={status.toLowerCase() === "completed" ? "text-emerald-600" : "text-foreground"}>Run {status.toLowerCase()}</li>
          </ol>
        </Section>

        {isUrl(trace) ? (
          <a
            href={trace}
            className="flex h-9 items-center justify-center rounded-md bg-primary text-xs font-semibold text-primary-foreground hover:opacity-90"
          >
            Open full trace →
          </a>
        ) : trace ? (
          <div className="rounded-md border bg-muted/30 px-3 py-2 text-center font-mono text-[11px] text-muted-foreground">
            trace {trace}
          </div>
        ) : (
          <p className="text-center text-xs text-muted-foreground">No trace captured.</p>
        )}
      </div>
    </aside>
  );
}

function RibbonNode({ label, value, accent }: { label: string; value: string; accent?: boolean }) {
  return (
    <div className="min-w-0">
      <span className="block text-[9px] uppercase tracking-wide text-muted-foreground">{label}</span>
      <span className={`block truncate text-xs font-semibold ${accent ? "text-emerald-600" : ""}`}>{value}</span>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="border-t pt-3 first:border-t-0 first:pt-0">
      <h3
        className="mb-2 text-[10px] uppercase tracking-wide text-muted-foreground"
        dangerouslySetInnerHTML={{ __html: title }}
      />
      {children}
    </section>
  );
}

function Dl({ children }: { children: React.ReactNode }) {
  return <div className="space-y-1.5">{children}</div>;
}

function Row({ label, value, mono, accent }: { label: string; value: string; mono?: boolean; accent?: boolean }) {
  return (
    <div className="flex items-center justify-between gap-3 text-xs">
      <span className="min-w-0 truncate text-muted-foreground">{label}</span>
      <span className={`${mono ? "font-mono" : "font-medium"} ${accent ? "text-emerald-600" : ""}`}>{value}</span>
    </div>
  );
}
