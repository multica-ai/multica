"use client";

import { useQuery } from "@tanstack/react-query";
import { agentPlanLimitsOptions } from "@multica/core/agent-usage/queries";
import type { AgentPlanUsageWindow } from "@multica/core/types";
import { Progress, ProgressLabel } from "@multica/ui/components/ui/progress";
import { useT } from "../i18n";

// Clamp to the bar's domain — Anthropic can briefly report >100 right at a
// window boundary, which would otherwise overflow the track.
function clampPercent(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.min(100, Math.max(0, value));
}

function PlanUsageBar({
  label,
  resetsLabel,
  window,
}: {
  label: string;
  resetsLabel: string;
  window: AgentPlanUsageWindow;
}) {
  const pct = clampPercent(window.utilization);
  const resetsAt = window.resets_at ? new Date(window.resets_at) : null;
  const title =
    resetsAt && !Number.isNaN(resetsAt.getTime())
      ? `${resetsLabel} ${resetsAt.toLocaleString()}`
      : undefined;

  return (
    <Progress value={pct} title={title} className="flex-col gap-1">
      <div className="flex w-full items-center justify-between gap-2">
        <ProgressLabel className="text-xs font-normal text-muted-foreground">
          {label}
        </ProgressLabel>
        <span className="text-xs tabular-nums text-muted-foreground">
          {`${Math.round(pct)}%`}
        </span>
      </div>
    </Progress>
  );
}

// PlanUsageWidget shows the operator's Claude subscription headroom — the
// rolling session (five-hour) and weekly (seven-day) windows — as two compact
// progress bars in the left toolbar. Renders nothing unless the OAuth broker
// reported usable data, so deployments without a Claude plan see no widget.
export function PlanUsageWidget() {
  const { t } = useT("layout");
  const { data } = useQuery(agentPlanLimitsOptions());

  if (!data || data.available !== true) return null;

  const session = data.five_hour;
  const weekly = data.seven_day;
  if (!session && !weekly) return null;

  const resetsLabel = t(($) => $.sidebar.plan_usage_resets);

  return (
    <div className="flex flex-col gap-2 px-1 pb-1">
      <span className="text-xs font-medium text-muted-foreground">
        {t(($) => $.sidebar.plan_usage_title)}
      </span>
      {session && (
        <PlanUsageBar
          label={t(($) => $.sidebar.plan_usage_session)}
          resetsLabel={resetsLabel}
          window={session}
        />
      )}
      {weekly && (
        <PlanUsageBar
          label={t(($) => $.sidebar.plan_usage_weekly)}
          resetsLabel={resetsLabel}
          window={weekly}
        />
      )}
    </div>
  );
}
