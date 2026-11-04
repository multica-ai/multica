"use client";

import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";
import type { GoalConfidence, GoalStatus } from "@multica/core/governance";
import { useT } from "../i18n";

/** A goal's status as a small badge. An unknown status reads as unknown. */
export function GoalStatusBadge({ status }: { status: GoalStatus }) {
  const { t } = useT("governance");
  return (
    <Badge variant="outline" data-testid="goal-status">
      {t(($) => $.goals.status[status])}
    </Badge>
  );
}

const CONFIDENCE_TONE: Record<GoalConfidence, string> = {
  on_track: "text-success",
  at_risk: "text-warning",
  off_track: "text-destructive",
  unknown: "text-muted-foreground",
};

/** A confidence chip. The label says so when the owner has overridden it. */
export function GoalConfidenceChip({ confidence, overridden }: { confidence: GoalConfidence; overridden?: boolean }) {
  const { t } = useT("governance");
  return (
    <Badge variant="secondary" data-testid="goal-confidence" className={cn(CONFIDENCE_TONE[confidence])}>
      {t(($) => $.goals.confidence[confidence])}
      {overridden ? <span className="ml-1 opacity-80">{t(($) => $.goals.confidence.overridden)}</span> : null}
    </Badge>
  );
}
