"use client";

import { useT } from "../../i18n";
import { Segmented, type Dim } from "./dashboard-shared";

/**
 * Daily / Weekly, scoped to the card it sits in.
 *
 * Renders only the dimensions the page's current range allows, which is what
 * removed the hidden coupling this control used to carry: it lived in the page
 * header next to the range, and picking a dimension the range disallowed
 * silently rewrote the range — moving every KPI on the page (MUL-5759). With
 * the illegal options simply absent there is nothing to reset.
 *
 * A single allowed dimension renders nothing at all: a one-option toggle is a
 * control that cannot be operated, and the chart title ("Weekly cost") already
 * says which grain is on screen.
 */
export function DimSegmented({
  allowedDims,
  value,
  onChange,
}: {
  allowedDims: readonly Dim[];
  value: Dim;
  onChange: (dim: Dim) => void;
}) {
  const { t } = useT("usage");
  if (allowedDims.length < 2) return null;

  const label: Record<Dim, string> = {
    daily: t(($) => $.dim.daily),
    weekly: t(($) => $.dim.weekly),
  };

  return (
    <Segmented
      label={t(($) => $.dim.label)}
      value={value}
      onChange={onChange}
      options={allowedDims.map((dim) => ({ label: label[dim], value: dim }))}
    />
  );
}
