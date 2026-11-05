import { Slot } from "radix-ui";
import { useMemo } from "react";
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
}: AgentUsageMeterProps): React.JSX.Element {
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
          <SpanBinding1 clampedPercent={clampedPercent} barColor={barColor} />
        </span>
      ) : null}
    </div>
  );
});

const SpanBinding1 = observer(function SpanBinding1({
  clampedPercent,
  barColor,
  ...slotProps
}: {
  clampedPercent: number;
  barColor: NonNullable<AgentUsageMeterProps>["barColor"];
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    clampedPercent: number;
    barColor: NonNullable<AgentUsageMeterProps>["barColor"];
  }
>) {
  const styleProp1Value1 = `${clampedPercent}%`;
  const styleProp1Value2 =
    barColor ?? "var(--agent-gui-text-primary,var(--text-primary))";
  const styleProp1 = useMemo<
    NonNullable<React.ComponentProps<"span">["style"]>
  >(
    () => ({ width: styleProp1Value1, background: styleProp1Value2 }),
    [styleProp1Value1, styleProp1Value2]
  );
  return (
    <Slot.Root {...slotProps}>
      <span
        className="absolute inset-y-0 left-0 min-w-0.5 rounded-full"
        style={styleProp1}
      />
    </Slot.Root>
  );
});

type AgentUsageMeterProps = {
  label: string;
  value: string;
  percent: number | null;
  /** Overrides the bar fill color; defaults to the neutral primary text color. */
  barColor?: string;
  testId?: string;
};

type SpanBinding1Props = {
  clampedPercent: number;
  barColor: NonNullable<{
    label: string;
    value: string;
    percent: number | null;
    /** Overrides the bar fill color; defaults to the neutral primary text color. */
    barColor?: string;
    testId?: string;
  }>["barColor"];
};
