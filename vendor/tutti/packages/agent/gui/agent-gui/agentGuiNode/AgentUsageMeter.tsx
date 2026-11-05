import { observer } from "mobx-react-lite";
import * as React from "react";

import {
  USAGE_CRITICAL_PERCENT,
  USAGE_WARN_PERCENT
} from "./model/agentUsageThresholds";

/**
 * Maps a used-percent to a meter bar color: healthy (green) below the warn
 * threshold, warning (amber) past it, and critical (red) near the limit.
 */
export function agentUsageBarColor(percentUsed: number): string {
  if (percentUsed >= USAGE_CRITICAL_PERCENT) {
    return "var(--state-danger)";
  }
  if (percentUsed >= USAGE_WARN_PERCENT) {
    return "var(--state-warning)";
  }
  return "var(--state-success)";
}

export const AgentUsageMeter = observer(function AgentUsageMeter({
  label,
  value,
  percent,
  barColor,
  testId
}: {
  label: string;
  value: string;
  percent: number | null;
  /** Overrides the bar fill color; defaults to the neutral primary text color. */
  barColor?: string;
  testId?: string;
}): React.JSX.Element {
  const clampedPercent =
    typeof percent === "number" && Number.isFinite(percent)
      ? Math.max(0, Math.min(100, percent))
      : null;

  return (
    <div className="grid min-w-0 gap-1" data-testid={testId}>
      <div className="flex min-w-0 items-center justify-between gap-3">
        <span className="min-w-0 truncate text-[var(--text-secondary)]">
          {label}
        </span>
        <span className="shrink-0 whitespace-nowrap text-[var(--text-secondary)]">
          {value}
        </span>
      </div>
      {clampedPercent !== null ? (
        <span
          aria-hidden="true"
          className="relative h-1.5 overflow-hidden rounded-full bg-[color-mix(in_srgb,var(--text-primary)_10%,transparent)]"
        >
          <span
            className="absolute inset-y-0 left-0 min-w-0.5 rounded-full"
            style={{
              width: `${clampedPercent}%`,
              background:
                barColor ?? "var(--agent-gui-text-primary,var(--text-primary))"
            }}
          />
        </span>
      ) : null}
    </div>
  );
});
