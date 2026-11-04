"use client";

import { useState } from "react";
import { toast } from "sonner";
import { metricsOptions, useReportMetricReading, type GoalScopeType } from "@multica/core/governance";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { useT } from "../i18n";

/**
 * Adds one reading to a metric. The scope defaults to the goal's, so the reading
 * lands where the key result reads it. A reading is a manual report; the server
 * refuses a built-in metric and checks the scope.
 */
export function GovernanceAddReading({
  wsId,
  defaultScope,
  defaultKey,
}: {
  wsId: string;
  defaultScope: { type: GoalScopeType; id: string | null };
  defaultKey?: string;
}) {
  const { t } = useT("governance");
  const metrics = useQuery(metricsOptions(wsId));
  const report = useReportMetricReading(wsId);
  const [key, setKey] = useState(defaultKey ?? "");
  const [value, setValue] = useState("");
  const [note, setNote] = useState("");

  const numeric = Number(value);
  const canSubmit = key !== "" && value.trim() !== "" && Number.isFinite(numeric) && !report.isPending;

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    report.mutate(
      {
        key,
        data: {
          scope_type: defaultScope.type,
          scope_id: defaultScope.type === "company" ? null : defaultScope.id,
          value: numeric,
          note: note.trim(),
        },
      },
      {
        onSuccess: () => {
          setValue("");
          setNote("");
          toast.success(t(($) => $.reading.saved));
        },
        onError: () => toast.error(t(($) => $.reading.failed)),
      },
    );
  };

  const metricItems = (metrics.data ?? []).map((m) => ({ value: m.key, label: `${m.name} (${m.key})` }));

  return (
    <form onSubmit={submit} className="flex flex-col gap-3" aria-label={t(($) => $.reading.title)}>
      <div className="flex flex-col gap-1">
        <Label htmlFor="reading-metric">{t(($) => $.reading.metric)}</Label>
        <Select items={metricItems} value={key} onValueChange={(next) => setKey(next ?? "")}>
          <SelectTrigger id="reading-metric" aria-label={t(($) => $.reading.metric)} className="w-full">
            <SelectValue placeholder={t(($) => $.reading.no_metrics)}>
              {metricItems.find((m) => m.value === key)?.label ?? t(($) => $.reading.no_metrics)}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {metricItems.map((m) => (
              <SelectItem key={m.value} value={m.value}>
                {m.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="reading-value">{t(($) => $.reading.value)}</Label>
        <Input
          id="reading-value"
          type="number"
          inputMode="decimal"
          step="any"
          value={value}
          onChange={(e) => setValue(e.target.value)}
        />
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="reading-note">{t(($) => $.reading.note)}</Label>
        <Input id="reading-note" value={note} onChange={(e) => setNote(e.target.value)} maxLength={500} />
      </div>
      <p className="text-caption text-muted-foreground">
        {t(($) => $.reading.scope_note, { scope: t(($) => $.goals.scope[defaultScope.type]) })}
      </p>
      <div>
        <Button type="submit" size="sm" disabled={!canSubmit} aria-busy={report.isPending}>
          {t(($) => $.reading.submit)}
        </Button>
      </div>
    </form>
  );
}

