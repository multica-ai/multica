"use client";

import { useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import type {
  AIImpactFunctionsResponse,
  AIImpactOverviewResponse,
  AIImpactQualityRiskResponse,
} from "../../core/ai-impact-api";
import {
  ControlRoomEmpty,
  ControlRoomLoading,
  ControlRoomPanel,
} from "./control-room-primitives";

type AIImpactView = "overview" | "functions" | "quality";

const VIEWS: { id: AIImpactView; label: string }[] = [
  { id: "overview", label: "Overview" },
  { id: "functions", label: "Functions" },
  { id: "quality", label: "Quality & Risk" },
];

export interface AIImpactLoadingState {
  overview: boolean;
  functions: boolean;
  qualityRisk: boolean;
}

export function AIImpactControlRoom({
  overview,
  functions,
  qualityRisk,
  isLoading,
  onOpenEvidence,
}: {
  overview: AIImpactOverviewResponse | undefined;
  functions: AIImpactFunctionsResponse | undefined;
  qualityRisk: AIImpactQualityRiskResponse | undefined;
  isLoading: AIImpactLoadingState;
  onOpenEvidence?: () => void;
}) {
  const [view, setView] = useState<AIImpactView>("overview");

  return (
    <div className="min-w-0 space-y-3 p-6">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold text-foreground">AI Impact</h2>
          <p className="text-[11px] text-muted-foreground">
            Evidence-backed outcomes, quality, and operating-loop decisions
          </p>
        </div>
        {onOpenEvidence && (
          <button
            type="button"
            onClick={onOpenEvidence}
            className="rounded-md border bg-background px-3 py-1.5 text-xs font-medium text-foreground transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            Open evidence
          </button>
        )}
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

      {view === "overview" ? (
        <OverviewView data={overview} loading={isLoading.overview} />
      ) : view === "functions" ? (
        <FunctionsView data={functions} loading={isLoading.functions} />
      ) : (
        <QualityRiskView data={qualityRisk} loading={isLoading.qualityRisk} />
      )}
    </div>
  );
}

function OverviewView({
  data,
  loading,
}: {
  data: AIImpactOverviewResponse | undefined;
  loading: boolean;
}) {
  if (loading) {
    return (
      <ControlRoomPanel title="Evidence overview" meta="Loading measured AI impact">
        <ControlRoomLoading rows={5} />
      </ControlRoomPanel>
    );
  }

  if (!data || data.families.every((family) => family.evidence.length === 0)) {
    return (
      <ControlRoomPanel title="Evidence overview" meta="Measured and estimated evidence">
        <ControlRoomEmpty>No AI impact evidence is available for this period.</ControlRoomEmpty>
      </ControlRoomPanel>
    );
  }

  return (
    <div className="grid gap-3 xl:grid-cols-2">
      {data.families.map((family) => (
        <ControlRoomPanel
          key={family.family}
          title={family.family}
          meta={`${family.evidence.length} evidence ${family.evidence.length === 1 ? "point" : "points"}`}
        >
          <div className="divide-y">
            {family.evidence.map((item) => (
              <div
                key={`${item.operating_loop_id}:${item.metric_id}:${item.period_start}`}
                className="flex items-start justify-between gap-4 px-4 py-3"
              >
                <div className="min-w-0">
                  <p className="truncate text-xs font-medium text-foreground">{item.metric_name}</p>
                  <p className="mt-0.5 truncate text-[10px] text-muted-foreground">
                    {item.function_name} · {item.operating_loop_name}
                  </p>
                </div>
                <div className="shrink-0 text-right">
                  <p className="font-mono text-sm font-semibold tabular-nums text-foreground">
                    {formatEvidenceValue(item.value, item.metric_unit)}
                  </p>
                  <p className="mt-0.5 text-[10px] text-muted-foreground">
                    {item.evidence_status} · {formatPercent(item.confidence)}
                  </p>
                </div>
              </div>
            ))}
          </div>
        </ControlRoomPanel>
      ))}
    </div>
  );
}

function FunctionsView({
  data,
  loading,
}: {
  data: AIImpactFunctionsResponse | undefined;
  loading: boolean;
}) {
  return (
    <ControlRoomPanel title="Function decisions" meta="Compare operating loops without ranking people">
      {loading ? (
        <ControlRoomLoading rows={5} />
      ) : !data || data.functions.length === 0 ? (
        <ControlRoomEmpty>No function-level evidence is available for this period.</ControlRoomEmpty>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b text-left text-[10px] uppercase tracking-wide text-muted-foreground">
                <th className="px-4 py-2 font-medium">Function</th>
                <th className="px-4 py-2 font-medium">Operating loop</th>
                <th className="px-4 py-2 font-medium">Decision</th>
              </tr>
            </thead>
            <tbody>
              {data.functions.flatMap((item) =>
                item.operating_loops.map((loop) => (
                  <tr key={`${item.id}:${loop.id}`} className="border-b last:border-0">
                    <td className="px-4 py-3 font-medium text-foreground">{item.name}</td>
                    <td className="px-4 py-3 text-muted-foreground">{loop.name}</td>
                    <td className="px-4 py-3 font-medium">{loop.decision}</td>
                  </tr>
                )),
              )}
            </tbody>
          </table>
        </div>
      )}
    </ControlRoomPanel>
  );
}

function QualityRiskView({
  data,
  loading,
}: {
  data: AIImpactQualityRiskResponse | undefined;
  loading: boolean;
}) {
  return (
    <ControlRoomPanel title="Quality & Risk decisions" meta="Operating-loop decisions from current evidence">
      {loading ? (
        <ControlRoomLoading rows={5} />
      ) : !data || data.decisions.length === 0 ? (
        <ControlRoomEmpty>No quality and risk decisions are available for this period.</ControlRoomEmpty>
      ) : (
        <div className="grid gap-2 p-3 md:grid-cols-2 xl:grid-cols-3">
          {data.decisions.map((decision) => (
            <article
              key={`${decision.function_id}:${decision.operating_loop_id}`}
              className="rounded-md border bg-background p-3"
            >
              <p className="text-[10px] text-muted-foreground">{decision.function_name}</p>
              <p className="mt-1 text-xs font-medium text-foreground">{decision.operating_loop_name}</p>
              <p className="mt-3 text-xl font-semibold text-foreground">{decision.decision}</p>
            </article>
          ))}
        </div>
      )}
    </ControlRoomPanel>
  );
}

function formatEvidenceValue(value: number, unit: string): string {
  return unit ? `${value.toLocaleString("en-US")} ${unit}` : value.toLocaleString("en-US");
}

function formatPercent(value: number): string {
  return `${Math.round(value * 100)}%`;
}
