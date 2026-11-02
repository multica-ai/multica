"use client";

import { useEffect, useState } from "react";
import { Check, LoaderCircle } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@multica/ui/components/ui/card";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

interface EngineeringStageProps {
  currentStage: number;
  modeSummary: string;
  stageTitles: ReadonlyMap<number, string>;
  canDecide: boolean;
  pending: boolean;
  onStageChange: (stage: number) => void;
  onSummarySave: (summary: string) => void;
}

export function EngineeringStage({ currentStage, modeSummary, stageTitles, canDecide, pending, onStageChange, onSummarySave }: EngineeringStageProps) {
  const { t } = useT("engineering");
  const [summary, setSummary] = useState(modeSummary);
  useEffect(() => setSummary(modeSummary), [modeSummary]);
  return (
    <Card data-engineering-region="stage">
      <CardHeader><CardTitle>{t(($) => $.stage.title)}</CardTitle></CardHeader>
      <CardContent className="space-y-5">
        <ol className="grid gap-2 sm:grid-cols-5" aria-label={t(($) => $.stage.flowLabel)}>
          {[1, 2, 3, 4, 5].map((stage) => {
            const current = currentStage === stage;
            const complete = currentStage > stage;
            return (
              <li key={stage} className="relative">
                <button
                  type="button"
                  disabled={!canDecide || pending}
                  onClick={() => onStageChange(stage)}
                  aria-current={current ? "step" : undefined}
                  className={cn(
                    "flex min-h-20 w-full items-start gap-2 rounded-lg border p-3 text-left transition-colors focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:opacity-60",
                    current ? "border-primary bg-primary/5" : "hover:bg-accent/60",
                  )}
                >
                  <span className={cn("flex size-6 shrink-0 items-center justify-center rounded-full border text-caption", current && "border-primary bg-primary text-primary-foreground", complete && "border-success bg-success/10 text-success")}>
                    {complete ? <Check className="size-3.5" aria-hidden="true" /> : stage}
                  </span>
                  <span className="min-w-0 text-caption font-medium">{stageTitles.get(stage) ?? t(($) => $.stage.defaults[String(stage) as "1" | "2" | "3" | "4" | "5"])}</span>
                </button>
              </li>
            );
          })}
        </ol>
        <div className="space-y-2">
          <label htmlFor="engineering-mode-summary" className="text-caption font-medium">{t(($) => $.stage.summaryLabel)}</label>
          <Textarea
            id="engineering-mode-summary"
            value={summary}
            onChange={(event) => setSummary(event.target.value)}
            maxLength={280}
            rows={2}
            disabled={!canDecide || pending}
          />
          {canDecide ? (
            <div className="flex justify-end">
              <Button size="sm" variant="outline" disabled={pending || summary === modeSummary} onClick={() => onSummarySave(summary)}>
                {pending ? <LoaderCircle className="animate-spin" aria-hidden="true" /> : null}
                {t(($) => $.actions.save)}
              </Button>
            </div>
          ) : null}
        </div>
      </CardContent>
    </Card>
  );
}
