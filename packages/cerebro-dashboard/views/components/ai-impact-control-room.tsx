"use client";

import { useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import {
  ControlRoomEmpty,
  ControlRoomLoading,
  ControlRoomPanel,
  formatDollars,
} from "./control-room-primitives";

export type AIImpactDecision = "Scale" | "Observe" | "Stop";
export type AIImpactEvidenceStatus = "Measured" | "Estimated" | "Missing";

export interface AIImpactFunction {
  id: string;
  name: string;
  operating_loops: number;
  realized_cash_cents: number;
  approved_capacity_cents: number;
  estimated_value_cents: number;
  ai_cost_cents: number;
  implementation_cost_cents: number;
  net_value_cents: number;
  decision: AIImpactDecision;
  evidence_status: AIImpactEvidenceStatus;
  confidence: number;
}

export interface AIImpactQualityGuardrail {
  id: string;
  name: string;
  value: string;
  target: string;
  passed: boolean;
  critical: boolean;
  evidence_status: AIImpactEvidenceStatus;
  confidence: number;
}

export interface AIImpactSummary {
  period_start: string;
  period_end: string;
  realized_cash_cents: number;
  approved_capacity_cents: number;
  estimated_value_cents: number;
  ai_cost_cents: number;
  implementation_cost_cents: number;
  net_value_cents: number;
  decision: AIImpactDecision;
  evidence_status: AIImpactEvidenceStatus;
  confidence: number;
  functions: AIImpactFunction[];
  quality_guardrails: AIImpactQualityGuardrail[];
}

type AIImpactView = "overview" | "functions" | "quality";

const VIEWS: { id: AIImpactView; label: string }[] = [
  { id: "overview", label: "Overview" },
  { id: "functions", label: "Functions" },
  { id: "quality", label: "Quality & Risk" },
];

export function AIImpactControlRoom({
  data,
  loading,
  onOpenEvidence,
}: {
  data: AIImpactSummary | undefined;
  loading: boolean;
  onOpenEvidence: () => void;
}) {
  const [view, setView] = useState<AIImpactView>("overview");

  return (
    <div className="min-w-0 space-y-3 p-6">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold text-foreground">AI Impact</h2>
          <p className="text-[11px] text-muted-foreground">
            Evidence-backed value, cost, capacity, and quality decisions
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={onOpenEvidence}
            className="rounded-md border bg-background px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            Open evidence
          </button>
        </div>
      </header>

      <nav className="flex gap-1 border-b" aria-label="AI Impact views">
        {VIEWS.map((item) => (
          <button
            key={item.id}
            type="button"
            onClick={() => setView(item.id)}
            aria-current={view === item.id ? "page" : undefined}
            className={cn(
              "border-b-2 px-3 py-2 text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              view === item.id
                ? "border-primary text-foreground"
                : "border-transparent text-muted-foreground hover:text-foreground",
            )}
          >
            {item.label}
          </button>
        ))}
      </nav>

      {loading ? (
        <ControlRoomPanel title="Value flow" meta="Loading measured AI impact">
          <ControlRoomLoading rows={5} />
        </ControlRoomPanel>
      ) : !data ? (
        <ControlRoomPanel title="Value flow" meta="Measured outcomes only">
          <ControlRoomEmpty>No AI impact evidence is available for this period.</ControlRoomEmpty>
        </ControlRoomPanel>
      ) : view === "overview" ? (
        <OverviewView data={data} />
      ) : view === "functions" ? (
        <FunctionsView functions={data.functions} />
      ) : (
        <QualityView guardrails={data.quality_guardrails} />
      )}
    </div>
  );
}

function OverviewView({ data }: { data: AIImpactSummary }) {
  const totalCost = data.ai_cost_cents + data.implementation_cost_cents;

  return (
    <div className="grid gap-3 xl:grid-cols-[minmax(0,2fr)_minmax(260px,1fr)]">
      <ControlRoomPanel
        title="Value flow"
        meta={`${formatPeriod(data.period_start, data.period_end)} · realized value and approved capacity only`}
      >
        <div className="grid divide-y sm:grid-cols-4 sm:divide-x sm:divide-y-0">
          <ValueStep
            label="AI cost"
            value={formatDollars(totalCost)}
            note={`${formatDollars(data.ai_cost_cents)} usage · ${formatDollars(data.implementation_cost_cents)} implementation`}
          />
          <ValueStep
            label="Capacity released"
            value={formatDollars(data.approved_capacity_cents)}
            note="Approved capacity value"
          />
          <ValueStep
            label="Outcome value"
            value={formatDollars(data.realized_cash_cents)}
            note={`${formatDollars(data.estimated_value_cents)} estimated separately`}
          />
          <ValueStep
            label="Net value"
            value={formatDollars(data.net_value_cents)}
            note="Realized cash + approved capacity − total cost"
            emphasized
          />
        </div>
      </ControlRoomPanel>

      <ControlRoomPanel title="Decision" meta="Evidence status and confidence">
        <div className="space-y-4 p-4">
          <div>
            <p className="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
              Recommendation
            </p>
            <p className="mt-1 text-2xl font-semibold text-foreground">{data.decision}</p>
          </div>
          <div className="grid grid-cols-2 gap-2">
            <EvidenceFact label="Evidence" value={data.evidence_status} />
            <EvidenceFact label="Confidence" value={formatPercent(data.confidence)} />
          </div>
        </div>
      </ControlRoomPanel>
    </div>
  );
}

function FunctionsView({ functions }: { functions: AIImpactFunction[] }) {
  return (
    <ControlRoomPanel title="Function decisions" meta="Compare value without ranking people">
      {functions.length === 0 ? (
        <ControlRoomEmpty>No function-level evidence is available for this period.</ControlRoomEmpty>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b text-left text-[10px] uppercase tracking-wide text-muted-foreground">
                <th className="px-4 py-2 font-medium">Function</th>
                <th className="px-4 py-2 font-medium">Operating loops</th>
                <th className="px-4 py-2 font-medium">Net value</th>
                <th className="px-4 py-2 font-medium">Evidence</th>
                <th className="px-4 py-2 font-medium">Decision</th>
              </tr>
            </thead>
            <tbody>
              {functions.map((item) => (
                <tr key={item.id} className="border-b last:border-0">
                  <td className="px-4 py-3 font-medium text-foreground">{item.name}</td>
                  <td className="px-4 py-3 font-mono tabular-nums">{item.operating_loops}</td>
                  <td className="px-4 py-3 font-mono tabular-nums">{formatDollars(item.net_value_cents)}</td>
                  <td className="px-4 py-3 text-muted-foreground">
                    {item.evidence_status} · {formatPercent(item.confidence)}
                  </td>
                  <td className="px-4 py-3 font-medium">{item.decision}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </ControlRoomPanel>
  );
}

function QualityView({ guardrails }: { guardrails: AIImpactQualityGuardrail[] }) {
  return (
    <ControlRoomPanel title="Quality guardrails" meta="Critical failures override value gains">
      {guardrails.length === 0 ? (
        <ControlRoomEmpty>No quality guardrail evidence is available for this period.</ControlRoomEmpty>
      ) : (
        <div className="grid gap-2 p-3 md:grid-cols-2 xl:grid-cols-3">
          {guardrails.map((guardrail) => (
            <article key={guardrail.id} className="rounded-md border bg-background p-3">
              <div className="flex items-start justify-between gap-3">
                <div>
                  <p className="text-xs font-medium text-foreground">{guardrail.name}</p>
                  <p className="mt-0.5 text-[10px] text-muted-foreground">Target {guardrail.target}</p>
                </div>
                <span className="text-[10px] font-medium text-muted-foreground">
                  {guardrail.passed ? "Within guardrail" : guardrail.critical ? "Critical risk" : "Needs attention"}
                </span>
              </div>
              <p className="mt-3 font-mono text-xl font-semibold tabular-nums">{guardrail.value}</p>
              <p className="mt-1 text-[10px] text-muted-foreground">
                {guardrail.evidence_status} · {formatPercent(guardrail.confidence)} confidence
              </p>
            </article>
          ))}
        </div>
      )}
    </ControlRoomPanel>
  );
}

function ValueStep({
  label,
  value,
  note,
  emphasized = false,
}: {
  label: string;
  value: string;
  note: string;
  emphasized?: boolean;
}) {
  return (
    <div className={cn("min-w-0 p-4", emphasized && "bg-primary/5")}>
      <p className="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">{label}</p>
      <p className="mt-2 font-mono text-2xl font-semibold tabular-nums text-foreground">{value}</p>
      <p className="mt-1 text-[10px] text-muted-foreground">{note}</p>
    </div>
  );
}

function EvidenceFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border bg-background p-3">
      <p className="text-[10px] text-muted-foreground">{label}</p>
      <p className="mt-1 font-mono text-sm font-semibold tabular-nums text-foreground">{value}</p>
    </div>
  );
}

function formatPercent(value: number): string {
  return `${Math.round(value * 100)}%`;
}

function formatPeriod(start: string, end: string): string {
  const formatter = new Intl.DateTimeFormat("en", { day: "numeric", month: "short" });
  const startDate = new Date(start);
  const endDate = new Date(end);
  if (Number.isNaN(startDate.getTime()) || Number.isNaN(endDate.getTime())) return "Selected period";
  return `${formatter.format(startDate)}–${formatter.format(endDate)}`;
}
