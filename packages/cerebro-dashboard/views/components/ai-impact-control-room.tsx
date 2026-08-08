"use client";

import { useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import type {
  AIImpactFunctionsResponse,
  AIImpactOverviewResponse,
  AIImpactQualityRiskResponse,
} from "../../core/ai-impact-api";
import type { DashboardAIImpactSelections } from "../../core/filter-state";
import { useDashboardStore } from "../../core/store";
import {
  ControlRoomEmpty,
  ControlRoomLoading,
  ControlRoomPanel,
} from "./control-room-primitives";

type AIImpactView = "overview" | "functions" | "quality";

// Client-side selection filters: the AI Impact read models are fetched whole,
// so function/loop/decision clicks narrow every AI Impact view without an API
// round-trip. Selections live in the shared Dashboard filter state (URL-backed).
interface AIImpactSelectionApi {
  selections: DashboardAIImpactSelections;
  toggleFunctionLoop: (functionName: string, operatingLoop: string) => void;
  toggleDecision: (decision: string) => void;
  clearSelection: (key: keyof DashboardAIImpactSelections) => void;
}

function useAIImpactSelections(): AIImpactSelectionApi {
  const selections = useDashboardStore((state) => state.aiImpactSelections);
  const setAIImpactSelection = useDashboardStore((state) => state.setAIImpactSelection);
  return {
    selections,
    toggleFunctionLoop: (functionName, operatingLoop) => {
      const active =
        selections.functionName === functionName && selections.operatingLoop === operatingLoop;
      setAIImpactSelection({
        functionName: active ? null : functionName,
        operatingLoop: active ? null : operatingLoop,
      });
    },
    toggleDecision: (decision) => {
      setAIImpactSelection({ decision: selections.decision === decision ? null : decision });
    },
    clearSelection: (key) =>
      setAIImpactSelection({ [key]: null } as Partial<DashboardAIImpactSelections>),
  };
}

function matchesSelections(
  selections: DashboardAIImpactSelections,
  functionName: string,
  operatingLoop: string,
  decision?: string,
): boolean {
  if (selections.functionName && functionName !== selections.functionName) return false;
  if (selections.operatingLoop && operatingLoop !== selections.operatingLoop) return false;
  if (selections.decision && decision !== undefined && decision !== selections.decision) return false;
  return true;
}

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
  const selectionApi = useAIImpactSelections();
  const { selections, clearSelection } = selectionApi;
  const activeChips: { key: keyof DashboardAIImpactSelections; label: string; value: string }[] = [];
  if (selections.functionName) activeChips.push({ key: "functionName", label: "Function", value: selections.functionName });
  if (selections.operatingLoop) activeChips.push({ key: "operatingLoop", label: "Operating loop", value: selections.operatingLoop });
  if (selections.decision) activeChips.push({ key: "decision", label: "Decision", value: selections.decision });

  return (
    <div className="min-w-0 space-y-3 p-6">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold text-foreground">AI Impact</h2>
          <p className="text-[11px] text-muted-foreground">
            Evidence-backed outcomes, quality, and operating-loop decisions · click a row or card
            to filter every AI Impact view
          </p>
          {activeChips.length > 0 && (
            <div className="mt-2 flex flex-wrap gap-1" aria-label="Active AI Impact filters">
              {activeChips.map((chip) => (
                <button
                  key={chip.key}
                  type="button"
                  onClick={() => clearSelection(chip.key)}
                  aria-label={`Remove ${chip.label} filter ${chip.value}`}
                  className="rounded-md border bg-background px-2 py-0.5 text-[10px] text-muted-foreground hover:text-foreground"
                >
                  {chip.label}: {chip.value} x
                </button>
              ))}
            </div>
          )}
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
        <OverviewView data={overview} loading={isLoading.overview} selectionApi={selectionApi} />
      ) : view === "functions" ? (
        <FunctionsView data={functions} loading={isLoading.functions} selectionApi={selectionApi} />
      ) : (
        <QualityRiskView data={qualityRisk} loading={isLoading.qualityRisk} selectionApi={selectionApi} />
      )}
    </div>
  );
}

