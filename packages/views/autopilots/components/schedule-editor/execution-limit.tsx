"use client";

import { useId, useState } from "react";
import { useT } from "../../../i18n";

export function validExecutionLimit(value: number | null): boolean {
  return value === null || (Number.isInteger(value) && value >= 1 && value <= 2147483647);
}

export function ExecutionLimitEditor({
  value,
  onChange,
  used = 0,
  disabled = false,
}: {
  value: number | null;
  onChange: (value: number | null) => void;
  used?: number;
  disabled?: boolean;
}) {
  const { t } = useT("autopilots");
  const id = useId();
  const [mode, setMode] = useState(value === null ? "unlimited" : value === 1 ? "once" : "custom");
  const valid = validExecutionLimit(value);
  return (
    <div className="mt-3 space-y-2">
      <label htmlFor={id} className="text-caption font-medium text-muted-foreground">
        {t(($) => $.execution_limit.label)}
      </label>
      <select
        id={id}
        value={mode}
        disabled={disabled}
        className="w-full rounded-md border bg-background px-3 py-2 text-body"
        onChange={(e) => {
          const next = e.target.value;
          setMode(next);
          onChange(next === "unlimited" ? null : next === "once" ? 1 : 2);
        }}
      >
        <option value="unlimited">{t(($) => $.execution_limit.unlimited)}</option>
        <option value="once">{t(($) => $.execution_limit.once)}</option>
        <option value="custom">{t(($) => $.execution_limit.custom)}</option>
      </select>
      {mode === "custom" && (
        <>
          <label htmlFor={`${id}-count`} className="sr-only">
            {t(($) => $.execution_limit.count)}
          </label>
          <input
            id={`${id}-count`}
            type="number"
            min={1}
            max={2147483647}
            step={1}
            value={value !== null && Number.isFinite(value) ? value : ""}
            disabled={disabled}
            aria-invalid={!valid}
            className="w-full rounded-md border bg-background px-3 py-2 text-body"
            onChange={(e) => onChange(e.target.value === "" ? Number.NaN : Number(e.target.value))}
          />
        </>
      )}
      {!valid && (
        <p role="alert" className="text-caption text-destructive">
          {t(($) => $.execution_limit.invalid)}
        </p>
      )}
      {used > 0 && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.execution_limit.used, { count: used })}
        </p>
      )}
    </div>
  );
}
