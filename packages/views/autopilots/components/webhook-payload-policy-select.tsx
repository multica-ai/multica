"use client";

import { useId } from "react";
import { useT } from "../../i18n";

export function WebhookPayloadPolicySelect({ value, onChange, disabled = false }: {
  value: "redacted" | "metadata_only";
  onChange: (value: "redacted" | "metadata_only") => void;
  disabled?: boolean;
}) {
  const { t } = useT("autopilots");
  const id = useId();
  return (
    <div className="space-y-1">
      <label htmlFor={id} className="text-caption font-medium">{t(($) => $.payload_policy.label)}</label>
      <select id={id} value={value} disabled={disabled} onChange={(event) => onChange(event.target.value === "metadata_only" ? "metadata_only" : "redacted")} aria-describedby={`${id}-help`} className="w-full rounded-md border bg-background px-3 py-2 text-body">
        <option value="redacted">{t(($) => $.payload_policy.redacted)}</option>
        <option value="metadata_only">{t(($) => $.payload_policy.metadata_only)}</option>
      </select>
      <p id={`${id}-help`} className="text-caption text-muted-foreground">{t(($) => $.payload_policy.help)}</p>
    </div>
  );
}