function OverviewView({
  data,
  loading,
  selectionApi,
}: {
  data: AIImpactOverviewResponse | undefined;
  loading: boolean;
  selectionApi: AIImpactSelectionApi;
}) {
  const { selections, toggleFunctionLoop } = selectionApi;
  if (loading) {
    return (
      <ControlRoomPanel title="Evidence overview" meta="Loading measured AI impact">
        <ControlRoomLoading rows={5} />
      </ControlRoomPanel>
    );
  }

  const families = (data?.families ?? [])
    .map((family) => ({
      ...family,
      evidence: family.evidence.filter((item) =>
        matchesSelections(selections, item.function_name, item.operating_loop_name),
      ),
    }))
    .filter((family) => family.evidence.length > 0);

  if (families.length === 0) {
    return (
      <ControlRoomPanel title="Evidence overview" meta="Measured and estimated evidence">
        <ControlRoomEmpty>No AI impact evidence matches the current selection.</ControlRoomEmpty>
      </ControlRoomPanel>
    );
  }

  return (
    <div className="grid gap-3 xl:grid-cols-2">
      {families.map((family) => (
        <ControlRoomPanel
          key={family.family}
          title={family.family}
          meta={`${family.evidence.length} evidence ${family.evidence.length === 1 ? "point" : "points"}`}
        >
          <div className="divide-y">
            {family.evidence.map((item) => (
              <button
                key={`${item.operating_loop_id}:${item.metric_id}:${item.period_start}`}
                type="button"
                onClick={() => toggleFunctionLoop(item.function_name, item.operating_loop_name)}
                aria-label={`Filter AI Impact by ${item.function_name} · ${item.operating_loop_name}`}
                className="flex w-full items-start justify-between gap-4 px-4 py-3 text-left hover:bg-muted/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
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
              </button>
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
  selectionApi,
}: {
  data: AIImpactFunctionsResponse | undefined;
  loading: boolean;
  selectionApi: AIImpactSelectionApi;
}) {
  const { selections, toggleFunctionLoop, toggleDecision } = selectionApi;
  const rows = (data?.functions ?? []).flatMap((item) =>
    item.operating_loops
      .filter((loop) => matchesSelections(selections, item.name, loop.name, loop.decision))
      .map((loop) => ({ item, loop })),
  );
  return (
    <ControlRoomPanel title="Function decisions" meta="Click a row to filter every AI Impact view">
      {loading ? (
        <ControlRoomLoading rows={5} />
      ) : rows.length === 0 ? (
        <ControlRoomEmpty>No function-level evidence matches the current selection.</ControlRoomEmpty>
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
              {rows.map(({ item, loop }) => (
                <tr key={`${item.id}:${loop.id}`} className="border-b transition-colors last:border-0 hover:bg-muted/60">
                  <td className="px-4 py-3 font-medium text-foreground">
                    <button type="button" onClick={() => toggleFunctionLoop(item.name, loop.name)} aria-label={`Filter AI Impact by ${item.name} · ${loop.name}`} className="hover:underline">
                      {item.name}
                    </button>
                  </td>
                  <td className="px-4 py-3 text-muted-foreground">
                    <button type="button" onClick={() => toggleFunctionLoop(item.name, loop.name)} aria-label={`Filter AI Impact by ${item.name} · ${loop.name}`} className="hover:underline">
                      {loop.name}
                    </button>
                  </td>
                  <td className="px-4 py-3 font-medium">
                    <button type="button" onClick={() => toggleDecision(loop.decision)} aria-label={`Filter AI Impact by decision ${loop.decision}`} className="hover:underline">
                      {loop.decision}
                    </button>
                  </td>
                </tr>
              ))}
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
  selectionApi,
}: {
  data: AIImpactQualityRiskResponse | undefined;
  loading: boolean;
  selectionApi: AIImpactSelectionApi;
}) {
  const { selections, toggleFunctionLoop } = selectionApi;
  const decisions = (data?.decisions ?? []).filter((decision) =>
    matchesSelections(
      selections,
      decision.function_name,
      decision.operating_loop_name,
      decision.decision,
    ),
  );
  return (
    <ControlRoomPanel title="Quality & Risk decisions" meta="Click a card to filter every AI Impact view">
      {loading ? (
        <ControlRoomLoading rows={5} />
      ) : decisions.length === 0 ? (
        <ControlRoomEmpty>No quality and risk decisions match the current selection.</ControlRoomEmpty>
      ) : (
        <div className="grid gap-2 p-3 md:grid-cols-2 xl:grid-cols-3">
          {decisions.map((decision) => (
            <button
              key={`${decision.function_id}:${decision.operating_loop_id}`}
              type="button"
              onClick={() => toggleFunctionLoop(decision.function_name, decision.operating_loop_name)}
              aria-label={`Filter AI Impact by ${decision.function_name} · ${decision.operating_loop_name}`}
              className="rounded-md border bg-background p-3 text-left hover:bg-muted/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <p className="text-[10px] text-muted-foreground">{decision.function_name}</p>
              <p className="mt-1 text-xs font-medium text-foreground">{decision.operating_loop_name}</p>
              <p className="mt-3 text-xl font-semibold text-foreground">{decision.decision}</p>
            </button>
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
