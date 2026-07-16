// FIR-3212 — the Quality tab on the agent detail page: each config version
// measured on the runs that actually ran under it (the immutable run prompt
// snapshots are the sample base, so numbers attribute to the version the run
// really read). Honesty rules enforced here:
//   - missing data renders as "—" with a note, never as a fabricated 0
//   - every metric shows its sample size
//   - satisfaction is behavioral signals (reactions, approvals/rejections),
//     never an invented CSAT/star scale

"use client";

import type { ComponentType, ReactNode } from "react";
import { Gauge } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { getAgentQuality, type AgentQualityVersion } from "../api";

export interface AgentQualityTabExtension {
  id: string;
  labelKey: "quality";
  icon: ComponentType<{ className?: string }>;
  render: (context: {
    agent: Agent;
    runtimes: AgentRuntime[];
    canEdit: boolean;
  }) => ReactNode;
}

export function createAgentQualityTabs(): AgentQualityTabExtension[] {
  return [
    {
      id: "quality",
      labelKey: "quality",
      icon: Gauge,
      render: ({ agent }) => <CerebroAgentQualityTab agent={agent} />,
    },
  ];
}

const qualityKey = (agentId: string) =>
  ["cerebro", "agent-quality", agentId] as const;

function formatDate(iso: string | null | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

export function CerebroAgentQualityTab({ agent }: { agent: Agent }) {
  const { data, isLoading, isError } = useQuery({
    queryKey: qualityKey(agent.id),
    queryFn: () => getAgentQuality(agent.id),
    enabled: !!agent.id,
    retry: false,
  });

  if (isLoading) {
    return <Skeleton className="h-48 w-full" />;
  }

  if (isError) {
    return (
      <p className="text-sm text-muted-foreground">
        Quality data could not be loaded. The measurement store may be
        unavailable — try again, or check that the backend is reachable.
      </p>
    );
  }

  const versions = data ?? [];

  return (
    <div className="space-y-4">
      <div>
        <h3 className="text-sm font-medium">Did the change work?</h3>
        <p className="text-xs text-muted-foreground">
          Each version is measured on the runs that actually ran under it —
          the only way to tell whether a new configuration made the agent
          better or just different.
        </p>
      </div>

      {versions.length === 0 ? (
        <p className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">
          No runs have recorded a config version yet. Runs started after this
          feature is enabled pin the version they read at claim time; the
          first measured run will appear here. Nothing is backfilled —
          historical runs cannot be attributed to a version after the fact.
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead>
              <tr className="border-b text-[10px] uppercase tracking-wide text-muted-foreground">
                <th className="py-2 pr-4 font-medium">Version</th>
                <th className="py-2 pr-4 font-medium">Runs</th>
                <th className="py-2 pr-4 font-medium">Active</th>
                <th className="py-2 pr-4 font-medium">Solution</th>
                <th className="py-2 pr-4 font-medium">Confidence</th>
                <th className="py-2 font-medium">Satisfaction</th>
              </tr>
            </thead>
            <tbody>
              {versions.map((v) => (
                <QualityVersionRow
                  key={v.agent_context_version_id ?? v.agent_context_version}
                  version={v}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className="grid gap-3 md:grid-cols-2">
        <section className="rounded-md border p-3">
          <h4 className="text-xs font-medium">
            Solution — did it do the work right?
          </h4>
          <p className="mt-1 text-xs text-muted-foreground">
            A judge scores the agent&apos;s result against what was asked
            (score 0–1, confidence, and an evidence link per measurement).
            Only runs with an actual verdict count — a version with no
            verdicts shows no score.
          </p>
        </section>
        <section className="rounded-md border p-3">
          <h4 className="text-xs font-medium">
            Satisfaction — did it help the human?
          </h4>
          <p className="mt-1 text-xs text-muted-foreground">
            Not a survey score. Read from what people actually did afterwards:
            reactions on the agent&apos;s comments, and approvals or
            rejections of its proposals. Signals only exist where humans
            acted — silence is shown as no data, not as satisfaction.
          </p>
        </section>
      </div>
    </div>
  );
}

function QualityVersionRow({ version }: { version: AgentQualityVersion }) {
  const sol = version.solution;
  const sat = version.satisfaction;
  const hasSolution = sol.measurements > 0;
  const hasSatisfaction =
    sat.reactions > 0 || sat.approvals > 0 || sat.rejections > 0;

  return (
    <tr className="border-b align-top last:border-0">
      <td className="py-2 pr-4">
        <span className="font-medium">{version.agent_context_version || "(unversioned)"}</span>
      </td>
      <td className="py-2 pr-4">{version.runs}</td>
      <td className="py-2 pr-4 text-xs text-muted-foreground">
        {formatDate(version.first_run_at)} – {formatDate(version.last_run_at)}
      </td>
      <td className="py-2 pr-4">
        {hasSolution ? (
          <div className="space-y-0.5">
            {typeof sol.avg_score === "number" ? (
              <span className="font-medium">{sol.avg_score.toFixed(2)}</span>
            ) : (
              <span>—</span>
            )}
            <p className="text-xs text-muted-foreground">
              {sol.passes} / {sol.scored} passed · {sol.measured_runs} of{" "}
              {version.runs} runs measured
            </p>
          </div>
        ) : (
          <div className="space-y-0.5">
            <span>—</span>
            <p className="text-xs text-muted-foreground">No measurements yet</p>
          </div>
        )}
      </td>
      <td className="py-2 pr-4">
        {typeof sol.avg_confidence === "number" ? (
          <span>{sol.avg_confidence.toFixed(2)}</span>
        ) : (
          <span>—</span>
        )}
      </td>
      <td className="py-2">
        {hasSatisfaction ? (
          <p className="text-xs">
            <span>{sat.reactions} reactions</span>
            {" · "}
            <span>
              {sat.approvals} approval{sat.approvals === 1 ? "" : "s"}
            </span>
            {" · "}
            <span>
              {sat.rejections} rejection{sat.rejections === 1 ? "" : "s"}
            </span>
            <span className="text-muted-foreground">
              {" "}
              ({sat.measured_runs} of {version.runs} runs)
            </span>
          </p>
        ) : (
          <div className="space-y-0.5">
            <span>—</span>
            <p className="text-xs text-muted-foreground">No signals yet</p>
          </div>
        )}
      </td>
    </tr>
  );
}
