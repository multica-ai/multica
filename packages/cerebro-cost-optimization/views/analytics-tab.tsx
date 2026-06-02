"use client";

import { useMemo, useState } from "react";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { COST_SAVINGS, type CostSavingKey } from "../registry";
import {
  formatAnalyticsSaved,
  type AnalyticsIssueRow,
  type AnalyticsSavingSummary,
} from "../analytics";
import {
  useCostOptimizationAnalyticsIssuesQuery,
  useCostOptimizationAnalyticsRunsQuery,
  useCostOptimizationAnalyticsSummaryQuery,
} from "./api";
import { CostOptimizationDashboard } from "./dashboard";

function SummaryRow({
  row,
  selected,
  onSelect,
}: {
  row: AnalyticsSavingSummary;
  selected: boolean;
  onSelect: () => void;
}) {
  const label =
    COST_SAVINGS.find((s) => s.key === row.savingKey)?.label ?? row.savingKey;
  return (
    <button
      type="button"
      onClick={onSelect}
      className={`w-full rounded-md border px-3 py-2 text-left transition-colors ${
        selected ? "border-primary bg-muted/50" : "hover:bg-muted/30"
      }`}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-medium">{label}</span>
        <Badge variant="secondary" className="text-xs">
          {row.runs30d} runs · 30d
        </Badge>
      </div>
      <p className="mt-1 text-xs text-muted-foreground tabular-nums">
        Saved: {formatAnalyticsSaved(row.metric, row.savedUnits30d, row.savedCents30d)}
        {" · "}
        7d: {formatAnalyticsSaved(row.metric, row.savedUnits7d, row.savedCents7d)}
        {" · "}
        Today: {formatAnalyticsSaved(row.metric, row.savedUnitsToday, row.savedCentsToday)}
      </p>
    </button>
  );
}

function IssueList({
  savingKey,
  selectedIssueId,
  onSelectIssue,
}: {
  savingKey: CostSavingKey;
  selectedIssueId: string | null;
  onSelectIssue: (issueId: string) => void;
}) {
  const issuesQuery = useCostOptimizationAnalyticsIssuesQuery(savingKey);

  if (issuesQuery.isLoading) {
    return <p className="text-xs text-muted-foreground">Loading issues…</p>;
  }
  if (issuesQuery.isError) {
    return (
      <p className="text-xs text-destructive">
        Failed to load issues:{" "}
        {issuesQuery.error instanceof Error
          ? issuesQuery.error.message
          : "unknown error"}
      </p>
    );
  }
  const issues = issuesQuery.data ?? [];
  if (issues.length === 0) {
    return (
      <p className="text-xs text-muted-foreground italic">
        No measured runs for this saving in the last 30 days.
      </p>
    );
  }

  return (
    <ul className="space-y-1">
      {issues.map((issue: AnalyticsIssueRow) => (
        <li key={issue.issueId}>
          <button
            type="button"
            onClick={() => onSelectIssue(issue.issueId)}
            className={`w-full rounded-md px-2 py-1.5 text-left text-xs ${
              selectedIssueId === issue.issueId
                ? "bg-muted font-medium"
                : "hover:bg-muted/40"
            }`}
          >
            <span className="font-mono text-muted-foreground">
              #{issue.issueNumber}
            </span>{" "}
            {issue.issueTitle || "(untitled)"}
            <span className="float-right tabular-nums text-muted-foreground">
              {issue.runCount} runs
            </span>
          </button>
        </li>
      ))}
    </ul>
  );
}

