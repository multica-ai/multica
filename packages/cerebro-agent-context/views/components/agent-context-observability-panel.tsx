"use client";

// Agent Office (FIR-1775 Phase 4) — the "overblik og målinger" panel. A
// read-only overview on the agent's own page answering the three drift
// questions: how often this agent's context changes, who approves the changes,
// and how much drift the lint finds. All data comes from
// GET /api/agents/{id}/context/observability, normalized defensively upstream.

import { useState } from "react";
import { Activity, ChevronDown, ChevronRight } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { agentContextObservabilityOptions } from "../../core/queries";

interface Props {
  agent: Agent;
  /** Header label; defaults to "Overview & metrics". */
  title?: string;
}

function formatRelativeTime(iso: string | null): string {
  if (!iso) return "never";
  const parsed = new Date(iso).getTime();
  if (Number.isNaN(parsed)) return "never";
  const diff = Date.now() - parsed;
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

function Metric({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="rounded-md border border-border bg-muted/30 px-3 py-2">
      <div className="text-lg font-semibold tabular-nums">{value}</div>
      <div className="text-xs text-muted-foreground">{label}</div>
    </div>
  );
}

export function AgentContextObservabilityPanel({
  agent,
  title = "Overview & metrics",
}: Props) {
  const [open, setOpen] = useState(true);
  const { data, isLoading, isError } = useQuery(
    agentContextObservabilityOptions(agent.id),
  );

  return (
    <div>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-1.5 text-left text-sm font-semibold"
      >
        {open ? (
          <ChevronDown className="size-4 text-muted-foreground" />
        ) : (
          <ChevronRight className="size-4 text-muted-foreground" />
        )}
        <Activity className="size-4 text-muted-foreground" />
        {title}
      </button>

      {open && (
        <div className="mt-3 space-y-4">
          {isLoading && (
            <p className="text-xs text-muted-foreground">Loading metrics…</p>
          )}
          {isError && (
            <p className="text-xs text-muted-foreground">
              Metrics are unavailable right now.
            </p>
          )}
          {!isLoading && !isError && data && (
            <>
              {/* How often it changes */}
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                <Metric label="Versions total" value={data.version_count} />
                <Metric
                  label="Changes (30 days)"
                  value={data.versions_last_30d}
                />
                <Metric
                  label="Current version"
                  value={data.context_version || "—"}
                />
                <Metric
                  label="Last changed"
                  value={formatRelativeTime(data.last_changed_at)}
                />
              </div>

              {/* Change requests by status */}
              <div>
                <div className="mb-1.5 text-xs font-medium text-muted-foreground">
                  Change requests
                </div>
                <div className="flex flex-wrap gap-1.5">
                  <Badge variant="secondary">
                    {data.change_requests.pending} pending
                  </Badge>
                  <Badge variant="secondary">
                    {data.change_requests.approved} approved
                  </Badge>
                  <Badge variant="secondary">
                    {data.change_requests.merged} merged
                  </Badge>
                  <Badge variant="secondary">
                    {data.change_requests.rejected} rejected
                  </Badge>
                </div>
              </div>

              {/* Who approves */}
              <div>
                <div className="mb-1.5 text-xs font-medium text-muted-foreground">
                  Approvers
                </div>
                {data.approvers.length === 0 ? (
                  <p className="text-xs text-muted-foreground">
                    No reviewed change requests yet.
                  </p>
                ) : (
                  <ul className="space-y-1">
                    {data.approvers.map((a) => (
                      <li
                        key={a.user_id}
                        className="flex items-center justify-between gap-3 text-sm"
                      >
                        <span className="truncate">
                          {a.name || "Unknown reviewer"}
                        </span>
                        <span className="shrink-0 text-xs text-muted-foreground tabular-nums">
                          {a.approved + a.merged} approved · {a.rejected}{" "}
                          rejected
                        </span>
                      </li>
                    ))}
                  </ul>
                )}
              </div>

              {/* Where it drifts */}
              <div>
                <div className="mb-1.5 text-xs font-medium text-muted-foreground">
                  Drift findings
                </div>
                {data.drift.total === 0 ? (
                  <p className="text-xs text-muted-foreground">
                    No drift detected.
                  </p>
                ) : (
                  <div className="flex flex-wrap gap-1.5">
                    <Badge variant="destructive">
                      {data.drift.errors} errors
                    </Badge>
                    <Badge variant="secondary">
                      {data.drift.warnings} warnings
                    </Badge>
                    <Badge variant="secondary">{data.drift.infos} info</Badge>
                  </div>
                )}
              </div>
            </>
          )}
        </div>
      )}
    </div>
  );
}
