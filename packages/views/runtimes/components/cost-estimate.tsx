"use client";

import type { ReactNode } from "react";
import { useCustomPricingStore } from "@multica/core/runtimes/custom-pricing-store";
import { useT } from "../../i18n";
import { estimateCostRange, formatUsd, type Priceable } from "../utils";

export function CostEstimate({ usage, children }: {
  usage: readonly Priceable[];
  children?: ReactNode;
}) {
  const { t } = useT("runtimes");
  useCustomPricingStore((s) => s.pricings);
  const { min, max } = estimateCostRange(usage);
  if (max <= min) return children ?? formatUsd(min);
  return (
    <span className="tabular-nums" title={t(($) => $.usage.peak_range_hint)}>
      {formatUsd(min)}–{formatUsd(max)}
    </span>
  );
}

export function CostRangeNotice({ usage }: { usage: readonly Priceable[] }) {
  const { t } = useT("runtimes");
  useCustomPricingStore((s) => s.pricings);
  const { min, max } = estimateCostRange(usage);
  if (max <= min) return null;
  return <p className="text-caption text-muted-foreground">{t(($) => $.usage.peak_range_hint)}</p>;
}