function RunList({
  savingKey,
  issueId,
}: {
  savingKey: CostSavingKey;
  issueId: string;
}) {
  const runsQuery = useCostOptimizationAnalyticsRunsQuery(savingKey, issueId);

  if (runsQuery.isLoading) {
    return <p className="text-xs text-muted-foreground">Loading runs…</p>;
  }
  if (runsQuery.isError) {
    return (
      <p className="text-xs text-destructive">
        Failed to load runs:{" "}
        {runsQuery.error instanceof Error
          ? runsQuery.error.message
          : "unknown error"}
      </p>
    );
  }
  const runs = runsQuery.data ?? [];
  if (runs.length === 0) {
    return (
      <p className="text-xs text-muted-foreground italic">No runs for this issue.</p>
    );
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-xs">
        <thead>
          <tr className="border-b text-left text-muted-foreground">
            <th className="py-1 pr-2">When</th>
            <th className="py-1 pr-2">Mode</th>
            <th className="py-1 pr-2">Saved</th>
            <th className="py-1">Arms</th>
          </tr>
        </thead>
        <tbody>
          {runs.map((run) => (
            <tr key={run.taskId} className="border-b border-muted/40">
              <td className="py-1.5 pr-2 whitespace-nowrap tabular-nums">
                {run.createdAt
                  ? new Date(run.createdAt).toLocaleString()
                  : "—"}
              </td>
              <td className="py-1.5 pr-2">{run.mode}</td>
              <td className="py-1.5 pr-2 tabular-nums">
                {formatAnalyticsSaved(run.metric, run.savedUnits, run.savedCents)}
              </td>
              <td className="py-1.5 text-muted-foreground">
                {run.heldOut
                  ? "holdout"
                  : run.applied
                    ? "applied"
                    : "shadow"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function CostOptimizationAnalyticsTab() {
  const summaryQuery = useCostOptimizationAnalyticsSummaryQuery();
  const [selectedKey, setSelectedKey] = useState<CostSavingKey | null>(null);
  const [selectedIssueId, setSelectedIssueId] = useState<string | null>(null);

  const summaryRows = useMemo(() => {
    const fromApi = summaryQuery.data ?? [];
    if (fromApi.length > 0) return fromApi;
    return COST_SAVINGS.map((def) => ({
      savingKey: def.key,
      metric: def.metric,
      runsToday: 0,
      savedUnitsToday: 0,
      savedCentsToday: 0,
      runs7d: 0,
      savedUnits7d: 0,
      savedCents7d: 0,
      runs30d: 0,
      savedUnits30d: 0,
      savedCents30d: 0,
    }));
  }, [summaryQuery.data]);

  const activeKey = selectedKey ?? summaryRows[0]?.savingKey ?? null;

  return (
    <section className="space-y-6">
      <p className="text-xs text-muted-foreground">
        Estimated vs. measured savings per agent saving. Use the dashboard for
        holdout A/B when a control arm exists; drill down by saving to see top
        issues and individual runs from measurement data.
      </p>

      <CostOptimizationDashboard />

      {summaryQuery.isError && (
        <p className="text-xs text-destructive">
          Failed to load analytics summary:{" "}
          {summaryQuery.error instanceof Error
            ? summaryQuery.error.message
            : "unknown error"}
        </p>
      )}

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1.2fr)]">
        <Card>
          <CardContent className="space-y-2 pt-4">
            <h3 className="text-sm font-medium">By saving</h3>
            <div className="space-y-2">
              {summaryRows.map((row) => (
                <SummaryRow
                  key={row.savingKey}
                  row={row}
                  selected={activeKey === row.savingKey}
                  onSelect={() => {
                    setSelectedKey(row.savingKey);
                    setSelectedIssueId(null);
                  }}
                />
              ))}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-2 pt-4">
            <h3 className="text-sm font-medium">Top issues</h3>
            {activeKey ? (
              <IssueList
                savingKey={activeKey}
                selectedIssueId={selectedIssueId}
                onSelectIssue={setSelectedIssueId}
              />
            ) : (
              <p className="text-xs text-muted-foreground">Select a saving.</p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardContent className="space-y-2 pt-4">
            <div className="flex items-center justify-between gap-2">
              <h3 className="text-sm font-medium">Runs</h3>
              {selectedIssueId && activeKey && (
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  className="h-7 text-xs"
                  onClick={() => setSelectedIssueId(null)}
                >
                  Clear issue
                </Button>
              )}
            </div>
            {activeKey && selectedIssueId ? (
              <RunList savingKey={activeKey} issueId={selectedIssueId} />
            ) : (
              <p className="text-xs text-muted-foreground italic">
                Select an issue to list runs.
              </p>
            )}
          </CardContent>
        </Card>
      </div>
    </section>
  );
}
